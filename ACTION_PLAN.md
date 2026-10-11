# Action plan: functional fixes

This plan turns [FUNCTIONAL_REVIEW.md](FUNCTIONAL_REVIEW.md) into concrete work.

- **Issue IDs** (C1, H2, M7, L12, I3, …) refer to that report.
- **New items** (N1–N3) came from adapting the plan to Catalyst Center 3.1.6 (section 2).
- **Work packages** (WP) are sized to fit one pull request each.
- **Estimates** are developer-days (d) for one developer who knows the codebase.

## 1. Target environment

| | Target |
|---|---|
| Catalyst Center | **3.1.6 or later** (minimum supported). Older releases are not supported or tested. |
| Grafana | 12.1 or later (unchanged) |
| Plugin platform | **linux/amd64 only** (D7) |
| Signing | **Unsigned** (D8). Grafana must allow it with `allow_loading_unsigned_plugins`. |
| Test lab | A Catalyst Center 3.1.6+ system (D10) |

## 2. API baseline for Catalyst Center 3.1.6

**Sources:**
- Cisco's 3.1.6 Python SDK (`catalystcentersdk/api/v3_1_6_0`);
- the Catalyst Center Go SDK (`catalystcenter-go-sdk`);
- the official OpenAPI specs (`catalyst-center-api-specs`: `IssuesList-3.1.6`, `siteHealthSummaries-1.0.3`);
- the DevNet API changelog.

| Purpose | Used today | 3.1.6 status | Use from now on |
|---|---|---|---|
| Login | `POST /dna/system/api/v1/auth/token` | Current | Keep. |
| Issues | `GET /dna/data/api/v1/assuranceIssues` | Current. The 3.1.6 spec matches the one used in the review: `limit` 1..25 (default 10), `offset` from 1, repeated keys for lists, `siteHierarchyId` with `*` wildcards. | Keep (all C1/H1/M1… fixes still apply). |
| Site list | `GET /dna/intent/api/v2/site` | **Deprecated since 2.3.7.6. The 3.1.6 docs show a "Sunset" banner.** | **`GET /dna/intent/api/v1/sites`** (new item N1). |
| Site health | `GET /dna/intent/api/v1/site-health` | Still present, but legacy. AREA/BUILDING only, a single snapshot by `timestamp`, no server-side site filter, `limit` ≤ 50. | **`GET /dna/data/api/v1/siteHealthSummaries`** (new item N2, decided in D12). |

### `GET /dna/intent/api/v1/sites`

- **Parameters:**
  - `name`, `nameHierarchy`;
  - `type` (`global`, `area`, `building`, `floor`);
  - `offset` from 1;
  - `limit` 1..500.
- **Fields per site:** `id`, `parentId`, `name`, `nameHierarchy` (e.g. `Global/USA/San Jose/Building1`), `type`, `siteHierarchyId`, plus address, coordinates and floor details.
- **What this solves:** it gives the hierarchy name (for picker labels and unique names), the site type, and `siteHierarchyId`. The issues and site-health filters need `siteHierarchyId`.

### `GET /dna/data/api/v1/siteHealthSummaries`

- **Parameters:**
  - `startTime`, `endTime`. Without `endTime` the API returns the latest data. With an `endTime` whose data is not ready, the request fails and the error names a usable `endTime`.
  - `limit` 1..**20** (default 10); `offset` from 1.
  - `sortBy`, `order`.
  - `siteHierarchy`, `siteHierarchyId` (repeated keys, `*` wildcards).
  - `siteType` (default `floor,building,area`), `id`, `view`, `attribute`.
- **Fields:**
  - site: `id`, `siteHierarchy`, `siteHierarchyId`, `siteType`, `latitude`, `longitude`;
  - clients: `clientCount`, `wiredClientCount`, `wirelessClientCount`, `clientGoodHealthPercentage`, `wiredClientGoodHealthPercentage`, `wirelessClientGoodHealthPercentage`, `*ClientGoodHealthCount`;
  - devices: `networkDeviceCount`, `networkDeviceGoodHealthPercentage`, plus count, good-health count and good-health percentage per role (access, core, distribution, router, wireless, AP, WLC, switch);
  - other: `clientDataUsage`, `p1IssueCount`…`p4IssueCount`, `issueCount`.

**Effect on the review:** moving site health to `siteHealthSummaries` removes the root cause of several issues:
- **M5:** one request series replaces the AREA/BUILDING double fetch, so errors no longer get swallowed.
- **M6:** the server filters by `siteHierarchyId`, which is ID-based and includes descendants.
- **M7:** floors become valid selections.
- **L5:** the fallback metric aliases are replaced by an explicit mapping.

**Cost:** the metric names change, so saved site-health queries need a metric-name migration (WP4.2).

## 3. Decisions

| # | Decision | Status |
|---|---|---|
| D1 | One shared `DefaultIssueLimit = 100`; backend hard limit clamped to 1..1000. | **Decided** |
| D2 | Legacy `deviceId`: UUID → `networkDeviceId`; IP → `networkDeviceIpAddress`; anything else → dropped, with a frame notice. | **Decided** |
| D3 | Issue site filter includes descendants via `siteHierarchyId`. Implemented as a prefix match on the site's own `siteHierarchyId` from `v1/sites` (`siteHierarchyId=<hierarchyId>*`), which is the precise form of `*<uuid>*`. Confirm in the lab (WP2.0). | **Decided** |
| D4 | Site-health selection includes descendants. | **Decided: yes** |
| D5 | Username stays in `secureJsonData`, shown with `SecretInput` and its "configured" state. | **Decided** |
| D6 | Issues frame: Title = `summary` (fallback `name`); add `Issue Type`, `Entity Type`, `Last Occurred` at the end; remove the always-empty `Rule` column. | **Decided** |
| D7 | Platform: linux/amd64 only. M22 becomes a documented limitation. | **Decided** |
| D8 | No signing. Document `allow_loading_unsigned_plugins`. | **Decided** |
| D9 | No alerting support for now; revisit after 1.2.0. | **Decided** |
| D10 | Develop and test for Catalyst Center 3.1.6+. | **Decided** |
| D11 | Site list moves to `GET /dna/intent/api/v1/sites` (N1). | **Required**: v2 is deprecated and marked "Sunset" in 3.1.6. |
| D12 | Site health moves to `siteHealthSummaries` (N2), with a migration of saved metric names. The legacy `site-health` path is removed in 1.2.0. | **Decided** |

## 4. Overview

| Release | Content | Work packages | Effort |
|---|---|---|---|
| **1.0.6** (patch) | Test harness; the 3 critical upgrade and paging bugs | WP0.1–WP0.4, WP1.1–WP1.5 | ~7.75 d |
| **1.1.0** | Correct issue data; per-datasource backend state; site list on `v1/sites`; 3.1.6 health check | WP2.0–WP2.5, WP3.1–WP3.7 | ~11 d |
| **1.2.0** | Site health on `siteHealthSummaries`; editors and template variables | WP4.1–WP4.4, WP5.1–WP5.5 | ~9.5 d |
| ongoing | Release pipeline, dev setup, docs, dead code | WP6.1–WP6.5 | ~2.25 d |

The total is about 30.5 developer-days, roughly 6–7 weeks for one developer.

### Rules for every work package

- Each WP is one pull request with tests. CI must be green: `go vet`, `go test -race ./...`, `npm run typecheck`, `npm run lint`, `npm run test:ci`.
- No saved query may break. The v1.0.4/v1.0.5 fixtures from WP0.3 must pass in every PR from Phase 1 on.
- Keep the external contract: the resource paths `sites` and `issues`, the issues frame column names (except where D6 says otherwise), and `queryType` routing.
- Every user-visible change gets a CHANGELOG entry.
- New API behavior is modelled on the 3.1.6 specs and confirmed in the 3.1.6 lab before release.

## 5. Work packages

### Phase 0 – Test harness (release 1.0.6)

**Status:** done on branch `fix/1.0.6`. WP0.4 is confirmed by the first CI run on a pull request. Twelve of the fourteen fixtures are marked `pending` and reproduce C1, C2 and C3 when run with `FIXTURES_RUN_PENDING=1`.

#### WP0.1 Fake Catalyst Center (3.1.6 APIs)
**Fixes:** prerequisite for all · **Effort:** 1.75 d · **Depends on:** –

**File:** `pkg/backend/fakecc_test.go` (new)

- [x] `httptest` server with these endpoints:
  - `POST /dna/system/api/v1/auth/token`
  - `GET /dna/data/api/v1/assuranceIssues`
  - `GET /dna/intent/api/v1/sites`
  - `GET /dna/data/api/v1/siteHealthSummaries`
  - and, until they are replaced, the legacy `GET /dna/intent/api/v2/site` and `GET /dna/intent/api/v1/site-health`.
- [x] Records every request (path, query, `X-Auth-Token`) for assertions.
- [x] Switches for:
  - `limit` mode: strict (400 above the spec maximum), cap (truncate), lenient;
  - 401 after token revocation; 403; 429 with `Retry-After`; 200 with an HTML body;
  - `siteHealthSummaries` "data not ready for endTime" error;
  - mounting under a path prefix with `http.StripPrefix`.
- [x] Fixture data from the 3.1.6 spec examples. The fixture site tree has at least 3 levels with `siteHierarchyId`, two buildings with the same leaf name, and floors.
- [x] Wildcard matching for `siteHierarchyId` (`prefix*`, `*x*`), so the filter work packages can be tested.

**Done when:** an example test drives `QueryData` end to end through the fake server.

#### WP0.2 Replace tests that call a real host
**Fixes:** L33, part of I1 · **Effort:** 1 d · **Depends on:** WP0.1

**Files:** `pkg/backend/datasource_test.go`, `client_test.go`

- [x] Rewrite the `QueryData` tests that call `https://catalyst.example.com` to use the fake server, and make them assert rows and request parameters.
- [x] Delete the unused `mockClient` and `mockSiteTranslator`.

**Done when:** `go test ./...` makes no outbound network calls.

#### WP0.3 v1.0.x saved-query fixtures
**Fixes:** prerequisite for C2, C3 · **Effort:** 0.5 d · **Depends on:** –

**Files:** `pkg/backend/testdata/queries/*.json`, `src/__fixtures__/queries.ts` (new)

- [x] Build fixtures from `git show v1.0.4:` and `git show v1.0.5:` editor output. Cover: no site; `siteId` as a string; `issueStatus`; `deviceId` as a UUID and as an IP; `aiDriven` as `'true'` and `'false'`; `priority` arrays; a missing `limit`.
- [x] Add current (pre-1.2.0) site-health queries with the old metric names, for WP4.2.
- [x] Each fixture lists the API parameters it must produce.

#### WP0.4 Run Go tests in CI
**Fixes:** L30, part of L29 · **Effort:** 0.5 d · **Depends on:** –

**Files:** `.github/workflows/ci.yml`, `.github/workflows/release.yml`

- [x] `ci.yml`: add `actions/setup-go` with `go-version-file: go.mod`, then `go vet ./...` and `go test -race ./...`.
- [x] `release.yml`: switch to `go-version-file: go.mod` (replacing `"1.21"`), and run `npm run test:ci` and `go test ./...` before building.

**Done when:** a deliberately failing Go test fails CI.

### Phase 1 – Critical fixes (release 1.0.6)

#### WP1.1 Issue paging within the API limit
**Fixes:** C1 · **Effort:** 0.5 d · **Depends on:** WP0.1

**Files:** `pkg/backend/client.go`, `params.go`, `client_test.go`

- [ ] Replace `pageSize := 50` with `const issuesPageSize = 25` (the 3.1.6 spec maximum).
- [ ] `buildAssuranceParamsFromQuery`: use `clampLimit(pageSize, 25, 1, 25)`.
- [ ] End pagination when `len(issues) < limitForThisPage`.
- [ ] Advance the offset by `len(issues)`.
- [ ] Update the pagination and hard-limit tests that assume pages of 50.

**Done when:** in both strict and cap mode, `limit=100` gives 4 requests (offsets 1, 26, 51, 76) and 100 rows, and no request has `limit > 25`.

#### WP1.2 Variable helper paging within the API limit
**Fixes:** part of M18 (paging) · **Effort:** 0.5 d · **Depends on:** –

**Files:** `src/datasource.ts`, `pkg/backend/datasource.go` (`resourceIssues`)

- [ ] `PAGE_SIZE = 25`; start at `offset=1`; then `offset += PAGE_SIZE`.
- [ ] `resourceIssues` clamps the forwarded `limit` to 1..25 and `offset` to ≥ 1.

**Done when:** Jest shows the first variable request is `limit=25&offset=1`, and a Go test shows the passthrough rewrites `offset=0&limit=100`.

#### WP1.3 Tolerant backend query model
**Fixes:** C2, C3 (backend) · **Effort:** 1.5 d · **Depends on:** WP0.3

**Files:** `pkg/backend/model.go`, `params.go`, `datasource.go`, `model_unmarshal_test.go`

- [ ] Add a `StringList` type with `UnmarshalJSON`. It accepts `null`, `""`, `"x"` and `["x", ""]`, drops empty entries, and returns `nil` for empty input. Use it for `SiteID`, `SiteName`, `Priority`, `Status` and `Metrics`.
- [ ] Add a `TriBool` type for `AIDriven` and `IsGlobal`:
  - JSON `true` → filter true;
  - JSON `false` → no filter (current switch "off");
  - string `"true"` / `"false"` → explicit filter (v1.0.5 meaning);
  - absent or `null` → no filter.

  Remove or replace the unused `StringOrBool`.
- [ ] Add the legacy fields `issueStatus`, `deviceId` and `severity`. `normalize()` maps:
  - `issueStatus` → `Status`;
  - `severity` → `Priority`;
  - `deviceId` per D2.
- [ ] `params.go`:
  - send `aiDriven=false` and `isGlobal=false` when explicitly false;
  - add `networkDeviceIpAddress`;
  - add a frame notice for a dropped `deviceId`.

**Done when:** every WP0.3 fixture decodes without error and produces exactly its expected parameters. `siteId: ""` and `siteName: ""` produce no site parameter.

#### WP1.4 Frontend query migration and normalization
**Fixes:** C2, C3 (frontend) · **Effort:** 1 d · **Depends on:** WP0.3

**Files:** `src/migrations.ts` (new), `src/datasource.ts`, `src/types.ts`, `src/components/QueryEditor.tsx`, tests

- [ ] `migrateQuery(q)`:
  - `siteId` / `siteName`: a string becomes `[s]`, and `""` or `null` becomes `[]`;
  - `issueStatus` → `status: [upper(issueStatus)]`;
  - `deviceId` → `networkDeviceId` (UUID only, per D2);
  - `aiDriven: 'true'` → `true`; keep `'false'` as a string until the tri-state UI arrives in WP5.5.
- [ ] `applyTemplateVariables`:
  - call `migrateQuery` first;
  - never turn `undefined` into `""`;
  - array fields always stay arrays.
- [ ] `QueryEditor`:
  - build the initial state from `migrateQuery(query)`;
  - the switch is checked only for `true` or `'true'`.
- [ ] Do **not** change the mount-time `onChange` (L16) in this WP; it currently repairs old queries.

**Done when:**
- `applyTemplateVariables` keeps `siteId` undefined or an array for every fixture.
- `QueryEditor` renders with `siteId: ""` and `siteId: "uuid"`.
- Rendering the output of `interpolateVariablesInQueries` does not throw.

#### WP1.5 Release 1.0.6
**Effort:** 0.5 d · **Depends on:** WP1.1–WP1.4, WP0.4

- [ ] CHANGELOG entries:
  - the fixed upgrade breakage (C2, C3) and the page-size fix (C1);
  - "Requires Catalyst Center 3.1.6 or later; linux/amd64 only; unsigned."
- [ ] Manual check against the 3.1.6 lab (or the dev Grafana with the fake data):
  1. Import a dashboard with v1.0.5 panels.
  2. All panels load, and the filters show in the request log.
  3. Panel → Explore does not crash.
- [ ] Tag `v1.0.6`.

### Phase 2 – Issue data correctness (release 1.1.0)

#### WP2.0 Lab verification on Catalyst Center 3.1.6
**Fixes:** confirms H5, D3 and the C1 server behavior; prepares Phase 4 · **Effort:** 1 d plus lab access · **Depends on:** D10

**File:** `docs/catalyst-api-notes.md` (new)

- [ ] **Issues API:**
  1. `limit=50`: 400 or capped?
  2. Issue counts for an area, a building and a floor with:
     - `siteId=<uuid>`;
     - `siteHierarchyId=<hierarchyId>*`;
     - `siteHierarchyId=*<uuid>*`.
  3. `entityType` values for device, AP and client issues.
  4. 429 behavior: is `Retry-After` sent?
  5. Whether `status`/`priority` values are case-sensitive.
- [ ] **`v1/sites`:** paging (offset from 1, limit 500), `type` values, and that `siteHierarchyId` matches the issues and site-health filters.
- [ ] **`siteHealthSummaries`:**
  - behavior without `endTime` and with past `endTime`s;
  - the exact text of the "data not ready" error;
  - the rate limit;
  - the values of the new metrics next to the old `site-health` metrics for the same sites (input for the WP4.2 mapping).
- [ ] Update the review items and this plan with the results.

#### WP2.1 One request series for all sites, newest first
**Fixes:** H1, M2, M3 · **Effort:** 1 d · **Depends on:** WP1.1

**Files:** `pkg/backend/client.go`, `params.go`, tests

- [ ] Remove the per-site loop in `FetchAllIssues`. Send the deduplicated site filter once per page.
- [ ] Always send `sortBy=mostRecentOccurredTime&order=desc`.
- [ ] Delete the wrong comment at `client.go:39` (part of I2).

**Done when:**
- A 3-site query sends one request per page carrying all three site filters.
- Every request has `sortBy`/`order`.
- With site A at 150 issues and B at 10 (B newest), the result includes B.

#### WP2.2 Site filter with descendants via `siteHierarchyId`
**Fixes:** H5 · **Effort:** 0.5 d · **Depends on:** WP2.0, WP2.1, WP3.6, D3

**Files:** `pkg/backend/params.go`, `datasource.go`, tests

- [ ] For each selected site, look up its `siteHierarchyId` in the site cache (`v1/sites`, WP3.6) and send `siteHierarchyId=<hierarchyId>*` instead of `siteId`.
- [ ] If a site is missing from the cache, fall back to `siteHierarchyId=*<uuid>*` and add a notice.

**Done when:** a fake-server test shows that selecting an area returns the issues of its buildings and floors, and the lab check from WP2.0 confirms it.

#### WP2.3 Issue frame columns
**Fixes:** M1, L4 · **Effort:** 1 d · **Depends on:** D6

**Files:** `pkg/backend/transformer.go`, `transformer_test.go`

- [ ] Device ID = `entityId` for device entity types; MAC = `entityId` for client entity types (types from WP2.0).
- [ ] Columns per D6: Title from `summary` (fallback `name`); add `Issue Type`, `Entity Type`, `Last Occurred`; remove `Rule`.
- [ ] Site Name: use the response's `siteName`/`siteHierarchy` first, then the site cache, then the ID. Parent Site: the parent segment of `siteHierarchy`.

**Done when:** 3.1.6 spec-example issues produce non-empty Device ID, MAC, Title and Site Name with no site cache available.

#### WP2.4 Shared request helper for issue calls
**Fixes:** M4, M14 · **Effort:** 1 d · **Depends on:** WP0.1

**Files:** `pkg/backend/request.go` (new), `client.go`

- [ ] `doRequest(ctx, req)` returns an error:
  - for non-2xx responses, with the status and the first 300 bytes of the body;
  - for a 2xx body that is neither a `{response: [...]}` envelope nor a JSON array.
- [ ] On 429: retry once if `Retry-After` is ≤ 10 s and the context deadline allows it. Otherwise return the error "Catalyst Center rate limit reached".
- [ ] Move the existing 401 retry into the helper. WP3.4 extends it to all calls.

**Done when:** fake-server tests pass for an HTML 200 response (error), 429 then 200 (success), and 429 with a long `Retry-After` (clear error).

#### WP2.5 Limit default and site-filter edge cases
**Fixes:** L1, L2 · **Effort:** 0.5 d · **Depends on:** D1

**Files:** `pkg/backend/datasource.go`, `src/types.ts`, `src/components/QueryEditor.tsx`

- [ ] Use `DefaultIssueLimit = 100` in the backend and frontend. Clamp the backend hard limit to 1..1000.
- [ ] Limit input: an empty field means the default; never store `NaN`.
- [ ] Backend: drop blank site IDs. If none of the given site names resolve, return the error "site 'X' not found".

**Done when:** unit tests pass for `limit` absent, `null`, `NaN` and 5000; for `siteId: ['a','']`; and for an unknown site name.

### Phase 3 – Per-datasource backend state (release 1.1.0)

#### WP3.1 Instance manager
**Fixes:** H2, M8, M12, I6 · **Effort:** 2 d · **Depends on:** WP0.2

**Files:** `cmd/grafana-catalyst-datasource/main.go`, `pkg/backend/datasource.go`, `instance.go` (new)

- [ ] `main.go`: `datasource.Manage("grafana-catalyst-datasource", newInstance, datasource.ManageOpts{})`.
- [ ] `newInstance(ctx, settings)` builds an instance holding: the settings, one HTTP client (WP3.2), one token source (WP3.3) and one site cache (WP3.6).
- [ ] Move `QueryData`, `CheckHealth` and `CallResource` onto the instance.
- [ ] Delete the global `translators` map and the UID-keyed token map.
- [ ] `Dispose()` closes idle connections.

**Done when:**
- Two `PluginContext`s that share a UID but differ in ID/org use separate caches.
- After a change to baseUrl, the TLS flag or the password, the `sites` resource and CheckHealth use the new settings without a restart.

#### WP3.2 Proxy-aware HTTP client from the SDK
**Fixes:** H3, L11 · **Effort:** 0.5 d · **Depends on:** WP3.1

- [ ] `opts, _ := settings.HTTPClientOptions(ctx)`; `opts.TLS = &httpclient.TLSOptions{InsecureSkipVerify: s.InsecureSkipVerify}`; set a 30 s timeout; `httpclient.New(opts)`.
- [ ] Create it once per instance and remove `httpClientFor`.

**Done when:** with `HTTP_PROXY` set to the fake server and a non-resolvable baseUrl host, CheckHealth succeeds.

#### WP3.3 Token source per instance
**Fixes:** L9, L10, L12 (and M8 together with WP3.1) · **Effort:** 1 d · **Depends on:** WP3.1

**Files:** `pkg/backend/token.go`, `token_test.go`

- [ ] `tokenSource{mu, token, expiresAt, group singleflight.Group, lastFailure}`.
  - `Get(ctx)`: concurrent logins share one request.
  - `Invalidate(tok)`: deletes only if `tok` is still the current token (compare-and-delete).
- [ ] Cache a login failure for 30 s.
- [ ] Manual `apiToken`: no refresh retry. The error says "API token rejected or expired (tokens are valid for 60 minutes)".
- [ ] Keep the 55-minute default expiry.

**Done when:** 10 parallel queries on a cold cache make 1 login; a wrong password makes 1 login per 30 s.

#### WP3.4 Authenticated requests for all calls
**Fixes:** M11 · **Effort:** 1 d · **Depends on:** WP2.4, WP3.3

- [ ] Extend `doRequest` into `doAuthed`: add the token; on 401 invalidate that token and retry once; on 403 do not log in again.
- [ ] Use it for every Catalyst Center call (issues, sites, site health, passthrough).
- [ ] Passthrough: map an upstream 401 to 502 with a clear message.

**Done when:** after the fake server revokes all tokens, each call path succeeds with exactly one new login.

#### WP3.5 Health check for 3.1.6
**Fixes:** M9, M10, N3 · **Effort:** 0.5 d · **Depends on:** WP3.3, WP3.4, WP3.6

- [ ] Always log in fresh (unless a manual token is set). Do not touch the shared caches.
- [ ] Pass/fail probe: `GET assuranceIssues?limit=1&offset=1`.
- [ ] Warning probes: `GET /dna/intent/api/v1/sites?limit=1` and `GET /dna/data/api/v1/siteHealthSummaries?limit=1`.
- [ ] A 404 on any of these APIs gives: "This plugin requires Catalyst Center 3.1.6 or later (API <path> not found)".

**Done when:**
- A wrong password → ERROR.
- An unreachable baseUrl → ERROR.
- Issues 403 → ERROR naming the issues API.
- Issues 404 → the version message.
- Site API 403 with issues OK → OK with a warning.

#### WP3.6 Site list on `GET /dna/intent/api/v1/sites`
**Fixes:** N1, L3, L8, M7 (backend part) · **Effort:** 1 d · **Depends on:** WP3.1, WP3.4

**Files:** `pkg/backend/site_translator.go`, `model.go`, `datasource.go` (`sites` resource)

- [ ] Replace `SiteURL` (`/dna/intent/api/v2/site`) with `/dna/intent/api/v1/sites`, paged with `offset` from 1 and `limit=500`.
- [ ] `Site{ID, ParentID, Name, NameHierarchy, Type, SiteHierarchyID}`, read directly from the response.
- [ ] Fetch outside the lock: build new maps, then swap them under the lock. On failure, keep the old data and wait 30 s before retrying.
- [ ] Name → ID: key by `nameHierarchy`. A leaf name that matches several sites returns an "ambiguous site name" error.
- [ ] The `sites` resource returns `id`, `name`, `nameHierarchy`, `type` and `siteHierarchyId`. The frontend reads `name`/`id` as before.

**Done when:**
- Fake-server tests cover paging over 500+ sites and duplicate leaf names.
- No request goes to `/v2/site`.
- The lock is not held during HTTP calls.

#### WP3.7 URL handling
**Fixes:** M13, L13, L14 · **Effort:** 0.5 d · **Depends on:** –

**Files:** `pkg/backend/model.go`, `datasource.go`

- [ ] One builder `endpointURL(base, path)` that clears the query and fragment, used for every endpoint.
- [ ] `dnacPrefix`: when there is no `dna` segment, use the whole path as the prefix.
- [ ] `ParseInstanceSettings`: trim spaces and require an `http` or `https` scheme.
- [ ] Update the ConfigEditor description to show both forms: `https://host` and `https://gw/prefix`.

**Done when:** `model_test` covers a prefix without `/dna`, a trailing slash, an existing `/dna` path, a baseUrl with `?x=1#y`, a value with trailing spaces and one without a scheme. A prefix-mounted fake server passes CheckHealth.

#### Release 1.1.0 checklist
- [ ] CHANGELOG lists:
  - newest-first sorting;
  - site filters include sub-sites;
  - the D6 column changes;
  - settings changes apply without a restart;
  - proxy support;
  - the new health check;
  - the site list now uses `v1/sites`.
- [ ] Manual check on the 3.1.6 lab:
  - change baseUrl, password and the TLS flag without a restart;
  - run behind a proxy;
  - two orgs with the same datasource UID;
  - a multi-site panel;
  - an area selection.

### Phase 4 – Site health on `siteHealthSummaries` (release 1.2.0, D12)

#### WP4.1 Site-health client and frame
**Fixes:** N2, M5, M6, L6 · **Effort:** 1.5 d · **Depends on:** WP2.0, WP3.4, WP3.6, D4, D12

**Files:** `pkg/backend/client.go`, `params.go`, `datasource.go` (`querySiteHealth`), `transformer.go`

- [ ] **Request:** `GET /dna/data/api/v1/siteHealthSummaries` with:
  - `limit=20` and `offset` from 1, paging until a short page;
  - `sortBy=siteHierarchy&order=asc`;
  - `startTime` = the panel's from-time;
  - `endTime` = the panel's to-time, but omitted when it is within 30 minutes of now, so the API returns the latest data.
- [ ] **Site filter:** for each selected site, `siteHierarchyId=<hierarchyId>*` (the site plus descendants, D4).
- [ ] **Site type filter:** repeated `siteType` keys (`area`, `building`, `floor`). Leave it out for all types.
- [ ] Remove the AREA/BUILDING double fetch, `filterSiteHealthData`, `siteMatchesQuery` and `matchSiteNameOrHierarchy`.
- [ ] **Frame:**
  - keep the columns `siteName` (leaf of `siteHierarchy`) and `siteId`;
  - add `siteHierarchy` and `siteType`;
  - then the selected metrics, or all numeric fields when none are selected;
  - skip object and array values.
- [ ] **Errors:** propagate them. A "data not ready for endTime" error is shown with the API's message, which names the recommended `endTime`.

**Done when:** fake-server tests cover a 3-level hierarchy (area selection returns buildings and floors), two same-named buildings (only the selected one), paging over 20+ sites, a 403 (shown as an error), and "data not ready" (a clear message).

#### WP4.2 Metric-name migration
**Fixes:** N2 (compatibility), L5 · **Effort:** 1 d · **Depends on:** WP4.1, WP2.0 (value comparison), WP0.3

**Files:** `pkg/backend/transformer.go` (remove `metricAliases`), `src/migrations.ts`, `src/components/QueryEditor.tsx` (metric options), tests

- [ ] Map saved metric names to the new fields in `migrateQuery` (frontend) and in `normalize()` (backend, for provisioned queries):

  | Old (`site-health`) | New (`siteHealthSummaries`) |
  |---|---|
  | `clientCount`, `numberOfClients` | `clientCount` |
  | `numberOfWiredClients`, `totalNumberOfConnectedWiredClients` | `wiredClientCount` |
  | `numberOfWirelessClients`, `totalNumberOfActiveWirelessClients` | `wirelessClientCount` |
  | `wiredGoodClients` | `wiredClientGoodHealthCount` |
  | `wirelessGoodClients` | `wirelessClientGoodHealthCount` |
  | `healthyClientsPercentage` | `clientGoodHealthPercentage` |
  | `clientHealthWired` / `clientHealthWireless` | `wiredClientGoodHealthPercentage` / `wirelessClientGoodHealthPercentage` |
  | `healthyNetworkDevicePercentage`, `networkHealthAverage` | `networkDeviceGoodHealthPercentage` |
  | `numberOfNetworkDevice` | `networkDeviceCount` |
  | `networkHealthAccess` / `Core` / `AP` / `WLC` / `Switch` | `accessDeviceGoodHealthPercentage` / `coreDeviceGoodHealthPercentage` / `apDeviceGoodHealthPercentage` / `wlcDeviceGoodHealthPercentage` / `switchDeviceGoodHealthPercentage` |
  | `accessTotalCount` / `accessGoodCount` | `accessDeviceCount` / `accessDeviceGoodHealthCount` |
  | `apDeviceTotalCount` / `apDeviceGoodCount` | `apDeviceCount` / `apDeviceGoodHealthCount` |
  | `switchDeviceTotalCount` / `switchDeviceGoodCount` | `switchDeviceCount` / `switchDeviceGoodHealthCount` |

- [ ] Where the new field is only the closest equivalent (the health and score rows), state it in the CHANGELOG. Adjust the table to the WP2.0 value comparison.
- [ ] New metric options in the editor: the fields from section 2, including `p1IssueCount`…`p4IssueCount` and `issueCount`.
- [ ] Unknown saved metric names produce an empty column plus a notice "metric X not available in Catalyst Center 3.1.6".

**Done when:** every old metric in the WP0.3 fixtures maps to a new field, and a saved pre-1.2.0 site-health query renders with values.

#### WP4.3 Site Type and site picker
**Fixes:** L7, M7 (frontend part) · **Effort:** 0.75 d · **Depends on:** WP3.6, WP4.1

- [ ] Site Type becomes a multi-select: Area, Building, Floor (values `area`, `building`, `floor`). Map saved values `AREA`/`BUILDING` to lowercase.
- [ ] The site picker labels options with `nameHierarchy` and shows floors, which are valid in both query types. It sends IDs; `siteName` is kept for display only.

#### WP4.4 Remove the legacy site-health path
**Effort:** 0.5 d · **Depends on:** WP4.1–WP4.3

- [ ] Delete the `/dna/intent/api/v1/site-health` code and its parameters (`timestamp`, `shouldIncludeSiteHealthTimestamp`), the `ParentSiteName`/`ParentSiteID` filters, and the legacy fake-server endpoints.
- [ ] Interim compatibility: a saved `parentSiteName` or `parentSiteId` becomes a site filter on that parent (descendants included), with a notice.

### Phase 5 – Editors and template variables (release 1.2.0)

#### WP5.1 Variable support
**Fixes:** H4, M18 (rest), M19, L25 · **Effort:** 2 d · **Depends on:** WP2.1, WP3.6

**Files:** `src/variables.ts` (new), `src/datasource.ts`, `src/components/VariableQueryEditor.tsx`, `pkg/backend/datasource.go`

- [ ] Use `CustomVariableSupport` with the existing `VariableQueryEditor`.
- [ ] The query handler accepts the object form (stored by v1.0.5) and string forms (`priorities()`, `sites(search:"x")`, plain `priorities`).
- [ ] New backend resource `variables?type=sites|devices|macs&from=&to=&search=`:
  - `sites` comes from the `v1/sites` cache, with `nameHierarchy` as text and `id` as value;
  - `devices` and `macs` come from issue `entityId` grouped by `entityType`;
  - it uses the variable's time range.
- [ ] Interpolate `search`. Remove `uniqueFromIssues` once the new resource is in use.

**Done when:** Jest covers each variable type, and a manual check creates new variables of every type and edits an old v1.0.5 variable.

#### WP5.2 Interpolation helper
**Fixes:** M15, M16 · **Effort:** 1 d · **Depends on:** WP1.4

- [ ] `interpolateList(values, scopedVars)`: call `templateSrv.replace` with a format function that returns JSON, then parse and flatten, so multi-value and "All" variables become arrays.
- [ ] Apply it to every field:
  - lists: `siteId`, `siteName`, `priority`, `status`, `metrics`, `siteType`, `networkDeviceId`, `macAddress`;
  - strings: `issueName`.
- [ ] Make `networkDeviceId` and `macAddress` arrays end to end.

**Done when:** a table test checks every field, and a multi-value `$site` and `$device` expand to arrays.

#### WP5.3 Variables in the pickers
**Fixes:** M17 · **Effort:** 0.75 d · **Depends on:** WP5.2

- [ ] Add dashboard variables as `$name` options to Site, Site Type, Priority, Status and Metrics, and allow custom values.

#### WP5.4 Editor state and behavior
**Fixes:** M20, M21, L15, L16 · **Effort:** 1 d · **Depends on:** WP1.4 (released)

- [ ] Derive the displayed values from `query` props. Keep local state only for debounced text inputs.
- [ ] Initialize every saved field.
- [ ] Call `onRunQuery()` after debounced user changes and after a query-type change, but not on mount.
- [ ] Do not write to the query on mount. This is safe only after WP1.3/WP1.4 have shipped.

**Done when:** Jest shows `onRunQuery` is called once per user change and never on mount, and saved values show after a reload.

#### WP5.5 Smaller editor and config fixes
**Fixes:** L17–L24 · **Effort:** 1 d · **Depends on:** WP5.4, D5

- [ ] **L17:** default `queryType: 'assuranceIssues'`; `'alerts'` shows as Assurance Issues.
- [ ] **L18:** username through `SecretInput` with the "configured" state (D5).
- [ ] **L19:** an input for `issueName`. `parentSiteId` is replaced by the site filter (WP4.4).
- [ ] **L20:** AI-Driven and Global Issues become a Select with Any, Yes and No (works with `TriBool`).
- [ ] **L21, L22:** load site options once and show load errors.
- [ ] **L23:** `filterQuery` returns `!query.hide`.
- [ ] **L24:** rename "API Endpoint" to "Default query type for new panels", or remove it.

#### Release 1.2.0 checklist
- [ ] CHANGELOG:
  - site health now uses `siteHealthSummaries` (floors supported, sub-sites included);
  - the metric-name mapping and the "closest equivalent" notes;
  - variable support and the editor changes.
- [ ] Manual check on the 3.1.6 lab: a saved pre-1.2.0 site-health panel; an area/floor selection; each variable type; `$site` with multi-select.

### Phase 6 – Pipeline, dev setup, docs, cleanup (ongoing)

#### WP6.1 linux/amd64 as a documented limitation
**Fixes:** M22 (accepted per D7) · **Effort:** 0.25 d · **Depends on:** D7

- [ ] Keep the linux/amd64-only build.
- [ ] `docker-compose.yaml`: set `platform: linux/amd64` on the Grafana service, so the dev container runs under emulation on ARM machines (e.g. Apple Silicon) and the plugin loads.
- [ ] `release.yml`: check that `dist/grafana-catalyst-datasource_linux_amd64` exists and is executable before packaging.
- [ ] `src/README.md` requirements: "Grafana on linux/amd64 only".

#### WP6.2 Release pipeline (unsigned)
**Fixes:** L27, L28, L29, L32 (documented per D8) · **Effort:** 0.25 d · **Depends on:** D8

- [ ] Use one version source (the git tag). `release.yml` runs `npm version --allow-same-version`, and `create_release.sh` uses the same value.
- [ ] Use Go 1.24 in `.config/Dockerfile` and in the docs.
- [ ] No signing. Add an installation section to `src/README.md` with `[plugins] allow_loading_unsigned_plugins = grafana-catalyst-datasource`, or the `GF_PLUGINS_ALLOW_LOADING_UNSIGNED_PLUGINS` environment variable. Remove the unused `sign` npm script.

#### WP6.3 Dev environment
**Fixes:** L26, L31, I5 · **Effort:** 0.5 d

- [ ] `provisioning/datasources/datasources.yml`: use `jsonData.baseUrl` and `secureJsonData.username/password` placeholders, read from environment variables.
- [ ] `supervisord.conf`: a working rebuild/watch command, an unstripped debug build for delve, and an anchored `pgrep -f 'dist/grafana-catalyst-datasource_linux_amd64'`.
- [ ] DEVELOPER_GUIDE: run `npm run build && mage` before `docker compose up`.

#### WP6.4 Remove dead code
**Fixes:** I1 · **Effort:** 0.25 d · **Depends on:** WP1.3, WP4.4, WP5.1

- [ ] Delete:
  - `src/cache.ts` and its test;
  - `validateQuery`, `assertValidConfig`, `assertValidQuery`;
  - `formatErrorMessage` (if still unused);
  - `metricOptions`;
  - the `enrich` and `endpoint` query fields (keep `endpoint` until WP5.5 decides);
  - `GetSiteName`;
  - Go's local `func min`;
  - the legacy site-health code left after WP4.4.

  Keep `VariableQueryEditor`, which WP5.1 uses again.

#### WP6.5 Docs and leftovers
**Fixes:** I2, I3, I4, I7 (per D9) · **Effort:** 1 d · **Depends on:** after 1.2.0 features

- [ ] `src/README.md`:
  - requirements: Catalyst Center 3.1.6+, Grafana 12.1+, linux/amd64, unsigned;
  - feature, query editor, site-health metric and variable sections matching 1.2.0.
- [ ] Fix stale claims in `.github/copilot-instructions.md` (API paths `v2/site`/`site-health`, 1000 records per request, page size) and `DEVELOPER_GUIDE.md`.
- [ ] Fix the HelpTooltip link (`kljama` account, Configuration section of the plugin README).
- [ ] Replace the template Playwright specs with real ones, or delete them.
- [ ] README roadmap: alerting not supported for now (D9).

## 6. Schedule (one developer)

| Week | Work | Result |
|---|---|---|
| 1–2 | WP0.1–WP0.4, WP1.1–WP1.5 | Release 1.0.6 |
| 2 (as soon as lab access exists) | WP2.0 | API notes for 3.1.6 |
| 3–5 | WP2.1–WP2.5, WP3.1–WP3.7 (WP2.2 after WP3.6) | Release 1.1.0 |
| 5–7 | WP4.1–WP4.4, WP5.1–WP5.5 | Release 1.2.0 |
| any | WP6.1–WP6.5 | Pipeline, dev setup, docs |

**Critical path:** WP0.1 → WP1.1/WP1.3 → WP1.5 → WP2.4 → WP3.1 → WP3.3 → WP3.4 → WP3.6 → WP2.2 / WP4.1 → WP4.2 → WP5.1.

## 7. Risks

| Risk | Effect | Mitigation |
|---|---|---|
| No 3.1.6 lab, or late access | H5/D3 filter semantics, the C1 server behavior and the WP4.2 value mapping stay unconfirmed | The C1 fix is safe in both cases. WP2.2 and Phase 4 ship only after WP2.0. |
| Site-health metric values differ between the old and new APIs | Dashboards show different numbers after 1.2.0 | Mapping table plus the WP2.0 value comparison; "closest equivalent" notes in the CHANGELOG. |
| `siteHealthSummaries` page size of 20 | More requests for large site trees (rate limit unknown) | Filter server-side by `siteHierarchyId`/`siteType`; measure in WP2.0; reuse the 429 handling from WP2.4. |
| "Data not ready" for a recent `endTime` | Errors on dashboards ending at "now" | Omit `endTime` near "now"; show the API's recommended `endTime` otherwise. |
| `v2/site` removed in a later release | The site list breaks | WP3.6 moves to `v1/sites` in 1.1.0. |
| Phase 3 touches every handler | Regressions | Phase 0 first; keep resource paths and frame shapes; fixture tests in every PR. |
| Column and metric changes (D6, WP4.2) | Panels with explicit column or override settings change | Keep existing column names; add new ones at the end; CHANGELOG. |
| `TriBool` meaning | Old `'false'` vs the new switch-off `false` could be confused | Distinguish by JSON type (WP1.3); covered by fixtures. |
| Editor on-mount write removed too early | Old queries break again | WP5.4 depends on WP1.3/WP1.4 being released. |

## 8. Issue coverage

Every issue in the review, plus the new items N1–N3, maps to a work package.

| Issue | WP | Issue | WP | Issue | WP |
|---|---|---|---|---|---|
| C1 | 1.1 | M11 | 3.4 | L12 | 3.3 |
| C2 | 1.3, 1.4 | M12 | 3.1 | L13 | 3.7 |
| C3 | 1.3, 1.4 | M13 | 3.7 | L14 | 3.7 |
| H1 | 2.1 | M14 | 2.4 | L15 | 5.4 |
| H2 | 3.1 | M15 | 5.2 | L16 | 5.4 |
| H3 | 3.2 | M16 | 5.2 | L17–L24 | 5.5 |
| H4 | 5.1 | M17 | 5.3 | L25 | 5.1 |
| H5 | 2.0, 2.2 | M18 | 1.2, 5.1 | L26 | 6.3 |
| M1 | 2.3 | M19 | 5.1 | L27 | 6.2 |
| M2 | 2.1 | M20 | 5.4 | L28 | 6.2 |
| M3 | 2.1 | M21 | 5.4 | L29 | 0.4, 6.2 |
| M4 | 2.4 | M22 | 6.1 (accepted, D7) | L30 | 0.4 |
| M5 | 4.1 | L1 | 2.5 | L31 | 6.3 |
| M6 | 4.1 | L2 | 2.5 | L32 | 6.2 (documented, D8) |
| M7 | 3.6, 4.3 | L3 | 3.6 | L33 | 0.2 |
| M8 | 3.1, 3.3 | L4 | 2.3 | I1 | 0.2, 6.4 |
| M9 | 3.5 | L5 | 4.2 | I2 | 2.1, 6.5 |
| M10 | 3.5 | L6 | 4.1 | I3 | 6.5 |
| N1 | 3.6 | L7 | 4.3 | I4 | 6.5 |
| N2 | 4.1–4.4 | L8 | 3.6 | I5 | 6.3 |
| N3 | 3.5 | L9 | 3.3 | I6 | 3.1 |
| | | L10 | 3.3 | I7 | 6.5 (D9) |
| | | L11 | 3.2 | | |

**New items found while adapting to 3.1.6:**

| ID | Sev | Issue |
|---|---|---|
| N1 | Medium | The site list uses `GET /dna/intent/api/v2/site`, deprecated since 2.3.7.6 and marked "Sunset" in the 3.1.6 docs. Site names, the site picker and site filters break when it is removed. |
| N2 | Medium | Site health uses the legacy `GET /dna/intent/api/v1/site-health`: AREA/BUILDING only, one snapshot, no server-side site filter. 3.1.6 provides `siteHealthSummaries`, with floors, hierarchy filters, sorting and issue counts. |
| N3 | Low | Nothing checks for the minimum Catalyst Center release. Older systems fail with unclear 404 errors. |
