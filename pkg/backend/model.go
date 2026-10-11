package backend

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

// InstanceSettings holds the configuration for a single instance of the datasource.
// This includes the base URL for the Catalyst Center API and connection-specific
// settings like TLS verification and credentials.
type InstanceSettings struct {
	// BaseURL is the root URL of the Catalyst Center API.
	// Example: https://catalyst.example.com
	BaseURL string
	// InsecureSkipVerify allows the user to disable TLS certificate verification.
	// This should only be used in development or for self-signed certificates.
	InsecureSkipVerify bool
	// Username for API authentication.
	Username string
	// Password for API authentication.
	Password string
	// APIToken allows for manual override of the token, bypassing username/password auth.
	APIToken string
}

// ParseInstanceSettings unmarshals and validates the datasource instance settings
// from the Grafana plugin context.
func ParseInstanceSettings(jsonData json.RawMessage, secureData map[string]string) (*InstanceSettings, error) {
	var jd struct {
		BaseURL            string `json:"baseUrl"`
		InsecureSkipVerify bool   `json:"insecureSkipVerify"`
	}
	_ = json.Unmarshal(jsonData, &jd)

	s := &InstanceSettings{
		BaseURL:            strings.TrimRight(jd.BaseURL, "/"),
		InsecureSkipVerify: jd.InsecureSkipVerify,
		Username:           secureData["username"],
		Password:           secureData["password"],
		APIToken:           secureData["apiToken"],
	}
	return s, nil
}

// dnacPrefix extracts any reverse-proxy prefix that appears BEFORE the /dna path segment.
// This is crucial for ensuring API calls are correctly routed when Catalyst Center
// is behind a reverse proxy.
// Example:
//
//	"/proxy/dnac/dna/intent/api/v1" -> "/proxy/dnac"
//	"/dna/intent/api/v1"            -> ""
//	"/something" (no /dna)          -> ""
func dnacPrefix(p string) string {
	if p == "" {
		return ""
	}
	segs := strings.Split(p, "/")
	idx := -1
	for i, s := range segs {
		if s == "dna" {
			idx = i
			break
		}
	}
	if idx == -1 {
		return ""
	}
	prefix := strings.Join(segs[:idx], "/")
	if prefix != "" && !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	return strings.TrimRight(prefix, "/")
}

// TokenURL constructs the full URL for the authentication token endpoint,
// preserving any reverse proxy prefix from the base URL.
// It always points to <prefix>/dna/system/api/v1/auth/token.
func TokenURL(base string) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	prefix := dnacPrefix(u.Path)
	u.Path = prefix + "/dna/system/api/v1/auth/token"
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

// IssuesURL constructs the full URL for the issues/alerts endpoint,
// preserving any reverse proxy prefix.
// It always points to <prefix>/dna/data/api/v1/assuranceIssues.
func IssuesURL(base string) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	prefix := dnacPrefix(u.Path)
	u.Path = prefix + "/dna/data/api/v1/assuranceIssues"
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

// SiteURL constructs the full URL for the site list endpoint,
// preserving any reverse proxy prefix.
// It always points to <prefix>/dna/intent/api/v1/sites. The older
// /dna/intent/api/v2/site is deprecated since Catalyst Center 2.3.7.6 and
// answers 502 on 3.1.6 systems.
func SiteURL(base string) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	prefix := dnacPrefix(u.Path)
	u.Path = prefix + "/dna/intent/api/v1/sites"
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

// StringList is a list of strings that also accepts the shapes older query
// editors saved: a single string ("x" becomes ["x"]), an empty string or null
// (both become an empty list). Blank entries are dropped and numbers are kept
// as their decimal text.
type StringList []string

func (l *StringList) UnmarshalJSON(b []byte) error {
	var raw any
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	var items []any
	switch v := raw.(type) {
	case nil:
		*l = nil
		return nil
	case []any:
		items = v
	default:
		items = []any{v}
	}
	var out StringList
	for _, it := range items {
		var s string
		switch v := it.(type) {
		case string:
			s = strings.TrimSpace(v)
		case float64:
			s = strconv.FormatFloat(v, 'f', -1, 64)
		default:
			return fmt.Errorf("expected a string or a list of strings, got %s", string(b))
		}
		if s != "" {
			out = append(out, s)
		}
	}
	*l = out
	return nil
}

// TriBool is an optional boolean filter (aiDriven, isGlobal). It keeps both
// meanings that saved dashboards use:
//   - JSON true sets the filter to true;
//   - JSON false, null or absent leaves it unset (the current editor's switch "off");
//   - the strings "true" and "false" (written by the v1.0.x editor) set an explicit
//     filter value, so "false" still means "only issues where this is false".
type TriBool struct {
	set   bool
	value bool
}

func (t *TriBool) UnmarshalJSON(b []byte) error {
	var raw any
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*t = TriBool{}
	switch v := raw.(type) {
	case bool:
		if v {
			*t = TriBool{set: true, value: true}
		}
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true", "yes", "1":
			*t = TriBool{set: true, value: true}
		case "false", "no", "0":
			*t = TriBool{set: true, value: false}
		}
	}
	return nil
}

// Param returns the API parameter value ("true" or "false") and whether the
// filter is set at all.
func (t TriBool) Param() (string, bool) {
	if !t.set {
		return "", false
	}
	return strconv.FormatBool(t.value), true
}

// QueryModel represents the query structure sent from the frontend.
// It is used by the backend to construct API requests.
type QueryModel struct {
	TimeRange backend.TimeRange `json:"timeRange"`
	QueryType string            `json:"queryType,omitempty"`
	Limit     *int64            `json:"limit,omitempty"`
	// Filters for assurance issues
	Priority        StringList `json:"priority,omitempty"`
	Status          StringList `json:"status,omitempty"`
	NetworkDeviceID string     `json:"networkDeviceId,omitempty"`
	NetworkDeviceIP string     `json:"networkDeviceIpAddress,omitempty"`
	MACAddress      string     `json:"macAddress,omitempty"`
	SiteID          StringList `json:"siteId,omitempty"`
	SiteName        StringList `json:"siteName,omitempty"`
	IssueName       string     `json:"issueName,omitempty"`
	AIDriven        TriBool    `json:"aiDriven,omitempty"`
	IsGlobal        TriBool    `json:"isGlobal,omitempty"`

	// Filters for site-health
	SiteType       string     `json:"siteType,omitempty"`
	ParentSiteName string     `json:"parentSiteName,omitempty"`
	ParentSiteID   string     `json:"parentSiteId,omitempty"`
	Metrics        StringList `json:"metrics,omitempty"`

	// Legacy fields saved by the v1.0.x query editor. normalize() moves them
	// into the current fields.
	IssueStatus string `json:"issueStatus,omitempty"`
	DeviceID    string `json:"deviceId,omitempty"`
	Severity    string `json:"severity,omitempty"`

	// notices are user-facing messages produced by normalize(); QueryData adds
	// them to the result frames.
	notices []string
}

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// normalize maps the legacy v1.0.x fields onto the current ones:
//   - issueStatus becomes status (unless status is already set);
//   - severity becomes priority (unless priority is already set);
//   - deviceId becomes networkDeviceId for a device UUID or networkDeviceIpAddress
//     for an IP address; any other value is dropped with a notice (decision D2).
func (q *QueryModel) normalize() {
	if len(q.Status) == 0 && strings.TrimSpace(q.IssueStatus) != "" {
		q.Status = StringList{strings.TrimSpace(q.IssueStatus)}
	}
	q.IssueStatus = ""
	if len(q.Priority) == 0 && strings.TrimSpace(q.Severity) != "" {
		q.Priority = StringList{strings.TrimSpace(q.Severity)}
	}
	q.Severity = ""
	if d := strings.TrimSpace(q.DeviceID); d != "" {
		switch {
		case uuidPattern.MatchString(d):
			if q.NetworkDeviceID == "" {
				q.NetworkDeviceID = d
			}
		case net.ParseIP(d) != nil:
			if q.NetworkDeviceIP == "" {
				q.NetworkDeviceIP = d
			}
		default:
			q.notices = append(q.notices, fmt.Sprintf("Ignored the legacy deviceId filter %q: it is neither a device UUID nor an IP address. Set the Device ID field to a device UUID.", d))
		}
	}
	q.DeviceID = ""
}

// IssuesEnvelope is used to unmarshal the 'response' array from the issues API.
// The API response is sometimes wrapped in a `{"response": [...]}` object.
// This envelope allows for consistent handling of the response data.
type IssuesEnvelope struct {
	Response []map[string]any `json:"response"`
}

// SiteEnvelope defines the structure for the site API response.
type SiteEnvelope struct {
	Response []Site `json:"response"`
}

// Site represents a single site returned by GET /dna/intent/api/v1/sites.
type Site struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	ParentID        string `json:"parentId,omitempty"`
	NameHierarchy   string `json:"nameHierarchy,omitempty"`   // e.g. Global/USA/San Jose/Building1
	Type            string `json:"type,omitempty"`            // global, area, building, floor
	SiteHierarchyID string `json:"siteHierarchyId,omitempty"` // site IDs from Global down to this site, joined by "/"
}
