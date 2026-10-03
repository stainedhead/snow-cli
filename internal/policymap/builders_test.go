package policymap

import (
	"reflect"
	"sort"
	"testing"

	"github.com/stainedhead/agent-cli-core/policy"
)

func fieldNames(r policy.Request) []string {
	var n []string
	for k := range r.Fields {
		n = append(n, k)
	}
	sort.Strings(n)
	return n
}

func TestReadBuilders(t *testing.T) {
	tests := []struct {
		name     string
		got      policy.Request
		verb     string
		resource string
		fields   []string
	}{
		{"table get", TableGet("incident", "number", "state"), VerbGet, "table:incident", []string{"number", "state"}},
		{"table list", TableList("sc_task", "number"), VerbList, "table:sc_task", []string{"number"}},
		{"table count", TableCount("incident"), VerbCount, "table:incident", nil},
		{"ci get", CIGet("name"), VerbGet, ResCMDBCI, []string{"name"}},
		{"ci search", CISearch("name", "sys_class_name"), VerbSearch, ResCMDBCI, []string{"name", "sys_class_name"}},
		{"ci related", CIRelated("parent"), VerbRelated, ResCMDBCI, []string{"parent"}},
		{"app", AppGet("name"), VerbGet, ResCMDBApp, []string{"name"}},
		{"incident get", WorkGet(ResIncident, "number"), VerbGet, ResIncident, []string{"number"}},
		{"request list", WorkList(ResRequest, "number"), VerbList, ResRequest, []string{"number"}},
		{"ritm list", WorkList(ResRITM), VerbList, ResRITM, nil},
		{"my work", WorkList(ResTask, "number"), VerbList, ResTask, []string{"number"}},
		{"catalog search", CatalogSearch(), VerbSearch, ResCatalogSearch, nil},
		{"catalog get", CatalogGet("abc"), VerbGet, "catalog:item:abc", nil},
		{"catalog vars", CatalogVars("abc"), VerbVars, "catalog:item:abc", nil},
		{"whoami", Whoami(), VerbWhoami, ResWhoami, nil},
		{"selftest", Selftest(), VerbSelftest, ResSelftest, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got.Verb != tc.verb || tc.got.Resource != tc.resource {
				t.Errorf("got %s %s want %s %s", tc.got.Verb, tc.got.Resource, tc.verb, tc.resource)
			}
			if got := fieldNames(tc.got); !reflect.DeepEqual(got, tc.fields) {
				t.Errorf("fields = %v want %v", got, tc.fields)
			}
			if !IsVerb(tc.got.Verb) || !IsResource(tc.got.Resource) {
				t.Error("builder produced a request outside the vocabulary")
			}
		})
	}
}

func TestWriteBuilders(t *testing.T) {
	vals := map[string]any{"impact": 2, "work_notes": "n"}
	tests := []struct {
		name     string
		got      policy.Request
		verb     string
		resource string
	}{
		{"create", IncidentCreate(vals), VerbCreate, ResIncident},
		{"update", IncidentUpdate(vals), VerbUpdate, ResIncident},
		{"resolve", IncidentResolve(map[string]any{"close_code": "x"}), VerbResolve, ResIncident},
		{"task", TaskUpdate(vals), VerbUpdate, ResTask},
		{"order", CatalogOrder("abc", map[string]any{"v": "1"}), VerbOrder, "catalog:item:abc"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got.Verb != tc.verb || tc.got.Resource != tc.resource {
				t.Errorf("got %s %s", tc.got.Verb, tc.got.Resource)
			}
			if len(tc.got.Fields) == 0 {
				t.Error("write requests carry values")
			}
		})
	}
	// Values are copied so later caller mutation cannot change the request.
	req := IncidentCreate(vals)
	vals["impact"] = 1
	if req.Fields["impact"] != 2 {
		t.Error("builder aliased the caller's map")
	}
}

func TestIncidentCreateNeverCarriesPriority(t *testing.T) {
	r := IncidentCreate(map[string]any{"impact": 2, "priority": 1})
	if _, ok := r.Fields["priority"]; !ok {
		t.Fatal("priority must remain visible so the policy can deny it")
	}
}
