package backend

import (
	"testing"

	_ "github.com/grafana/grafana-plugin-sdk-go/data"
)

func TestSiteHealthResponseToDataFrame_Empty(t *testing.T) {
	frame := SiteHealthResponseToDataFrame([]map[string]any{}, "A", []string{"clientCount"})
	if frame == nil {
		t.Fatal("expected non-nil frame")
	}
	if frame.Name != "A" {
		t.Fatalf("expected frame name A, got %s", frame.Name)
	}
	if frame.Meta == nil || len(frame.Meta.Notices) == 0 {
		t.Fatal("expected notices on empty data")
	}
	if frame.Meta.Notices[0].Text != "No site health data found for the selected filters." {
		t.Fatalf("unexpected notice text: %s", frame.Meta.Notices[0].Text)
	}
}

func TestSiteHealthResponseToDataFrame_WithSelectedMetrics(t *testing.T) {
	dataRows := []map[string]any{
		{
			"siteId":                             "site-uuid-1",
			"siteName":                           "ORF Mediencampus",
			"clientCount":                        42.0,
			"totalNumberOfConnectedWiredClients": 12,
			"totalNumberOfActiveWirelessClients": 30.0,
		},
		{
			"siteId":                             "site-uuid-2",
			"siteName":                           "Building B",
			"clientCount":                        10.0,
			"totalNumberOfConnectedWiredClients": 4,
			"totalNumberOfActiveWirelessClients": nil,
		},
	}

	selected := []string{
		"clientCount",
		"totalNumberOfConnectedWiredClients",
		"totalNumberOfActiveWirelessClients",
	}

	frame := SiteHealthResponseToDataFrame(dataRows, "A", selected)
	if frame == nil {
		t.Fatal("expected non-nil frame")
	}

	// Should have siteName, siteId + 3 selected metrics = 5 fields
	if len(frame.Fields) != 5 {
		t.Fatalf("expected 5 fields, got %d", len(frame.Fields))
	}

	expectedNames := []string{
		"siteName",
		"siteId",
		"clientCount",
		"totalNumberOfConnectedWiredClients",
		"totalNumberOfActiveWirelessClients",
	}

	for i, name := range expectedNames {
		if frame.Fields[i].Name != name {
			t.Errorf("field %d: expected name %q, got %q", i, name, frame.Fields[i].Name)
		}
	}

	// Verify all fields have equal row count
	rowLen := frame.Fields[0].Len()
	if rowLen != 2 {
		t.Fatalf("expected 2 rows, got %d", rowLen)
	}
	for _, field := range frame.Fields {
		if field.Len() != rowLen {
			t.Errorf("field %s length mismatch: got %d, want %d", field.Name, field.Len(), rowLen)
		}
	}

	// Verify siteName values
	siteName0 := frame.Fields[0].At(0).(*string)
	if siteName0 == nil || *siteName0 != "ORF Mediencampus" {
		t.Fatalf("row 0 siteName mismatch: %v", siteName0)
	}

	siteName1 := frame.Fields[0].At(1).(*string)
	if siteName1 == nil || *siteName1 != "Building B" {
		t.Fatalf("row 1 siteName mismatch: %v", siteName1)
	}

	// Verify clientCount values
	cc0 := frame.Fields[2].At(0).(*float64)
	if cc0 == nil || *cc0 != 42.0 {
		t.Fatalf("row 0 clientCount mismatch: %v", cc0)
	}

	// Verify wired clients (was integer 12 in map, should be float64 in frame)
	wc0 := frame.Fields[3].At(0).(*float64)
	if wc0 == nil || *wc0 != 12.0 {
		t.Fatalf("row 0 wired clients mismatch: %v", wc0)
	}

	// Verify wireless clients on row 1 was nil
	wc1 := frame.Fields[4].At(1).(*float64)
	if wc1 != nil {
		t.Fatalf("row 1 wireless clients should be nil, got: %v", wc1)
	}
}

func TestSiteHealthResponseToDataFrame_AutoMetricDiscovery(t *testing.T) {
	dataRows := []map[string]any{
		{
			"siteId":      "site-1",
			"siteName":    "Site A",
			"clientCount": 5.0,
		},
		{
			"siteId":      "site-2",
			"siteName":    "Site B",
			"healthScore": 95.0,
		},
	}

	// Empty selected metrics -> should discover clientCount and healthScore
	frame := SiteHealthResponseToDataFrame(dataRows, "B", nil)
	if frame == nil {
		t.Fatal("expected non-nil frame")
	}

	// siteName, siteId, clientCount, healthScore (sorted)
	if len(frame.Fields) != 4 {
		t.Fatalf("expected 4 fields, got %d", len(frame.Fields))
	}

	if frame.Fields[2].Name != "clientCount" || frame.Fields[3].Name != "healthScore" {
		t.Fatalf("expected sorted discovered metrics, got: %s, %s", frame.Fields[2].Name, frame.Fields[3].Name)
	}

	// All fields must have 2 rows
	for _, f := range frame.Fields {
		if f.Len() != 2 {
			t.Errorf("field %s expected len 2, got %d", f.Name, f.Len())
		}
	}
}

func TestSiteHealthResponseToDataFrame_MetricAliases(t *testing.T) {
	dataRows := []map[string]any{
		{
			"siteId":                  "site-wien",
			"siteName":                "Wien",
			"numberOfClients":         50,
			"numberOfWiredClients":    20,
			"numberOfWirelessClients": 30,
		},
		{
			"siteId":                  "site-orf",
			"siteName":                "ORF Mediencampus",
			"numberOfWiredClients":    15,
			"numberOfWirelessClients": 25,
			// numberOfClients omitted to test summation fallback
		},
	}

	selected := []string{
		"clientCount",
		"totalNumberOfConnectedWiredClients",
		"totalNumberOfActiveWirelessClients",
	}

	frame := SiteHealthResponseToDataFrame(dataRows, "A", selected)
	if frame == nil {
		t.Fatal("expected non-nil frame")
	}

	if len(frame.Fields) != 5 {
		t.Fatalf("expected 5 fields, got %d", len(frame.Fields))
	}

	// Row 0: Wien
	// clientCount -> numberOfClients = 50
	c0 := frame.Fields[2].At(0).(*float64)
	if c0 == nil || *c0 != 50.0 {
		t.Fatalf("expected row 0 clientCount to resolve to 50, got %v", c0)
	}

	// totalNumberOfConnectedWiredClients -> numberOfWiredClients = 20
	w0 := frame.Fields[3].At(0).(*float64)
	if w0 == nil || *w0 != 20.0 {
		t.Fatalf("expected row 0 wired clients to resolve to 20, got %v", w0)
	}

	// totalNumberOfActiveWirelessClients -> numberOfWirelessClients = 30
	wl0 := frame.Fields[4].At(0).(*float64)
	if wl0 == nil || *wl0 != 30.0 {
		t.Fatalf("expected row 0 wireless clients to resolve to 30, got %v", wl0)
	}

	// Row 1: ORF Mediencampus
	// clientCount -> sum of wired (15) + wireless (25) = 40
	c1 := frame.Fields[2].At(1).(*float64)
	if c1 == nil || *c1 != 40.0 {
		t.Fatalf("expected row 1 clientCount sum to be 40, got %v", c1)
	}

	w1 := frame.Fields[3].At(1).(*float64)
	if w1 == nil || *w1 != 15.0 {
		t.Fatalf("expected row 1 wired clients to resolve to 15, got %v", w1)
	}

	wl1 := frame.Fields[4].At(1).(*float64)
	if wl1 == nil || *wl1 != 25.0 {
		t.Fatalf("expected row 1 wireless clients to resolve to 25, got %v", wl1)
	}
}
