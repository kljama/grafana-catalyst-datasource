package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

// ==================== QueryData against the fake Catalyst Center ====================
//
// These tests run against fakeCC (fakecc_test.go). They assert behaviour that
// must stay true after the fixes in ACTION_PLAN.md, not the current request
// shapes (page size, site filter parameter), which are planned to change.

// TestDatasource_QueryData_MainFlow runs an unfiltered issues query end to end.
func TestDatasource_QueryData_MainFlow(t *testing.T) {
	f := newFakeCC(t)

	dr := f.runQuery(t, NewDatasource(), "main-flow", map[string]any{"queryType": "assuranceIssues"})
	if dr.Error != nil {
		t.Fatalf("unexpected query error: %v", dr.Error)
	}
	if got := frameRows(dr); got != len(f.Issues) {
		t.Fatalf("expected %d rows, got %d", len(f.Issues), got)
	}
	ids := fieldValues(t, dr, "Issue ID")
	priorities := fieldValues(t, dr, "Priority")
	for i, want := range []string{"P2", "P3", "P2", "P1"} {
		if ids[i] != fakeUUID(1001+i) {
			t.Errorf("row %d: Issue ID = %v, want %s", i, ids[i], fakeUUID(1001+i))
		}
		if priorities[i] != want {
			t.Errorf("row %d: Priority = %v, want %s", i, priorities[i], want)
		}
	}

	for _, r := range f.Requests(epIssues) {
		if r.Token == "" {
			t.Errorf("issues request without X-Auth-Token: %v", r.Query)
		}
		if r.Query.Has("siteId") || r.Query.Has("siteHierarchyId") {
			t.Errorf("unfiltered query sent a site filter: %v", r.Query)
		}
	}
}

// TestDatasource_QueryData_WithClientError verifies that an invalid query model
// becomes an error on that query's response, not a top-level error.
func TestDatasource_QueryData_WithClientError(t *testing.T) {
	f := newFakeCC(t)

	resp, err := NewDatasource().QueryData(context.Background(), &backend.QueryDataRequest{
		PluginContext: f.pluginContext("client-error"),
		Queries:       []backend.DataQuery{{RefID: "A", TimeRange: fakeTimeRange, JSON: []byte(`{invalid json`)}},
	})
	if err != nil {
		t.Fatalf("QueryData should not return a top-level error, got: %v", err)
	}
	dr, ok := resp.Responses["A"]
	if !ok {
		t.Fatal("expected response for RefID 'A'")
	}
	if dr.Error == nil || !strings.Contains(dr.Error.Error(), "invalid query model") {
		t.Fatalf("expected an 'invalid query model' error, got: %v", dr.Error)
	}
	if n := len(f.Requests(epIssues)); n != 0 {
		t.Fatalf("expected no issues requests for an invalid query, got %d", n)
	}
}

// TestDatasource_QueryData_SiteNameResolution verifies that a site name without
// a site ID is resolved through the site list and sent as a site filter.
func TestDatasource_QueryData_SiteNameResolution(t *testing.T) {
	f := newFakeCC(t)
	graz := f.site("Global/Österreich/Graz")

	dr := f.runQuery(t, NewDatasource(), "site-name", map[string]any{
		"queryType": "assuranceIssues",
		"siteName":  []string{"Graz"},
	})
	if dr.Error != nil {
		t.Fatalf("unexpected query error: %v", dr.Error)
	}
	reqs := f.Requests(epIssues)
	if len(reqs) == 0 {
		t.Fatal("expected an issues request")
	}
	for _, r := range reqs {
		if !f.filtersSite(r, graz) {
			t.Errorf("issues request is not filtered to site Graz: %v", r.Query)
		}
	}
}

// TestDatasource_QueryData_MultipleQueries verifies that each query in a request
// gets its own response with its own filters.
func TestDatasource_QueryData_MultipleQueries(t *testing.T) {
	f := newFakeCC(t)
	qa, _ := json.Marshal(map[string]any{"queryType": "assuranceIssues"})
	qb, _ := json.Marshal(map[string]any{"queryType": "assuranceIssues", "priority": []string{"p1"}})

	resp, err := NewDatasource().QueryData(context.Background(), &backend.QueryDataRequest{
		PluginContext: f.pluginContext("multi"),
		Queries: []backend.DataQuery{
			{RefID: "A", TimeRange: fakeTimeRange, JSON: qa},
			{RefID: "B", TimeRange: fakeTimeRange, JSON: qb},
		},
	})
	if err != nil {
		t.Fatalf("unexpected top-level error: %v", err)
	}
	if len(resp.Responses) != 2 {
		t.Fatalf("expected 2 responses, got: %d", len(resp.Responses))
	}
	a, b := resp.Responses["A"], resp.Responses["B"]
	if a.Error != nil || b.Error != nil {
		t.Fatalf("unexpected errors: A=%v B=%v", a.Error, b.Error)
	}
	if got := frameRows(a); got != len(f.Issues) {
		t.Errorf("A: expected %d rows, got %d", len(f.Issues), got)
	}
	if got := frameRows(b); got != 1 {
		t.Errorf("B: expected 1 P1 row, got %d", got)
	}

	sawP1 := false
	for _, r := range f.Requests(epIssues) {
		if r.Query.Get("priority") == "P1" {
			sawP1 = true
		}
	}
	if !sawP1 {
		t.Error("expected an issues request with priority=P1 (normalized from p1)")
	}
}

// TestDatasource_QueryData_SiteHealthFlow runs an unfiltered site-health query end to end.
func TestDatasource_QueryData_SiteHealthFlow(t *testing.T) {
	f := newFakeCC(t)

	dr := f.runQuery(t, NewDatasource(), "site-health", map[string]any{"queryType": "siteHealth"})
	if dr.Error != nil {
		t.Fatalf("unexpected query error: %v", dr.Error)
	}
	if frameRows(dr) == 0 {
		t.Fatal("expected site-health rows")
	}
	found := false
	for _, v := range fieldValues(t, dr, "siteName") {
		if v == "Wien" {
			found = true
		}
	}
	if !found {
		t.Error("expected a row for site Wien")
	}
}

// TestDatasource_QueryData_LimitHandling verifies that the query limit caps the
// number of rows across pages.
func TestDatasource_QueryData_LimitHandling(t *testing.T) {
	f := newFakeCC(t)
	f.addIssues(146, f.site("Global/Österreich/Wien/HQ")) // 150 issues in total

	for _, limit := range []int{30, 100} {
		dr := f.runQuery(t, NewDatasource(), fmt.Sprintf("limit-%d", limit), map[string]any{
			"queryType": "assuranceIssues",
			"limit":     limit,
		})
		if dr.Error != nil {
			t.Fatalf("limit %d: unexpected query error: %v", limit, dr.Error)
		}
		if got := frameRows(dr); got != limit {
			t.Errorf("limit %d: expected %d rows, got %d", limit, limit, got)
		}
	}
}

// TestDatasource_QueryData_NoPluginContext verifies error handling when
// plugin context is missing
func TestDatasource_QueryData_NoPluginContext(t *testing.T) {
	ctx := context.Background()

	ds := NewDatasource()

	req := &backend.QueryDataRequest{
		PluginContext: backend.PluginContext{
			DataSourceInstanceSettings: nil, // Missing settings
		},
		Queries: []backend.DataQuery{
			{
				RefID: "A",
				TimeRange: backend.TimeRange{
					From: time.Now().Add(-1 * time.Hour),
					To:   time.Now(),
				},
				JSON: []byte(`{"queryType": "assuranceIssues"}`),
			},
		},
	}

	resp, err := ds.QueryData(ctx, req)

	// Should return error at top level for missing plugin context
	if err == nil {
		t.Fatal("expected error for missing plugin context")
	}

	if resp != nil {
		t.Fatal("expected nil response when plugin context is missing")
	}

	t.Logf("Correctly returned error: %v", err)
}

// TestDatasource_NewDatasource verifies that NewDatasource creates
// a properly initialized instance
func TestDatasource_NewDatasource(t *testing.T) {
	ds := NewDatasource()

	if ds == nil {
		t.Fatal("expected non-nil datasource")
	}

	if ds.tm == nil {
		t.Fatal("expected non-nil token manager")
	}

	if ds.translators == nil {
		t.Fatal("expected non-nil translators map")
	}

	if len(ds.translators) != 0 {
		t.Fatal("expected empty translators map initially")
	}
}

// TestDatasource_GetSiteTranslator verifies that getSiteTranslator
// correctly caches translators per instance
func TestDatasource_GetSiteTranslator(t *testing.T) {
	ds := NewDatasource()

	inst := &dsInstance{
		Settings: &InstanceSettings{
			BaseURL:  "https://catalyst.example.com",
			Username: "testuser",
			Password: "testpass",
		},
		UID: "test-instance-1",
	}

	// Get translator first time
	translator1 := ds.getSiteTranslator(inst, nil)
	if translator1 == nil {
		t.Fatal("expected non-nil translator")
	}

	// Get translator second time - should be cached
	translator2 := ds.getSiteTranslator(inst, nil)
	if translator2 == nil {
		t.Fatal("expected non-nil translator")
	}

	// Should be the same instance (cached)
	if translator1 != translator2 {
		t.Fatal("expected same translator instance from cache")
	}

	// Verify it was added to the map
	if len(ds.translators) != 1 {
		t.Fatalf("expected 1 translator in map, got: %d", len(ds.translators))
	}

	// Different instance should get different translator
	inst2 := &dsInstance{
		Settings: &InstanceSettings{
			BaseURL:  "https://catalyst2.example.com",
			Username: "testuser2",
			Password: "testpass2",
		},
		UID: "test-instance-2",
	}

	translator3 := ds.getSiteTranslator(inst2, nil)
	if translator3 == nil {
		t.Fatal("expected non-nil translator")
	}

	if translator3 == translator1 {
		t.Fatal("expected different translator for different instance")
	}

	// Now should have 2 translators
	if len(ds.translators) != 2 {
		t.Fatalf("expected 2 translators in map, got: %d", len(ds.translators))
	}
}

// ==================== Mock CallResourceResponseSender ====================

type mockResourceSender struct {
	resp *backend.CallResourceResponse
}

func (m *mockResourceSender) Send(resp *backend.CallResourceResponse) error {
	m.resp = resp
	return nil
}

func TestDatasource_CallResource_NotFound(t *testing.T) {
	ctx := context.Background()
	ds := NewDatasource()

	sender := &mockResourceSender{}
	req := &backend.CallResourceRequest{
		Path: "unknown_path",
	}

	err := ds.CallResource(ctx, req, sender)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sender.resp == nil || sender.resp.Status != http.StatusNotFound {
		t.Fatalf("expected 404 Not Found, got: %v", sender.resp)
	}
}

func TestDatasource_CallResource_MissingSettings(t *testing.T) {
	ctx := context.Background()
	ds := NewDatasource()

	for _, path := range []string{"sites", "issues"} {
		sender := &mockResourceSender{}
		req := &backend.CallResourceRequest{
			Path: path,
		}
		err := ds.CallResource(ctx, req, sender)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sender.resp == nil || sender.resp.Status != http.StatusInternalServerError {
			t.Fatalf("path %s: expected 500 error on missing settings, got: %v", path, sender.resp)
		}
	}
}

func TestDatasource_CallResource_Issues(t *testing.T) {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/dna/system/api/v1/auth/token":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"Token": "test-token"}`))
		case "/dna/data/api/v1/assuranceIssues":
			if r.Header.Get("X-Auth-Token") != "test-token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if r.URL.Query().Get("limit") != "10" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"response":[{"issueId":"iss-1","name":"ap_down"}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	ds := NewDatasource()
	sender := &mockResourceSender{}
	req := &backend.CallResourceRequest{
		Path: "issues",
		URL:  "issues?limit=10",
		PluginContext: backend.PluginContext{
			DataSourceInstanceSettings: &backend.DataSourceInstanceSettings{
				UID:      "test-instance-issues",
				JSONData: json.RawMessage(fmt.Sprintf(`{"baseUrl": %q}`, server.URL)),
				DecryptedSecureJSONData: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
		},
	}

	err := ds.CallResource(ctx, req, sender)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sender.resp == nil || sender.resp.Status != http.StatusOK {
		t.Fatalf("expected 200 OK, got: %v", sender.resp)
	}
	if !strings.Contains(string(sender.resp.Body), "iss-1") {
		t.Fatalf("expected response body to contain iss-1, got: %s", string(sender.resp.Body))
	}
}

func TestDatasource_CallResource_Sites(t *testing.T) {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/dna/system/api/v1/auth/token":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"Token": "test-token"}`))
		case "/dna/intent/api/v1/sites":
			if r.Header.Get("X-Auth-Token") != "test-token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"response":[{"id":"site-1","name":"Building A"}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	ds := NewDatasource()
	sender := &mockResourceSender{}
	req := &backend.CallResourceRequest{
		Path: "sites",
		PluginContext: backend.PluginContext{
			DataSourceInstanceSettings: &backend.DataSourceInstanceSettings{
				UID:      "test-instance-sites",
				JSONData: json.RawMessage(fmt.Sprintf(`{"baseUrl": %q}`, server.URL)),
				DecryptedSecureJSONData: map[string]string{
					"username": "user",
					"password": "pass",
				},
			},
		},
	}

	err := ds.CallResource(ctx, req, sender)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sender.resp == nil || sender.resp.Status != http.StatusOK {
		t.Fatalf("expected 200 OK, got: %v", sender.resp)
	}
	if !strings.Contains(string(sender.resp.Body), "Building A") {
		t.Fatalf("expected response body to contain Building A, got: %s", string(sender.resp.Body))
	}
}

func TestFilterSiteHealthData(t *testing.T) {
	sampleData := []map[string]any{
		{
			"siteId":         "d4ef280a-d71e-4a2f-8f9f-c1740a406075",
			"siteName":       "ORF Mediencampus",
			"siteType":       "area",
			"parentSiteId":   "root-id",
			"parentSiteName": "Global",
			"clientCount":    100.0,
		},
		{
			"siteId":         "bldg-1-uuid",
			"siteName":       "Building 1",
			"siteType":       "building",
			"parentSiteId":   "d4ef280a-d71e-4a2f-8f9f-c1740a406075",
			"parentSiteName": "ORF Mediencampus",
			"clientCount":    40.0,
		},
		{
			"siteId":         "other-uuid",
			"siteName":       "Global/Austria/Vienna/OtherSite",
			"siteType":       "building",
			"parentSiteId":   "vienna-id",
			"parentSiteName": "Vienna",
			"clientCount":    10.0,
		},
	}

	// 1. Unfiltered query
	res1 := filterSiteHealthData(sampleData, QueryModel{})
	if len(res1) != 3 {
		t.Fatalf("expected 3 items unfiltered, got %d", len(res1))
	}

	// 2. Filter by siteId case-insensitively
	res2 := filterSiteHealthData(sampleData, QueryModel{
		SiteID: []string{"D4EF280A-D71E-4A2F-8F9F-C1740A406075"},
	})
	// Should match the area AND the child building (which has parentSiteId matching)
	if len(res2) != 2 {
		t.Fatalf("expected 2 items (area + child building), got %d", len(res2))
	}

	// 3. Filter by siteName
	res3 := filterSiteHealthData(sampleData, QueryModel{
		SiteName: []string{"ORF Mediencampus"},
	})
	if len(res3) != 2 {
		t.Fatalf("expected 2 items matching ORF Mediencampus directly and as parent, got %d", len(res3))
	}

	// 4. Filter by hierarchy leaf name
	res4 := filterSiteHealthData(sampleData, QueryModel{
		SiteName: []string{"OtherSite"},
	})
	if len(res4) != 1 || res4[0]["siteId"] != "other-uuid" {
		t.Fatalf("expected 1 item matching OtherSite, got %d", len(res4))
	}

	// 5. Filter by parentSiteName explicitly
	res5 := filterSiteHealthData(sampleData, QueryModel{
		ParentSiteName: "Vienna",
	})
	if len(res5) != 1 || res5[0]["siteId"] != "other-uuid" {
		t.Fatalf("expected 1 item matching parent Vienna, got %d", len(res5))
	}

	// 6. Filter by parentSiteName with hierarchy path segment and leading/trailing whitespace
	hierarchyData := []map[string]any{
		{
			"siteId":         "bldg-orf",
			"siteName":       "ORF Mediencampus",
			"siteType":       "building",
			"parentSiteName": "Global / Österreich / Wien",
			"clientCount":    50.0,
		},
	}
	res6 := filterSiteHealthData(hierarchyData, QueryModel{
		ParentSiteName: "Wien",
	})
	if len(res6) != 1 || res6[0]["siteId"] != "bldg-orf" {
		t.Fatalf("expected 1 item matching Wien in hierarchy path, got %d", len(res6))
	}

	res7 := filterSiteHealthData(hierarchyData, QueryModel{
		ParentSiteName: "Österreich",
	})
	if len(res7) != 1 || res7[0]["siteId"] != "bldg-orf" {
		t.Fatalf("expected 1 item matching Österreich in hierarchy path, got %d", len(res7))
	}

	// 7. Subpath matching
	res8 := filterSiteHealthData(hierarchyData, QueryModel{
		ParentSiteName: "Österreich/Wien",
	})
	if len(res8) != 1 || res8[0]["siteId"] != "bldg-orf" {
		t.Fatalf("expected 1 item matching subpath Österreich/Wien, got %d", len(res8))
	}

	// 8. Empty strings in siteID and siteName slices should not filter out results if parent matches
	res9 := filterSiteHealthData(hierarchyData, QueryModel{
		SiteID:         []string{""},
		SiteName:       []string{""},
		ParentSiteName: "Wien",
	})
	if len(res9) != 1 || res9[0]["siteId"] != "bldg-orf" {
		t.Fatalf("expected 1 item when SiteID/SiteName have empty strings, got %d", len(res9))
	}
}

// TestDatasource_CallResource_IssuesPagingClamped verifies that the issues
// passthrough keeps paging within the API contract (WP1.2): the frontend helper
// used to send offset=0 and limit=100.
func TestDatasource_CallResource_IssuesPagingClamped(t *testing.T) {
	f := newFakeCC(t)
	f.LimitMode = limitStrict

	sender := &mockResourceSender{}
	err := NewDatasource().CallResource(context.Background(), &backend.CallResourceRequest{
		Path:          "issues",
		URL:           "issues?startTime=1&endTime=2&limit=100&offset=0",
		PluginContext: f.pluginContext("passthrough"),
	}, sender)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sender.resp == nil || sender.resp.Status != http.StatusOK {
		t.Fatalf("expected 200 OK, got: %+v", sender.resp)
	}
	reqs := f.Requests(epIssues)
	if len(reqs) != 1 {
		t.Fatalf("expected 1 issues request, got %d", len(reqs))
	}
	q := reqs[0].Query
	if q.Get("limit") != "25" || q.Get("offset") != "1" || q.Get("startTime") != "1" || q.Get("endTime") != "2" {
		t.Fatalf("unexpected forwarded query: %v", q)
	}
}

// TestDatasource_CallResource_SitesErrorIsJSON: Grafana parses resource bodies as
// JSON. A plain-text error showed up as "Unexpected token 'i', "failed to fe"...
// is not valid JSON" in the site picker.
func TestDatasource_CallResource_SitesErrorIsJSON(t *testing.T) {
	f := newFakeCC(t)
	f.SetStatus(epSites, http.StatusBadGateway)

	sender := &mockResourceSender{}
	if err := NewDatasource().CallResource(context.Background(), &backend.CallResourceRequest{
		Path:          "sites",
		PluginContext: f.pluginContext("sites-error"),
	}, sender); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sender.resp == nil || sender.resp.Status != http.StatusBadGateway {
		t.Fatalf("expected 502, got: %+v", sender.resp)
	}
	var body struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(sender.resp.Body, &body); err != nil {
		t.Fatalf("error body is not JSON: %v (%s)", err, sender.resp.Body)
	}
	if !strings.Contains(body.Message, "site endpoint returned 502") {
		t.Fatalf("message does not name the cause: %q", body.Message)
	}
	if ct := sender.resp.Headers["Content-Type"]; len(ct) == 0 || ct[0] != "application/json" {
		t.Fatalf("Content-Type = %v, want application/json", ct)
	}
}
