package backend

import (
	"fmt"
	"net/url"
	"testing"
	"time"
)

func TestNormalizePriority(t *testing.T) {
	tests := []struct {
		priority string
		severity string
		want     string
		ok       bool
	}{
		{"P1", "", "P1", true},
		{"p2", "", "P2", true},
		{"", "P3", "P3", true},
		{"", "p4", "P4", true},
		{"", "", "", false},
		{"weird", "nope", "", false},
		{"P1", "P2", "P1", true}, // Primary field (priority) wins
		{"  p1  ", "", "P1", true}, // Test trimming
	}
	for _, tt := range tests {
		got, ok := normalizePriority(tt.priority, tt.severity)
		if got != tt.want || ok != tt.ok {
			t.Fatalf("normalizePriority(%q,%q) = (%q,%v), want (%q,%v)", tt.priority, tt.severity, got, ok, tt.want, tt.ok)
		}
	}
}

func TestNormalizeIssueStatus(t *testing.T) {
	tests := []struct {
		issueStatus string
		status      string
		want        string
		ok          bool
	}{
		{"ACTIVE", "", "active", true},
		{"resolved", "", "resolved", true},
		{"", "ignored", "ignored", true},
		{"", "active", "active", true},
		{"", "", "", false},
		{"bad", "also_bad", "", false},
		{"ACTIVE", "RESOLVED", "active", true}, // Primary field (issueStatus) wins
		{"  resolved  ", "", "resolved", true},  // Test trimming
	}
	for _, tt := range tests {
		got, ok := normalizeIssueStatus(tt.issueStatus, tt.status)
		if got != tt.want || ok != tt.ok {
			t.Errorf("normalizeIssueStatus(%q, %q) = (%q, %v), want (%q, %v)", tt.issueStatus, tt.status, got, ok, tt.want, tt.ok)
		}
	}
}

func TestBuildAssuranceParamsFromQuery(t *testing.T) {
	q := QueryModel{
		SiteID:          []string{"site-123"},
		NetworkDeviceID: "dev-456",
		MACAddress:      "00:11:22:33:44:55",
		Priority:        []string{"P2"},
		Status:          []string{"resolved"},
	}

	params := buildAssuranceParamsFromQuery(q, 1700000000000, 1700003600000, 100, 1)

	want := url.Values{
		"siteId":          []string{"site-123"},
		"networkDeviceId": []string{"dev-456"},
		"macAddress":      []string{"00:11:22:33:44:55"},
		"priority":        []string{"P2"},
		"status":          []string{"resolved"},
		"limit":           []string{"25"}, // clamped to the API maximum
		"offset":          []string{"1"},
		"startTime":       []string{"1700000000000"},
		"endTime":         []string{"1700003600000"},
	}

	if got := params.Encode(); got != want.Encode() {
		t.Fatalf("params mismatch\ngot:  %q\nwant: %q", got, want.Encode())
	}
}

func TestBuildAssuranceParams_SkipEmpties(t *testing.T) {
	q := QueryModel{
		Priority: []string{"P3"}, // use Priority field as per new struct
	}

	params := buildAssuranceParamsFromQuery(q, 0, 0, -5, 0) // bad page/offset should be clamped/fixed
	if _, ok := params["priority"]; !ok {
		t.Fatal("expected priority from severity")
	}
	if params.Get("priority") != "P3" {
		t.Fatalf("priority = %q, want P3", params.Get("priority"))
	}
	if params.Get("limit") != "25" { // default page size = API maximum
		t.Fatalf("limit = %q, want 25", params.Get("limit"))
	}
	if params.Get("offset") != "1" {
		t.Fatalf("offset = %q, want 1", params.Get("offset"))
	}
	// No startTime/endTime unless non-zero
	if _, ok := params["startTime"]; ok {
		t.Fatal("startTime should be omitted")
	}
	if _, ok := params["endTime"]; ok {
		t.Fatal("endTime should be omitted")
	}
}

func TestBuildSiteHealthParamsFromQuery(t *testing.T) {
	// Case 1: Recent timestamp (e.g. now) should be omitted to fetch latest snapshot
	recentTs := time.Now().UnixMilli()
	q1 := QueryModel{
		SiteType: "BUILDING",
	}
	p1 := buildSiteHealthParamsFromQuery(q1, recentTs, 50, 1)
	if _, exists := p1["timestamp"]; exists {
		t.Errorf("expected timestamp to be omitted for recent time, but got: %s", p1.Get("timestamp"))
	}
	if p1.Get("siteType") != "BUILDING" {
		t.Errorf("expected siteType BUILDING, got %s", p1.Get("siteType"))
	}
	if p1.Get("limit") != "50" {
		t.Errorf("expected limit 50, got %s", p1.Get("limit"))
	}
	if p1.Get("offset") != "1" {
		t.Errorf("expected offset 1, got %s", p1.Get("offset"))
	}

	// Case 2: Historical timestamp (e.g. 2 hours ago) should be included
	histTs := time.Now().Add(-2 * time.Hour).UnixMilli()
	p2 := buildSiteHealthParamsFromQuery(q1, histTs, 50, 1)
	if p2.Get("timestamp") != fmt.Sprintf("%d", histTs) {
		t.Errorf("expected timestamp %d, got %s", histTs, p2.Get("timestamp"))
	}

	// Case 3: Zero or negative timestamp should be omitted
	p3 := buildSiteHealthParamsFromQuery(q1, 0, 10, 0)
	if _, exists := p3["timestamp"]; exists {
		t.Errorf("expected timestamp to be omitted for zero, but got: %s", p3.Get("timestamp"))
	}
	if p3.Get("offset") != "1" {
		t.Errorf("expected offset clamped to 1, got %s", p3.Get("offset"))
	}
	if p3.Get("limit") != "10" {
		t.Errorf("expected limit 10, got %s", p3.Get("limit"))
	}
}

