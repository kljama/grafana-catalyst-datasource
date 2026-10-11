// Package backend contains the core logic for the Catalyst datasource.
// This file, params.go, is responsible for converting the frontend query model
// into the URL query parameters expected by the Catalyst Center API. It handles
// normalization, validation, and formatting of filter values.

package backend

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// shouldIncludeSiteHealthTimestamp checks if the timestamp should be included in
// the site-health request. The Catalyst Center API treats timestamp as optional
// for querying historical snapshots. If timestamp is non-positive or within recent time
// (e.g., within 30 minutes of now or in the future), we omit it so that Catalyst Center
// returns the latest available real-time snapshot instead of an empty list.
func shouldIncludeSiteHealthTimestamp(timestamp int64) bool {
	if timestamp <= 0 {
		return false
	}
	t := time.UnixMilli(timestamp)
	if time.Since(t) < 30*time.Minute {
		return false
	}
	return true
}

// buildSiteHealthParamsFromQuery converts a QueryModel into url.Values for the site-health endpoint.
func buildSiteHealthParamsFromQuery(q QueryModel, timestamp int64, limit int, offset int) url.Values {
	p := url.Values{}
	if shouldIncludeSiteHealthTimestamp(timestamp) {
		p.Set("timestamp", fmt.Sprintf("%d", timestamp))
	}

	if q.SiteType != "" {
		p.Set("siteType", q.SiteType)
	}

	// The site-health endpoint does not support filtering by siteId or siteName directly in the query params.
	// This filtering is performed in the backend after fetching the data.

	// Clamp limit to sane values: default 50, min 1, max 50 (API max)
	clampedLimit := clampLimit(limit, 50, 1, 50)
	p.Set("limit", strconv.Itoa(clampedLimit))
	
	// Ensure offset is at least 1 (API uses 1-based indexing)
	if offset < 1 {
		offset = 1
	}
	p.Set("offset", strconv.Itoa(offset))

	return p
}

// Allowed value sets for validation and normalization.
var (
	// allowedPriority defines the valid priority values for the API.
	// Priority values are case-insensitive (p1, P1) but normalized to uppercase for API.
	allowedPriority = map[string]struct{}{"P1": {}, "P2": {}, "P3": {}, "P4": {}}
	// allowedIssueStatus defines the valid status values for the API.
	// Status values are case-insensitive (ACTIVE, active) but normalized to lowercase for API.
	allowedIssueStatus = map[string]struct{}{"active": {}, "resolved": {}, "ignored": {}}
)

// normalizePriority returns a valid priority string (P1-P4) in uppercase if the input
// matches a known value. Accepts case-insensitive input (e.g., P1, p1, P2).
// Supports legacy field alias: if priority is empty, falls back to severity.
func normalizePriority(priority string, severity string) (string, bool) {
	// Use priority if provided, otherwise fall back to severity (legacy field)
	value := priority
	if value == "" {
		value = severity
	}
	
	p := strings.ToUpper(strings.TrimSpace(value))
	if _, ok := allowedPriority[p]; ok {
		return p, true
	}
	return "", false
}

// normalizeIssueStatus returns a valid status string in lowercase if the input matches a known
// value. Accepts case-insensitive input (e.g., ACTIVE, active, Active).
// Supports legacy field alias: if issueStatus is empty, falls back to status.
func normalizeIssueStatus(issueStatus string, status string) (string, bool) {
	// Use issueStatus if provided, otherwise fall back to status (legacy field)
	value := issueStatus
	if value == "" {
		value = status
	}
	
	s := strings.ToLower(strings.TrimSpace(value))
	if _, ok := allowedIssueStatus[s]; ok {
		return s, true
	}
	return "", false
}

// clampLimit enforces sane bounds on the limit parameter, preventing excessively
// large or invalid values from being sent to the API.
func clampLimit(n, def, min, max int) int {
	if n <= 0 {
		return def
	}
	if n < min {
		return min
	}
	if n > max {
		return max
	}
	return n
}

// buildAssuranceParamsFromQuery converts a QueryModel from the frontend into a
// url.Values map for querying the assurance issues endpoint.
func buildAssuranceParamsFromQuery(q QueryModel, startTime, endTime int64, pageSize, offset int) url.Values {
	p := url.Values{}
	
	// Only set time parameters if they are non-zero
	if startTime > 0 {
		p.Set("startTime", strconv.FormatInt(startTime, 10))
	}
	if endTime > 0 {
		p.Set("endTime", strconv.FormatInt(endTime, 10))
	}

       if len(q.Priority) > 0 {
	       for _, prio := range q.Priority {
		       normPrio, ok := normalizePriority(prio, "")
		       if ok {
			       p.Add("priority", normPrio)
		       }
	       }
       }
       if len(q.Status) > 0 {
	       for _, stat := range q.Status {
		       normStatus, ok := normalizeIssueStatus(stat, "")
		       if ok {
			       p.Add("status", normStatus)
		       }
	       }
       }
	if q.NetworkDeviceID != "" {
		p.Set("networkDeviceId", q.NetworkDeviceID)
	}
	if q.NetworkDeviceIP != "" {
		p.Set("networkDeviceIpAddress", q.NetworkDeviceIP)
	}
	if q.MACAddress != "" {
		p.Set("macAddress", q.MACAddress)
	}
       // Send all site IDs as repeated keys, per API spec.
       if len(q.SiteID) > 0 {
	       for _, id := range q.SiteID {
		       if id != "" {
			       p.Add("siteId", id)
		       }
	       }
       }
	if q.IssueName != "" {
		// The 'IssueName' from the UI corresponds to the 'name' of the issue in the API
		p.Set("name", q.IssueName)
	}
	// Unset filters are omitted; an explicit "false" (saved by v1.0.x) is sent.
	if v, ok := q.AIDriven.Param(); ok {
		p.Set("aiDriven", v)
	}
	if v, ok := q.IsGlobal.Param(); ok {
		p.Set("isGlobal", v)
	}

	// The API accepts a limit of 1..25 (issuesPageSize); anything larger is
	// rejected or capped by Catalyst Center.
	clampedLimit := clampLimit(pageSize, issuesPageSize, 1, issuesPageSize)
	p.Set("limit", strconv.Itoa(clampedLimit))
	
	// Ensure offset is at least 1 (API uses 1-based indexing)
	if offset < 1 {
		offset = 1
	}
	p.Set("offset", strconv.Itoa(offset))

	return p
}
