package backend

// Tests for the fake Catalyst Center itself (fakecc_test.go), plus an
// end-to-end example that drives Datasource.QueryData through it.

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"testing"
)

// fakeGet sends a request to the fake and decodes the JSON body (nil for non-JSON bodies).
func fakeGet(t *testing.T, f *fakeCC, method, path, token string, q url.Values) (int, http.Header, map[string]any) {
	t.Helper()
	u := f.URL() + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequest(method, u, nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("X-Auth-Token", token)
	}
	if method == http.MethodPost {
		req.SetBasicAuth(f.Username, f.Password)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&body)
	return resp.StatusCode, resp.Header, body
}

func fakeLogin(t *testing.T, f *fakeCC) string {
	t.Helper()
	status, _, body := fakeGet(t, f, http.MethodPost, "/dna/system/api/v1/auth/token", "", nil)
	if status != http.StatusOK {
		t.Fatalf("login: status %d", status)
	}
	tok, _ := body["Token"].(string)
	if tok == "" {
		t.Fatal("login: no Token in body")
	}
	return tok
}

func fakeResponseLen(body map[string]any) int {
	items, _ := body["response"].([]any)
	return len(items)
}

func TestFakeCC_Auth(t *testing.T) {
	f := newFakeCC(t)

	req, _ := http.NewRequest(http.MethodPost, f.URL()+"/dna/system/api/v1/auth/token", nil)
	req.SetBasicAuth(f.Username, "wrong")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong password: want 401, got %d", resp.StatusCode)
	}

	tok := fakeLogin(t, f)
	if status, _, _ := fakeGet(t, f, http.MethodGet, "/dna/data/api/v1/assuranceIssues", tok, nil); status != http.StatusOK {
		t.Fatalf("valid token: want 200, got %d", status)
	}
	f.RevokeTokens()
	if status, _, _ := fakeGet(t, f, http.MethodGet, "/dna/data/api/v1/assuranceIssues", tok, nil); status != http.StatusUnauthorized {
		t.Fatalf("revoked token: want 401, got %d", status)
	}
}

func TestFakeCC_IssuesLimitModes(t *testing.T) {
	f := newFakeCC(t)
	f.addIssues(46, f.site("Global/Österreich/Wien/HQ")) // 50 issues in total
	tok := fakeLogin(t, f)
	q := url.Values{"limit": {"50"}, "offset": {"1"}}

	f.LimitMode = limitStrict
	if status, _, _ := fakeGet(t, f, http.MethodGet, "/dna/data/api/v1/assuranceIssues", tok, q); status != http.StatusBadRequest {
		t.Fatalf("strict, limit 50: want 400, got %d", status)
	}
	if status, _, _ := fakeGet(t, f, http.MethodGet, "/dna/data/api/v1/assuranceIssues", tok, url.Values{"offset": {"0"}}); status != http.StatusBadRequest {
		t.Fatalf("strict, offset 0: want 400, got %d", status)
	}

	f.LimitMode = limitCap
	if _, _, body := fakeGet(t, f, http.MethodGet, "/dna/data/api/v1/assuranceIssues", tok, q); fakeResponseLen(body) != 25 {
		t.Fatalf("cap, limit 50: want 25 items, got %d", fakeResponseLen(body))
	}

	f.LimitMode = limitLenient
	if _, _, body := fakeGet(t, f, http.MethodGet, "/dna/data/api/v1/assuranceIssues", tok, q); fakeResponseLen(body) != 50 {
		t.Fatalf("lenient, limit 50: want 50 items, got %d", fakeResponseLen(body))
	}

	// Without limit the documented default of 10 applies; offset is 1-based.
	_, _, body := fakeGet(t, f, http.MethodGet, "/dna/data/api/v1/assuranceIssues", tok, url.Values{"offset": {"11"}})
	if got := fakeResponseLen(body); got != 10 {
		t.Fatalf("default limit: want 10 items, got %d", got)
	}
	first := body["response"].([]any)[0].(map[string]any)["issueId"]
	if first != fakeUUID(1011) {
		t.Fatalf("offset 11: want issue %s first, got %v", fakeUUID(1011), first)
	}
}

func TestFakeCC_IssueFilters(t *testing.T) {
	f := newFakeCC(t)
	tok := fakeLogin(t, f)
	wien := f.site("Global/Österreich/Wien")
	cases := []struct {
		name string
		q    url.Values
		want int
	}{
		{"site and descendants by hierarchy prefix", url.Values{"siteHierarchyId": {f.siteHierarchyID(wien) + "*"}}, 2},
		{"exact siteId does not include children", url.Values{"siteId": {wien.ID}}, 0},
		{"repeated siteId is OR", url.Values{"siteId": {f.site("Global/Österreich/Wien/HQ").ID, f.site("Global/Österreich/Graz").ID}}, 2},
		{"priority", url.Values{"priority": {"P2"}}, 2},
		{"status lowercase", url.Values{"status": {"resolved"}}, 1},
		{"device by entityId", url.Values{"networkDeviceId": {fakeUUID(101)}}, 1},
		{"mac by entityId", url.Values{"macAddress": {"68:CA:E4:79:3F:20"}}, 1},
		{"global only", url.Values{"isGlobal": {"true"}}, 1},
	}
	for _, c := range cases {
		_, _, body := fakeGet(t, f, http.MethodGet, "/dna/data/api/v1/assuranceIssues", tok, c.q)
		if got := fakeResponseLen(body); got != c.want {
			t.Errorf("%s: want %d issues, got %d", c.name, c.want, got)
		}
	}

	// sortBy/order
	_, _, body := fakeGet(t, f, http.MethodGet, "/dna/data/api/v1/assuranceIssues", tok, url.Values{"sortBy": {"mostRecentOccurredTime"}, "order": {"desc"}})
	first := body["response"].([]any)[0].(map[string]any)["issueId"]
	if first != fakeUUID(1004) {
		t.Fatalf("sort desc: want newest issue %s first, got %v", fakeUUID(1004), first)
	}
}

func TestFakeCC_Sites(t *testing.T) {
	f := newFakeCC(t)
	tok := fakeLogin(t, f)

	_, _, body := fakeGet(t, f, http.MethodGet, "/dna/intent/api/v1/sites", tok, url.Values{"type": {"building"}})
	items := body["response"].([]any)
	if len(items) != 2 {
		t.Fatalf("v1 sites type=building: want 2, got %d", len(items))
	}
	b := items[0].(map[string]any)
	if b["nameHierarchy"] != "Global/Österreich/Wien/HQ" || b["siteHierarchyId"] != f.siteHierarchyID(f.site("Global/Österreich/Wien/HQ")) {
		t.Fatalf("v1 sites: unexpected record %v", b)
	}

	_, _, body = fakeGet(t, f, http.MethodGet, "/dna/intent/api/v2/site", tok, url.Values{"offset": {"1"}, "limit": {"500"}})
	if got := fakeResponseLen(body); got != len(f.Sites) {
		t.Fatalf("v2 site: want %d sites, got %d", len(f.Sites), got)
	}
}

func TestFakeCC_SiteHealth(t *testing.T) {
	f := newFakeCC(t)
	tok := fakeLogin(t, f)

	// siteHealthSummaries: all areas, buildings and floors by default, sorted by hierarchy.
	_, _, body := fakeGet(t, f, http.MethodGet, "/dna/data/api/v1/siteHealthSummaries", tok, url.Values{"limit": {"20"}})
	items := body["response"].([]any)
	if len(items) != len(f.Sites)-1 {
		t.Fatalf("summaries: want %d records, got %d", len(f.Sites)-1, len(items))
	}
	if items[0].(map[string]any)["siteHierarchy"] != "Global/Österreich" {
		t.Fatalf("summaries: want sort by siteHierarchy, first is %v", items[0])
	}
	// Hierarchy filter covers the site and its descendants.
	wien := f.site("Global/Österreich/Wien")
	_, _, body = fakeGet(t, f, http.MethodGet, "/dna/data/api/v1/siteHealthSummaries", tok, url.Values{"siteHierarchyId": {f.siteHierarchyID(wien) + "*"}})
	if got := fakeResponseLen(body); got != 3 {
		t.Fatalf("summaries for Wien subtree: want 3, got %d", got)
	}
	f.LimitMode = limitStrict
	if status, _, _ := fakeGet(t, f, http.MethodGet, "/dna/data/api/v1/siteHealthSummaries", tok, url.Values{"limit": {"25"}}); status != http.StatusBadRequest {
		t.Fatalf("summaries strict limit 25: want 400, got %d", status)
	}
	f.SetDataNotReady(true)
	if status, _, _ := fakeGet(t, f, http.MethodGet, "/dna/data/api/v1/siteHealthSummaries", tok, url.Values{"endTime": {strconv.FormatInt(fakeIssueTime, 10)}}); status != http.StatusBadRequest {
		t.Fatalf("data not ready: want 400, got %d", status)
	}

	// Legacy site-health: AREA by default, BUILDING on request.
	_, _, body = fakeGet(t, f, http.MethodGet, "/dna/intent/api/v1/site-health", tok, nil)
	if got := fakeResponseLen(body); got != 3 {
		t.Fatalf("legacy site-health default AREA: want 3, got %d", got)
	}
	_, _, body = fakeGet(t, f, http.MethodGet, "/dna/intent/api/v1/site-health", tok, url.Values{"siteType": {"BUILDING"}})
	if got := fakeResponseLen(body); got != 2 {
		t.Fatalf("legacy site-health BUILDING: want 2, got %d", got)
	}
}

func TestFakeCC_FaultsAndPrefix(t *testing.T) {
	f := newFakeCC(t)
	tok := fakeLogin(t, f)
	issues := "/dna/data/api/v1/assuranceIssues"

	f.SetStatus(epIssues, http.StatusForbidden)
	if status, _, _ := fakeGet(t, f, http.MethodGet, issues, tok, nil); status != http.StatusForbidden {
		t.Fatalf("forced status: want 403, got %d", status)
	}
	f.SetStatus(epIssues, 0)

	f.RateLimit(epIssues, 1, "2")
	status, hdr, _ := fakeGet(t, f, http.MethodGet, issues, tok, nil)
	if status != http.StatusTooManyRequests || hdr.Get("Retry-After") != "2" {
		t.Fatalf("rate limit: want 429 with Retry-After 2, got %d %q", status, hdr.Get("Retry-After"))
	}
	if status, _, _ := fakeGet(t, f, http.MethodGet, issues, tok, nil); status != http.StatusOK {
		t.Fatalf("after rate limit: want 200, got %d", status)
	}

	f.SetHTML(epIssues, true)
	status, hdr, body := fakeGet(t, f, http.MethodGet, issues, tok, nil)
	if status != http.StatusOK || hdr.Get("Content-Type") != "text/html" || body != nil {
		t.Fatalf("html: want 200 text/html, got %d %q", status, hdr.Get("Content-Type"))
	}
	f.SetHTML(epIssues, false)

	p := newFakeCC(t)
	p.Prefix = "/proxy/dnac"
	if status, _, _ := fakeGet(t, p, http.MethodPost, "/dna/system/api/v1/auth/token", "", nil); status != http.StatusOK {
		t.Fatalf("prefix: want 200 under prefix, got %d", status)
	}
	resp, err := http.Post(p.srv.URL+"/dna/system/api/v1/auth/token", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("prefix: want 404 without prefix, got %d", resp.StatusCode)
	}

	if got := len(f.Requests(epToken)); got != 1 {
		t.Fatalf("request log: want 1 token request, got %d", got)
	}
}

// TestFakeCC_QueryDataEndToEnd drives the whole issues path through the fake:
// login, site list, issue paging and frame building.
func TestFakeCC_QueryDataEndToEnd(t *testing.T) {
	f := newFakeCC(t)
	f.LimitMode = limitStrict
	ds := NewDatasource()

	dr := f.runQuery(t, ds, "e2e", map[string]any{"queryType": "assuranceIssues", "limit": 25})
	if dr.Error != nil {
		t.Fatalf("query error: %v", dr.Error)
	}
	if got := frameRows(dr); got != len(f.Issues) {
		t.Fatalf("want %d rows, got %d", len(f.Issues), got)
	}
	sites := fieldValues(t, dr, "Site Name")
	want := []string{"Floor 1", "HQ", "Floor 1", "Graz"}
	for i, w := range want {
		if sites[i] != w {
			t.Errorf("row %d: Site Name = %v, want %s", i, sites[i], w)
		}
	}

	reqs := f.Requests(epIssues)
	if len(reqs) == 0 {
		t.Fatal("no issues request")
	}
	r := reqs[0]
	if r.Token == "" || r.Query.Get("startTime") != strconv.FormatInt(fakeTimeRange.From.UnixMilli(), 10) || r.Query.Get("endTime") != strconv.FormatInt(fakeTimeRange.To.UnixMilli(), 10) {
		t.Fatalf("issues request: unexpected token or time range: %+v", r)
	}
	if len(f.Requests(epToken)) != 1 {
		t.Fatalf("want 1 login, got %d", len(f.Requests(epToken)))
	}
}
