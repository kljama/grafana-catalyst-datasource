package backend

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestStringList_Unmarshal(t *testing.T) {
	cases := []struct {
		in   string
		want StringList
	}{
		{`null`, nil},
		{`""`, nil},
		{`"  "`, nil},
		{`"site-1"`, StringList{"site-1"}},
		{`["a", "", " b "]`, StringList{"a", "b"}},
		{`[]`, nil},
		{`[25]`, StringList{"25"}},
	}
	for _, c := range cases {
		var got StringList
		if err := json.Unmarshal([]byte(c.in), &got); err != nil {
			t.Errorf("%s: unexpected error %v", c.in, err)
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %#v, want %#v", c.in, got, c.want)
		}
	}
	var bad StringList
	if err := json.Unmarshal([]byte(`{"a":1}`), &bad); err == nil {
		t.Error("object: expected an error")
	}
}

func TestTriBool_Unmarshal(t *testing.T) {
	cases := []struct {
		in        string
		wantValue string
		wantSet   bool
	}{
		{`true`, "true", true},
		{`false`, "", false}, // current editor: switch off = no filter
		{`null`, "", false},
		{`"true"`, "true", true},
		{`"false"`, "false", true}, // v1.0.x: explicit "only non-AI issues"
		{`"YES"`, "true", true},
		{`"no"`, "false", true},
		{`""`, "", false},
		{`"maybe"`, "", false},
	}
	for _, c := range cases {
		var tb TriBool
		if err := json.Unmarshal([]byte(c.in), &tb); err != nil {
			t.Errorf("%s: unexpected error %v", c.in, err)
			continue
		}
		if v, ok := tb.Param(); v != c.wantValue || ok != c.wantSet {
			t.Errorf("%s: Param() = (%q, %v), want (%q, %v)", c.in, v, ok, c.wantValue, c.wantSet)
		}
	}

	// Absent field: unset.
	var qm QueryModel
	if err := json.Unmarshal([]byte(`{"queryType":"assuranceIssues"}`), &qm); err != nil {
		t.Fatal(err)
	}
	if _, ok := qm.AIDriven.Param(); ok {
		t.Error("absent aiDriven: expected unset")
	}
}

func TestQueryModel_Normalize(t *testing.T) {
	decode := func(js string) QueryModel {
		t.Helper()
		var qm QueryModel
		if err := json.Unmarshal([]byte(js), &qm); err != nil {
			t.Fatalf("%s: %v", js, err)
		}
		qm.normalize()
		return qm
	}

	qm := decode(`{"issueStatus":"ACTIVE","severity":"P3","deviceId":"00000000-0000-4000-8000-000000000101"}`)
	if !reflect.DeepEqual(qm.Status, StringList{"ACTIVE"}) || !reflect.DeepEqual(qm.Priority, StringList{"P3"}) {
		t.Errorf("legacy issueStatus/severity not mapped: status=%v priority=%v", qm.Status, qm.Priority)
	}
	if qm.NetworkDeviceID != "00000000-0000-4000-8000-000000000101" || qm.DeviceID != "" {
		t.Errorf("UUID deviceId not mapped: %+v", qm)
	}

	// Current fields win over legacy ones.
	qm = decode(`{"status":["resolved"],"issueStatus":"ACTIVE","priority":["P1"],"severity":"P3"}`)
	if !reflect.DeepEqual(qm.Status, StringList{"resolved"}) || !reflect.DeepEqual(qm.Priority, StringList{"P1"}) {
		t.Errorf("current fields overwritten: status=%v priority=%v", qm.Status, qm.Priority)
	}

	qm = decode(`{"deviceId":"10.10.20.5"}`)
	if qm.NetworkDeviceIP != "10.10.20.5" || qm.NetworkDeviceID != "" {
		t.Errorf("IP deviceId not mapped: %+v", qm)
	}

	qm = decode(`{"deviceId":"sw-wien-01"}`)
	if qm.NetworkDeviceID != "" || qm.NetworkDeviceIP != "" || len(qm.notices) != 1 || !strings.Contains(qm.notices[0], "deviceId") {
		t.Errorf("unrecognized deviceId: want dropped with one notice, got %+v", qm)
	}
}
