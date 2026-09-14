// Package backend contains the core logic for the Catalyst datasource.
// This file provides data transformation functions to convert API responses
// into Grafana data frames.
package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	log "github.com/grafana/grafana-plugin-sdk-go/backend/log"
	"github.com/grafana/grafana-plugin-sdk-go/data"
)

// IssueResponseToDataFrame transforms a list of assurance issues from the API
// into a Grafana data.Frame. It enriches the data by resolving site IDs to names
// using the provided SiteTranslator.
func IssueResponseToDataFrame(
	ctx context.Context,
	allIssues []map[string]any,
	refID string,
	timeRange backend.TimeRange,
	hardLimit int64,
	siteTranslator *SiteTranslator,
) *data.Frame {
	// Internal row structure for organizing issue data
	type row struct {
		TimeMs     int64
		ID         string
		Title      string
		Severity   string
		Status     string
		Category   string
		Device     string
		MAC        string
		Site       string
		ParentSite string
		Rule       string
		Details    string
	}

	issueRows := make([]row, 0, min(len(allIssues), int(hardLimit)))
	for _, it := range allIssues[:min(len(allIssues), int(hardLimit))] {
		getStr := func(k string) string {
			if v, ok := it[k]; ok && v != nil {
				if s, ok2 := v.(string); ok2 {
					return s
				}
			}
			return ""
		}
		getNum := func(k string) int64 {
			if v, ok := it[k]; ok && v != nil {
				switch x := v.(type) {
				case float64:
					return int64(x)
				case int64:
					return x
				case json.Number:
					n, _ := x.Int64()
					return n
				}
			}
			return 0
		}

		siteIDValue := getStr("siteId")
		siteNameValue := siteIDValue // Fallback to ID.
		parentSiteNameValue := ""      // Default to empty string
		var site Site
		var parentSite Site
		// Use the translator to get the site and its parent.
		site, err := siteTranslator.GetSiteByID(ctx, siteIDValue)
		if err == nil {
			siteNameValue = site.Name
			if site.ParentID != "" {
				parentSite, err = siteTranslator.GetSiteByID(ctx, site.ParentID)
				if err == nil {
					parentSiteNameValue = parentSite.Name
				} else {
					log.DefaultLogger.Warn("failed to resolve parent site ID to name", "parentSiteId", site.ParentID, "err", err)
				}
			}
		} else {
			log.DefaultLogger.Warn("failed to resolve site ID to name", "siteId", siteIDValue, "err", err)
		}

		r := row{
			TimeMs:     firstNonZero(getNum("timestamp"), getNum("firstOccurredTime"), getNum("startTime")),
			ID:         firstNonEmpty(getStr("issueId"), getStr("id"), getStr("instanceId")),
			Title:      firstNonEmpty(getStr("name"), getStr("title"), getStr("issueTitle")),
			Severity:   firstNonEmpty(getStr("priority"), getStr("severity")),
			Status:     firstNonEmpty(getStr("issueStatus"), getStr("status")),
			Category:   firstNonEmpty(getStr("category"), getStr("type")),
			Device:     firstNonEmpty(getStr("networkDeviceId"), getStr("deviceId"), getStr("deviceIp"), getStr("device")),
			MAC:        firstNonEmpty(getStr("macAddress"), getStr("clientMac")),
			Site:       siteNameValue,
			ParentSite: parentSiteNameValue,
			Rule:       getStr("ruleId"),
			Details:    firstNonEmpty(getStr("description"), getStr("details"), getStr("issueDescription")),
		}
		if r.TimeMs == 0 {
			r.TimeMs = timeRange.From.UnixMilli()
		}
		issueRows = append(issueRows, r)
	}

	// Build the Grafana data.Frame.
	frame := data.NewFrame(refID)
	fTime := data.NewField("Time", nil, make([]time.Time, 0, len(issueRows)))
	fID := data.NewField("Issue ID", nil, make([]string, 0, len(issueRows)))
	fTitle := data.NewField("Title", nil, make([]string, 0, len(issueRows)))
	fSeverity := data.NewField("Priority", nil, make([]string, 0, len(issueRows)))
	fStatus := data.NewField("Status", nil, make([]string, 0, len(issueRows)))
	fCategory := data.NewField("Category", nil, make([]string, 0, len(issueRows)))
	fDevice := data.NewField("Device ID", nil, make([]string, 0, len(issueRows)))
	fMAC := data.NewField("MAC", nil, make([]string, 0, len(issueRows)))
	fSite := data.NewField("Site Name", nil, make([]string, 0, len(issueRows)))
	fParentSite := data.NewField("Parent Site", nil, make([]string, 0, len(issueRows)))
	fRule := data.NewField("Rule", nil, make([]string, 0, len(issueRows)))
	fDetails := data.NewField("Details", nil, make([]string, 0, len(issueRows)))

	for _, r := range issueRows {
		fTime.Append(time.UnixMilli(r.TimeMs))
		fID.Append(r.ID)
		fTitle.Append(r.Title)
		fSeverity.Append(r.Severity)
		fStatus.Append(r.Status)
		fCategory.Append(r.Category)
		fDevice.Append(r.Device)
		fMAC.Append(r.MAC)
		fSite.Append(r.Site)
		fParentSite.Append(r.ParentSite)
		fRule.Append(r.Rule)
		fDetails.Append(r.Details)
	}

	frame.Fields = append(frame.Fields,
		fTime, fID, fTitle, fSeverity, fStatus, fCategory, fDevice, fMAC, fSite, fParentSite, fRule, fDetails,
	)

	if len(issueRows) == 0 {
		frame.SetMeta(&data.FrameMeta{
			Notices: []data.Notice{
				{
					Severity: data.NoticeSeverityInfo,
					Text:     "No issues found for the selected time range/filters",
				},
			},
		})
	}

	return frame
}

// metricAliases maps common metric query names to their possible alternative field names
// across different versions and endpoints of Cisco Catalyst Center.
var metricAliases = map[string][]string{
	"clientCount": {
		"numberOfClients",
		"totalNumberOfClients",
		"clientTotalCount",
		"totalClients",
	},
	"numberOfClients": {
		"clientCount",
		"totalNumberOfClients",
		"clientTotalCount",
		"totalClients",
	},
	"totalNumberOfConnectedWiredClients": {
		"numberOfWiredClients",
		"totalNumberOfWiredClients",
		"wiredClientCount",
		"connectedWiredClients",
		"wiredClients",
		"wiredGoodClients",
	},
	"numberOfWiredClients": {
		"totalNumberOfConnectedWiredClients",
		"totalNumberOfWiredClients",
		"wiredClientCount",
		"connectedWiredClients",
		"wiredClients",
		"wiredGoodClients",
	},
	"totalNumberOfActiveWirelessClients": {
		"numberOfWirelessClients",
		"totalNumberOfWirelessClients",
		"wirelessClientCount",
		"activeWirelessClients",
		"wirelessClients",
		"wirelessGoodClients",
	},
	"numberOfWirelessClients": {
		"totalNumberOfActiveWirelessClients",
		"totalNumberOfWirelessClients",
		"wirelessClientCount",
		"activeWirelessClients",
		"wirelessClients",
		"wirelessGoodClients",
	},
	"numberOfNetworkDevice": {
		"networkDeviceCount",
		"totalNumberOfNetworkDevices",
		"networkDeviceTotalCount",
	},
}

// toFloat converts numeric and string representations of numbers to float64.
func toFloat(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		if f, err := v.Float64(); err == nil {
			return f, true
		}
	case string:
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f, true
		}
	}
	return 0, false
}

// getMetricValue resolves a metric key from a site health item, supporting direct keys,
// known Catalyst Center field aliases, case-insensitivity, and client total summation.
func getMetricValue(item map[string]any, metricKey string) (any, bool) {
	// 1. Direct match
	if val, ok := item[metricKey]; ok && val != nil {
		return val, true
	}

	// 2. Check predefined aliases
	if aliases, ok := metricAliases[metricKey]; ok {
		for _, alias := range aliases {
			if val, exists := item[alias]; exists && val != nil {
				return val, true
			}
		}
	}

	// 3. Case-insensitive match on metricKey or its aliases
	allTargets := append([]string{metricKey}, metricAliases[metricKey]...)
	for itemKey, val := range item {
		if val == nil {
			continue
		}
		for _, target := range allTargets {
			if strings.EqualFold(itemKey, target) {
				return val, true
			}
		}
	}

	// 4. If total client count is requested and not found, attempt summing wired + wireless
	if metricKey == "clientCount" || metricKey == "numberOfClients" || metricKey == "totalClients" {
		wiredVal, hasWired := getMetricValue(item, "numberOfWiredClients")
		wirelessVal, hasWireless := getMetricValue(item, "numberOfWirelessClients")
		if hasWired || hasWireless {
			var total float64
			hasAny := false
			if hasWired {
				if f, ok := toFloat(wiredVal); ok {
					total += f
					hasAny = true
				}
			}
			if hasWireless {
				if f, ok := toFloat(wirelessVal); ok {
					total += f
					hasAny = true
				}
			}
			if hasAny {
				return total, true
			}
		}
	}

	return nil, false
}

// SiteHealthResponseToDataFrame transforms site health data from the API
// into a Grafana data.Frame. It creates fields based on the user's metric
// selection or available metrics in the data.
func SiteHealthResponseToDataFrame(
	allSiteHealthData []map[string]any,
	refID string,
	selectedMetrics []string,
) *data.Frame {
	// Handle empty data case
	if len(allSiteHealthData) == 0 {
		frame := data.NewFrame(refID)
		frame.SetMeta(&data.FrameMeta{
			Notices: []data.Notice{{Severity: data.NoticeSeverityInfo, Text: "No site health data found for the selected filters."}},
		})
		return frame
	}

	frame := data.NewFrame(refID)
	// Always include site name and ID as nullable strings
	frame.Fields = append(frame.Fields, data.NewField("siteName", nil, make([]*string, 0, len(allSiteHealthData))))
	frame.Fields = append(frame.Fields, data.NewField("siteId", nil, make([]*string, 0, len(allSiteHealthData))))

	// Determine which metrics to display and their order
	var metricsToAdd []string
	seenMetrics := map[string]struct{}{
		"siteName": {},
		"siteId":   {},
	}

	if len(selectedMetrics) > 0 {
		for _, m := range selectedMetrics {
			m = strings.TrimSpace(m)
			if m == "" {
				continue
			}
			if _, exists := seenMetrics[m]; !exists {
				seenMetrics[m] = struct{}{}
				metricsToAdd = append(metricsToAdd, m)
			}
		}
	} else {
		// Collect all unique keys from all items (not just firstItem)
		for _, item := range allSiteHealthData {
			for k := range item {
				if _, exists := seenMetrics[k]; !exists {
					seenMetrics[k] = struct{}{}
					metricsToAdd = append(metricsToAdd, k)
				}
			}
		}
		sort.Strings(metricsToAdd)
	}

	// Helper to determine field type by inspecting all data items
	inferMetricType := func(key string) data.FieldType {
		for _, item := range allSiteHealthData {
			val, exists := getMetricValue(item, key)
			if !exists || val == nil {
				continue
			}
			switch val.(type) {
			case float64, float32, int, int32, int64, json.Number:
				return data.FieldTypeNullableFloat64
			case bool:
				return data.FieldTypeNullableBool
			case string:
				return data.FieldTypeNullableString
			}
		}
		// Default to numeric for empty/unseen metrics as site health metrics are typically numbers
		return data.FieldTypeNullableFloat64
	}

	for _, metric := range metricsToAdd {
		fieldType := inferMetricType(metric)
		switch fieldType {
		case data.FieldTypeNullableBool:
			frame.Fields = append(frame.Fields, data.NewField(metric, nil, make([]*bool, 0, len(allSiteHealthData))))
		case data.FieldTypeNullableString:
			frame.Fields = append(frame.Fields, data.NewField(metric, nil, make([]*string, 0, len(allSiteHealthData))))
		default:
			frame.Fields = append(frame.Fields, data.NewField(metric, nil, make([]*float64, 0, len(allSiteHealthData))))
		}
	}

	// Populate the frame rows
	for _, item := range allSiteHealthData {
		for _, field := range frame.Fields {
			value, exists := getMetricValue(item, field.Name)
			if !exists || value == nil {
				field.Append(nil)
				continue
			}

			switch field.Type() {
			case data.FieldTypeNullableString:
				strVal := fmt.Sprintf("%v", value)
				field.Append(&strVal)
			case data.FieldTypeNullableFloat64:
				if f, ok := toFloat(value); ok {
					field.Append(&f)
				} else {
					field.Append(nil)
				}
			case data.FieldTypeNullableBool:
				var b bool
				switch v := value.(type) {
				case bool:
					b = v
				case string:
					b = strings.EqualFold(v, "true")
				default:
					field.Append(nil)
					continue
				}
				field.Append(&b)
			default:
				strVal := fmt.Sprintf("%v", value)
				field.Append(&strVal)
			}
		}
	}

	return frame
}
