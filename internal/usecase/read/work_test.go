package read_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/snow-cli/internal/domain"
	"github.com/stainedhead/snow-cli/internal/usecase/read"
)

func TestParseKind(t *testing.T) {
	for _, k := range []string{"incident", "request", "ritm", "task", "problem", "change"} {
		if got, err := read.ParseKind(k); err != nil || string(got) != k {
			t.Errorf("%s: %v %v", k, got, err)
		}
	}
	if _, err := read.ParseKind("approval"); exitOf(t, err) != output.ExitValidation {
		t.Error("unknown kind must be a validation error")
	}
}

func TestWorkGetByNumberUsesKindTableAndResource(t *testing.T) {
	cases := []struct {
		kind  read.Kind
		num   string
		table string
		res   string
	}{
		{read.KindIncident, "inc0010001", "incident", "incident"},
		{read.KindRequest, "REQ0000001", "sc_request", "request"},
		{read.KindRITM, "RITM0000001", "sc_req_item", "ritm"},
		{read.KindTask, "SCTASK0000001", "sc_task", "task"},
		{read.KindProblem, "PRB0000001", "problem", "problem"},
		{read.KindChange, "CHG0000001", "change_request", "change"},
	}
	for _, tc := range cases {
		ft := &fakeTables{records: map[string][]domain.Record{tc.table: {rec(tc.table, "sys_id", sid1, "number", strings.ToUpper(tc.num))}}}
		g := newGuard(t, allowAll)
		got, err := svc(t, ft, g).WorkGet(context.Background(), tc.kind, tc.num, read.Options{})
		if err != nil || got["number"] != strings.ToUpper(tc.num) {
			t.Fatalf("%s: %v %v", tc.kind, got, err)
		}
		q := ft.lists[0]
		if q.Table != tc.table || q.Query != "number="+strings.ToUpper(tc.num) || q.Limit != 1 {
			t.Errorf("%s: list = %+v", tc.kind, q)
		}
		if g.Requests[0].Verb != "get" || g.Requests[0].Resource != tc.res {
			t.Errorf("%s: request = %+v", tc.kind, g.Requests[0])
		}
	}
}

func TestWorkGetBySysID(t *testing.T) {
	ft := &fakeTables{records: map[string][]domain.Record{"incident": {rec("incident", "sys_id", sid1, "number", "INC1")}}}
	got, err := svc(t, ft, newGuard(t, allowAll)).WorkGet(context.Background(), read.KindIncident, sid1, read.Options{Fields: []string{"number"}})
	if err != nil || got["number"] != "INC1" || len(ft.gets) != 1 || len(ft.lists) != 0 {
		t.Fatalf("got %v err %v gets %d", got, err, len(ft.gets))
	}
}

func TestWorkGetErrors(t *testing.T) {
	s := svc(t, &fakeTables{records: map[string][]domain.Record{}}, newGuard(t, allowAll))
	ctx := context.Background()
	if _, err := s.WorkGet(ctx, read.KindIncident, "INC0000009", read.Options{}); exitOf(t, err) != output.ExitNotFound {
		t.Errorf("missing record exit = %d", output.ExitOf(err))
	}
	if _, err := s.WorkGet(ctx, read.KindIncident, "REQ0000001", read.Options{}); exitOf(t, err) != output.ExitValidation {
		t.Errorf("wrong kind exit = %d", output.ExitOf(err))
	}
	if _, err := s.WorkGet(ctx, read.KindIncident, "garbage", read.Options{}); exitOf(t, err) != output.ExitValidation {
		t.Errorf("bad ref exit = %d", output.ExitOf(err))
	}
	s.Tables = &fakeTables{err: errBoom}
	if _, err := s.WorkGet(ctx, read.KindIncident, "INC1", read.Options{}); err != errBoom {
		t.Errorf("err = %v", err)
	}
}

func TestWorkGetDeniedByPolicy(t *testing.T) {
	pol := "version: 1\nrules:\n  - {id: r, effect: allow, verbs: [get], resources: [incident]}\n"
	ft := &fakeTables{}
	_, err := svc(t, ft, newGuard(t, pol)).WorkGet(context.Background(), read.KindProblem, "PRB1", read.Options{})
	if exitOf(t, err) != output.ExitPolicyDenied || len(ft.lists) != 0 {
		t.Errorf("exit %d lists %d", output.ExitOf(err), len(ft.lists))
	}
}

func TestWorkListFilters(t *testing.T) {
	ft := &fakeTables{records: map[string][]domain.Record{}}
	s := svc(t, ft, newGuard(t, allowAll))
	_, err := s.WorkList(context.Background(), read.KindIncident, read.WorkFilter{
		Mine: true, CI: "web01", App: sid2, Group: "Service Desk", State: "2",
	}, read.ListOptions{Query: "priority=1"})
	if err != nil {
		t.Fatal(err)
	}
	want := "assigned_to.user_name=agent.bot^cmdb_ci.name=web01^business_service=" + sid2 + "^assignment_group.name=Service Desk^state=2^priority=1"
	if ft.lists[0].Query != want {
		t.Errorf("query = %q\nwant    %q", ft.lists[0].Query, want)
	}
	if ft.lists[0].Table != "incident" {
		t.Errorf("table = %s", ft.lists[0].Table)
	}
}

func TestWorkListMineOnRequestUsesRequestedFor(t *testing.T) {
	ft := &fakeTables{records: map[string][]domain.Record{}}
	if _, err := svc(t, ft, newGuard(t, allowAll)).WorkList(context.Background(), read.KindRequest, read.WorkFilter{Mine: true}, read.ListOptions{}); err != nil {
		t.Fatal(err)
	}
	if ft.lists[0].Query != "requested_for.user_name=agent.bot" {
		t.Errorf("query = %q", ft.lists[0].Query)
	}
}

func TestWorkListRejectsInjectionAndUnsupportedFilters(t *testing.T) {
	s := svc(t, &fakeTables{}, newGuard(t, allowAll))
	ctx := context.Background()
	if _, err := s.WorkList(ctx, read.KindIncident, read.WorkFilter{State: "1^ORactive=true"}, read.ListOptions{}); exitOf(t, err) != output.ExitValidation {
		t.Error("'^' in a filter value must be refused")
	}
	if _, err := s.WorkList(ctx, read.KindTask, read.WorkFilter{CI: "web01"}, read.ListOptions{}); exitOf(t, err) != output.ExitValidation {
		t.Error("--ci is only supported on incidents")
	}
	if _, err := s.WorkList(ctx, read.KindTask, read.WorkFilter{App: "x"}, read.ListOptions{}); exitOf(t, err) != output.ExitValidation {
		t.Error("--app is only supported on incidents")
	}
}

func TestWorkListMineNeedsIdentity(t *testing.T) {
	s := svc(t, &fakeTables{}, newGuard(t, allowAll))
	s.Identity = fakeIdentity{err: errBoom}
	if _, err := s.WorkList(context.Background(), read.KindIncident, read.WorkFilter{Mine: true}, read.ListOptions{}); err != errBoom {
		t.Errorf("err = %v", err)
	}
	s.Identity = fakeIdentity{}
	if _, err := s.WorkList(context.Background(), read.KindIncident, read.WorkFilter{Mine: true}, read.ListOptions{}); exitOf(t, err) != output.ExitValidation {
		t.Error("an empty identity must be refused")
	}
}

func TestMyWorkQueriesTaskTable(t *testing.T) {
	ft := &fakeTables{records: map[string][]domain.Record{"task": {rec("task", "number", "INC1")}}, total: intp(1)}
	g := newGuard(t, allowAll)
	got, err := svc(t, ft, g).MyWork(context.Background(), "", read.ListOptions{})
	if err != nil || len(got.Items) != 1 {
		t.Fatalf("%+v %v", got, err)
	}
	if q := ft.lists[0]; q.Table != "task" || q.Query != "active=true^assigned_to.user_name=agent.bot" {
		t.Errorf("list = %+v", q)
	}
	if r := g.Requests[0]; r.Verb != "list" || r.Resource != "table:task" {
		t.Errorf("request = %+v", r)
	}
	for kind, want := range map[string]string{
		"incident": "sys_class_name=incident",
		"request":  "sys_class_nameINsc_request,sc_req_item",
		"task":     "sys_class_name=sc_task",
		"change":   "sys_class_name=change_request",
	} {
		ft.lists = nil
		if _, err := svc(t, ft, newGuard(t, allowAll)).MyWork(context.Background(), kind, read.ListOptions{}); err != nil {
			t.Fatal(err)
		}
		if q := ft.lists[0].Query; !strings.Contains(q, "^"+want) {
			t.Errorf("%s: query %q lacks %q", kind, q, want)
		}
	}
	if _, err := svc(t, ft, newGuard(t, allowAll)).MyWork(context.Background(), "problem", read.ListOptions{}); exitOf(t, err) != output.ExitValidation {
		t.Error("unsupported kind")
	}
}
