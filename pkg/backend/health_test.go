package backend

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

func checkHealth(t *testing.T, f *fakeCC, uid string) *backend.CheckHealthResult {
	t.Helper()
	res, err := NewDatasource().CheckHealth(context.Background(), &backend.CheckHealthRequest{PluginContext: f.pluginContext(uid)})
	if err != nil {
		t.Fatalf("CheckHealth: %v", err)
	}
	return res
}

// TestCheckHealth_Catalyst316 reproduces the setup reported on Catalyst Center
// 3.1.6: the deprecated GET /dna/intent/api/v2/site answers 502 Bad Gateway.
// The plugin must use GET /dna/intent/api/v1/sites instead.
func TestCheckHealth_Catalyst316(t *testing.T) {
	f := newFakeCC(t)
	f.SetStatus(epSitesV2, http.StatusBadGateway)

	res := checkHealth(t, f, "health-316")
	if res.Status != backend.HealthStatusOk || strings.Contains(res.Message, "Warning") {
		t.Fatalf("want a clean OK, got %v: %s", res.Status, res.Message)
	}
	if n := len(f.Requests(epSitesV2)); n != 0 {
		t.Errorf("the deprecated v2/site endpoint was called %d times", n)
	}
	if len(f.Requests(epSites)) == 0 {
		t.Error("GET /dna/intent/api/v1/sites was not called")
	}
	if len(f.Requests(epIssues)) == 0 {
		t.Error("the assurance issues API was not probed")
	}

	// Site names still resolve, now from v1/sites.
	dr := f.runQuery(t, NewDatasource(), "health-316-query", map[string]any{"queryType": "assuranceIssues", "limit": 25})
	if dr.Error != nil {
		t.Fatalf("query error: %v", dr.Error)
	}
	if got := fieldValues(t, dr, "Site Name"); got[0] != "Floor 1" {
		t.Errorf("Site Name = %v, want Floor 1", got[0])
	}
}

func TestCheckHealth_SiteListFailureIsAWarning(t *testing.T) {
	f := newFakeCC(t)
	f.SetStatus(epSites, http.StatusBadGateway)

	res := checkHealth(t, f, "health-sites-down")
	if res.Status != backend.HealthStatusOk || !strings.Contains(res.Message, "site list is unavailable") {
		t.Fatalf("want OK with a site-list warning, got %v: %s", res.Status, res.Message)
	}
}

func TestCheckHealth_IssuesAPIFailures(t *testing.T) {
	cases := []struct {
		status int
		want   string
	}{
		{http.StatusForbidden, "assurance issues API returned 403"},
		{http.StatusNotFound, "requires Catalyst Center 3.1.6 or later"},
	}
	for _, c := range cases {
		f := newFakeCC(t)
		f.SetStatus(epIssues, c.status)
		res := checkHealth(t, f, "health-issues")
		if res.Status != backend.HealthStatusError || !strings.Contains(res.Message, c.want) {
			t.Errorf("issues %d: want error containing %q, got %v: %s", c.status, c.want, res.Status, res.Message)
		}
	}
}

func TestCheckHealth_WrongPassword(t *testing.T) {
	f := newFakeCC(t)
	f.Password = "other" // the datasource is configured with the fake's password, the server expects another
	res, err := NewDatasource().CheckHealth(context.Background(), &backend.CheckHealthRequest{PluginContext: backend.PluginContext{
		DataSourceInstanceSettings: func() *backend.DataSourceInstanceSettings {
			s := f.settings("health-wrong-pw")
			s.DecryptedSecureJSONData["password"] = "wrong"
			return s
		}(),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != backend.HealthStatusError || !strings.Contains(res.Message, "token") {
		t.Fatalf("want a token error, got %v: %s", res.Status, res.Message)
	}
}

// TestSiteList_FallsBackToSmallerPages: a system whose site service fails on
// pages of 500 (502 from the API gateway) still delivers the site list.
func TestSiteList_FallsBackToSmallerPages(t *testing.T) {
	f := newFakeCC(t)
	f.BadGatewayAboveLimit[epSites] = 100

	res := checkHealth(t, f, "sites-fallback")
	if res.Status != backend.HealthStatusOk || strings.Contains(res.Message, "Warning") {
		t.Fatalf("want a clean OK, got %v: %s", res.Status, res.Message)
	}
	var limits []string
	for _, r := range f.Requests(epSites) {
		limits = append(limits, r.Query.Get("limit"))
	}
	if strings.Join(limits, ",") != "500,100" {
		t.Errorf("site page sizes = %v, want [500 100]", limits)
	}
}

// TestIssueSiteNames_WithoutSiteList: when the site list is unavailable, the
// issues table takes site names from the issue's siteName/siteHierarchy
// instead of showing site UUIDs.
func TestIssueSiteNames_WithoutSiteList(t *testing.T) {
	f := newFakeCC(t)
	f.SetStatus(epSites, http.StatusBadGateway)

	dr := f.runQuery(t, NewDatasource(), "names-without-sites", map[string]any{"queryType": "assuranceIssues", "limit": 25})
	if dr.Error != nil {
		t.Fatalf("query error: %v", dr.Error)
	}
	names := fieldValues(t, dr, "Site Name")
	parents := fieldValues(t, dr, "Parent Site")
	wantNames := []string{"Floor 1", "HQ", "Floor 1", "Graz"}
	wantParents := []string{"HQ", "Wien", "HQ", "Österreich"}
	for i := range wantNames {
		if names[i] != wantNames[i] || parents[i] != wantParents[i] {
			t.Errorf("row %d: site %v / parent %v, want %s / %s", i, names[i], parents[i], wantNames[i], wantParents[i])
		}
	}
}
