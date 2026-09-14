package backend

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestSiteTranslator_SuccessfulLookup verifies that GetSiteName() correctly returns
// a site name for a known ID.
func TestSiteTranslator_SuccessfulLookup(t *testing.T) {
	ctx := context.Background()

	// Mock server that returns site data
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Mock token endpoint
		if strings.Contains(r.URL.Path, "/auth/token") {
			w.Header().Set("X-Auth-Token", "test-token")
			w.WriteHeader(http.StatusOK)
			return
		}

		// Mock site endpoint
		response := SiteEnvelope{
			Response: []Site{
				{ID: "site-001", Name: "Building A"},
				{ID: "site-002", Name: "Building B"},
				{ID: "site-003", Name: "Campus Main"},
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
	st := NewSiteTranslator(server.Client(), inst, tm)

	// Ensure cache is populated
	err := st.EnsureCache(ctx)
	if err != nil {
		t.Fatalf("EnsureCache failed: %v", err)
	}

	// Test successful lookup
	name, err := st.GetSiteName(ctx, "site-001")
	if err != nil {
		t.Fatalf("GetSiteName failed: %v", err)
	}
	if name != "Building A" {
		t.Fatalf("expected 'Building A', got %q", name)
	}

	// Test another ID
	name, err = st.GetSiteName(ctx, "site-003")
	if err != nil {
		t.Fatalf("GetSiteName failed: %v", err)
	}
	if name != "Campus Main" {
		t.Fatalf("expected 'Campus Main', got %q", name)
	}
}

// TestSiteTranslator_Caching verifies that a second call for the same ID returns
// the cached value without triggering a new API fetch.
func TestSiteTranslator_Caching(t *testing.T) {
	ctx := context.Background()
	callCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/auth/token") {
			w.Header().Set("X-Auth-Token", "test-token")
			w.WriteHeader(http.StatusOK)
			return
		}

		callCount++
		response := SiteEnvelope{
			Response: []Site{
				{ID: "site-001", Name: "Building A"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	inst := &dsInstance{
		Settings: &InstanceSettings{
			BaseURL:  server.URL,
			Username: "user",
			Password: "pass",
		},
		UID: "test-instance",
	}

	tm := newTokenManager()
	st := NewSiteTranslator(server.Client(), inst, tm)

	// First call - should hit the API to populate cache
	err := st.EnsureCache(ctx)
	if err != nil {
		t.Fatalf("EnsureCache failed: %v", err)
	}

	if callCount != 1 {
		t.Fatalf("expected 1 API call, got %d", callCount)
	}

	// Now use GetSiteName - should use cached data
	_, err = st.GetSiteName(ctx, "site-001")
	if err != nil {
		t.Fatalf("first GetSiteName failed: %v", err)
	}

	if callCount != 1 {
		t.Fatalf("expected still 1 API call (cached), got %d", callCount)
	}

	// Second call - should use cache
	_, err = st.GetSiteName(ctx, "site-001")
	if err != nil {
		t.Fatalf("second GetSiteName failed: %v", err)
	}

	if callCount != 1 {
		t.Fatalf("expected still 1 API call (cached), got %d", callCount)
	}

	// Third call with GetSiteByID - should still use cache
	_, err = st.GetSiteByID(ctx, "site-001")
	if err != nil {
		t.Fatalf("GetSiteByID failed: %v", err)
	}

	if callCount != 1 {
		t.Fatalf("expected still 1 API call (cached), got %d", callCount)
	}
}

// TestSiteTranslator_UnknownID tests how the translator handles a Site ID
// that is not in its cache.
func TestSiteTranslator_UnknownID(t *testing.T) {
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/auth/token") {
			w.Header().Set("X-Auth-Token", "test-token")
			w.WriteHeader(http.StatusOK)
			return
		}

		response := SiteEnvelope{
			Response: []Site{
				{ID: "site-001", Name: "Building A"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	inst := &dsInstance{
		Settings: &InstanceSettings{
			BaseURL:  server.URL,
			Username: "user",
			Password: "pass",
		},
		UID: "test-instance",
	}

	tm := newTokenManager()
	st := NewSiteTranslator(server.Client(), inst, tm)

	// Ensure cache is populated
	err := st.EnsureCache(ctx)
	if err != nil {
		t.Fatalf("EnsureCache failed: %v", err)
	}

	// Try to get a site that doesn't exist
	_, err = st.GetSiteName(ctx, "site-999")
	if err == nil {
		t.Fatal("expected error for unknown site ID")
	}

	expectedErr := "site with ID 'site-999' not found in cache"
	if !strings.Contains(err.Error(), expectedErr) {
		t.Fatalf("expected error containing %q, got %q", expectedErr, err.Error())
	}
}

// TestSiteTranslator_CacheRefresh verifies that the cache is re-fetched after
// the TTL expires.
func TestSiteTranslator_CacheRefresh(t *testing.T) {
	ctx := context.Background()
	callCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/auth/token") {
			w.Header().Set("X-Auth-Token", "test-token")
			w.WriteHeader(http.StatusOK)
			return
		}

		callCount++
		response := SiteEnvelope{
			Response: []Site{
				{ID: "site-001", Name: "Building A"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	inst := &dsInstance{
		Settings: &InstanceSettings{
			BaseURL:  server.URL,
			Username: "user",
			Password: "pass",
		},
		UID: "test-instance",
	}

	tm := newTokenManager()
	st := NewSiteTranslator(server.Client(), inst, tm)

	// Set a very short TTL for testing
	st.cacheTTL = 100 * time.Millisecond

	// First call - populates cache
	err := st.EnsureCache(ctx)
	if err != nil {
		t.Fatalf("first EnsureCache failed: %v", err)
	}

	if callCount != 1 {
		t.Fatalf("expected 1 API call, got %d", callCount)
	}

	// Second call immediately - should use cache
	err = st.EnsureCache(ctx)
	if err != nil {
		t.Fatalf("second EnsureCache failed: %v", err)
	}

	if callCount != 1 {
		t.Fatalf("expected still 1 API call (cached), got %d", callCount)
	}

	// Wait for cache to expire
	time.Sleep(150 * time.Millisecond)

	// Third call - cache should be stale, triggers refresh
	err = st.EnsureCache(ctx)
	if err != nil {
		t.Fatalf("third EnsureCache failed: %v", err)
	}

	if callCount != 2 {
		t.Fatalf("expected 2 API calls (cache refreshed), got %d", callCount)
	}
}

// TestSiteTranslator_GetSiteByID verifies the GetSiteByID method returns
// the full Site object.
func TestSiteTranslator_GetSiteByID(t *testing.T) {
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/auth/token") {
			w.Header().Set("X-Auth-Token", "test-token")
			w.WriteHeader(http.StatusOK)
			return
		}

		response := SiteEnvelope{
			Response: []Site{
				{ID: "site-001", Name: "Building A", ParentID: "parent-001"},
				{ID: "site-002", Name: "Building B", ParentID: "parent-002"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	inst := &dsInstance{
		Settings: &InstanceSettings{
			BaseURL:  server.URL,
			Username: "user",
			Password: "pass",
		},
		UID: "test-instance",
	}

	tm := newTokenManager()
	st := NewSiteTranslator(server.Client(), inst, tm)

	err := st.EnsureCache(ctx)
	if err != nil {
		t.Fatalf("EnsureCache failed: %v", err)
	}

	// Get full site object
	site, err := st.GetSiteByID(ctx, "site-001")
	if err != nil {
		t.Fatalf("GetSiteByID failed: %v", err)
	}

	if site.ID != "site-001" {
		t.Fatalf("expected ID 'site-001', got %q", site.ID)
	}
	if site.Name != "Building A" {
		t.Fatalf("expected Name 'Building A', got %q", site.Name)
	}
	if site.ParentID != "parent-001" {
		t.Fatalf("expected ParentID 'parent-001', got %q", site.ParentID)
	}
}

// TestSiteTranslator_GetSiteID verifies case-insensitive name-to-ID lookup.
func TestSiteTranslator_GetSiteID(t *testing.T) {
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/auth/token") {
			w.Header().Set("X-Auth-Token", "test-token")
			w.WriteHeader(http.StatusOK)
			return
		}

		response := SiteEnvelope{
			Response: []Site{
				{ID: "site-001", Name: "Building A"},
				{ID: "site-002", Name: "Campus Main"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	inst := &dsInstance{
		Settings: &InstanceSettings{
			BaseURL:  server.URL,
			Username: "user",
			Password: "pass",
		},
		UID: "test-instance",
	}

	tm := newTokenManager()
	st := NewSiteTranslator(server.Client(), inst, tm)

	// Test exact case
	id, err := st.GetSiteID(ctx, "Building A")
	if err != nil {
		t.Fatalf("GetSiteID failed: %v", err)
	}
	if id != "site-001" {
		t.Fatalf("expected 'site-001', got %q", id)
	}

	// Test lowercase
	id, err = st.GetSiteID(ctx, "building a")
	if err != nil {
		t.Fatalf("GetSiteID (lowercase) failed: %v", err)
	}
	if id != "site-001" {
		t.Fatalf("expected 'site-001', got %q", id)
	}

	// Test uppercase
	id, err = st.GetSiteID(ctx, "CAMPUS MAIN")
	if err != nil {
		t.Fatalf("GetSiteID (uppercase) failed: %v", err)
	}
	if id != "site-002" {
		t.Fatalf("expected 'site-002', got %q", id)
	}

	// Test unknown name
	_, err = st.GetSiteID(ctx, "Unknown Building")
	if err == nil {
		t.Fatal("expected error for unknown site name")
	}
}

// TestSiteTranslator_GetAllSites verifies that GetAllSites returns all cached sites.
func TestSiteTranslator_GetAllSites(t *testing.T) {
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/auth/token") {
			w.Header().Set("X-Auth-Token", "test-token")
			w.WriteHeader(http.StatusOK)
			return
		}

		response := SiteEnvelope{
			Response: []Site{
				{ID: "site-001", Name: "Building A"},
				{ID: "site-002", Name: "Building B"},
				{ID: "site-003", Name: "Campus Main"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	inst := &dsInstance{
		Settings: &InstanceSettings{
			BaseURL:  server.URL,
			Username: "user",
			Password: "pass",
		},
		UID: "test-instance",
	}

	tm := newTokenManager()
	st := NewSiteTranslator(server.Client(), inst, tm)

	sites, err := st.GetAllSites(ctx)
	if err != nil {
		t.Fatalf("GetAllSites failed: %v", err)
	}

	if len(sites) != 3 {
		t.Fatalf("expected 3 sites, got %d", len(sites))
	}

	// Verify the returned slice is a copy (modifying it shouldn't affect internal cache)
	sites[0].Name = "Modified Name"

	sites2, _ := st.GetAllSites(ctx)
	if sites2[0].Name == "Modified Name" {
		t.Fatal("GetAllSites should return a copy, not the internal slice")
	}
}

// TestSiteTranslator_Pagination verifies that the translator handles paginated
// API responses correctly.
func TestSiteTranslator_Pagination(t *testing.T) {
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/auth/token") {
			w.Header().Set("X-Auth-Token", "test-token")
			w.WriteHeader(http.StatusOK)
			return
		}

		// Check offset parameter to determine which page to return
		offset := r.URL.Query().Get("offset")

		var response SiteEnvelope
		switch offset {
		case "1":
			// First page - return 500 sites (simulating full page)
			sites := make([]Site, 500)
			for i := 0; i < 500; i++ {
				sites[i] = Site{
					ID:   "site-00" + strconv.Itoa(i),
					Name: "Site " + strconv.Itoa(i),
				}
			}
			response = SiteEnvelope{Response: sites}
		case "501":
			// Second page - return 100 sites (last page)
			sites := make([]Site, 100)
			for i := 0; i < 100; i++ {
				sites[i] = Site{
					ID:   "site-50" + strconv.Itoa(i),
					Name: "Site 50" + strconv.Itoa(i),
				}
			}
			response = SiteEnvelope{Response: sites}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	inst := &dsInstance{
		Settings: &InstanceSettings{
			BaseURL:  server.URL,
			Username: "user",
			Password: "pass",
		},
		UID: "test-instance",
	}

	tm := newTokenManager()
	st := NewSiteTranslator(server.Client(), inst, tm)

	err := st.EnsureCache(ctx)
	if err != nil {
		t.Fatalf("EnsureCache failed: %v", err)
	}

	sites, err := st.GetAllSites(ctx)
	if err != nil {
		t.Fatalf("GetAllSites failed: %v", err)
	}

	// Should have loaded all 600 sites from both pages
	if len(sites) != 600 {
		t.Fatalf("expected 600 sites from pagination, got %d", len(sites))
	}
}

// TestSiteTranslator_ThreadSafety verifies that concurrent access to the translator
// is handled safely.
func TestSiteTranslator_ThreadSafety(t *testing.T) {
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/auth/token") {
			w.Header().Set("X-Auth-Token", "test-token")
			w.WriteHeader(http.StatusOK)
			return
		}

		response := SiteEnvelope{
			Response: []Site{
				{ID: "site-001", Name: "Building A"},
				{ID: "site-002", Name: "Building B"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	inst := &dsInstance{
		Settings: &InstanceSettings{
			BaseURL:  server.URL,
			Username: "user",
			Password: "pass",
		},
		UID: "test-instance",
	}

	tm := newTokenManager()
	st := NewSiteTranslator(server.Client(), inst, tm)

	// Ensure cache is populated before concurrent access
	err := st.EnsureCache(ctx)
	if err != nil {
		t.Fatalf("initial EnsureCache failed: %v", err)
	}

	// Make concurrent calls to various methods
	const numGoroutines = 20
	var wg sync.WaitGroup
	errors := make([]error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			// Mix different operations
			switch idx % 4 {
			case 0:
				_, errors[idx] = st.GetSiteName(ctx, "site-001")
			case 1:
				_, errors[idx] = st.GetSiteByID(ctx, "site-002")
			case 2:
				_, errors[idx] = st.GetAllSites(ctx)
			case 3:
				errors[idx] = st.EnsureCache(ctx)
			}
		}(i)
	}

	wg.Wait()

	// Verify all operations completed without error
	for i, err := range errors {
		if err != nil {
			t.Fatalf("goroutine %d got error: %v", i, err)
		}
	}
}

// TestSiteTranslator_EmptyResponse verifies handling of empty site list.
func TestSiteTranslator_EmptyResponse(t *testing.T) {
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/auth/token") {
			w.Header().Set("X-Auth-Token", "test-token")
			w.WriteHeader(http.StatusOK)
			return
		}

		response := SiteEnvelope{
			Response: []Site{}, // Empty list
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	inst := &dsInstance{
		Settings: &InstanceSettings{
			BaseURL:  server.URL,
			Username: "user",
			Password: "pass",
		},
		UID: "test-instance",
	}

	tm := newTokenManager()
	st := NewSiteTranslator(server.Client(), inst, tm)

	err := st.EnsureCache(ctx)
	if err != nil {
		t.Fatalf("EnsureCache should handle empty response: %v", err)
	}

	sites, err := st.GetAllSites(ctx)
	if err != nil {
		t.Fatalf("GetAllSites failed: %v", err)
	}

	if len(sites) != 0 {
		t.Fatalf("expected 0 sites, got %d", len(sites))
	}
}

// TestSiteTranslator_HTTPError verifies proper error handling for API failures.
func TestSiteTranslator_HTTPError(t *testing.T) {
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/auth/token") {
			w.Header().Set("X-Auth-Token", "test-token")
			w.WriteHeader(http.StatusOK)
			return
		}

		// Return error response
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Internal server error"))
	}))
	defer server.Close()

	inst := &dsInstance{
		Settings: &InstanceSettings{
			BaseURL:  server.URL,
			Username: "user",
			Password: "pass",
		},
		UID: "test-instance",
	}

	tm := newTokenManager()
	st := NewSiteTranslator(server.Client(), inst, tm)

	err := st.EnsureCache(ctx)
	if err == nil {
		t.Fatal("expected error for HTTP 500 response")
	}

	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("expected error to mention status code, got: %v", err)
	}
}

// TestSiteTranslator_DoubleCheckLock verifies the double-check locking pattern
// prevents redundant refreshes when cache is fresh.
func TestSiteTranslator_DoubleCheckLock(t *testing.T) {
	ctx := context.Background()
	var callCount int
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/auth/token") {
			w.Header().Set("X-Auth-Token", "test-token")
			w.WriteHeader(http.StatusOK)
			return
		}

		mu.Lock()
		callCount++
		mu.Unlock()

		// Add a small delay to simulate network latency
		time.Sleep(10 * time.Millisecond)

		response := SiteEnvelope{
			Response: []Site{{ID: "site-001", Name: "Building A"}},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	inst := &dsInstance{
		Settings: &InstanceSettings{
			BaseURL:  server.URL,
			Username: "user",
			Password: "pass",
		},
		UID: "test-instance",
	}

	tm := newTokenManager()
	st := NewSiteTranslator(server.Client(), inst, tm)

	// Normal TTL - cache will be fresh after first call
	st.cacheTTL = 5 * time.Minute

	// Make the first call to populate cache
	err := st.EnsureCache(ctx)
	if err != nil {
		t.Fatalf("initial EnsureCache failed: %v", err)
	}

	mu.Lock()
	initialCount := callCount
	mu.Unlock()

	if initialCount != 1 {
		t.Fatalf("expected 1 initial API call, got %d", initialCount)
	}

	// Now make multiple concurrent calls - cache is fresh, so no refreshes should occur
	const numGoroutines = 10
	var wg sync.WaitGroup
	errors := make([]error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			errors[idx] = st.EnsureCache(ctx)
		}(i)
	}

	wg.Wait()

	// All should succeed
	for i, err := range errors {
		if err != nil {
			t.Fatalf("goroutine %d got error: %v", i, err)
		}
	}

	// Cache is fresh, so no additional API calls should have been made
	mu.Lock()
	finalCount := callCount
	mu.Unlock()

	if finalCount != initialCount {
		t.Fatalf("expected no additional API calls (cache fresh), got %d total calls", finalCount)
	}

	t.Logf("Successfully avoided redundant refreshes: %d API call for initial load + %d concurrent checks", initialCount, numGoroutines)
}
