package backend

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

// TestClient_FetchAllIssues_SuccessfulFetch verifies that a standard 200 OK response
// is correctly handled and issues are returned.
func TestClient_FetchAllIssues_SuccessfulFetch(t *testing.T) {
	ctx := context.Background()
	callCount := 0

	// Mock server that returns a single page of issues
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Mock token endpoint
		if strings.Contains(r.URL.Path, "/auth/token") {
			w.Header().Set("X-Auth-Token", "test-token")
			w.WriteHeader(http.StatusOK)
			return
		}

		// Verify token is present
		token := r.Header.Get("X-Auth-Token")
		if token != "test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		callCount++
		
		// Return a single page with 3 issues
		response := IssuesEnvelope{
			Response: []map[string]any{
				{"issueId": "issue-1", "name": "Issue 1"},
				{"issueId": "issue-2", "name": "Issue 2"},
				{"issueId": "issue-3", "name": "Issue 3"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Setup client
	settings := &InstanceSettings{
		BaseURL:  server.URL,
		Username: "testuser",
		Password: "testpass",
	}
	inst := &dsInstance{
		Settings: settings,
		UID:      "test-instance-1",
	}
	tm := newTokenManager()
	client := NewClient(server.Client(), tm, inst)

	// Create query model
	qm := QueryModel{
		TimeRange: backend.TimeRange{
			From: time.Now().Add(-1 * time.Hour),
			To:   time.Now(),
		},
	}

	// Fetch issues
	issuesURL := server.URL + "/api/issues"
	issues, err := client.FetchAllIssues(ctx, issuesURL, qm, 1000)

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if len(issues) != 3 {
		t.Fatalf("expected 3 issues, got: %d", len(issues))
	}

	if callCount != 1 {
		t.Fatalf("expected 1 API call, got: %d", callCount)
	}

	// Verify issue content
	if issues[0]["issueId"] != "issue-1" {
		t.Fatalf("expected issue-1, got: %v", issues[0]["issueId"])
	}
}

// TestClient_FetchAllIssues_Pagination verifies that pagination works correctly
// by returning 2 pages of data and then an empty page to stop.
func TestClient_FetchAllIssues_Pagination(t *testing.T) {
	ctx := context.Background()
	callCount := 0

	// Mock server that returns 2 pages then empty
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/auth/token") {
			w.Header().Set("X-Auth-Token", "test-token")
			w.WriteHeader(http.StatusOK)
			return
		}

		callCount++

		// Parse offset from query params
		offset := r.URL.Query().Get("offset")

		var response IssuesEnvelope

		switch offset {
		case "1":
			// First page: 25 issues (full page at the API maximum)
			issues := make([]map[string]any, 25)
			for i := 0; i < 25; i++ {
				issues[i] = map[string]any{
					"issueId": "issue-" + strconv.Itoa(i+1),
					"name":    "Issue " + strconv.Itoa(i+1),
				}
			}
			response.Response = issues
		case "26":
			// Second page: 10 issues (partial page, should stop pagination)
			issues := make([]map[string]any, 10)
			for i := 0; i < 10; i++ {
				issues[i] = map[string]any{
					"issueId": "issue-" + strconv.Itoa(i+26),
					"name":    "Issue " + strconv.Itoa(i+26),
				}
			}
			response.Response = issues
		default:
			// Any other offset: empty page
			response.Response = []map[string]any{}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	settings := &InstanceSettings{
		BaseURL:  server.URL,
		Username: "testuser",
		Password: "testpass",
	}
	inst := &dsInstance{
		Settings: settings,
		UID:      "test-instance-1",
	}
	tm := newTokenManager()
	client := NewClient(server.Client(), tm, inst)

	qm := QueryModel{
		TimeRange: backend.TimeRange{
			From: time.Now().Add(-1 * time.Hour),
			To:   time.Now(),
		},
	}

	issuesURL := server.URL + "/api/issues"
	issues, err := client.FetchAllIssues(ctx, issuesURL, qm, 1000)

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Should get 25 (page 1) + 10 (page 2) = 35 issues
	if len(issues) != 35 {
		t.Fatalf("expected 35 issues from 2 pages, got: %d", len(issues))
	}

	// Should have made 2 API calls (page 1 and page 2)
	if callCount != 2 {
		t.Fatalf("expected 2 API calls, got: %d", callCount)
	}
}

// TestClient_FetchIssuesPage_401Error verifies that a 401 error triggers
// token refresh and retry logic.
func TestClient_FetchIssuesPage_401Error(t *testing.T) {
	ctx := context.Background()
	firstCall := true
	tokenRefreshCount := 0

	// Mock server that returns 401 on first call, then succeeds
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/auth/token") {
			tokenRefreshCount++
			if tokenRefreshCount == 1 {
				w.Header().Set("X-Auth-Token", "initial-token")
			} else {
				w.Header().Set("X-Auth-Token", "refreshed-token")
			}
			w.WriteHeader(http.StatusOK)
			return
		}

		// Issues endpoint
		if firstCall {
			firstCall = false
			// Return 401 on first attempt
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte("Unauthorized"))
			return
		}

		// Return success on retry
		response := IssuesEnvelope{
			Response: []map[string]any{
				{"issueId": "issue-1", "name": "Issue 1"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	settings := &InstanceSettings{
		BaseURL:  server.URL,
		Username: "testuser",
		Password: "testpass",
	}
	inst := &dsInstance{
		Settings: settings,
		UID:      "test-instance-1",
	}
	tm := newTokenManager()
	client := NewClient(server.Client(), tm, inst)

	// Create params for single page fetch
	params := url.Values{}
	params.Set("limit", "50")
	params.Set("offset", "1")

	issuesURL := server.URL + "/api/issues"
	issues, err := client.fetchIssuesPage(ctx, issuesURL, params)

	if err != nil {
		t.Fatalf("expected no error after retry, got: %v", err)
	}

	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got: %d", len(issues))
	}

	// Should have refreshed token (2 calls: initial + refresh)
	if tokenRefreshCount != 2 {
		t.Fatalf("expected token to be refreshed (2 calls), got: %d", tokenRefreshCount)
	}
}

// TestClient_FetchIssuesPage_403Error verifies that a 403 error also triggers
// token refresh and retry logic (similar to 401).
func TestClient_FetchIssuesPage_403Error(t *testing.T) {
	ctx := context.Background()
	firstCall := true

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/auth/token") {
			w.Header().Set("X-Auth-Token", "test-token")
			w.WriteHeader(http.StatusOK)
			return
		}

		if firstCall {
			firstCall = false
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte("Forbidden"))
			return
		}

		response := IssuesEnvelope{
			Response: []map[string]any{
				{"issueId": "issue-1", "name": "Success after retry"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	settings := &InstanceSettings{
		BaseURL:  server.URL,
		Username: "testuser",
		Password: "testpass",
	}
	inst := &dsInstance{
		Settings: settings,
		UID:      "test-instance-1",
	}
	tm := newTokenManager()
	client := NewClient(server.Client(), tm, inst)

	params := url.Values{}
	params.Set("limit", "50")
	params.Set("offset", "1")

	issuesURL := server.URL + "/api/issues"
	issues, err := client.fetchIssuesPage(ctx, issuesURL, params)

	if err != nil {
		t.Fatalf("expected no error after retry, got: %v", err)
	}

	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got: %d", len(issues))
	}
}

// TestClient_FetchIssuesPage_500Error verifies that a 500 error is correctly
// propagated as a failure without retry.
func TestClient_FetchIssuesPage_500Error(t *testing.T) {
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/auth/token") {
			w.Header().Set("X-Auth-Token", "test-token")
			w.WriteHeader(http.StatusOK)
			return
		}

		// Return 500 error
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Internal Server Error"))
	}))
	defer server.Close()

	settings := &InstanceSettings{
		BaseURL:  server.URL,
		Username: "testuser",
		Password: "testpass",
	}
	inst := &dsInstance{
		Settings: settings,
		UID:      "test-instance-1",
	}
	tm := newTokenManager()
	client := NewClient(server.Client(), tm, inst)

	params := url.Values{}
	params.Set("limit", "50")
	params.Set("offset", "1")

	issuesURL := server.URL + "/api/issues"
	_, err := client.fetchIssuesPage(ctx, issuesURL, params)

	if err == nil {
		t.Fatal("expected error for 500 status, got nil")
	}

	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("expected error to mention 500 status, got: %v", err)
	}
}

// TestClient_FetchIssuesPage_EmptyResponse verifies that a 200 OK with
// an empty list of issues is handled correctly.
func TestClient_FetchIssuesPage_EmptyResponse(t *testing.T) {
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/auth/token") {
			w.Header().Set("X-Auth-Token", "test-token")
			w.WriteHeader(http.StatusOK)
			return
		}

		// Return empty array
		response := IssuesEnvelope{
			Response: []map[string]any{},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	settings := &InstanceSettings{
		BaseURL:  server.URL,
		Username: "testuser",
		Password: "testpass",
	}
	inst := &dsInstance{
		Settings: settings,
		UID:      "test-instance-1",
	}
	tm := newTokenManager()
	client := NewClient(server.Client(), tm, inst)

	params := url.Values{}
	params.Set("limit", "50")
	params.Set("offset", "1")

	issuesURL := server.URL + "/api/issues"
	issues, err := client.fetchIssuesPage(ctx, issuesURL, params)

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if len(issues) != 0 {
		t.Fatalf("expected 0 issues, got: %d", len(issues))
	}
}

// TestClient_FetchAllIssues_HardLimit verifies that the hard limit is respected
// and pagination stops when the limit is reached.
func TestClient_FetchAllIssues_HardLimit(t *testing.T) {
	ctx := context.Background()
	callCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/auth/token") {
			w.Header().Set("X-Auth-Token", "test-token")
			w.WriteHeader(http.StatusOK)
			return
		}

		callCount++

		// Parse the limit from query params to respect it
		limitParam := r.URL.Query().Get("limit")
		limit := 50
		if limitParam != "" {
			if l, err := strconv.Atoi(limitParam); err == nil {
				limit = l
			}
		}

		// Return the requested number of issues (respecting API behavior)
		issues := make([]map[string]any, limit)
		for i := 0; i < limit; i++ {
			issues[i] = map[string]any{
				"issueId": "issue-" + strconv.Itoa(callCount*50+i),
				"name":    "Issue " + strconv.Itoa(callCount*50+i),
			}
		}
		response := IssuesEnvelope{Response: issues}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	settings := &InstanceSettings{
		BaseURL:  server.URL,
		Username: "testuser",
		Password: "testpass",
	}
	inst := &dsInstance{
		Settings: settings,
		UID:      "test-instance-1",
	}
	tm := newTokenManager()
	client := NewClient(server.Client(), tm, inst)

	qm := QueryModel{
		TimeRange: backend.TimeRange{
			From: time.Now().Add(-1 * time.Hour),
			To:   time.Now(),
		},
	}

	issuesURL := server.URL + "/api/issues"
	// Set hard limit to 75
	issues, err := client.FetchAllIssues(ctx, issuesURL, qm, 75)

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Should get exactly 75 issues (3 full pages of 25)
	if len(issues) != 75 {
		t.Fatalf("expected 75 issues (hard limit), got: %d", len(issues))
	}

	// Should have made 3 API calls, then stopped at the hard limit
	if callCount != 3 {
		t.Fatalf("expected 3 API calls, got: %d", callCount)
	}
}

// TestClient_FetchAllSiteHealth_SuccessfulFetch verifies that site health data
// is fetched correctly with pagination.
func TestClient_FetchAllSiteHealth_SuccessfulFetch(t *testing.T) {
	ctx := context.Background()
	callCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/auth/token") {
			w.Header().Set("X-Auth-Token", "test-token")
			w.WriteHeader(http.StatusOK)
			return
		}

		callCount++

		// Parse offset from query params
		offset := r.URL.Query().Get("offset")

		var response struct {
			Response []map[string]any `json:"response"`
		}

		switch offset {
		case "1":
			// First page: 50 items
			items := make([]map[string]any, 50)
			for i := 0; i < 50; i++ {
				items[i] = map[string]any{
					"siteId":   "site-" + string(rune(i+1)),
					"siteName": "Site " + string(rune(i+1)),
				}
			}
			response.Response = items
		case "51":
			// Second page: 20 items (partial, should stop)
			items := make([]map[string]any, 20)
			for i := 0; i < 20; i++ {
				items[i] = map[string]any{
					"siteId":   "site-" + string(rune(i+51)),
					"siteName": "Site " + string(rune(i+51)),
				}
			}
			response.Response = items
		default:
			response.Response = []map[string]any{}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	settings := &InstanceSettings{
		BaseURL:  server.URL,
		Username: "testuser",
		Password: "testpass",
	}
	inst := &dsInstance{
		Settings: settings,
		UID:      "test-instance-1",
	}
	tm := newTokenManager()
	client := NewClient(server.Client(), tm, inst)

	qm := QueryModel{
		TimeRange: backend.TimeRange{
			From: time.Now().Add(-1 * time.Hour),
			To:   time.Now(),
		},
	}

	siteHealthURL, _ := url.Parse(server.URL + "/api/site-health")
	siteHealthData, err := client.FetchAllSiteHealth(ctx, siteHealthURL, qm)

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Should get 50 + 20 = 70 items
	if len(siteHealthData) != 70 {
		t.Fatalf("expected 70 site health items, got: %d", len(siteHealthData))
	}

	// Should have made 2 API calls
	if callCount != 2 {
		t.Fatalf("expected 2 API calls, got: %d", callCount)
	}
}

// TestClient_FetchAllSiteHealth_EmptyResponse verifies that an empty response
// is handled correctly.
func TestClient_FetchAllSiteHealth_EmptyResponse(t *testing.T) {
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/auth/token") {
			w.Header().Set("X-Auth-Token", "test-token")
			w.WriteHeader(http.StatusOK)
			return
		}

		// Return empty response
		response := struct {
			Response []map[string]any `json:"response"`
		}{
			Response: []map[string]any{},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	settings := &InstanceSettings{
		BaseURL:  server.URL,
		Username: "testuser",
		Password: "testpass",
	}
	inst := &dsInstance{
		Settings: settings,
		UID:      "test-instance-1",
	}
	tm := newTokenManager()
	client := NewClient(server.Client(), tm, inst)

	qm := QueryModel{
		TimeRange: backend.TimeRange{
			From: time.Now().Add(-1 * time.Hour),
			To:   time.Now(),
		},
	}

	siteHealthURL, _ := url.Parse(server.URL + "/api/site-health")
	siteHealthData, err := client.FetchAllSiteHealth(ctx, siteHealthURL, qm)

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if len(siteHealthData) != 0 {
		t.Fatalf("expected 0 items, got: %d", len(siteHealthData))
	}
}

// TestClient_FetchAllIssues_WithSiteIDs verifies that when site IDs are provided,
// the client queries each site individually.
func TestClient_FetchAllIssues_WithSiteIDs(t *testing.T) {
	ctx := context.Background()
	queriedSites := make(map[string]int)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/auth/token") {
			w.Header().Set("X-Auth-Token", "test-token")
			w.WriteHeader(http.StatusOK)
			return
		}

		// Track which siteId was queried
		siteID := r.URL.Query().Get("siteId")
		queriedSites[siteID]++

		// Return different issues based on site
		var response IssuesEnvelope
		switch siteID {
		case "site-1":
			response.Response = []map[string]any{
				{"issueId": "issue-1-1", "siteId": "site-1"},
				{"issueId": "issue-1-2", "siteId": "site-1"},
			}
		case "site-2":
			response.Response = []map[string]any{
				{"issueId": "issue-2-1", "siteId": "site-2"},
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	settings := &InstanceSettings{
		BaseURL:  server.URL,
		Username: "testuser",
		Password: "testpass",
	}
	inst := &dsInstance{
		Settings: settings,
		UID:      "test-instance-1",
	}
	tm := newTokenManager()
	client := NewClient(server.Client(), tm, inst)

	qm := QueryModel{
		SiteID: []string{"site-1", "site-2"},
		TimeRange: backend.TimeRange{
			From: time.Now().Add(-1 * time.Hour),
			To:   time.Now(),
		},
	}

	issuesURL := server.URL + "/api/issues"
	issues, err := client.FetchAllIssues(ctx, issuesURL, qm, 1000)

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Should get 2 issues from site-1 + 1 issue from site-2 = 3 total
	if len(issues) != 3 {
		t.Fatalf("expected 3 issues, got: %d", len(issues))
	}

	// Should have queried both sites
	if queriedSites["site-1"] != 1 {
		t.Fatalf("expected site-1 to be queried once, got: %d", queriedSites["site-1"])
	}
	if queriedSites["site-2"] != 1 {
		t.Fatalf("expected site-2 to be queried once, got: %d", queriedSites["site-2"])
	}
}

// TestClient_FetchIssuesPage_ResponseWithoutEnvelope verifies that responses
// that are not wrapped in an envelope are still parsed correctly.
func TestClient_FetchIssuesPage_ResponseWithoutEnvelope(t *testing.T) {
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/auth/token") {
			w.Header().Set("X-Auth-Token", "test-token")
			w.WriteHeader(http.StatusOK)
			return
		}

		// Return array directly without envelope
		response := []map[string]any{
			{"issueId": "issue-1", "name": "Direct Array Issue"},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	settings := &InstanceSettings{
		BaseURL:  server.URL,
		Username: "testuser",
		Password: "testpass",
	}
	inst := &dsInstance{
		Settings: settings,
		UID:      "test-instance-1",
	}
	tm := newTokenManager()
	client := NewClient(server.Client(), tm, inst)

	params := url.Values{}
	params.Set("limit", "50")
	params.Set("offset", "1")

	issuesURL := server.URL + "/api/issues"
	issues, err := client.fetchIssuesPage(ctx, issuesURL, params)

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got: %d", len(issues))
	}

	if issues[0]["issueId"] != "issue-1" {
		t.Fatalf("expected issue-1, got: %v", issues[0]["issueId"])
	}
}

// TestFetchAllIssues_PageSizeWithinAPILimit covers review issue C1: the
// assuranceIssues API accepts a limit of at most 25. A server that rejects
// larger pages (strict) and one that caps them (cap) must both return all rows.
func TestFetchAllIssues_PageSizeWithinAPILimit(t *testing.T) {
	for _, mode := range []fakeLimitMode{limitStrict, limitCap} {
		f := newFakeCC(t)
		f.LimitMode = mode
		f.addIssues(146, f.site("Global/Österreich/Wien/HQ")) // 150 issues in total

		dr := f.runQuery(t, NewDatasource(), "page-size", map[string]any{"queryType": "assuranceIssues", "limit": 100})
		if dr.Error != nil {
			t.Fatalf("mode %d: query error: %v", mode, dr.Error)
		}
		if got := frameRows(dr); got != 100 {
			t.Errorf("mode %d: rows = %d, want 100", mode, got)
		}
		var offsets []string
		for _, r := range f.Requests(epIssues) {
			if l, _ := strconv.Atoi(r.Query.Get("limit")); l < 1 || l > 25 {
				t.Errorf("mode %d: request limit %q outside 1..25", mode, r.Query.Get("limit"))
			}
			offsets = append(offsets, r.Query.Get("offset"))
		}
		if strings.Join(offsets, ",") != "1,26,51,76" {
			t.Errorf("mode %d: offsets = %v, want [1 26 51 76]", mode, offsets)
		}
	}
}
