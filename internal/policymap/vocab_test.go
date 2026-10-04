package policymap

import (
	"sort"
	"testing"

	"github.com/stainedhead/agent-cli-core/policy"
)

func TestVerbVocabulary(t *testing.T) {
	want := []string{"count", "create", "get", "list", "order", "related", "resolve", "search", "selftest", "update", "vars", "whoami"}
	got := append([]string(nil), Verbs()...)
	sort.Strings(got)
	if len(got) != len(want) {
		t.Fatalf("verbs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("verbs = %v, want %v", got, want)
		}
	}
	if VerbGet != "get" || VerbOrder != "order" || VerbResolve != "resolve" {
		t.Error("verb constants drifted")
	}
}

func TestResourceBuilders(t *testing.T) {
	tests := []struct{ got, want string }{
		{Table("incident"), "table:incident"},
		{CatalogItem("abc"), "catalog:item:abc"},
		{ResCMDBCI, "cmdb:ci"},
		{ResCMDBApp, "cmdb:app"},
		{ResIncident, "incident"},
		{ResRequest, "request"},
		{ResRITM, "ritm"},
		{ResTask, "task"},
		{ResChange, "change"},
		{ResProblem, "problem"},
		{ResCatalogSearch, "catalog:search"},
		{ResWhoami, "whoami"},
		{ResSelftest, "selftest"},
	}
	for _, tc := range tests {
		if tc.got != tc.want {
			t.Errorf("got %q want %q", tc.got, tc.want)
		}
	}
}

func TestIsVerb(t *testing.T) {
	if !IsVerb("get") || IsVerb("delete") || IsVerb("") {
		t.Error("IsVerb")
	}
}

func TestIsResource(t *testing.T) {
	good := []string{"table:incident", "cmdb:ci", "incident", "catalog:item:0123", "catalog:search", "whoami", "table:cmdb_ci*"}
	for _, r := range good {
		if !IsResource(r) {
			t.Errorf("IsResource(%q) = false", r)
		}
	}
	for _, r := range []string{"", "table:", "sys_user", "catalog:item:", "raw:rest"} {
		if IsResource(r) {
			t.Errorf("IsResource(%q) = true", r)
		}
	}
}

func TestRequestBuilderSkeleton(t *testing.T) {
	r := WithFields(NewRequest(VerbGet, Table("incident")), "number", "state")
	want := policy.Request{Verb: "get", Resource: "table:incident", Fields: map[string]any{"number": nil, "state": nil}}
	if r.Verb != want.Verb || r.Resource != want.Resource || len(r.Fields) != 2 {
		t.Errorf("request = %+v", r)
	}
	if _, ok := r.Fields["number"]; !ok {
		t.Error("field names must be keys of Fields")
	}
	w := WithValues(NewRequest(VerbCreate, ResIncident), map[string]any{"impact": 2})
	if w.Fields["impact"] != 2 {
		t.Errorf("values = %+v", w.Fields)
	}
	if !contains(Verbs(), VerbWhoami) {
		t.Error("whoami verb")
	}
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
