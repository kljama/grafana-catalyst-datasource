# Saved-query fixtures

Each file is one query as Grafana stores it in a dashboard, plus what the backend
must send to Catalyst Center for it. The Go tests (`pkg/backend/fixtures_test.go`)
and the Jest tests (`src/__fixtures__/queries.ts`) both read these files. Work
package WP0.3 in `ACTION_PLAN.md` describes their purpose.

## Format

```json
{
  "name": "v1x-site-string",
  "source": "Saved by the v1.0.4/v1.0.5 query editor",
  "pending": "WP1.3",
  "description": "What the fixture covers.",
  "query": { "refId": "A", "queryType": "alerts", "siteId": "..." },
  "expect": { "sites": ["..."], "maxRows": 25 }
}
```

| Field | Meaning |
|---|---|
| `name` | Must equal the file name without `.json`. |
| `source` | Where this query shape comes from (v1.0.x editor, current editor, provisioning, hand-edited JSON). |
| `pending` | Work package that makes the fixture pass. The Go runner skips the fixture while this is set. Remove it in the PR that implements the work package. |
| `query` | The saved query JSON, sent unchanged to `QueryData`. |
| `expect` | What must hold. All keys are optional. |

`expect` keys. They apply to the requests for the query's API: assuranceIssues for
issue queries, siteHealthSummaries for `queryType: "siteHealth"`.

| Key | Check |
|---|---|
| `params` | Every request carries exactly these values for each listed parameter (order ignored). |
| `absentParams` | No request carries any of these parameters. |
| `sites` | Every request is restricted to each of these site IDs, by `siteId` or by a `siteHierarchyId` pattern that covers the site. |
| `rows` | The result frame has exactly this many rows. |
| `maxRows` | The result frame has at most this many rows. |
| `notice` | A frame notice contains this text. |
| `metrics` | The result frame has a field with each of these names. |

The fake Catalyst Center (`pkg/backend/fakecc_test.go`) provides the data: its
default site tree and issues, plus 150 generated active P2 issues at
`Global/Österreich/Wien/HQ` (site `00000000-0000-4000-8000-000000000004`). It runs
in strict limit mode, which rejects page sizes above the API maximum.
