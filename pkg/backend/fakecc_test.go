package backend

// fakecc_test.go provides an in-process fake of the Catalyst Center REST API for
// end-to-end tests. It follows the Catalyst Center 3.1.6 contract (see
// ACTION_PLAN.md, section 2) and also serves the two legacy endpoints the plugin
// still uses (/dna/intent/api/v2/site and /dna/intent/api/v1/site-health).
//
// Behaviour that is not specified by Cisco (exact error texts, time filtering)
// is kept simple and documented on the respective handler.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

// fakeEndpoint identifies one Catalyst Center API served by fakeCC.
type fakeEndpoint string

const (
	epToken               fakeEndpoint = "token"               // POST /dna/system/api/v1/auth/token
	epIssues              fakeEndpoint = "assuranceIssues"     // GET  /dna/data/api/v1/assuranceIssues
	epSites               fakeEndpoint = "sites"               // GET  /dna/intent/api/v1/sites
	epSiteHealthSummaries fakeEndpoint = "siteHealthSummaries" // GET  /dna/data/api/v1/siteHealthSummaries
	epSitesV2             fakeEndpoint = "sitesV2"             // GET  /dna/intent/api/v2/site (legacy, deprecated since 2.3.7.6)
	epSiteHealthV1        fakeEndpoint = "siteHealthV1"        // GET  /dna/intent/api/v1/site-health (legacy)
)

var fakeEndpointPaths = map[string]fakeEndpoint{
	"/dna/system/api/v1/auth/token":        epToken,
	"/dna/data/api/v1/assuranceIssues":     epIssues,
	"/dna/intent/api/v1/sites":             epSites,
	"/dna/data/api/v1/siteHealthSummaries": epSiteHealthSummaries,
	"/dna/intent/api/v2/site":              epSitesV2,
	"/dna/intent/api/v1/site-health":       epSiteHealthV1,
}

// fakeLimitSpec holds the documented default and maximum of an endpoint's limit parameter.
type fakeLimitSpec struct{ def, max int }

var fakeLimits = map[fakeEndpoint]fakeLimitSpec{
	epIssues:              {def: 10, max: 25},
	epSites:               {def: 500, max: 500},
	epSiteHealthSummaries: {def: 10, max: 20},
	epSitesV2:             {def: 500, max: 500},
	epSiteHealthV1:        {def: 25, max: 50},
}

// fakeLimitMode selects how the fake reacts to limit/offset values outside the spec.
type fakeLimitMode int

const (
	limitLenient fakeLimitMode = iota // honour any limit, default offset < 1 to 1
	limitStrict                       // 400 Bad Request, like a server that validates against the spec
	limitCap                          // silently cap limit to the maximum and offset to 1
)

// fakeRequest is one request as received by the fake.
type fakeRequest struct {
	Endpoint fakeEndpoint
	Method   string
	Path     string
	Query    url.Values
	Token    string
}

// fakeSite is one node of the fake site hierarchy.
type fakeSite struct {
	ID       string
	ParentID string
	Name     string
	Type     string // global, area, building, floor
}

// fakeCC is the fake Catalyst Center. Create it with newFakeCC and change the
// exported fields before the first request.
type fakeCC struct {
	t   *testing.T
	srv *httptest.Server

	Username  string
	Password  string
	Prefix    string // path prefix the API is mounted under, e.g. "/proxy/dnac" (reverse proxy)
	LimitMode fakeLimitMode

	// BadGatewayAboveLimit makes an endpoint answer 502 with the API gateway's
	// message for pages larger than the given limit. This mimics a Catalyst
	// Center 3.1.6 system whose site service fails on large pages.
	BadGatewayAboveLimit map[fakeEndpoint]int

	Sites            []fakeSite
	Issues           []map[string]any
	HealthSummaries  []map[string]any // nil: generated from Sites
	LegacySiteHealth []map[string]any // nil: generated from Sites

	mu           sync.Mutex
	requests     []fakeRequest
	tokens       map[string]bool
	tokenSeq     int
	status       map[fakeEndpoint]int
	html         map[fakeEndpoint]bool
	rateLimited  map[fakeEndpoint]int
	retryAfter   string
	dataNotReady bool
}

// fakeUUID returns a deterministic, UUID-shaped identifier.
func fakeUUID(n int) string {
	return fmt.Sprintf("00000000-0000-4000-8000-%012d", n)
}

// fakeDefaultSites is a three-level hierarchy below Global. It contains two
// buildings named "HQ" and two floors named "Floor 1" to exercise duplicate names.
//
//	Global
//	└── Österreich (area)
//	    ├── Wien (area)
//	    │   └── HQ (building)
//	    │       └── Floor 1 (floor)
//	    └── Graz (area)
//	        └── HQ (building)
//	            └── Floor 1 (floor)
func fakeDefaultSites() []fakeSite {
	return []fakeSite{
		{ID: fakeUUID(1), Name: "Global", Type: "global"},
		{ID: fakeUUID(2), ParentID: fakeUUID(1), Name: "Österreich", Type: "area"},
		{ID: fakeUUID(3), ParentID: fakeUUID(2), Name: "Wien", Type: "area"},
		{ID: fakeUUID(4), ParentID: fakeUUID(3), Name: "HQ", Type: "building"},
		{ID: fakeUUID(5), ParentID: fakeUUID(4), Name: "Floor 1", Type: "floor"},
		{ID: fakeUUID(6), ParentID: fakeUUID(2), Name: "Graz", Type: "area"},
		{ID: fakeUUID(7), ParentID: fakeUUID(6), Name: "HQ", Type: "building"},
		{ID: fakeUUID(8), ParentID: fakeUUID(7), Name: "Floor 1", Type: "floor"},
	}
}

// fakeIssueTime is the firstOccurredTime of the first fixture issue (epoch ms).
const fakeIssueTime int64 = 1760000000000

// newFakeCC starts a fake Catalyst Center with the default site tree and four
// issues (see defaultIssues). The server is closed when the test ends.
func newFakeCC(t *testing.T) *fakeCC {
	t.Helper()
	f := &fakeCC{
		t:                    t,
		BadGatewayAboveLimit: map[fakeEndpoint]int{},
		Username:             "admin",
		Password:             "secret",
		Sites:                fakeDefaultSites(),
		tokens:               map[string]bool{},
		status:               map[fakeEndpoint]int{},
		html:                 map[fakeEndpoint]bool{},
		rateLimited:          map[fakeEndpoint]int{},
	}
	f.Issues = f.defaultIssues()
	f.srv = httptest.NewServer(f)
	t.Cleanup(f.srv.Close)
	return f
}

// URL is the base URL a datasource should be configured with.
func (f *fakeCC) URL() string { return f.srv.URL + f.Prefix }

// settings returns datasource instance settings pointing at the fake.
func (f *fakeCC) settings(uid string) *backend.DataSourceInstanceSettings {
	jd, _ := json.Marshal(map[string]any{"baseUrl": f.URL()})
	return &backend.DataSourceInstanceSettings{
		UID:      uid,
		JSONData: jd,
		DecryptedSecureJSONData: map[string]string{
			"username": f.Username,
			"password": f.Password,
		},
	}
}

// pluginContext returns a plugin context for a datasource pointing at the fake.
func (f *fakeCC) pluginContext(uid string) backend.PluginContext {
	return backend.PluginContext{DataSourceInstanceSettings: f.settings(uid)}
}

// site returns the fixture site with the given name hierarchy, e.g. "Global/Österreich/Wien/HQ".
func (f *fakeCC) site(nameHierarchy string) fakeSite {
	f.t.Helper()
	for _, s := range f.Sites {
		if f.nameHierarchy(s) == nameHierarchy {
			return s
		}
	}
	f.t.Fatalf("fakeCC: no site %q", nameHierarchy)
	return fakeSite{}
}

// Requests returns the requests received for one endpoint, oldest first.
func (f *fakeCC) Requests(ep fakeEndpoint) []fakeRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []fakeRequest
	for _, r := range f.requests {
		if r.Endpoint == ep {
			out = append(out, r)
		}
	}
	return out
}

// RevokeTokens invalidates every issued token, like a Catalyst Center restart.
func (f *fakeCC) RevokeTokens() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tokens = map[string]bool{}
}

// SetStatus makes an endpoint answer with the given status code (0 clears it).
func (f *fakeCC) SetStatus(ep fakeEndpoint, code int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if code == 0 {
		delete(f.status, ep)
		return
	}
	f.status[ep] = code
}

// SetHTML makes an endpoint answer 200 with an HTML page, like an SSO or proxy login page.
func (f *fakeCC) SetHTML(ep fakeEndpoint, on bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.html[ep] = on
}

// RateLimit makes the next n requests to an endpoint fail with 429 and the given Retry-After value.
func (f *fakeCC) RateLimit(ep fakeEndpoint, n int, retryAfter string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rateLimited[ep] = n
	f.retryAfter = retryAfter
}

// SetDataNotReady makes siteHealthSummaries reject requests that carry an endTime.
func (f *fakeCC) SetDataNotReady(on bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dataNotReady = on
}

// ---- fixture helpers ----

func (f *fakeCC) siteByID(id string) (fakeSite, bool) {
	for _, s := range f.Sites {
		if s.ID == id {
			return s, true
		}
	}
	return fakeSite{}, false
}

// ancestors returns the path from Global down to s (inclusive).
func (f *fakeCC) ancestors(s fakeSite) []fakeSite {
	path := []fakeSite{s}
	for cur := s; cur.ParentID != ""; {
		p, ok := f.siteByID(cur.ParentID)
		if !ok {
			break
		}
		path = append([]fakeSite{p}, path...)
		cur = p
	}
	return path
}

func (f *fakeCC) nameHierarchy(s fakeSite) string {
	var names []string
	for _, a := range f.ancestors(s) {
		names = append(names, a.Name)
	}
	return strings.Join(names, "/")
}

func (f *fakeCC) siteHierarchyID(s fakeSite) string {
	var ids []string
	for _, a := range f.ancestors(s) {
		ids = append(ids, a.ID)
	}
	return strings.Join(ids, "/")
}

// issue builds a spec-shaped issue (IssuesList 3.1.6) located at site s.
func (f *fakeCC) issue(n int, s fakeSite, name, summary, priority, status, entityType, entityID string) map[string]any {
	first := fakeIssueTime + int64(n)*60_000
	return map[string]any{
		"issueId":                fakeUUID(1000 + n),
		"name":                   name,
		"summary":                summary,
		"description":            summary + ".",
		"priority":               priority,
		"severity":               "medium",
		"deviceType":             "switch",
		"category":               "Device",
		"entityType":             entityType,
		"entityId":               entityID,
		"firstOccurredTime":      first,
		"mostRecentOccurredTime": first + 30_000,
		"status":                 status,
		"isGlobal":               false,
		"siteId":                 s.ID,
		"siteHierarchyId":        f.siteHierarchyID(s),
		"siteName":               s.Name,
		"siteHierarchy":          f.nameHierarchy(s),
		"suggestedActions":       []any{map[string]any{"message": "Check the device.", "steps": []any{}}},
		"additionalAttributes":   []any{map[string]any{"key": "source", "value": "fakecc"}},
	}
}

// defaultIssues returns four issues: a device issue and a client issue in Wien,
// an AP issue on Graz/HQ/Floor 1 and a resolved global P1 issue on the Graz area.
func (f *fakeCC) defaultIssues() []map[string]any {
	wienFloor := f.site("Global/Österreich/Wien/HQ/Floor 1")
	wienHQ := f.site("Global/Österreich/Wien/HQ")
	grazFloor := f.site("Global/Österreich/Graz/HQ/Floor 1")
	graz := f.site("Global/Österreich/Graz")

	global := f.issue(4, graz, "global_ap_disconnect_trigger", `Many APs at "Graz" disconnected`, "P1", "resolved", "networkDevice", fakeUUID(104))
	global["isGlobal"] = true
	return []map[string]any{
		f.issue(1, wienFloor, "switch_cpu_high", `Network device "sw-wien-01" is experiencing high CPU utilization`, "P2", "active", "networkDevice", fakeUUID(101)),
		f.issue(2, wienHQ, "client_poor_rssi", `Client "68:ca:e4:79:3f:20" has a poor signal`, "P3", "active", "client", "68:ca:e4:79:3f:20"),
		f.issue(3, grazFloor, "ap_down", `AP "ap-graz-01" went down`, "P2", "active", "networkDevice", "a0:b1:c2:d3:e4:f5"),
		global,
	}
}

// addIssues appends n active P2 device issues at site s and returns the new issues.
func (f *fakeCC) addIssues(n int, s fakeSite) []map[string]any {
	start := len(f.Issues) + 1
	var added []map[string]any
	for i := 0; i < n; i++ {
		k := start + i
		added = append(added, f.issue(k, s, fmt.Sprintf("generated_issue_%d", k), fmt.Sprintf("Generated issue %d", k), "P2", "active", "networkDevice", fakeUUID(10000+k)))
	}
	f.Issues = append(f.Issues, added...)
	return added
}

// ---- HTTP handling ----

func (f *fakeCC) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if f.Prefix != "" {
		if !strings.HasPrefix(path, f.Prefix+"/") {
			writeFakeError(w, http.StatusNotFound, "not found")
			return
		}
		path = strings.TrimPrefix(path, f.Prefix)
	}
	ep, ok := fakeEndpointPaths[path]
	if !ok {
		writeFakeError(w, http.StatusNotFound, "not found")
		return
	}

	f.mu.Lock()
	f.requests = append(f.requests, fakeRequest{Endpoint: ep, Method: r.Method, Path: path, Query: r.URL.Query(), Token: r.Header.Get("X-Auth-Token")})
	forced := f.status[ep]
	html := f.html[ep]
	limited := f.rateLimited[ep] > 0
	if limited {
		f.rateLimited[ep]--
	}
	retryAfter := f.retryAfter
	f.mu.Unlock()

	switch {
	case forced != 0:
		writeFakeError(w, forced, http.StatusText(forced))
		return
	case limited:
		if retryAfter != "" {
			w.Header().Set("Retry-After", retryAfter)
		}
		writeFakeError(w, http.StatusTooManyRequests, "rate limit exceeded")
		return
	case html:
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body>Please log in</body></html>"))
		return
	}

	if ep == epToken {
		f.handleToken(w, r)
		return
	}
	if r.Method != http.MethodGet {
		writeFakeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	f.mu.Lock()
	valid := f.tokens[r.Header.Get("X-Auth-Token")]
	f.mu.Unlock()
	if !valid {
		writeFakeError(w, http.StatusUnauthorized, "invalid token")
		return
	}

	switch ep {
	case epIssues:
		f.handleIssues(w, r.URL.Query())
	case epSites:
		f.handleSites(w, r.URL.Query())
	case epSiteHealthSummaries:
		f.handleSiteHealthSummaries(w, r.URL.Query())
	case epSitesV2:
		f.handleSitesV2(w, r.URL.Query())
	case epSiteHealthV1:
		f.handleSiteHealthV1(w, r.URL.Query())
	}
}

func writeFakeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeFakeError writes the Catalyst Center error envelope.
func writeFakeError(w http.ResponseWriter, status int, msg string) {
	writeFakeJSON(w, status, map[string]any{
		"response": []any{map[string]any{"errorCode": status, "message": msg, "detail": msg}},
		"version":  "1.0",
	})
}

func (f *fakeCC) handleToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeFakeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	user, pass, ok := r.BasicAuth()
	if !ok || user != f.Username || pass != f.Password {
		writeFakeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	f.mu.Lock()
	f.tokenSeq++
	tok := fmt.Sprintf("fake-token-%d", f.tokenSeq)
	f.tokens[tok] = true
	f.mu.Unlock()
	writeFakeJSON(w, http.StatusOK, map[string]any{"Token": tok})
}

// paging validates limit and offset for ep according to LimitMode.
func (f *fakeCC) paging(w http.ResponseWriter, ep fakeEndpoint, q url.Values) (offset, limit int, ok bool) {
	spec := fakeLimits[ep]
	limit, offset = spec.def, 1
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			writeFakeError(w, http.StatusBadRequest, "invalid limit")
			return 0, 0, false
		}
		limit = n
	}
	if v := q.Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			writeFakeError(w, http.StatusBadRequest, "invalid offset")
			return 0, 0, false
		}
		offset = n
	}
	if max, ok := f.BadGatewayAboveLimit[ep]; ok && limit > max {
		writeFakeJSON(w, http.StatusBadGateway, map[string]any{"message": "An invalid response was received from the upstream server"})
		return 0, 0, false
	}
	switch f.LimitMode {
	case limitStrict:
		if limit < 1 || limit > spec.max {
			writeFakeError(w, http.StatusBadRequest, fmt.Sprintf("limit must be between 1 and %d", spec.max))
			return 0, 0, false
		}
		if offset < 1 {
			writeFakeError(w, http.StatusBadRequest, "offset must be at least 1")
			return 0, 0, false
		}
	case limitCap:
		if limit > spec.max {
			limit = spec.max
		}
		if limit < 1 {
			limit = spec.def
		}
		if offset < 1 {
			offset = 1
		}
	default:
		if limit < 1 {
			limit = spec.def
		}
		if offset < 1 {
			offset = 1
		}
	}
	return offset, limit, true
}

// pageOf returns the 1-based page [offset, offset+limit) of items.
func pageOf(items []map[string]any, offset, limit int) []map[string]any {
	start := offset - 1
	if start >= len(items) {
		return []map[string]any{}
	}
	end := start + limit
	if end > len(items) {
		end = len(items)
	}
	return items[start:end]
}

// fakeWildcardMatch matches s against a Catalyst Center filter value, where '*'
// matches any sequence of characters.
func fakeWildcardMatch(pattern, s string) bool {
	if !strings.Contains(pattern, "*") {
		return pattern == s
	}
	parts := strings.Split(pattern, "*")
	for i, p := range parts {
		parts[i] = regexp.QuoteMeta(p)
	}
	return regexp.MustCompile("^" + strings.Join(parts, ".*") + "$").MatchString(s)
}

// matchesAny reports whether value matches one of the filter values. An empty
// filter matches everything.
func matchesAny(filter []string, value string, fold bool) bool {
	if len(filter) == 0 {
		return true
	}
	for _, p := range filter {
		if fold {
			if fakeWildcardMatch(strings.ToLower(p), strings.ToLower(value)) {
				return true
			}
		} else if fakeWildcardMatch(p, value) {
			return true
		}
	}
	return false
}

func fakeString(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

// sortItems sorts by the given field and order ("asc"/"desc"); no field keeps the input order.
func sortItems(items []map[string]any, field, order string) {
	if field == "" {
		return
	}
	less := func(a, b any) bool {
		switch av := a.(type) {
		case int64:
			bv, _ := b.(int64)
			return av < bv
		case float64:
			bv, _ := b.(float64)
			return av < bv
		default:
			return fmt.Sprint(a) < fmt.Sprint(b)
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		if strings.EqualFold(order, "desc") {
			return less(items[j][field], items[i][field])
		}
		return less(items[i][field], items[j][field])
	})
}

// handleIssues serves GET /dna/data/api/v1/assuranceIssues. Filters: siteId,
// siteHierarchyId, siteName, siteHierarchy (wildcards), priority and status
// (case-insensitive), networkDeviceId and macAddress (matched against entityId),
// name, isGlobal. startTime, endTime and aiDriven are recorded but not applied.
func (f *fakeCC) handleIssues(w http.ResponseWriter, q url.Values) {
	offset, limit, ok := f.paging(w, epIssues, q)
	if !ok {
		return
	}
	var matched []map[string]any
	for _, is := range f.Issues {
		if !matchesAny(q["siteId"], fakeString(is, "siteId"), false) ||
			!matchesAny(q["siteHierarchyId"], fakeString(is, "siteHierarchyId"), false) ||
			!matchesAny(q["siteName"], fakeString(is, "siteName"), false) ||
			!matchesAny(q["siteHierarchy"], fakeString(is, "siteHierarchy"), false) ||
			!matchesAny(q["priority"], fakeString(is, "priority"), true) ||
			!matchesAny(q["status"], fakeString(is, "status"), true) ||
			!matchesAny(q["networkDeviceId"], fakeString(is, "entityId"), false) ||
			!matchesAny(q["macAddress"], fakeString(is, "entityId"), true) ||
			!matchesAny(q["name"], fakeString(is, "name"), false) {
			continue
		}
		if q.Get("isGlobal") == "true" {
			if g, _ := is["isGlobal"].(bool); !g {
				continue
			}
		}
		matched = append(matched, is)
	}
	sortItems(matched, q.Get("sortBy"), q.Get("order"))
	page := pageOf(matched, offset, limit)
	writeFakeJSON(w, http.StatusOK, map[string]any{
		"response": page,
		"page":     map[string]any{"limit": limit, "offset": offset, "count": len(page)},
		"version":  "1.0",
	})
}

// handleSites serves GET /dna/intent/api/v1/sites. Filters: name, nameHierarchy (wildcards), type.
func (f *fakeCC) handleSites(w http.ResponseWriter, q url.Values) {
	offset, limit, ok := f.paging(w, epSites, q)
	if !ok {
		return
	}
	var items []map[string]any
	for _, s := range f.Sites {
		if !matchesAny(q["name"], s.Name, false) || !matchesAny(q["nameHierarchy"], f.nameHierarchy(s), false) || !matchesAny(q["type"], s.Type, false) {
			continue
		}
		items = append(items, map[string]any{
			"id":              s.ID,
			"parentId":        s.ParentID,
			"name":            s.Name,
			"nameHierarchy":   f.nameHierarchy(s),
			"type":            s.Type,
			"siteHierarchyId": f.siteHierarchyID(s),
		})
	}
	writeFakeJSON(w, http.StatusOK, map[string]any{"response": pageOf(items, offset, limit), "version": "1.0"})
}

// handleSitesV2 serves the legacy GET /dna/intent/api/v2/site. Filter: type (area, building, floor).
func (f *fakeCC) handleSitesV2(w http.ResponseWriter, q url.Values) {
	offset, limit, ok := f.paging(w, epSitesV2, q)
	if !ok {
		return
	}
	var items []map[string]any
	for _, s := range f.Sites {
		if t := q.Get("type"); t != "" && t != s.Type {
			continue
		}
		items = append(items, map[string]any{
			"id":                 s.ID,
			"parentId":           s.ParentID,
			"name":               s.Name,
			"groupNameHierarchy": f.nameHierarchy(s),
			"groupHierarchy":     f.siteHierarchyID(s),
			"additionalInfo": []any{map[string]any{
				"nameSpace":  "Location",
				"attributes": map[string]any{"type": s.Type},
			}},
		})
	}
	writeFakeJSON(w, http.StatusOK, map[string]any{"response": pageOf(items, offset, limit)})
}

// defaultHealthSummaries generates one siteHealthSummaries record per area, building and floor.
func (f *fakeCC) defaultHealthSummaries() []map[string]any {
	var out []map[string]any
	for i, s := range f.Sites {
		if s.Type == "global" {
			continue
		}
		wired, wireless := float64(10*(i+1)), float64(20*(i+1))
		out = append(out, map[string]any{
			"id":                                 s.ID,
			"siteHierarchy":                      f.nameHierarchy(s),
			"siteHierarchyId":                    f.siteHierarchyID(s),
			"siteType":                           s.Type,
			"latitude":                           48.2,
			"longitude":                          16.37,
			"clientCount":                        wired + wireless,
			"wiredClientCount":                   wired,
			"wirelessClientCount":                wireless,
			"clientGoodHealthPercentage":         float64(90 - i),
			"wiredClientGoodHealthPercentage":    float64(95 - i),
			"wirelessClientGoodHealthPercentage": float64(85 - i),
			"networkDeviceCount":                 float64(5 + i),
			"networkDeviceGoodHealthPercentage":  float64(80 + i),
			"p1IssueCount":                       float64(i % 2),
			"p2IssueCount":                       float64(i % 3),
			"p3IssueCount":                       0.0,
			"p4IssueCount":                       0.0,
			"issueCount":                         float64(i%2 + i%3),
		})
	}
	return out
}

// handleSiteHealthSummaries serves GET /dna/data/api/v1/siteHealthSummaries.
// Filters: siteType (default floor, building, area), siteHierarchyId and
// siteHierarchy (wildcards), id. The default order is siteHierarchy ascending.
// With SetDataNotReady, any request with an endTime fails with 400; the error
// text is a placeholder until WP2.0 records the real one.
func (f *fakeCC) handleSiteHealthSummaries(w http.ResponseWriter, q url.Values) {
	f.mu.Lock()
	notReady := f.dataNotReady
	f.mu.Unlock()
	if notReady && q.Get("endTime") != "" {
		writeFakeError(w, http.StatusBadRequest, "Data is not ready for the requested endTime. Recommended endTime: "+strconv.FormatInt(fakeIssueTime, 10))
		return
	}
	offset, limit, ok := f.paging(w, epSiteHealthSummaries, q)
	if !ok {
		return
	}
	records := f.HealthSummaries
	if records == nil {
		records = f.defaultHealthSummaries()
	}
	types := q["siteType"]
	if len(types) == 0 {
		types = []string{"floor", "building", "area"}
	}
	var matched []map[string]any
	for _, rec := range records {
		if !matchesAny(types, fakeString(rec, "siteType"), true) ||
			!matchesAny(q["siteHierarchyId"], fakeString(rec, "siteHierarchyId"), false) ||
			!matchesAny(q["siteHierarchy"], fakeString(rec, "siteHierarchy"), false) ||
			!matchesAny(q["id"], fakeString(rec, "id"), false) {
			continue
		}
		matched = append(matched, rec)
	}
	sortBy := q.Get("sortBy")
	if sortBy == "" {
		sortBy = "siteHierarchy"
	}
	sortItems(matched, sortBy, q.Get("order"))
	page := pageOf(matched, offset, limit)
	writeFakeJSON(w, http.StatusOK, map[string]any{
		"response": page,
		"page":     map[string]any{"limit": limit, "offset": offset, "count": len(page)},
		"version":  "1.0",
	})
}

// defaultLegacySiteHealth generates one legacy site-health record per area and building.
func (f *fakeCC) defaultLegacySiteHealth() []map[string]any {
	var out []map[string]any
	for i, s := range f.Sites {
		if s.Type != "area" && s.Type != "building" {
			continue
		}
		parentName := ""
		if p, ok := f.siteByID(s.ParentID); ok {
			parentName = p.Name
		}
		wired, wireless := float64(10*(i+1)), float64(20*(i+1))
		out = append(out, map[string]any{
			"siteName":                       s.Name,
			"siteId":                         s.ID,
			"parentSiteId":                   s.ParentID,
			"parentSiteName":                 parentName,
			"siteType":                       s.Type,
			"numberOfClients":                wired + wireless,
			"numberOfWiredClients":           wired,
			"numberOfWirelessClients":        wireless,
			"healthyClientsPercentage":       float64(90 - i),
			"healthyNetworkDevicePercentage": float64(80 + i),
			"numberOfNetworkDevice":          float64(5 + i),
		})
	}
	return out
}

// handleSiteHealthV1 serves the legacy GET /dna/intent/api/v1/site-health.
// siteType is AREA or BUILDING (case-insensitive) and defaults to AREA.
func (f *fakeCC) handleSiteHealthV1(w http.ResponseWriter, q url.Values) {
	offset, limit, ok := f.paging(w, epSiteHealthV1, q)
	if !ok {
		return
	}
	records := f.LegacySiteHealth
	if records == nil {
		records = f.defaultLegacySiteHealth()
	}
	siteType := strings.ToLower(q.Get("siteType"))
	if siteType == "" {
		siteType = "area"
	}
	var matched []map[string]any
	for _, rec := range records {
		if strings.EqualFold(fakeString(rec, "siteType"), siteType) {
			matched = append(matched, rec)
		}
	}
	writeFakeJSON(w, http.StatusOK, map[string]any{"response": pageOf(matched, offset, limit)})
}

// ---- helpers for driving the datasource ----

// fakeTimeRange covers all fixture issues.
var fakeTimeRange = backend.TimeRange{
	From: time.UnixMilli(fakeIssueTime - 3_600_000),
	To:   time.UnixMilli(fakeIssueTime + 3_600_000),
}

// runQuery sends one query (refId "A") through Datasource.QueryData and returns its response.
func (f *fakeCC) runQuery(t *testing.T, ds *Datasource, uid string, query map[string]any) backend.DataResponse {
	t.Helper()
	q, err := json.Marshal(query)
	if err != nil {
		t.Fatalf("marshal query: %v", err)
	}
	resp, err := ds.QueryData(context.Background(), &backend.QueryDataRequest{
		PluginContext: f.pluginContext(uid),
		Queries:       []backend.DataQuery{{RefID: "A", TimeRange: fakeTimeRange, JSON: q}},
	})
	if err != nil {
		t.Fatalf("QueryData: %v", err)
	}
	dr, ok := resp.Responses["A"]
	if !ok {
		t.Fatal("no response for refId A")
	}
	return dr
}

// fieldValues returns the values of the named field in the first frame of dr.
func fieldValues(t *testing.T, dr backend.DataResponse, name string) []any {
	t.Helper()
	if len(dr.Frames) == 0 {
		t.Fatal("response has no frames")
	}
	field, _ := dr.Frames[0].FieldByName(name)
	if field == nil {
		t.Fatalf("frame has no field %q", name)
	}
	out := make([]any, field.Len())
	for i := range out {
		v := field.At(i)
		if p, ok := v.(*string); ok && p != nil {
			v = *p
		} else if p, ok := v.(*float64); ok && p != nil {
			v = *p
		}
		out[i] = v
	}
	return out
}

// frameRows returns the row count of the first frame of dr (0 without frames).
func frameRows(dr backend.DataResponse) int {
	if len(dr.Frames) == 0 {
		return 0
	}
	n, _ := dr.Frames[0].RowLen()
	return n
}

// filtersSite reports whether an issues request is restricted to site s, either
// by siteId or by a siteHierarchyId pattern that covers s.
func (f *fakeCC) filtersSite(r fakeRequest, s fakeSite) bool {
	for _, id := range r.Query["siteId"] {
		if id == s.ID {
			return true
		}
	}
	for _, p := range r.Query["siteHierarchyId"] {
		if fakeWildcardMatch(p, f.siteHierarchyID(s)) {
			return true
		}
	}
	return false
}
