package backend

// Saved-query fixtures (testdata/queries, see its README.md): every query shape
// that dashboards may contain, and what the backend must send for it.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

type queryFixture struct {
	Name        string          `json:"name"`
	Source      string          `json:"source"`
	Pending     string          `json:"pending"`
	Description string          `json:"description"`
	Query       json.RawMessage `json:"query"`
	Expect      struct {
		Params       map[string][]string `json:"params"`
		AbsentParams []string            `json:"absentParams"`
		Sites        []string            `json:"sites"`
		Rows         *int                `json:"rows"`
		MaxRows      *int                `json:"maxRows"`
		Notice       string              `json:"notice"`
		Metrics      []string            `json:"metrics"`
	} `json:"expect"`

	file string
}

func loadQueryFixtures(t *testing.T) []queryFixture {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("testdata", "queries", "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no query fixtures found: %v", err)
	}
	var out []queryFixture
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var fx queryFixture
		dec := json.NewDecoder(strings.NewReader(string(raw)))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&fx); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		fx.file = file
		out = append(out, fx)
	}
	return out
}

func TestQueryFixtures_WellFormed(t *testing.T) {
	for _, fx := range loadQueryFixtures(t) {
		if want := strings.TrimSuffix(filepath.Base(fx.file), ".json"); fx.Name != want {
			t.Errorf("%s: name %q does not match the file name", fx.file, fx.Name)
		}
		if fx.Source == "" || fx.Description == "" {
			t.Errorf("%s: source and description are required", fx.file)
		}
		var q map[string]any
		if err := json.Unmarshal(fx.Query, &q); err != nil {
			t.Errorf("%s: query is not a JSON object: %v", fx.file, err)
			continue
		}
		if q["refId"] == nil || q["queryType"] == nil {
			t.Errorf("%s: query needs refId and queryType", fx.file)
		}
		e := fx.Expect
		if e.Params == nil && e.AbsentParams == nil && e.Sites == nil && e.Rows == nil && e.MaxRows == nil && e.Notice == "" && e.Metrics == nil {
			t.Errorf("%s: expect is empty", fx.file)
		}
	}
}

// TestQueryFixtures_Backend sends each fixture through QueryData against the
// fake Catalyst Center (strict limit mode) and checks its expectations.
// Fixtures with "pending" set are skipped until that work package lands; set
// FIXTURES_RUN_PENDING=1 to run them anyway while working on that package.
func TestQueryFixtures_Backend(t *testing.T) {
	runPending := os.Getenv("FIXTURES_RUN_PENDING") == "1"
	for _, fx := range loadQueryFixtures(t) {
		fx := fx
		t.Run(fx.Name, func(t *testing.T) {
			if fx.Pending != "" && !runPending {
				t.Skipf("pending %s", fx.Pending)
			}
			f := newFakeCC(t)
			f.LimitMode = limitStrict
			f.addIssues(150, f.site("Global/Österreich/Wien/HQ"))

			var q map[string]any
			if err := json.Unmarshal(fx.Query, &q); err != nil {
				t.Fatal(err)
			}
			dr := f.runQuery(t, NewDatasource(), "fixture-"+fx.Name, q)
			if dr.Error != nil {
				t.Fatalf("query error: %v", dr.Error)
			}

			ep := epIssues
			if q["queryType"] == "siteHealth" {
				ep = epSiteHealthSummaries
			}
			reqs := f.Requests(ep)
			if len(reqs) == 0 {
				t.Fatalf("no %s request", ep)
			}
			for _, r := range reqs {
				for name, want := range fx.Expect.Params {
					got := append([]string(nil), r.Query[name]...)
					want = append([]string(nil), want...)
					sort.Strings(got)
					sort.Strings(want)
					if strings.Join(got, ",") != strings.Join(want, ",") {
						t.Errorf("param %s = %v, want %v (request %v)", name, got, want, r.Query)
					}
				}
				for _, name := range fx.Expect.AbsentParams {
					if r.Query.Has(name) {
						t.Errorf("param %s must be absent, got %v", name, r.Query[name])
					}
				}
				for _, id := range fx.Expect.Sites {
					s, ok := f.siteByID(id)
					if !ok {
						t.Fatalf("expect.sites: %s is not a fake site", id)
					}
					if !f.filtersSite(r, s) {
						t.Errorf("request is not restricted to site %s: %v", id, r.Query)
					}
				}
			}

			rows := frameRows(dr)
			if e := fx.Expect.Rows; e != nil && rows != *e {
				t.Errorf("rows = %d, want %d", rows, *e)
			}
			if e := fx.Expect.MaxRows; e != nil && rows > *e {
				t.Errorf("rows = %d, want at most %d", rows, *e)
			}
			if fx.Expect.Notice != "" && !frameHasNotice(dr, fx.Expect.Notice) {
				t.Errorf("no frame notice contains %q", fx.Expect.Notice)
			}
			for _, m := range fx.Expect.Metrics {
				if len(dr.Frames) == 0 {
					t.Fatal("no frame")
				}
				if field, _ := dr.Frames[0].FieldByName(m); field == nil {
					t.Errorf("frame has no metric field %q", m)
				}
			}
		})
	}
}

func frameHasNotice(dr backend.DataResponse, text string) bool {
	for _, fr := range dr.Frames {
		if fr.Meta == nil {
			continue
		}
		for _, n := range fr.Meta.Notices {
			if strings.Contains(n.Text, text) {
				return true
			}
		}
	}
	return false
}
