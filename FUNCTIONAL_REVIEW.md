# Functional review: grafana-catalyst-datasource

- **Commit reviewed:** `3a49f3d` (branch `main`), 2026-10-11
- **Scope:** functional correctness only: wrong or missing data, broken features, settings that are not applied, gaps between the UI and the backend, upgrade compatibility, packaging, dead code, and docs that contradict behavior.
- **Out of scope:** security issues (TLS skip-verify, secrets in logs and similar) are deliberately excluded.

## 1. Summary

**70 issues: 3 critical, 5 high, 22 medium, 33 low, 7 info.** The existing test suites pass (`go test`, `go vet`, `tsc`, 197 Jest tests), but none of them catch the problems below.

**Critical problems**

1. **C1 – page size too large.** The backend requests 50 issues per page, but every published Catalyst Center API spec caps `limit` at 25. Depending on how the server enforces this, issue panels fail with HTTP 400 or never show more than 25 issues per site.
2. **C2 and C3 – upgrading from v1.0.4/v1.0.5 breaks saved issue panels.**
   - **C2:** the frontend turns a missing `siteId` into `""`, and the backend rejects that.
   - **C3:** the backend no longer accepts the old field types and names.
   - **Result:** every saved issues panel shows an error, or silently loses its status filter. Opening such a panel in Explore crashes the query editor.

**Next most important problems**

- With several sites selected, sites after the first can be dropped completely (H1).
- Settings changes are ignored until Grafana restarts (H2).
- The HTTP proxy environment is ignored (H3).
- Template variables do not work (H4).
- Selecting an area or building probably returns no issues (H5).

**Fix strategy.** Fixes are grouped into 6 phases (section 6):

1. Build a test harness that fakes Catalyst Center.
2. Ship a small patch release for the 3 critical issues.
3. Fix issue-data correctness.
4. Refactor backend state into per-datasource instances with the SDK instance manager. This one change resolves about 12 issues.
5. Fix the site-health path and then the frontend editors and variables.
6. Clean up packaging, CI, docs and dead code.

## 2. How the issues were found and verified

- **Code reading** of all Go and TypeScript sources, build scripts, CI workflows and docs.
- **Primary sources:**
  - Cisco's official OpenAPI specs (`cisco-en-programmability/catalyst-center-api-specs`, `Assurance/CE_Cat_Center_Org-IssuesList-{1.0.0,1.0.1,3.1.6,3.2.2,3.4.1}`);
  - DevNet API pages for 2.3.7.x;
  - `dnacenter-go-sdk` parameter structs;
  - Grafana v12.2.0 sources and the installed `@grafana/*` packages;
  - `grafana-plugin-sdk-go` v0.279.0;
  - git history, including the released tags `v1.0.4` and `v1.0.5`.
- **Reproduction:** 4 area agents wrote throwaway Go tests against a fake Catalyst Center (`httptest`) and Jest tests, in isolated worktrees. Most findings below were reproduced by a test that fails on the current code.
- **Adversarial check:** 4 independent challengers re-checked every claim and severity. Where they disagreed, the lower severity was usually kept, and the reason is noted.
- **Limits:**
  - No live Catalyst Center was available.
  - For C1, whether the server rejects `limit>25` or silently caps it is unknown. Both behaviors break the plugin, as shown with tests.
  - H5 depends on server-side filter semantics and is marked as needing confirmation on a lab system.

**Verification labels used below**

| Label | Meaning |
|---|---|
| **test** | Reproduced by an automated test against the current code. |
| **spec** | Confirmed against the Cisco spec or Grafana source. |
| **code** | Confirmed by tracing the code; independently re-checked. |
| **likely** | Strong evidence, not confirmed on a live system. |

## 3. Severity scale

| Severity | Meaning |
|---|---|
| Critical | A core feature is broken or returns wrong data in typical setups, or saved dashboards break on upgrade. |
| High | A main feature fails or returns wrong data in common scenarios (multi-site, proxies, config changes, variables), with no easy workaround. |
| Medium | Breaks in less common scenarios, or the UI misleads the user; a workaround exists. |
| Low | Minor edge cases, small inefficiencies, cosmetic or consistency problems. |
| Info | Dead code, scaffold leftovers, or doc mismatches without runtime effect. |

## 4. Ranked issue list

| ID | Sev | Title | Area | Verified |
|---|---|---|---|---|
| C1 | Critical | Issues page size 50 exceeds API maximum of 25 | backend | test, spec |
| C2 | Critical | Missing `siteId`/`siteName` becomes `""`; backend rejects the query; Explore crashes | frontend + backend | test |
| C3 | Critical | Saved v1.0.x queries fail or silently lose filters | backend + frontend | test |
| H1 | High | Limit shared across selected sites; later sites never queried | backend | test |
| H2 | High | Site cache bound forever to the first settings and HTTP client | backend | test |
| H3 | High | `HTTP(S)_PROXY`/`NO_PROXY` ignored | backend | test |
| H4 | High | Template variables broken: variable editor not registered | frontend | test |
| H5 | High | Selecting an area/building probably returns no issues | backend | likely, spec |
| M1 | Medium | Device ID, MAC, Rule columns always empty; Title shows internal key | backend | test, spec |
| M2 | Medium | No `sortBy`/`order`: limit keeps an arbitrary subset | backend | spec, code |
| M3 | Medium | One request series per selected site (API rate limit 50/min) | backend | test |
| M4 | Medium | No handling of HTTP 429 / `Retry-After` | backend | test |
| M5 | Medium | Site-health API errors shown as "No site health data" | backend | test |
| M6 | Medium | Site-health filter matches same-named sites; only one hierarchy level | backend | test |
| M7 | Medium | Site picker shows only leaf names and offers floors in site-health mode | frontend + backend | code |
| M8 | Medium | Token cache keyed by UID only; old token used after settings change | backend | test |
| M9 | Medium | Save & test passes from caches (wrong password, dead URL → OK) | backend | test |
| M10 | Medium | Health check probes the site list, not the issues API | backend | test |
| M11 | Medium | 401 refresh/retry only for issue pages | backend | test |
| M12 | Medium | Same UID in different orgs shares token and site caches | backend | test |
| M13 | Medium | Reverse-proxy path prefix dropped unless baseUrl contains `/dna` | backend | test |
| M14 | Medium | Non-JSON 2xx response (SSO/proxy page) shown as "No issues found" | backend | code |
| M15 | Medium | Multi-value variables become one string `{a,b}` | frontend | test |
| M16 | Medium | `applyTemplateVariables` skips several fields | frontend | test |
| M17 | Medium | Variables cannot be picked in Site/Priority/Status/Metrics | frontend | test |
| M18 | Medium | Variable helper sends `offset=0`, `limit=100`, fixed 24h window | frontend | test, spec |
| M19 | Medium | `sites` variable returns UUIDs; `devices`/`macs` always empty | frontend | test |
| M20 | Medium | Query editor never runs the query after changes | frontend | test |
| M21 | Medium | Saved Site Type not shown in the editor but still applied | frontend | test |
| M22 | Medium | Backend built only for linux/amd64 | build | code |
| L1–L33 | Low | See section 5.4 | | |
| I1–I7 | Info | See section 5.5 | | |

## 5. Issue details

### 5.1 Critical

#### C1 – Issues page size 50 exceeds the API maximum of 25

- **Problem:**
  - `FetchAllIssues` uses `pageSize := 50`, and `buildAssuranceParamsFromQuery` only clamps `limit` to 1..500.
  - The Cisco spec for `GET /dna/data/api/v1/assuranceIssues` defines `limit` with minimum 1, **maximum 25** and default 10, in every version (1.0.0 to 3.4.1). The repo's own CHANGELOG 1.0.2 says the same.
  - The first commit and v1.0.5 used 25. Commit `2acddbf` changed it to 50.
- **Impact:** for any limit above 25, which includes the default 100 and the 1000 used when no limit is set:
  - **Strict server:** every request fails with "issues endpoint returned 400".
  - **Capping server:** the first page returns 25 rows. Since 25 < 50, the loop treats it as the last page, so the panel never shows more than 25 issues per site and gives no warning.
  - A limit of 25 or less still works.
- **Where:** `pkg/backend/client.go:34`, `:64-68`, `:92`; `pkg/backend/params.go:173`.
- **Evidence:**
  - Test against a strict fake server: one request with `limit=50`, then a 400 error.
  - Test against a capping fake server: 1 request, 25 rows (100 expected).
- **Fix:**
  - Use a page size of 25 and clamp the API `limit` to 1..25.
  - Stop paginating when a page returns fewer rows than requested, or use `page.count`.
  - Advance `offset` by the number of rows received.
  - Update `client_test.go`, whose pagination tests assume pages of 50.
- **Test:**
  - Fake servers that reject `limit>25` and that cap at 25.
  - Assert that every request has `limit<=25`, and that `limit=100` returns 100 rows over 4 pages with offsets 1, 26, 51 and 76.

#### C2 – Missing `siteId`/`siteName` becomes `""`, the backend rejects the whole query, and Explore crashes the editor

- **Problem:**
  - `applyTemplateVariables` passes `undefined` through `templateSrv.replace`, which returns `""`. So `siteId: undefined` becomes `siteId: ""`.
  - The backend `QueryModel.SiteID` is `[]string`. Decoding `""` into it fails with `invalid query model: json: cannot unmarshal string into Go struct field QueryModel.siteId of type []string`.
  - Grafana's Explore link runs the same function (`interpolateVariablesInQueries`). The editor then gets `siteId: ""`, which `??` does not replace, so `filters.siteId?.map` throws `TypeError`.
- **Impact:**
  - Every query saved without a `siteId` key fails. That covers every v1.0.4/v1.0.5 issues panel without a site filter, because v1.0.5 wrote `siteId: undefined`, and every provisioned or API-created query.
  - "Explore" on such a panel opens a crashed query row.
  - Opening the panel in the new editor accidentally repairs the query, because the editor writes `siteId: []` on mount (see L16).
- **Where:** `src/datasource.ts:57-74`; `src/components/QueryEditor.tsx:83`, `:203`; `pkg/backend/model.go:171-172`; `pkg/backend/datasource.go:118-124`.
- **Evidence:**
  - Jest: `applyTemplateVariables` output contains `siteId:""` and `siteName:""`.
  - Go: `QueryData` with that payload returns the unmarshal error.
  - Jest: rendering `QueryEditor` with the output of `interpolateVariablesInQueries` throws.
- **Fix:**
  - **Frontend:** leave `undefined`/`null` as-is. Always emit arrays for array fields: wrap a string into `[s]` and drop empty entries.
  - **Backend:** add a tolerant `StringList` type (accepts `null`, `""`, `"x"` and `["x"]`) for `siteId`, `siteName`, `status` and `priority`.
  - **Editor:** normalize array fields in the initial state with `Array.isArray(v) ? v : v ? [v] : []`.
  - **Ordering:** ship this together with, or before, any change to the mount-time `onChange` (L16), because that `onChange` is currently the only thing that repairs these queries.
- **Test:**
  - Jest: `applyTemplateVariables` keeps `siteId` undefined or an array.
  - Jest: the editor renders with `siteId: ""` and with `siteId: "uuid"`.
  - Go: `QueryData` accepts `siteId` as `""`, `"uuid"` and `["uuid"]`.

#### C3 – Saved v1.0.x queries fail or silently lose filters

- **Problem:** v1.0.5 saved `{queryType:'alerts', siteId:'<uuid>', deviceId, priority:[...], issueStatus:'ACTIVE', aiDriven:'true'|'false', limit:25}`. The current `QueryModel` has `SiteID []string`, `Status []string`, `AIDriven bool` and no `issueStatus` or `deviceId`. As a result:
  - A string `siteId` or `aiDriven` makes the whole query fail with `invalid query model`.
  - `issueStatus` is silently ignored, so "ACTIVE" panels also show resolved and ignored issues.
  - `aiDriven: 'false'` ("only non-AI issues") can no longer be expressed (see L20).
  - The editor crashes on a string `siteId` (`filters.siteId?.map is not a function`).
  - The `StringOrBool` type that handled this in v1.0.5 still exists but is unused.
- **Impact:** after upgrading, saved dashboards show errors or wrong data. Wrong data is the worse case, because the user gets no hint.
- **Where:** `pkg/backend/model.go:162-182`; `pkg/backend/datasource.go:118-124`; `src/components/QueryEditor.tsx:82-94`, `:203`.
- **Evidence:**
  - Go tests with v1.0.5-shaped JSON: unmarshal errors for `siteId` and `aiDriven`.
  - `issueStatus:'RESOLVED'` produces a request with no `status` parameter.
  - Jest: the editor throws on `siteId: 'uuid'`.
  - `git show v1.0.5:pkg/backend/model.go`.
- **Fix:**
  - **Backend:** add `QueryModel.UnmarshalJSON` with tolerant types (`StringList` from C2, `StringOrBool` or `*bool` for `aiDriven`/`isGlobal` so that an explicit `false` is still sent). Map `issueStatus` and a string `status` to `Status`, and `severity` to `Priority`.
  - **`deviceId`:** v1.0.5 sent it as an API parameter the API does not accept, so it was probably already a no-op. Map it to `networkDeviceId` only when the value is a UUID; an IP belongs in `networkDeviceIpAddress`.
  - **Frontend:** add a `migrateQuery()` used by `applyTemplateVariables` and by the editor's initial state, with the same mapping. Also map `queryType 'alerts'` to `'assuranceIssues'`.
  - **Missing limit:** v1.0.5 treated it as 25. Decide whether to keep that (see L1).
- **Test:** fixture tests with real v1.0.4/v1.0.5 query JSON through `migrateQuery`, the editor render and `QueryData`. Assert the outgoing `siteId`, `status=active` and `aiDriven=true`/`false`.

### 5.2 High

#### H1 – Limit shared across selected sites; later sites are never queried

- **Problem:** `FetchAllIssues` loops over sites with one shared result list. When the first site fills the limit, `break siteQueryLoop` ends the loop before the other sites are requested.
- **Impact:** a panel for sites A and B with Limit 100 shows only A when A has 100 or more issues. B's issues, including P1s, are missing without warning.
- **Where:** `pkg/backend/client.go:48-98` (`:59-61`).
- **Evidence:** test with site A = 150 issues, B = 10, limit 100: 2 requests, both for A; 0 rows from B.
- **Fix:**
  - Send one request series with all site IDs as repeated `siteId` keys; the API supports OR (see M3).
  - Add `sortBy=mostRecentOccurredTime&order=desc` (see M2), so the limit keeps the newest issues across all sites.
- **Test:** 2 sites, the first with more issues than the limit. Assert that site B is requested and that its newest issues appear.

#### H2 – Site cache bound forever to the first settings and HTTP client

- **Problem:**
  - `getSiteTranslator` creates one `SiteTranslator` per UID and stores the first request's settings and `http.Client`, including the TLS flag, in it. It is never replaced.
  - Later changes to baseUrl, credentials or "Skip TLS verification" are ignored for site lookups until Grafana restarts.
  - The stale translator keeps logging in to the old server and overwrites the shared token slot.
- **Impact:** after a datasource is edited:
  - the site picker lists the old server's sites or fails;
  - the Site Name column shows raw UUIDs;
  - Save & test keeps failing with errors that name the old host or TLS setting, although the new settings are correct.

  The issues query itself uses the new settings, so the plugin is internally inconsistent.
- **Where:** `pkg/backend/datasource.go:48-59`, `:104`, `:424`, `:473`; `pkg/backend/site_translator.go:67-81`.
- **Evidence:**
  - Test: sites fetched from server A, then baseUrl changed to B. The `sites` resource still returns A's sites, and B gets 0 requests.
  - Test with a TLS server: after enabling Skip TLS, CheckHealth still fails with `x509: certificate signed by unknown authority`.
- **Fix:** per-instance state through the SDK instance manager (phase 3). As a minimal fix, key the translator by `(OrgID, UID, Updated)` or by a hash of the relevant settings, and replace it when the key changes.
- **Test:** CheckHealth and the `sites` resource, after changing baseUrl, TLS flag or credentials (same UID, newer `Updated`), hit the new server with the new client.

#### H3 – `HTTP(S)_PROXY` / `NO_PROXY` are ignored

- **Problem:** `httpClientFor` builds a literal `http.Transport{TLSClientConfig: ...}` without `Proxy: http.ProxyFromEnvironment`. The SDK's own HTTP client sets it.
- **Impact:** in networks where Grafana reaches Catalyst Center only through a proxy, every request goes direct and fails with `no such host` or a timeout, while other datasources work. There is no workaround.
- **Where:** `pkg/backend/datasource.go:62-67`.
- **Evidence:** test with `HTTP_PROXY` pointing to a fake server and a non-resolvable baseUrl host. A plain client with `ProxyFromEnvironment` succeeds; the plugin's CheckHealth fails with `no such host`.
- **Fix:** build one client per instance with the SDK (`settings.HTTPClientOptions(ctx)`, TLS options, `httpclient.New(opts)`), or at least clone `httpclient.NewHTTPTransport()`. Reuse that client; this also fixes L11.
- **Test:** CheckHealth succeeds through a proxy set in the environment; assert `transport.Proxy != nil`.

#### H4 – Template variables broken: variable editor not registered

- **Problem:**
  - v1.0.5 called `.setVariableQueryEditor(VariableQueryEditor)`; the current `module.ts` does not.
  - Grafana 12 then shows its legacy free-text editor, which stores a string.
  - `metricFindQuery` switches on `q.type`, so any string, even `"priorities"`, returns `[]`.
  - Existing v1.0.5 variables keep their stored object and still resolve, until someone edits them. After that they are stored as the string `"[object Object]"` and return nothing.
- **Impact:** new Catalyst variables never have values, and existing ones break when edited. Together with M18 and M19, the documented variable feature does not work end to end.
- **Where:** `src/module.ts:15-17`; `src/datasource.ts:83-105`; `src/components/VariableQueryEditor.tsx:63-116`.
- **Evidence:**
  - Jest: `plugin.components.VariableQueryEditor` is undefined.
  - Jest: `metricFindQuery('priorities')` returns `[]`.
- **Fix:**
  - Register the editor, or better, use `CustomVariableSupport` (`this.variables`) with the existing editor.
  - Also let `metricFindQuery` parse string forms such as `priorities()` and `sites(search:"x")`.
- **Test:**
  - The variable editor or `ds.variables` is defined.
  - String queries return values.

#### H5 – Selecting an area or building probably returns no issues (needs lab confirmation)

- **Problem:**
  - The spec describes `siteId` as "the UUID of the site (Ex. `flooruuid`)", matched against the issue's own `siteId`. Issues carry the floor or building UUID; the full chain is only in `siteHierarchyId`.
  - The site picker offers every site type, including areas and Global.
  - The plugin never uses `siteHierarchyId`, which supports `*` wildcards.
- **Impact:** a panel filtered to an area or building likely shows "No issues found" while its floors have issues.
- **Where:** `pkg/backend/client.go:52`; `pkg/backend/params.go:153-160`; `src/components/QueryEditor.tsx:116-122`.
- **Evidence:** spec text and examples (prover and challenger agree). Not tested on a live system.
- **Fix:**
  1. Confirm on a lab system first.
  2. Then, for selected sites, send repeated `siteHierarchyId=*<uuid>*`, which covers the site and all its descendants, instead of `siteId`. Do this at least for area and building types, using the site type from the site cache.
- **Test:**
  - Params test: an area ID produces `siteHierarchyId=*<id>*`.
  - Lab check: compare issue counts for an area and for one of its floors.

### 5.3 Medium

**M1 – Device ID, MAC and Rule columns are always empty; Title shows the internal key**
- **Problem:** the transformer reads `networkDeviceId`/`deviceId`/`deviceIp`/`device`, `macAddress`/`clientMac` and `ruleId`. None of these exist in the assuranceIssues response; device and client identity is in `entityType` plus `entityId`.
- **Also:** `Title` uses `name`, which is the internal key (e.g. `switch_cpu_high`); the readable text is in `summary`.
- **Downgraded from high:** the Details column (`description`) usually names the device.
- **Where:** `pkg/backend/transformer.go:93-106`.
- **Fix:**
  - Device ID = `entityId` for device entity types; MAC = `entityId` for client entity types.
  - Add Summary, Last Occurred (`mostRecentOccurredTime`) and Entity Type columns.
  - Keep the existing column names so field overrides on saved panels still apply.
- **Test:** spec-example issues produce non-empty Device ID and MAC.

**M2 – No sort order sent**
- **Problem:** no `sortBy`/`order` is sent; the API default order is `asc`. The limit therefore keeps an arbitrary, likely oldest-first, subset, and offset paging has no stable order.
- **Where:** `pkg/backend/params.go:120-183`.
- **Fix:** always send `sortBy=mostRecentOccurredTime&order=desc`.

**M3 – One request series per selected site**
- **Problem:** the per-site loop sends one request series per site, although the API accepts repeated `siteId` keys as OR. The code comment at `client.go:39` says the opposite.
- **Impact:** S sites mean S times the requests, against a documented limit of 50 requests/minute. This loop is also the cause of H1.
- **Where:** `pkg/backend/client.go:37-55`.
- **Fix:** remove the loop and send the deduplicated site IDs in one request series.

**M4 – No handling of HTTP 429 / `Retry-After`**
- **Problem:** any non-2xx response, 429 included, aborts the query.
- **Note:** retrying a per-minute limit can take up to 60 s, longer than the 30 s client timeout. Reducing call volume (C1, M3, L1) matters more than retries.
- **Where:** `pkg/backend/client.go:151-154`, `:176-198`.
- **Fix:** add a shared request helper with a bounded, context-aware retry that honors `Retry-After`.

**M5 – Site-health API errors shown as "No site health data"**
- **Problem:** when Site Type is empty, errors from the AREA and BUILDING requests are only logged.
  - If both fail (for example 403), the panel shows "No site health data found".
  - If one fails, rows are silently missing.
- **Where:** `pkg/backend/datasource.go:219-250`.
- **Fix:**
  - Return `errors.Join(...)` when both fail.
  - Add a warning notice to the frame when only one fails.

**M6 – Site-health filter matches by ID or by name**
- **Problem:** `siteMatchesQuery` accepts a match by ID or by name. The picker always sends both `siteId` and `siteName`.
- **Impact:**
  - Selecting "Wien/HQ" also returns "Graz/HQ".
  - Parent matching covers one level only: selecting a country returns its areas but not their buildings.
- **Where:** `pkg/backend/datasource.go:281-344`.
- **Fix:**
  - When IDs are given, match by ID only.
  - Include all descendants using a parent map built from the records or the site cache.
  - Send only IDs from the picker.

**M7 – Site picker shows only leaf names and offers floors in site-health mode**
- **Problem:** picker labels are leaf names only, so duplicate names cannot be told apart. In site-health mode the picker also offers floors, which never match because site-health returns only areas and buildings.
- **Where:** `pkg/backend/model.go:197-201`; `src/components/QueryEditor.tsx:116-123`.
- **Fix:**
  - Add `groupNameHierarchy` and type to `Site`, and label options with the hierarchy.
  - Hide floors in site-health mode.

**M8 – Token cache keyed only by datasource UID**
- **Problem:** after a baseUrl or credential change, the old token is reused for up to 55 minutes.
- **Impact:** site-health then shows an empty table (because of M11), and the variable passthrough returns 401.
- **Where:** `pkg/backend/token.go:45-60`.
- **Fix:** store the token per instance (phase 3), or include a fingerprint of the settings in the cache key.

**M9 – Save & test passes from caches**
- **Problem:** a wrong password, an unreachable baseUrl (`http://127.0.0.1:1`), or a switch to a different server can all report OK without any network request.
- **Where:** `pkg/backend/datasource.go:405-436`.
- **Fix:** in CheckHealth, always fetch a fresh token and make live requests, without writing to the shared caches.

**M10 – Health check probes the wrong API**
- **Problem:** CheckHealth only fetches the site list. v1.0.5 probed `assuranceIssues?limit=1`.
- **Impact, both directions:**
  - Missing Assurance permission still reports OK, and then every issue panel fails with 403/404.
  - A forbidden site API reports ERROR, although issue panels work.
- **Where:** `pkg/backend/datasource.go:423-435`.
- **Fix:**
  - Use `assuranceIssues?limit=1&offset=1` as the pass/fail probe.
  - Report site-API and site-health problems as warning text.

**M11 – 401 refresh-and-retry only for issue pages**
- **Problem:** after a token is revoked or Catalyst Center restarts:
  - site-health shows an empty table;
  - the site picker fails;
  - the variable passthrough fails.
- **Where:** `pkg/backend/client.go:171-198`; `pkg/backend/site_translator.go:81-109`; `pkg/backend/datasource.go:529-559`.
- **Fix:**
  - One `doAuthed` helper for all calls: on 401, invalidate that exact token and retry once.
  - Do not re-authenticate on 403; that is a permissions problem.

**M12 – Same UID in different orgs shares caches**
- **Problem:** Grafana makes datasource UIDs unique per (org, uid) only, but both caches are keyed by UID alone.
- **Impact:** org 2's site picker showed org 1's sites in a test, and org 2 used org 1's token.
- **Where:** `pkg/backend/datasource.go:48-59`; `pkg/backend/token.go:23`.
- **Fix:** use the instance manager, which is keyed by datasource ID, or add OrgID to every cache key.

**M13 – Reverse-proxy path prefix dropped unless baseUrl contains `/dna`**
- **Problem:** `https://gw/proxy/dnac` becomes `https://gw/dna/...`. The config tooltip tells users to "include any proxy prefixes".
- **Where:** `pkg/backend/model.go:56-76`.
- **Fix:** when there is no `dna` segment, treat the whole path as the prefix. Existing values keep working.

**M14 – Non-JSON 2xx response shown as "No issues found"**
- **Problem:** a 2xx response that is not issue JSON (SSO or proxy login page, error envelope) is shown as "No issues found" with no error. The second `json.Unmarshal` error is ignored.
- **Where:** `pkg/backend/client.go:157-167`.
- **Fix:** return an error when the body is neither the envelope nor an array.

**M15 – Multi-value and "All" variables become one glob string**
- **Problem:** `templateSrv.replace` is called without a format, so multi-value and "All" variables become one string like `{id1,id2}`.
- **Impact:** an API error or no rows. Device ID and MAC are free-text inputs, so this is reachable from the UI.
- **Where:** `src/datasource.ts:59-73`; `pkg/backend/params.go:147-160`.
- **Fix:**
  - Interpolate with a format that can be split into arrays.
  - Make `networkDeviceId` and `macAddress` arrays end to end.

**M16 – `applyTemplateVariables` skips several fields**
- **Problem:** it skips `siteType`, `issueName`, `parentSiteId`, `priority`, `status` and `metrics`.
- **Impact:**
  - `$stype` reaches the API literally.
  - Templated priority or status values are dropped by normalization, so the panel silently returns unfiltered data.
- **Where:** `src/datasource.ts:57-74`.
- **Fix:** interpolate every field through one helper that also expands multi-value variables (M15).

**M17 – Variables cannot be selected in the Site, Priority, Status or Metrics pickers**
- **Problem:** the Site picker only lists API sites, and Priority, Status and Metrics are fixed lists, so `$variable` cannot be entered. This was never supported, so it is a gap, not a regression.
- **Where:** `src/components/QueryEditor.tsx:116-242`.
- **Fix:** add dashboard variables as options and allow custom values.

**M18 – Variable helper uses wrong paging and a fixed time window**
- **Problem:** the variable helper sends `offset=0` (the API starts at 1) and `limit=100` (the API max is 25), always uses the last 24 h, and ignores the variable's time range. The backend passthrough forwards these values unchanged.
- **Where:** `src/datasource.ts:29-30`, `:114-172`; `pkg/backend/datasource.go:500-560`.
- **Fix:**
  - Use page size 25 and offsets starting at 1.
  - Use `options.range`.
  - Better: add a backend resource that reuses `FetchAllIssues`.

**M19 – `sites` variable returns UUIDs; `devices`/`macs` always empty**
- **Problem:** the `sites` variable returns UUIDs as text. `devices` and `macs` read fields that do not exist in the response, so they are always empty.
- **Where:** `src/datasource.ts:93-100`, `:139-149`.
- **Fix:**
  - Build `sites` from the `sites` resource, with the hierarchy name as text.
  - Take devices and MACs from `entityId` by `entityType`.

**M20 – Query editor never runs the query**
- **Problem:** the editor never calls `onRunQuery` (v1.0.5 did). After changing a filter, the panel keeps showing old data.
- **Where:** `src/components/QueryEditor.tsx:102-114`, `:147-149`.
- **Fix:** call `onRunQuery()` after debounced user changes and after a query-type change, but not on mount.

**M21 – Saved Site Type not shown but still applied**
- **Problem:** the editor's initial state has no `siteType`, so the input is empty while the backend still filters by the saved value. AREA rows disappear with no visible reason.
- **Where:** `src/components/QueryEditor.tsx:82-94`, `:155-162`.
- **Fix:** initialize every saved field; see also L15.

**M22 – Backend built only for linux/amd64**
- **Problem:** only `linux/amd64` is built, so the plugin cannot start on arm64 (Graviton, Raspberry Pi, Docker on Apple Silicon, including this repo's own dev container on ARM Macs), macOS or Windows.
- **Where:** `Magefile.go:44`; `create_release.sh:35`; `.github/workflows/release.yml:56`.
- **Fix:** import the SDK build targets (`// mage:import`) **and** set `RootPackagePath` to `./cmd/grafana-catalyst-datasource` in `build.SetBeforeBuildCallback`.
  - Without the callback, the SDK looks for `./pkg` and every target fails. This was tested by the challenger.
  - Remove the custom Clean/Build/BuildAll/Coverage targets, which clash with the imported names. Note that the SDK's `Clean` deletes all of `dist/`.
  - Alternative: extend the custom loop to linux/{amd64,arm64,arm}, darwin/{amd64,arm64} and windows/amd64, and mirror the list in `create_release.sh`.

### 5.4 Low

| ID | Issue | Where | Fix |
|---|---|---|---|
| L1 | Limit defaults differ. The frontend uses 100. The backend uses 1000 when `limit` is missing, null (cleared input becomes NaN, then null) or ≤ 0. v1.0.5 used 25. | `datasource.go:168-171`, `QueryEditor.tsx:243-250` | One shared default; treat NaN/empty as the default; validate the input. |
| L2 | An unresolvable `siteName`, or a blank entry in `siteId`, silently widens the query to all sites. `['sA','']` returns sA's issues twice plus every other site. Not reachable from the UI. | `datasource.go:131-145`, `client.go:40-55` | Drop blank IDs; return an error or empty result when no name resolves. |
| L3 | Name → ID resolution keeps only the last of several sites with the same name. | `site_translator.go:127-135` | Resolve by hierarchy name, or report ambiguity. |
| L4 | The Site Name column ignores the issue's own `siteName`/`siteHierarchy` and shows a UUID when the site cache is unavailable. | `transformer.go:72-91` | Use the response fields first, the cache second. |
| L5 | Metric alias fallback shows another metric's value (e.g. `wiredGoodClients` when `numberOfWiredClients` is null). | `transformer.go:163-213`, `:242-296` | Remove `*GoodClients` from the alias lists; treat present-but-null as null. |
| L6 | Automatic site-health columns include nested objects/arrays as all-null numeric fields. | `transformer.go:339-369` | Skip map and slice values. |
| L7 | Site Type is free text; typos reach the API (the API is case-insensitive). | `QueryEditor.tsx:155-162` | Use a Select with AREA/BUILDING. |
| L8 | Site cache refresh holds the write lock during all network calls and does not cache failures. | `site_translator.go:67-144` | Fetch outside the lock; brief negative caching. |
| L9 | No backoff after a failed login: each issue query sends 2 failed logins. | `token.go`, `site_translator.go` | Cache login failures briefly. |
| L10 | No single-flight for token requests; concurrent panels each log in. | `token.go:54-78` | `singleflight` per instance. |
| L11 | New `http.Client` and `Transport` per request; no connection reuse, idle connections accumulate. | `datasource.go:62-67` | One client per instance (H3). |
| L12 | A rejected manual API token is retried unchanged; the UI does not say tokens expire after 60 min. Never falling back to username/password is documented. | `client.go:130-149`, `ConfigEditor.tsx:200` | Skip the retry for manual tokens; add the expiry hint. |
| L13 | The site-health URL keeps the query/fragment of baseUrl, so the parameters get lost. | `datasource.go:200-205` | Use a shared URL builder that clears query and fragment. |
| L14 | baseUrl is not normalized: trailing spaces pass the UI check but fail in Go; a missing scheme gives a relative URL. | `model.go:31-46` | Trim and require an http(s) scheme in the backend. |
| L15 | Local editor state is never re-synced from props; stale values can overwrite external changes (edge case). | `QueryEditor.tsx:82-114` | Derive values from props, or re-sync on change. |
| L16 | Opening the editor rewrites the saved query with defaults (e.g. `limit:100`). Do not change this before the C2 fix. | `QueryEditor.tsx:99-114` | Seed `lastSig` from the query, after C2 is fixed. |
| L17 | Default `queryType 'alerts'` matches no Query Type option, so the selector looks empty for new queries and all v1.0.x queries. | `datasource.ts:43-47`, `QueryEditor.tsx:73-76` | Default to `assuranceIssues`; map `alerts` when reading. |
| L18 | Username is a plain input bound to `secureJsonData`, so it shows empty after save. | `ConfigEditor.tsx:167-172` | Move to `jsonData`, or use `SecretInput` with the "configured" state. |
| L19 | No UI for `issueName` and `parentSiteId`, although the backend supports them. | `QueryEditor.tsx` | Add inputs, or drop the fields. |
| L20 | The AI-Driven and Global switches cannot express "false" (v1.0.5 could). | `QueryEditor.tsx:252-263`, `params.go:165-170` | Tri-state select (Any/Yes/No). |
| L21 | The site picker requests `sites` on every keystroke. | `QueryEditor.tsx:116-137` | `cacheOptions`, or load once. |
| L22 | The site picker hides load errors and shows "No options found". | `QueryEditor.tsx:133-136` | Show the error. |
| L23 | Hidden queries still run, because `filterQuery` always returns true. | `datasource.ts:51-53` | `return !query.hide`. |
| L24 | The config "API Endpoint" setting only changes the default type of new queries, but reads like a global switch. | `ConfigEditor.tsx:100-115` | Rename it or remove it. |
| L25 | The variable "Search" field claims variable support but is not interpolated. | `datasource.ts:114-118` | Interpolate it, or change the placeholder. |
| L26 | The provisioned dev datasource is the create-plugin template (`path`, `apiKey`, no baseUrl), so the dev Grafana starts with a broken datasource. | `provisioning/datasources/datasources.yml` | Provide baseUrl/credentials placeholders, or remove it. |
| L27 | `create_release.sh` names the zip after the last git tag (1.0.5), but the `plugin.json` inside says 0.1.0. | `create_release.sh:7-33` | Take the version from one source. |
| L28 | `release.yml` runs `npm version` without `--allow-same-version`, so the release aborts once `package.json` matches the tag. | `.github/workflows/release.yml:40` | Add the flag. |
| L29 | Go pins (CI 1.21, dev Dockerfile 1.21.6, docs ≥ 1.21) conflict with `go.mod` `go 1.24.6`; builds rely on automatic toolchain download. | `release.yml:30`, `.config/Dockerfile:10` | Align to 1.24. |
| L30 | CI never builds or tests the Go backend; releases run no tests. | `.github/workflows/ci.yml`, `release.yml` | Add `go vet`, `go test` and `test:ci` steps. |
| L31 | Docker dev mode runs `mage -v watch`, which does not exist. Binaries are stripped, so delve has no debug info. `pgrep -f` matches its own wrapper shell. | `.config/supervisord/supervisord.conf`, `Magefile.go:71` | SDK targets (M22); unstripped debug build; anchored `pgrep`. |
| L32 | Releases are unsigned, and the user docs do not say Grafana must allow unsigned plugins. | `release.yml`, `src/README.md` | Sign, or document `allow_loading_unsigned_plugins`. |
| L33 | Several backend tests send requests to `https://catalyst.example.com` and assert nothing about data; mocks are defined but never used. They give false confidence. | `pkg/backend/datasource_test.go` | Replace with fake-server tests (phase 0). |

### 5.5 Info

| ID | Issue | Where |
|---|---|---|
| I1 | Dead or test-only code: `src/cache.ts`; `validateQuery` and `assertValid*`; `formatErrorMessage`; `metricOptions` (with metric names that do not exist); the `enrich` and `endpoint` query fields; `GetSiteName`; unused Go mocks; `func min` (shadows the Go builtin). `VariableQueryEditor` is dead until H4 is fixed. `StringOrBool` is unused today but needed for C3. | `src/*`, `pkg/backend/*` |
| I2 | Docs contradict the behavior. `src/README.md` promises site-health time series, `healthScore`/`accessPointCount`, comma-separated priorities and working variables. CHANGELOG has no entries for the breaking changes after 1.0.5. `copilot-instructions.md` claims 1000 records per request. The code comment at `client.go:39` claims the API lacks OR for siteId. The comment at `datasource.go:181` points to wrong lines. | docs, comments |
| I3 | The HelpTooltip "Learn more" link uses the old account name (redirects) and points to an anchor in the wrong README. | `ConfigEditor.tsx:124` |
| I4 | `tests/*.spec.ts` are unchanged create-plugin templates that cannot pass; the e2e job is disabled. | `tests/` |
| I5 | The Docker dev instructions never build the plugin, so on a fresh clone the mounted `dist/` is empty. | `DEVELOPER_GUIDE.md`, `README.md` |
| I6 | `main.go` uses the deprecated `backend.Serve` instead of `datasource.Manage`. | `cmd/grafana-catalyst-datasource/main.go:15` |
| I7 | `plugin.json` has no `"alerting": true` (feature request; only site-health frames would be usable). | `src/plugin.json` |

### 5.6 Checked and not an issue

- **Lowercase status values** (`active`, `resolved`, `ignored`) are correct; the spec examples use lowercase.
- **Site Type case:** the site-health API treats it as case-insensitive. Only the missing validation remains (L7).
- **"`siteId`/`siteName` are only returned with `view=site`"** is wrong. With no `view` parameter, the API returns all attributes.
- **Manual API token never falls back to username/password:** this is documented behavior. Only the useless retry remains (L12).
- **Variables of v1.0.5 dashboards keep working until edited:** merged into H4. **Stale translator overwrites the token slot:** merged into H2.

## 6. Fix plan

> **Superseded by [ACTION_PLAN.md](ACTION_PLAN.md).** The action plan adapts this outline to Catalyst Center 3.1.6+, records the decisions D1–D12, and adds the items N1–N3. This section is kept for reference only.

### Phase 0 – Test harness (prerequisite, effort M)

- **Fake Catalyst Center:** a reusable `httptest` server for token, assuranceIssues, site v2 and site-health. Base it on the spec examples, with switches for strict/capping `limit`, 401, 403, 429 and HTML bodies.
- **Replace weak tests:** swap the `QueryData` tests that call `catalyst.example.com` for fake-server tests (L33).
- **Fixtures:** add v1.0.4/v1.0.5 saved-query fixtures.
- **CI:** add `go vet` and `go test` to CI, and run all tests in `release.yml` (L30).
- **Verify:** CI runs the Go and Jest suites on every push.

### Phase 1 – Patch release for the critical issues (C1, C2, C3; effort M)

1. Page size 25, API `limit` clamped to 1..25, pagination stop and offset fixed (C1). Also fix the variable helper paging (M18) to stay within the API contract.
2. Backend: tolerant `QueryModel.UnmarshalJSON` with a `StringList` type, `StringOrBool`/`*bool`, and the legacy field mapping (C2, C3).
3. Frontend:
   - `applyTemplateVariables` keeps `undefined` and always emits arrays;
   - `migrateQuery()` for v1.0.x fields;
   - editor initial state normalizes non-array values (C2, C3).
4. Do **not** change the editor's mount-time `onChange` in this phase (L16), because it currently repairs old queries.
5. Release as 1.0.6 with a CHANGELOG entry that names the fixed upgrade breakage.

- **Risks:**
  - Saved panels whose limit was above 25 now make more, smaller requests. This is fine within the rate limit.
  - Keep the `queryType 'alerts'` routing.
- **Verify:**
  - The C1–C3 regression tests.
  - Load a v1.0.5 dashboard JSON in the dev Grafana: no errors, and status filters are applied.

### Phase 2 – Issue data correctness (H1, H5, M1–M4, M14, L1, L2, L4; effort M)

1. **One request series:** send one request series with all site IDs (repeated keys) plus `sortBy=mostRecentOccurredTime&order=desc`. Remove the per-site loop (H1, M2, M3).
2. **Area/building selection (H5):** confirm on a lab system, then switch site filtering to `siteHierarchyId=*<uuid>*` for areas and buildings.
3. **Columns:**
   - map Device ID and MAC from `entityType`/`entityId`;
   - add Summary, Last Occurred and Entity Type columns;
   - fill Site Name from the response first.

   Keep the column names (M1, L4).
4. **Shared request helper:** use it for all issue calls, with an error for non-JSON 2xx bodies (M14) and bounded 429 handling (M4).
5. **Limit and site filters:** one default limit; NaN and empty input treated as the default. Drop blank site IDs; unresolvable names give an error (L1, L2).

- **Risks:**
  - Row order changes to newest first.
  - New columns can shift table layouts in panels without explicit column settings.
- **Verify:** fake-server tests for multiple sites, sorting, the column mapping and 429.

### Phase 3 – Per-instance backend state (H2, H3, M8–M13, L8–L14, I6; effort L)

This is the structural fix. One change resolves most lifecycle problems.

1. Switch `main.go` to `datasource.Manage` with `datasource.NewInstanceManager(factory)`. The instance holds:
   - the parsed settings;
   - one SDK HTTP client (proxy-aware, TLS options);
   - its own token manager;
   - its own site translator.

   Grafana recreates the instance when the settings change, and keys it by datasource ID across orgs. Implement `Dispose` to close idle connections (H2, H3, M8, M12, L11, I6).
2. **Authenticated requests:** one `doAuthed` helper for every call (issues, site-health, site list, passthrough). It invalidates the exact token on 401 and retries once, does not re-authenticate on 403, uses single-flight login and caches login failures briefly (M11, L9, L10, L12).
3. **CheckHealth:** fresh login, live `assuranceIssues?limit=1&offset=1` probe as pass/fail, and site-API problems reported as warnings. It must not write to the shared caches (M9, M10).
4. **Site translator:** fetch outside the lock; store `groupNameHierarchy` and type (L8, M7 backend part).
5. **URLs:** one shared URL builder, also for site-health (L13). Normalize baseUrl (L14). Keep the whole path as the proxy prefix when there is no `/dna` segment (M13).

- **Risks:** this is a larger refactor of `datasource.go`. Keep the external behavior: the resource paths `sites` and `issues`, and the frame shapes.
- **Verify:** tests for a settings change (baseUrl, TLS, password), two orgs with the same UID, `HTTP_PROXY`, token revocation, and Save & test with a wrong password or dead URL.

### Phase 4 – Site health (M5, M6, M7, L5, L6, L7; effort M)

1. **Errors:** surface them (both failed: error; one failed: warning notice) (M5).
2. **Filtering:** filter by ID only when IDs are given; include all descendants (M6).
3. **Site picker:** label options with the hierarchy name, hide floors in site-health mode, and send only IDs (M7).
4. **Columns:** remove the `*GoodClients` fallbacks from the metric aliases and skip nested objects in automatic columns (L5, L6).
5. **Site Type:** use a Select with AREA/BUILDING (L7).

- **Verify:** filter tests with duplicate names and three hierarchy levels.

### Phase 5 – Frontend editors and variables (H4, M15–M21, L15–L25; effort L)

1. **Variable support:**
   - use `CustomVariableSupport` (or register `VariableQueryEditor`);
   - make `metricFindQuery` accept string forms;
   - add a backend resource for variable values that reuses the phase 2 paging and the variable's time range;
   - `sites` from the site resource with hierarchy names;
   - devices and MACs from `entityId` (H4, M18, M19, L25).
2. **Interpolation:** one helper for every field, with multi-value expansion. `networkDeviceId` and `macAddress` become arrays end to end (M15, M16).
3. **Pickers:** offer dashboard variables in Site, Priority, Status and Metrics (M17).
4. **Editor behavior:**
   - run the query after user changes (M20);
   - initialize all saved fields and derive values from props (M21, L15);
   - only now change the mount-time `onChange`, after phase 1 (L16).
5. **Smaller items:**
   - default `queryType` `assuranceIssues` (L17);
   - username field (L18);
   - inputs for `issueName`/`parentSiteId` (L19);
   - tri-state AI-Driven/Global (L20);
   - cached site options and visible errors (L21, L22);
   - `filterQuery` respects `hide` (L23);
   - rename or remove "API Endpoint" (L24).

- **Risks:** variables stored by v1.0.5 as objects must keep working, so the variable support must read both the object and the string form.
- **Verify:**
  - Jest tests per variable type.
  - Manual check in the dev Grafana: create variables, use `$site` with multi-select, and edit an old variable.

### Phase 6 – Packaging, CI, docs, cleanup (M22, L26–L32, I1–I5, I7; effort M)

1. **Builds:** multi-platform via the SDK build targets with `RootPackagePath`, or an extended custom loop. Mirror it in `create_release.sh` and document the supported platforms (M22).
2. **Release:** one version source for zip and `plugin.json`, `--allow-same-version`, Go 1.24 everywhere, and signing or documentation of unsigned loading (L27–L29, L32).
3. **Dev setup:** working watch/debug targets, unstripped debug builds, anchored `pgrep`, a real provisioning example, and docs that build the plugin before `docker compose up` (L26, L31, I5).
4. **Cleanup:** remove dead code (I1), the template e2e specs or real e2e tests (I4), and update README, `src/README.md`, CHANGELOG and copilot-instructions (I2, I3).
5. **Alerting (optional):** decide on `"alerting": true` together with a numeric issue-count frame (I7).

- **Verify:** CI checks that all platform binaries exist; release dry-run; docs reviewed against the UI.

### Dependencies

- Phase 0 comes before everything else.
- Phase 1 ships first and on its own.
- Phase 2 step 1 (single request series) and phase 3 step 2 (`doAuthed`) both touch the request code. Do them in that order, or merge them.
- L16 must not change before phase 1 is released.
- H5 needs a lab check before its code change.
