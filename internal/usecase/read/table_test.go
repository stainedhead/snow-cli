package read_test

import (
	"context"
	"testing"

	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/agent-cli-core/policy"
	"github.com/stainedhead/snow-cli/internal/domain"
	"github.com/stainedhead/snow-cli/internal/usecase/read"
)

const incidentOnly = `
version: 1
rules:
  - id: inc
    effect: allow
    verbs: [get, list, count]
    resources: ["table:incident"]
    fields: [number, short_description, state, sys_id]
`

func incidentTables() *fakeTables {
	return &fakeTables{records: map[string][]domain.Record{"incident": {
		rec("incident", "sys_id", sid1, "number", "INC1", "short_description", "disk full"),
		rec("incident", "sys_id", sid2, "number", "INC2", "short_description", "cpu"),
	}}}
}

func TestTableGetChecksPolicyWithFieldsAndReturnsRecord(t *testing.T) {
	ft, g := incidentTables(), newGuard(t, incidentOnly)
	got, err := svc(t, ft, g).TableGet(context.Background(), "incident", sid1, read.Options{Fields: []string{"number", "short_description"}})
	if err != nil {
		t.Fatal(err)
	}
	if got["number"] != "INC1" {
		t.Errorf("got %v", got)
	}
	r := g.Requests[0]
	if r.Verb != "get" || r.Resource != "table:incident" || len(r.Fields) != 2 {
		t.Errorf("request = %+v", r)
	}
	if ft.gets[0].Table != "incident" || ft.gets[0].SysID != sid1 {
		t.Errorf("get = %+v", ft.gets[0])
	}
}

func TestTableGetDeniedTableAndFieldNeverHitReader(t *testing.T) {
	ft, g := incidentTables(), newGuard(t, incidentOnly)
	s := svc(t, ft, g)
	if _, err := s.TableGet(context.Background(), "sys_user", sid1, read.Options{}); exitOf(t, err) != output.ExitPolicyDenied {
		t.Errorf("denied table exit = %d", output.ExitOf(err))
	}
	if _, err := s.TableGet(context.Background(), "incident", sid1, read.Options{Fields: []string{"caller_id"}}); exitOf(t, err) != output.ExitPolicyDenied {
		t.Errorf("field outside allowlist exit = %d", output.ExitOf(err))
	}
	if len(ft.gets) != 0 {
		t.Error("reader must not be called after a denial")
	}
}

func TestTableGetValidatesInput(t *testing.T) {
	s := svc(t, incidentTables(), newGuard(t, allowAll))
	for name, tc := range map[string]struct{ table, id string }{
		"bad table":   {"Incident; drop", sid1},
		"empty table": {"", sid1},
		"bad sys_id":  {"incident", "INC123"},
	} {
		if _, err := s.TableGet(context.Background(), tc.table, tc.id, read.Options{}); exitOf(t, err) != output.ExitValidation {
			t.Errorf("%s: exit = %d", name, output.ExitOf(err))
		}
	}
}

func TestTableGetPropagatesReaderErrors(t *testing.T) {
	ft := incidentTables()
	s := svc(t, ft, newGuard(t, allowAll))
	if _, err := s.TableGet(context.Background(), "incident", sid3, read.Options{}); exitOf(t, err) != output.ExitNotFound {
		t.Errorf("exit = %d", output.ExitOf(err))
	}
}

func TestFieldSubstitutionUsesPolicyAllowlistThenDefaults(t *testing.T) {
	// With an allowlist hook the policy fields are sent; the reader never sees an
	// empty (all fields) selection.
	ft, g := incidentTables(), newGuard(t, incidentOnly)
	s := svc(t, ft, allowlistGuard{g, []string{"number", "state"}})
	if _, err := s.TableGet(context.Background(), "incident", sid1, read.Options{}); err != nil {
		t.Fatal(err)
	}
	if got := sorted(ft.gets[0].Opts.Fields); len(got) != 2 || got[0] != "number" || got[1] != "state" {
		t.Errorf("substituted fields = %v", got)
	}
	// Without the hook, defaults are sent (sysparm_fields is always set).
	ft2 := incidentTables()
	s2 := svc(t, ft2, newGuard(t, allowAll))
	if _, err := s2.TableGet(context.Background(), "incident", sid1, read.Options{}); err != nil {
		t.Fatal(err)
	}
	if len(ft2.gets[0].Opts.Fields) == 0 {
		t.Error("an empty field list would read every column")
	}
}

func TestTableListShapePageAndOrdering(t *testing.T) {
	ft := incidentTables()
	ft.total = intp(10)
	g := newGuard(t, allowAll)
	got, err := svc(t, ft, g).TableList(context.Background(), "incident", read.ListOptions{
		Fields: []string{"number"}, Limit: 2, Offset: 4, Query: "active=true", OrderBy: "-number",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 2 || got.Page.Offset != 4 || got.Page.Returned != 2 || *got.Page.Total != 10 ||
		got.Page.NextOffset == nil || *got.Page.NextOffset != 6 || got.ACLFilteredPossible {
		t.Errorf("got %+v", got)
	}
	q := ft.lists[0]
	if q.Table != "incident" || q.Query != "active=true" || q.Limit != 2 || q.Offset != 4 ||
		q.OrderBy != "ORDERBYDESCnumber^ORDERBYsys_id" {
		t.Errorf("list query = %+v", q)
	}
	if g.Requests[0].Verb != "list" {
		t.Errorf("verb = %s", g.Requests[0].Verb)
	}
}

func TestTableListOrderByAscendingAndSysID(t *testing.T) {
	for in, want := range map[string]string{
		"number":  "ORDERBYnumber^ORDERBYsys_id",
		"-number": "ORDERBYDESCnumber^ORDERBYsys_id",
		"sys_id":  "ORDERBYsys_id",
		"":        "",
	} {
		ft := incidentTables()
		if _, err := svc(t, ft, newGuard(t, allowAll)).TableList(context.Background(), "incident", read.ListOptions{OrderBy: in}); err != nil {
			t.Fatal(err)
		}
		if ft.lists[0].OrderBy != want {
			t.Errorf("order %q -> %q, want %q", in, ft.lists[0].OrderBy, want)
		}
	}
	if _, err := svc(t, incidentTables(), newGuard(t, allowAll)).TableList(context.Background(), "incident", read.ListOptions{OrderBy: "a;b"}); exitOf(t, err) != output.ExitValidation {
		t.Error("bad order field must be a validation error")
	}
}

func TestTableListDefaultAndClampedLimit(t *testing.T) {
	ft := incidentTables()
	s := svc(t, ft, newGuard(t, allowAll))
	if _, err := s.TableList(context.Background(), "incident", read.ListOptions{}); err != nil {
		t.Fatal(err)
	}
	if ft.lists[0].Limit != read.DefaultLimit {
		t.Errorf("default limit = %d", ft.lists[0].Limit)
	}
	s.Limits = policy.Limits{MaxResults: 5}
	if _, err := s.TableList(context.Background(), "incident", read.ListOptions{Limit: 500}); err != nil {
		t.Fatal(err)
	}
	if ft.lists[1].Limit != 5 {
		t.Errorf("clamped limit = %d", ft.lists[1].Limit)
	}
}

func TestTableListACLFilteredPossible(t *testing.T) {
	// Empty page while the total says more exist (PRD 7.1).
	ft := &fakeTables{total: intp(30), records: map[string][]domain.Record{"incident": nil}}
	got, err := svc(t, ft, newGuard(t, allowAll)).TableList(context.Background(), "incident", read.ListOptions{Limit: 10, Offset: 0})
	if err != nil {
		t.Fatal(err)
	}
	if !got.ACLFilteredPossible || got.Page.NextOffset != nil || len(got.Items) != 0 {
		t.Errorf("got %+v", got)
	}
	// Full last page: no flag.
	ft = &fakeTables{total: intp(2), records: map[string][]domain.Record{"incident": {rec("incident", "number", "1"), rec("incident", "number", "2")}}}
	got, _ = svc(t, ft, newGuard(t, allowAll)).TableList(context.Background(), "incident", read.ListOptions{Limit: 2})
	if got.ACLFilteredPossible || got.Page.NextOffset != nil {
		t.Errorf("exhausted set must not flag: %+v", got)
	}
	// Items slice is never nil so JSON shows [].
	ft = &fakeTables{records: map[string][]domain.Record{}}
	got, _ = svc(t, ft, newGuard(t, allowAll)).TableList(context.Background(), "incident", read.ListOptions{})
	if got.Items == nil {
		t.Error("items must be a non-nil empty slice")
	}
}

func TestTableListRejectsScriptQueries(t *testing.T) {
	for _, q := range []string{"assigned_to=javascript:gs.getUserID()", "name=<SCRIPT>alert(1)</script>", "a=JavaScript:1"} {
		ft := incidentTables()
		_, err := svc(t, ft, newGuard(t, allowAll)).TableList(context.Background(), "incident", read.ListOptions{Query: q})
		if exitOf(t, err) != output.ExitValidation || len(ft.lists) != 0 {
			t.Errorf("query %q: exit %d, lists %d", q, output.ExitOf(err), len(ft.lists))
		}
	}
}

func TestTableListPropagatesErrors(t *testing.T) {
	ft := &fakeTables{err: errBoom}
	if _, err := svc(t, ft, newGuard(t, allowAll)).TableList(context.Background(), "incident", read.ListOptions{}); err != errBoom {
		t.Errorf("err = %v", err)
	}
}

func TestTableCount(t *testing.T) {
	ft, g := incidentTables(), newGuard(t, incidentOnly)
	got, err := svc(t, ft, g).TableCount(context.Background(), "incident", "active=true")
	if err != nil || got.Count != 42 || got.Table != "incident" {
		t.Fatalf("got %+v err %v", got, err)
	}
	if g.Requests[0].Verb != "count" || g.Requests[0].Resource != "table:incident" {
		t.Errorf("request = %+v", g.Requests[0])
	}
	if ft.counts[0] != "incident|active=true" {
		t.Errorf("count call = %v", ft.counts)
	}
	if _, err := svc(t, ft, g).TableCount(context.Background(), "sys_user", ""); exitOf(t, err) != output.ExitPolicyDenied {
		t.Error("denied count must be exit 6")
	}
	if _, err := svc(t, ft, g).TableCount(context.Background(), "incident", "x=javascript:1"); exitOf(t, err) != output.ExitValidation {
		t.Error("script query must be rejected")
	}
	ft.err = errBoom
	if _, err := svc(t, ft, g).TableCount(context.Background(), "incident", ""); err != errBoom {
		t.Errorf("err = %v", err)
	}
}
