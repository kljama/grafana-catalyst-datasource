package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	log "github.com/grafana/grafana-plugin-sdk-go/backend/log"
)

// Client handles all direct API interactions with the Catalyst Center.
// It encapsulates pagination, token refresh, retry logic, and HTTP communication.
type Client struct {
	httpClient *http.Client
	tm         *tokenManager
	inst       *dsInstance
}

// NewClient creates a new API client for the given datasource instance.
func NewClient(httpClient *http.Client, tm *tokenManager, inst *dsInstance) *Client {
	return &Client{
		httpClient: httpClient,
		tm:         tm,
		inst:       inst,
	}
}

// issuesPageSize is the largest `limit` the assuranceIssues API accepts. Every
// published spec version, including Catalyst Center 3.1.6, allows 1..25.
const issuesPageSize = 25

// FetchAllIssues retrieves all assurance issues from the API, handling pagination
// and respecting the specified hard limit.
func (c *Client) FetchAllIssues(ctx context.Context, issuesURL string, qm QueryModel, hardLimit int64) ([]map[string]any, error) {
	allIssues := make([]map[string]any, 0, min(int(hardLimit), 1000))

	// If no sites are selected, run one global query.
	// If sites are selected, we will iterate and run a query for each site.
	// This is because the API doesn't support OR logic for multiple site IDs with other filters.
	siteIDsToQuery := qm.SiteID
	if len(siteIDsToQuery) == 0 {
		siteIDsToQuery = []string{""} // One empty ID to trigger a single global query
		log.DefaultLogger.Info("No sites selected, performing a global query")
	} else {
		log.DefaultLogger.Info("Sites selected, querying each site individually", "siteIDs", siteIDsToQuery)
	}

siteQueryLoop:
	for _, siteID := range siteIDsToQuery {
		queryModelForSite := qm
		if siteID != "" {
			queryModelForSite.SiteID = []string{siteID} // Query for one site at a time
		} else {
			queryModelForSite.SiteID = []string{} // Global query
		}

		offsetForSite := 0
		for {
			if int64(len(allIssues)) >= hardLimit {
				log.DefaultLogger.Info("Hard limit reached, breaking all loops", "totalIssues", len(allIssues), "hardLimit", hardLimit)
				break siteQueryLoop
			}

			limitForThisPage := issuesPageSize
			remaining := int(hardLimit - int64(len(allIssues)))
			if remaining < limitForThisPage {
				limitForThisPage = remaining
			}

			params := buildAssuranceParamsFromQuery(
				queryModelForSite,
				qm.TimeRange.From.UnixMilli(),
				qm.TimeRange.To.UnixMilli(),
				limitForThisPage,
				offsetForSite+1,
			)

			// Fetch one page of issues
			issues, err := c.fetchIssuesPage(ctx, issuesURL, params)
			if err != nil {
				return allIssues, err
			}

			if len(issues) == 0 {
				log.DefaultLogger.Info("Received 0 issues, ending pagination for this site.", "siteId", siteID)
				break
			}

			allIssues = append(allIssues, issues...)
			log.DefaultLogger.Info("Total issues collected so far", "count", len(allIssues))

			// A short page is the last one. Compare against what was requested,
			// not the page size: the last page before the hard limit asks for less.
			if len(issues) < limitForThisPage {
				log.DefaultLogger.Info("Received fewer issues than requested, ending pagination for this site.", "siteId", siteID, "count", len(issues), "requested", limitForThisPage)
				break
			}
			offsetForSite += len(issues)
		}
	}

	log.DefaultLogger.Info("Data fetching complete", "totalIssues", len(allIssues))
	return allIssues, nil
}

// fetchIssuesPage retrieves a single page of issues from the API.
// It handles token acquisition, retry on auth failures, and error handling.
func (c *Client) fetchIssuesPage(ctx context.Context, issuesURL string, params url.Values) ([]map[string]any, error) {
	// Get a valid token
	token, err := c.tm.getToken(ctx, c.inst.UID, c.inst.Settings, c.httpClient)
	if err != nil {
		log.DefaultLogger.Error("Failed to get token", "err", err)
		return nil, fmt.Errorf("token: %w", err)
	}

	reqURL := issuesURL + "?" + params.Encode()
	httpReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	httpReq.Header.Set("X-Auth-Token", token)

	log.DefaultLogger.Info("Requesting assurance issues", "url", reqURL)
	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		log.DefaultLogger.Error("HTTP request failed", "err", err)
		return nil, fmt.Errorf("issues request failed: %w", err)
	}
	body, _ := io.ReadAll(httpResp.Body)
	httpResp.Body.Close()
	log.DefaultLogger.Info("Received response", "status", httpResp.Status)
	log.DefaultLogger.Debug("Response body", "body", string(body))

	// Handle token refresh on 401/403
	if httpResp.StatusCode == http.StatusUnauthorized || httpResp.StatusCode == http.StatusForbidden {
		log.DefaultLogger.Warn("Unauthorized; refreshing token and retrying")
		c.tm.set(c.inst.UID, "") // Force refresh
		token, err = c.tm.getToken(ctx, c.inst.UID, c.inst.Settings, c.httpClient)
		if err != nil {
			log.DefaultLogger.Error("Failed to get token on retry", "err", err)
			return nil, fmt.Errorf("token refresh: %w", err)
		}
		httpReq, _ = http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		httpReq.Header.Set("X-Auth-Token", token)
		httpResp, err = c.httpClient.Do(httpReq)
		if err != nil {
			log.DefaultLogger.Error("HTTP request failed on retry", "err", err)
			return nil, fmt.Errorf("issues request retry failed: %w", err)
		}
		body, _ = io.ReadAll(httpResp.Body)
		httpResp.Body.Close()
		log.DefaultLogger.Info("Received response on retry", "status", httpResp.Status)
		log.DefaultLogger.Debug("Response body on retry", "body", string(body))
	}

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		log.DefaultLogger.Error("API returned non-2xx status", "status", httpResp.Status, "body", string(body))
		return nil, fmt.Errorf("issues endpoint returned %s: %s", httpResp.Status, string(body))
	}

	// Parse the response
	var env IssuesEnvelope
	var arr []map[string]any
	if err := json.Unmarshal(body, &env); err == nil && len(env.Response) > 0 {
		arr = env.Response
	} else {
		// If the envelope unmarshal fails, try unmarshalling directly into a slice
		_ = json.Unmarshal(body, &arr)
	}
	log.DefaultLogger.Info("Parsed issues from response", "count", len(arr))

	return arr, nil
}

// FetchAllSiteHealth retrieves all site health data from the API, handling pagination.
func (c *Client) FetchAllSiteHealth(ctx context.Context, siteHealthURL *url.URL, qm QueryModel) ([]map[string]any, error) {
	var allSiteHealthData []map[string]any
	pageSize := 50 // As per API doc, max is 50
	offset := 0

	for {
		params := buildSiteHealthParamsFromQuery(qm, qm.TimeRange.To.UnixMilli(), pageSize, offset+1)
		reqURL := siteHealthURL.String() + "?" + params.Encode()

		token, err := c.tm.getToken(ctx, c.inst.UID, c.inst.Settings, c.httpClient)
		if err != nil {
			return nil, fmt.Errorf("token: %w", err)
		}

		httpReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		httpReq.Header.Set("X-Auth-Token", token)

		httpResp, err := c.httpClient.Do(httpReq)
		if err != nil {
			return nil, fmt.Errorf("site health request failed: %w", err)
		}

		body, _ := io.ReadAll(httpResp.Body)
		httpResp.Body.Close()

		if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
			return nil, fmt.Errorf("site health endpoint returned %s: %s", httpResp.Status, string(body))
		}

		var envelope struct {
			Response []map[string]any `json:"response"`
		}
		if err := json.Unmarshal(body, &envelope); err != nil {
			return nil, fmt.Errorf("failed to unmarshal site health response: %w", err)
		}

		if len(envelope.Response) == 0 {
			break // No more data
		}

		allSiteHealthData = append(allSiteHealthData, envelope.Response...)

		if len(envelope.Response) < pageSize {
			break // Last page
		}
		offset += pageSize
	}

	return allSiteHealthData, nil
}
