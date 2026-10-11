// Package backend contains the core logic for the Catalyst datasource.
package backend

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	log "github.com/grafana/grafana-plugin-sdk-go/backend/log"
	"github.com/grafana/grafana-plugin-sdk-go/data"
)

// Datasource is the main backend implementation for the Catalyst datasource.
// It holds shared resources like the token manager and site translators.
type Datasource struct {
	tm *tokenManager
	// A map of site translators, one for each datasource instance UID.
	translators   map[string]*SiteTranslator
	translatorMux sync.Mutex
}

// dsInstance represents a single configured instance of the datasource.
// It holds the parsed settings and the unique instance UID.
type dsInstance struct {
	Settings *InstanceSettings
	UID      string
}

// NewDatasource creates a new datasource instance.
func NewDatasource() *Datasource {
	return &Datasource{
		tm:          newTokenManager(),
		translators: make(map[string]*SiteTranslator),
	}
}

// getSiteTranslator returns a site translator for the given instance.
// If one doesn't exist, it creates and caches it.
func (d *Datasource) getSiteTranslator(inst *dsInstance, httpClient *http.Client) *SiteTranslator {
	d.translatorMux.Lock()
	defer d.translatorMux.Unlock()

	if t, ok := d.translators[inst.UID]; ok {
		return t
	}
	t := NewSiteTranslator(httpClient, inst, d.tm)
	d.translators[inst.UID] = t
	log.DefaultLogger.Info("Created new site translator for instance", "uid", inst.UID)
	return t
}

// httpClientFor creates an HTTP client that respects the InsecureSkipVerify setting.
func (d *Datasource) httpClientFor(s *InstanceSettings) *http.Client {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: s.InsecureSkipVerify}, //nolint:gosec
	}
	return &http.Client{Timeout: 30 * time.Second, Transport: tr}
}

// ---- helpers to read instance settings directly from PluginContext ----

// getInstanceFromPluginContext retrieves and parses the settings for the current
// datasource instance from the plugin context provided by Grafana.
func getInstanceFromPluginContext(pc backend.PluginContext) (*dsInstance, error) {
	ds := pc.DataSourceInstanceSettings
	if ds == nil {
		return nil, fmt.Errorf("no datasource instance settings in plugin context")
	}
	cfg, err := ParseInstanceSettings(ds.JSONData, ds.DecryptedSecureJSONData)
	if err != nil {
		return nil, err
	}
	return &dsInstance{
		Settings: cfg,
		UID:      ds.UID,
	}, nil
}

// ---- QueryData ----

// QueryData is the primary method for handling data queries from Grafana panels.
// It executes the following steps:
//  1. Parses the query from the frontend.
//  2. Uses the Client to fetch all relevant issues from the API (Client handles pagination and retries).
//  3. Enriches the data by resolving site IDs to names using the SiteTranslator.
//  4. Transforms the API response into a Grafana data.Frame.
func (d *Datasource) QueryData(ctx context.Context, req *backend.QueryDataRequest) (*backend.QueryDataResponse, error) {
	resp := backend.NewQueryDataResponse()

	inst, err := getInstanceFromPluginContext(req.PluginContext)
	if err != nil {
		return nil, err
	}
	httpClient := d.httpClientFor(inst.Settings)
	siteTranslator := d.getSiteTranslator(inst, httpClient)

	// Ensure the site cache is warm before processing queries. This prevents
	// repeated cache checks inside the loop.
	if err := siteTranslator.EnsureCache(ctx); err != nil {
		// Log the error but don't fail the whole query; it might still succeed
		// if the cache is partially available or if no site enrichment is needed.
		log.DefaultLogger.Error("Failed to ensure site cache", "err", err)
	}

	for _, q := range req.Queries {
		dr := backend.DataResponse{}

		// 1. Unmarshal the query model sent from the frontend.
		var qm QueryModel
		if err := json.Unmarshal(q.JSON, &qm); err != nil {
			dr.Error = fmt.Errorf("invalid query model: %w", err)
			resp.Responses[q.RefID] = dr
			log.DefaultLogger.Error("Failed to unmarshal query model", "err", err, "json", string(q.JSON))
			continue
		}
		qm.TimeRange = q.TimeRange
		log.DefaultLogger.Info("Executing query", "refId", q.RefID, "queryType", qm.QueryType, "limit", qm.Limit)
		log.DefaultLogger.Debug("Full query model", "query", fmt.Sprintf("%+v", qm))

		// If site IDs are not provided, but site names are, resolve them.
		// This handles cases where the user might manually enter a site name.
		if len(qm.SiteID) == 0 && len(qm.SiteName) > 0 {
			log.DefaultLogger.Info("Resolving site names to IDs", "names", qm.SiteName)
			var resolvedIDs []string
			for _, name := range qm.SiteName {
				siteID, err := siteTranslator.GetSiteID(ctx, name)
				if err != nil {
					// Log the error but continue, so one bad name doesn't fail the whole query.
					log.DefaultLogger.Warn("failed to resolve site name", "name", name, "err", err)
					continue
				}
				resolvedIDs = append(resolvedIDs, siteID)
			}
			qm.SiteID = resolvedIDs
			log.DefaultLogger.Info("Resolved site IDs", "ids", resolvedIDs)
		}

		if strings.TrimSpace(qm.QueryType) == "siteHealth" {
			log.DefaultLogger.Info("Routing to siteHealth handler")
			frame, err := d.querySiteHealth(ctx, inst, qm, q.RefID, httpClient)
			if err != nil {
				dr.Error = err
			} else {
				dr.Frames = append(dr.Frames, frame)
			}
			resp.Responses[q.RefID] = dr
			continue
		}

		log.DefaultLogger.Info("Routing to assuranceIssues handler")
		issuesURL, err := IssuesURL(inst.Settings.BaseURL)
		if err != nil {
			dr.Error = err
			resp.Responses[q.RefID] = dr
			continue
		}

		// 2. Determine the hard limit for this query.
		var hardLimit int64 = 1000 // A higher default hard limit
		if qm.Limit != nil && *qm.Limit > 0 {
			hardLimit = *qm.Limit
		}
		log.DefaultLogger.Info("Pagination configured", "hardLimit", hardLimit)

		// Log warning if many sites are selected (may impact performance)
		if len(qm.SiteID) > 5 {
			log.DefaultLogger.Warn("Large number of sites selected may increase query time", "count", len(qm.SiteID))
		}

		// 3. Use the Client to fetch all issues (Client handles pagination and retries).
		// Note: qm.Enrich field is reserved for future use (e.g., full device details).
		// Site name enrichment always runs as it is efficient (see lines 228-241 in transformer.go).
		client := NewClient(httpClient, d.tm, inst)
		allIssues, err := client.FetchAllIssues(ctx, issuesURL, qm, hardLimit)
		if err != nil {
			dr.Error = err
			resp.Responses[q.RefID] = dr
			continue
		}

		// 4. Transform API response into Grafana data.Frame
		frame := IssueResponseToDataFrame(ctx, allIssues, q.RefID, q.TimeRange, hardLimit, siteTranslator)
		dr.Frames = append(dr.Frames, frame)
		resp.Responses[q.RefID] = dr
	}
	return resp, nil
}

// querySiteHealth handles the specific logic for the 'siteHealth' query type.
func (d *Datasource) querySiteHealth(ctx context.Context, inst *dsInstance, qm QueryModel, refID string, httpClient *http.Client) (*data.Frame, error) {
	siteHealthURL, err := url.Parse(inst.Settings.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid base URL: %w", err)
	}
	prefix := dnacPrefix(siteHealthURL.Path)
	siteHealthURL.Path = prefix + "/dna/intent/api/v1/site-health"

	client := NewClient(httpClient, d.tm, inst)
	var allSiteHealthData []map[string]any

	siteType := strings.TrimSpace(qm.SiteType)
	if siteType != "" {
		qmSite := qm
		qmSite.SiteType = siteType
		data, err := client.FetchAllSiteHealth(ctx, siteHealthURL, qmSite)
		if err != nil {
			return nil, err
		}
		allSiteHealthData = data
	} else {
		// When siteType is not specified, Catalyst Center API defaults to AREA only.
		// To ensure both areas and buildings (like child buildings under a city/area) are retrieved,
		// we fetch both AREA and BUILDING.
		qmArea := qm
		qmArea.SiteType = "AREA"
		areas, err := client.FetchAllSiteHealth(ctx, siteHealthURL, qmArea)
		if err != nil {
			log.DefaultLogger.Warn("Failed to fetch area site health", "err", err)
		}

		qmBuilding := qm
		qmBuilding.SiteType = "BUILDING"
		buildings, err := client.FetchAllSiteHealth(ctx, siteHealthURL, qmBuilding)
		if err != nil {
			log.DefaultLogger.Warn("Failed to fetch building site health", "err", err)
		}

		seen := make(map[string]struct{})
		for _, item := range append(areas, buildings...) {
			id, _ := item["siteId"].(string)
			id = strings.TrimSpace(id)
			if id != "" {
				lowerID := strings.ToLower(id)
				if _, exists := seen[lowerID]; exists {
					continue
				}
				seen[lowerID] = struct{}{}
			}
			allSiteHealthData = append(allSiteHealthData, item)
		}
	}

	// Filter data based on user selections: siteId, siteName, parentSiteId, parentSiteName
	allSiteHealthData = filterSiteHealthData(allSiteHealthData, qm)

	if len(allSiteHealthData) > 0 {
		var sampleKeys []string
		for k := range allSiteHealthData[0] {
			sampleKeys = append(sampleKeys, k)
		}
		sort.Strings(sampleKeys)
		log.DefaultLogger.Debug("Site health sample record keys", "count", len(allSiteHealthData), "keys", sampleKeys, "sampleSite", allSiteHealthData[0]["siteName"])
	}

	// Transform API response into Grafana data.Frame
	frame := SiteHealthResponseToDataFrame(allSiteHealthData, refID, qm.Metrics)
	return frame, nil
}

// hasNonEmptyStrings checks if a slice contains at least one non-blank string.
func hasNonEmptyStrings(slice []string) bool {
	for _, s := range slice {
		if strings.TrimSpace(s) != "" {
			return true
		}
	}
	return false
}

// filterSiteHealthData filters the site health results based on siteId, siteName,
// parentSiteId, and parentSiteName filters from the query model.
func filterSiteHealthData(allSiteHealthData []map[string]any, qm QueryModel) []map[string]any {
	hasSiteFilter := hasNonEmptyStrings(qm.SiteID) || hasNonEmptyStrings(qm.SiteName)
	hasParentFilter := strings.TrimSpace(qm.ParentSiteID) != "" || strings.TrimSpace(qm.ParentSiteName) != ""

	if !hasSiteFilter && !hasParentFilter {
		return allSiteHealthData
	}

	var filteredData []map[string]any
	for _, siteData := range allSiteHealthData {
		if hasSiteFilter && !siteMatchesQuery(siteData, qm.SiteID, qm.SiteName) {
			continue
		}
		if hasParentFilter {
			parentIDVal, _ := siteData["parentSiteId"].(string)
			parentNameVal, _ := siteData["parentSiteName"].(string)
			matchedParent := false
			if qm.ParentSiteID != "" && strings.EqualFold(strings.TrimSpace(parentIDVal), strings.TrimSpace(qm.ParentSiteID)) {
				matchedParent = true
			}
			if qm.ParentSiteName != "" && matchSiteNameOrHierarchy(parentNameVal, qm.ParentSiteName) {
				matchedParent = true
			}
			if !matchedParent {
				continue
			}
		}
		filteredData = append(filteredData, siteData)
	}
	return filteredData
}

// siteMatchesQuery checks if a site health record matches the requested site IDs or site names.
// It supports case-insensitive comparison, hierarchy paths, and parent site relationships.
func siteMatchesQuery(siteData map[string]any, siteIDs []string, siteNames []string) bool {
	idVal, _ := siteData["siteId"].(string)
	nameVal, _ := siteData["siteName"].(string)
	parentIDVal, _ := siteData["parentSiteId"].(string)
	parentNameVal, _ := siteData["parentSiteName"].(string)

	trimmedID := strings.TrimSpace(idVal)
	trimmedParentID := strings.TrimSpace(parentIDVal)

	for _, targetID := range siteIDs {
		targetID = strings.TrimSpace(targetID)
		if targetID == "" {
			continue
		}
		if strings.EqualFold(trimmedID, targetID) || strings.EqualFold(trimmedParentID, targetID) {
			return true
		}
	}

	for _, targetName := range siteNames {
		targetName = strings.TrimSpace(targetName)
		if targetName == "" {
			continue
		}
		if matchSiteNameOrHierarchy(nameVal, targetName) || matchSiteNameOrHierarchy(parentNameVal, targetName) {
			return true
		}
	}
	return false
}

// matchSiteNameOrHierarchy checks if a site name matches target directly or as any segment in a hierarchy path.
// e.g., "Global/Österreich/Wien" matches "Wien", "Österreich", or "Global".
// It also handles leading/trailing whitespace (e.g. " All Sites") and subpaths (e.g. "Österreich/Wien").
func matchSiteNameOrHierarchy(fullName, target string) bool {
	fullName = strings.TrimSpace(fullName)
	target = strings.TrimSpace(target)
	if fullName == "" || target == "" {
		return false
	}
	if strings.EqualFold(fullName, target) {
		return true
	}

	cleanFullParts := strings.Split(fullName, "/")
	cleanTargetParts := strings.Split(target, "/")

	for i := range cleanFullParts {
		cleanFullParts[i] = strings.TrimSpace(cleanFullParts[i])
	}
	for i := range cleanTargetParts {
		cleanTargetParts[i] = strings.TrimSpace(cleanTargetParts[i])
	}

	// 1. Match against any single segment
	for _, seg := range cleanFullParts {
		if strings.EqualFold(seg, target) {
			return true
		}
	}

	// 2. Normalized path subpath match (e.g. "Österreich/Wien" in "Global/Österreich/Wien")
	normFull := "/" + strings.ToLower(strings.Join(cleanFullParts, "/")) + "/"
	normTarget := "/" + strings.ToLower(strings.Join(cleanTargetParts, "/")) + "/"
	return strings.Contains(normFull, normTarget)
}

// firstNonEmpty returns the first non-empty string from a list of arguments.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// firstNonZero returns the first non-zero int64 from a list of arguments.
func firstNonZero(vals ...int64) int64 {
	for _, v := range vals {
		if v != 0 {
			return v
		}
	}
	return 0
}

// ---- CheckHealth ----

// CheckHealth is called by Grafana to verify that the datasource is configured correctly.
func (d *Datasource) CheckHealth(ctx context.Context, req *backend.CheckHealthRequest) (*backend.CheckHealthResult, error) {
	inst, err := getInstanceFromPluginContext(req.PluginContext)
	if err != nil {
		return &backend.CheckHealthResult{
			Status:  backend.HealthStatusError,
			Message: "instance error: " + err.Error(),
		}, nil
	}
	httpClient := d.httpClientFor(inst.Settings)

	// 1. Verify that we can obtain an authentication token.
	if _, err := d.tm.getToken(ctx, inst.UID, inst.Settings, httpClient); err != nil {
		return &backend.CheckHealthResult{
			Status:  backend.HealthStatusError,
			Message: "token error: " + err.Error(),
		}, nil
	}

	// 2. Perform a lightweight API call to the site translator to ensure connectivity.
	siteTranslator := d.getSiteTranslator(inst, httpClient)
	if _, err := siteTranslator.GetAllSites(ctx); err != nil {
		return &backend.CheckHealthResult{
			Status:  backend.HealthStatusError,
			Message: "Health check failed: could not fetch sites. " + err.Error(),
		}, nil
	}

	return &backend.CheckHealthResult{
		Status:  backend.HealthStatusOk,
		Message: "Successfully connected to Catalyst Center and fetched sites.",
	}, nil
}

// ---- CallResource ----

// CallResource handles custom API requests from the frontend.
func (d *Datasource) CallResource(ctx context.Context, req *backend.CallResourceRequest, sender backend.CallResourceResponseSender) error {
	switch req.Path {
	case "sites":
		return d.handleSitesRequest(ctx, req, sender)
	case "issues":
		inst, err := getInstanceFromPluginContext(req.PluginContext)
		if err != nil {
			return sender.Send(&backend.CallResourceResponse{
				Status: http.StatusInternalServerError,
				Body:   []byte("failed to get instance settings: " + err.Error()),
			})
		}
		httpClient := d.httpClientFor(inst.Settings)
		return d.resourceIssues(ctx, inst, req, sender, httpClient)
	default:
		return sender.Send(&backend.CallResourceResponse{
			Status: http.StatusNotFound,
			Body:   []byte("not found"),
		})
	}
}

// handleSitesRequest fetches all sites using the translator and returns them as JSON.
func (d *Datasource) handleSitesRequest(ctx context.Context, req *backend.CallResourceRequest, sender backend.CallResourceResponseSender) error {
	inst, err := getInstanceFromPluginContext(req.PluginContext)
	if err != nil {
		return sender.Send(&backend.CallResourceResponse{
			Status: http.StatusInternalServerError,
			Body:   []byte("failed to get instance settings: " + err.Error()),
		})
	}
	httpClient := d.httpClientFor(inst.Settings)
	siteTranslator := d.getSiteTranslator(inst, httpClient)

	sites, err := siteTranslator.GetAllSites(ctx)
	if err != nil {
		return sender.Send(&backend.CallResourceResponse{
			Status: http.StatusInternalServerError,
			Body:   []byte("failed to fetch sites: " + err.Error()),
		})
	}

	body, err := json.Marshal(sites)
	if err != nil {
		return sender.Send(&backend.CallResourceResponse{
			Status: http.StatusInternalServerError,
			Body:   []byte("failed to marshal sites to JSON: " + err.Error()),
		})
	}

	return sender.Send(&backend.CallResourceResponse{
		Status:  http.StatusOK,
		Body:    body,
		Headers: map[string][]string{"Content-Type": {"application/json"}},
	})
}

// clampPassthroughPaging keeps the paging parameters of a forwarded issues query
// within the API contract: limit 1..25 (issuesPageSize) and offset from 1. Other
// parameters are passed through unchanged.
func clampPassthroughPaging(rawQuery string) string {
	vals, err := url.ParseQuery(rawQuery)
	if err != nil {
		return rawQuery
	}
	if v, ok := vals["limit"]; ok && len(v) > 0 {
		n, _ := strconv.Atoi(v[0])
		vals.Set("limit", strconv.Itoa(clampLimit(n, issuesPageSize, 1, issuesPageSize)))
	}
	if v, ok := vals["offset"]; ok && len(v) > 0 {
		if n, err := strconv.Atoi(v[0]); err != nil || n < 1 {
			vals.Set("offset", "1")
		}
	}
	return vals.Encode()
}

// resourceIssues handles requests to the /issues resource path. It forwards the
// query parameters from the frontend to the Catalyst Center issues API.
func (d *Datasource) resourceIssues(ctx context.Context, inst *dsInstance, req *backend.CallResourceRequest, sender backend.CallResourceResponseSender, httpClient *http.Client) error {
	issuesURL, err := IssuesURL(inst.Settings.BaseURL)
	if err != nil {
		return sender.Send(&backend.CallResourceResponse{
			Status: http.StatusBadRequest,
			Body:   []byte("bad baseUrl: " + err.Error()),
		})
	}

	var rawQuery string
	if req.URL != "" {
		if u, err := url.Parse(req.URL); err == nil {
			rawQuery = u.RawQuery
		}
	}

	q := ""
	if rawQuery != "" {
		q = "?" + clampPassthroughPaging(rawQuery)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, issuesURL+q, nil)
	if err != nil {
		return sender.Send(&backend.CallResourceResponse{
			Status: http.StatusInternalServerError,
			Body:   []byte("failed to create request: " + err.Error()),
		})
	}

	tok, err := d.tm.getToken(ctx, inst.UID, inst.Settings, httpClient)
	if err != nil {
		return sender.Send(&backend.CallResourceResponse{
			Status: http.StatusUnauthorized,
			Body:   []byte("token: " + err.Error()),
		})
	}
	httpReq.Header.Set("X-Auth-Token", tok)

	httpResp, err := httpClient.Do(httpReq)
	if err != nil {
		return sender.Send(&backend.CallResourceResponse{
			Status: http.StatusBadGateway,
			Body:   []byte("request failed: " + err.Error()),
		})
	}
	defer httpResp.Body.Close()

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return sender.Send(&backend.CallResourceResponse{
			Status: http.StatusInternalServerError,
			Body:   []byte("failed to read response: " + err.Error()),
		})
	}

	return sender.Send(&backend.CallResourceResponse{
		Status:  httpResp.StatusCode,
		Body:    body,
		Headers: map[string][]string{"Content-Type": {"application/json"}},
	})
}

// min returns the minimum of two integers.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
