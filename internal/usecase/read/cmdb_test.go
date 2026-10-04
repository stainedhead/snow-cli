package read_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/snow-cli/internal/domain"
	"github.com/stainedhead/snow-cli/internal/usecase"
	"github.com/stainedhead/snow-cli/internal/usecase/read"
)

func ci(id, name, class string) domain.Record {
	return rec("cmdb_ci", "sys_id", id, "name", name, "sys_class_name", class)
}

func rel(id, parent, pname, child, cname string) domain.Record {
	return rec("cmdb_rel_ci", "sys_id", id, "parent", parent, "parent.name", pname, "child", child, "child.name", cname,
		"parent.sys_class_name", "cmdb_ci_server", "child.sys_class_name", "cmdb_ci_app_server", "type.name", "Depends on::Used by")
}

// cmdbTables answers cmdb_ci by exact name and cmdb_rel_ci by parent=/child= clauses.
func cmdbTables(rels ...domain.Record) *fakeTables {
	ft := &fakeTables{records: map[string][]domain.Record{
		"cmdb_ci": {ci(sid1, "web01", "cmdb_ci_server"), ci(sid2, "db01", "cmdb_ci_db_instance"),
			ci(sid3, "dup", "cmdb_ci_server"), ci("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "dup", "cmdb_ci_linux_server")},
		"cmdb_ci_service": {rec("cmdb_ci_service", "sys_id", sid1, "name", "Checkout", "owned_by", "Ann", "support_group", "Platform")},
		"cmdb_ci_appl":    {rec("cmdb_ci_appl", "sys_id", sid2, "name", "Payments", "owned_by", "Bob")},
	}}
	ft.listFn = func(q usecase.ListQuery) (usecase.ListResult, error) {
		if q.Table == "cmdb_rel_ci" {
			var out []domain.Record
			for _, r := range rels {
				for _, clause := range strings.Split(q.Query, "^OR") {
					field, val, _ := strings.Cut(strings.TrimPrefix(clause, "^"), "=")
					if r.Get(field) == strings.Split(val, "^")[0] {
						out = append(out, r)
						break
					}
				}
			}
			return usecase.ListResult{Items: out}, nil
		}
		var out []domain.Record
		if name, ok := strings.CutPrefix(q.Query, "name="); ok {
			for _, r := range ft.records[q.Table] {
				if r.Get("name") == name {
					out = append(out, r)
				}
			}
		}
		return usecase.ListResult{Items: out}, nil
	}
	return ft
}

func TestCIGetBySysID(t *testing.T) {
	ft, g := cmdbTables(), newGuard(t, allowAll)
	got, err := svc(t, ft, g).CIGet(context.Background(), sid1, read.Options{Fields: []string{"name"}})
	if err != nil || text(got["name"]) != "web01" {
		t.Fatalf("%v %v", got, err)
	}
	if ft.gets[0].Table != "cmdb_ci" {
		t.Errorf("get = %+v", ft.gets[0])
	}
	if r := g.Requests[0]; r.Verb != "get" || r.Resource != "cmdb:ci" {
		t.Errorf("request = %+v", r)
	}
}

func TestCIGetByNameNeedsIdentifyingFieldsInPolicyRequest(t *testing.T) {
	ft, g := cmdbTables(), newGuard(t, allowAll)
	got, err := svc(t, ft, g).CIGet(context.Background(), "web01", read.Options{Fields: []string{"ip_address"}})
	if err != nil || text(got["name"]) != "web01" {
		t.Fatalf("%v %v", got, err)
	}
	if len(g.Requests[0].Fields) != 4 { // ip_address + sys_id, name, sys_class_name
		t.Errorf("policy fields = %v", g.Requests[0].Fields)
	}
	if q := ft.lists[0]; q.Query != "name=web01" || q.Table != "cmdb_ci" {
		t.Errorf("list = %+v", q)
	}
}

func TestCIGetAmbiguousNameListsCandidates(t *testing.T) {
	_, err := svc(t, cmdbTables(), newGuard(t, allowAll)).CIGet(context.Background(), "dup", read.Options{})
	if exitOf(t, err) != output.ExitValidation {
		t.Fatalf("exit = %d", output.ExitOf(err))
	}
	msg := err.Error()
	if !strings.Contains(msg, sid3) || !strings.Contains(msg, "cmdb_ci_linux_server") {
		t.Errorf("candidates missing: %s", msg)
	}
}

func TestCIGetNotFoundAndBadNames(t *testing.T) {
	s := svc(t, cmdbTables(), newGuard(t, allowAll))
	if _, err := s.CIGet(context.Background(), "ghost", read.Options{}); exitOf(t, err) != output.ExitNotFound {
		t.Errorf("exit = %d", output.ExitOf(err))
	}
	if _, err := s.CIGet(context.Background(), "a^ORname=b", read.Options{}); exitOf(t, err) != output.ExitValidation {
		t.Errorf("injection exit = %d", output.ExitOf(err))
	}
	s.Tables = &fakeTables{err: errBoom}
	if _, err := s.CIGet(context.Background(), "web01", read.Options{}); err != errBoom {
		t.Errorf("err = %v", err)
	}
}

func TestCISearchClassHierarchy(t *testing.T) {
	ft, g := cmdbTables(), newGuard(t, allowAll)
	s := svc(t, ft, g)
	if _, err := s.CISearch(context.Background(), "cmdb_ci_server", read.ListOptions{Query: "operational_status=1"}); err != nil {
		t.Fatal(err)
	}
	if q := ft.lists[0]; q.Table != "cmdb_ci_server" || q.Query != "operational_status=1" {
		t.Errorf("list = %+v", q)
	}
	if r := g.Requests[0]; r.Verb != "search" || r.Resource != "cmdb:ci" {
		t.Errorf("request = %+v", r)
	}
	if _, err := s.CISearch(context.Background(), "", read.ListOptions{}); err != nil || ft.lists[1].Table != "cmdb_ci" {
		t.Errorf("default class: %v", err)
	}
	for _, bad := range []string{"sys_user", "incident", "cmdb_ci;x", "CMDB_CI"} {
		if _, err := s.CISearch(context.Background(), bad, read.ListOptions{}); exitOf(t, err) != output.ExitValidation {
			t.Errorf("class %q must be refused", bad)
		}
	}
}

func chain() []domain.Record {
	// sid1 -> sid2 -> sid3 -> sid1 (cycle)
	return []domain.Record{
		rel("r1", sid1, "web01", sid2, "db01"),
		rel("r2", sid2, "db01", sid3, "dup"),
		rel("r3", sid3, "dup", sid1, "web01"),
	}
}

func TestCIRelatedDownBoundedDepthAndCycleSafe(t *testing.T) {
	g := newGuard(t, allowAll)
	s := svc(t, cmdbTables(chain()...), g)
	got, err := s.CIRelated(context.Background(), sid1, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if got.Direction != "down" || got.Depth != 2 || got.Root.SysID != sid1 || got.Root.Name != "web01" {
		t.Errorf("got %+v", got)
	}
	if len(got.Nodes) != 2 || got.Nodes[0].SysID != sid2 || got.Nodes[0].Depth != 1 || got.Nodes[1].SysID != sid3 || got.Nodes[1].Depth != 2 {
		t.Errorf("nodes = %+v", got.Nodes)
	}
	if len(got.Edges) != 2 || got.Edges[0].ParentName != "web01" || got.Edges[0].ChildName != "db01" || got.Edges[0].Type != "Depends on::Used by" {
		t.Errorf("edges = %+v", got.Edges)
	}
	// Depth 3 follows the cycle back to the root: the edge is shown, no node repeats.
	got, err = s.CIRelated(context.Background(), sid1, "down", 3)
	if err != nil || len(got.Nodes) != 2 || len(got.Edges) != 3 {
		t.Errorf("cycle: nodes %d edges %d err %v", len(got.Nodes), len(got.Edges), err)
	}
	if r := g.Requests[0]; r.Verb != "related" || r.Resource != "cmdb:ci" {
		t.Errorf("request = %+v", r)
	}
}

func TestCIRelatedUpFollowsChildToParent(t *testing.T) {
	got, err := svc(t, cmdbTables(chain()...), newGuard(t, allowAll)).CIRelated(context.Background(), sid1, "up", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Nodes) != 1 || got.Nodes[0].SysID != sid3 || got.Edges[0].Parent != sid3 || got.Edges[0].Child != sid1 {
		t.Errorf("got %+v", got)
	}
}

func TestCIRelatedBatchesFrontierPerLevel(t *testing.T) {
	rels := []domain.Record{rel("r1", sid1, "web01", sid2, "db01"), rel("r2", sid1, "web01", sid3, "dup"), rel("r3", sid2, "db01", "cccccccccccccccccccccccccccccccc", "x")}
	ft := cmdbTables(rels...)
	got, err := svc(t, ft, newGuard(t, allowAll)).CIRelated(context.Background(), sid1, "down", 2)
	if err != nil || len(got.Nodes) != 3 {
		t.Fatalf("%+v %v", got, err)
	}
	var relQueries []string
	for _, q := range ft.lists {
		if q.Table == "cmdb_rel_ci" {
			relQueries = append(relQueries, q.Query)
		}
	}
	if len(relQueries) != 2 || relQueries[1] != "parent="+sid2+"^ORparent="+sid3 {
		t.Errorf("one query per level expected, got %v", relQueries)
	}
}

func TestCIRelatedLimitsAndNodeCap(t *testing.T) {
	s := svc(t, cmdbTables(chain()...), newGuard(t, allowAll))
	ctx := context.Background()
	for name, tc := range map[string]struct {
		dir   string
		depth int
	}{"depth too deep": {"down", 6}, "negative depth": {"down", -1}, "bad direction": {"sideways", 1}} {
		if _, err := s.CIRelated(ctx, sid1, tc.dir, tc.depth); exitOf(t, err) != output.ExitValidation {
			t.Errorf("%s must be refused", name)
		}
	}
	s.RelatedNodeCap = 1
	got, err := s.CIRelated(ctx, sid1, "down", 3)
	if err != nil || !got.Truncated || len(got.Nodes) != 1 {
		t.Errorf("node cap: %+v %v", got, err)
	}
	if _, err := s.CIRelated(ctx, "ghost", "down", 1); exitOf(t, err) != output.ExitNotFound {
		t.Error("unknown root must be not found")
	}
	s.Tables = &fakeTables{err: errBoom}
	if _, err := s.CIRelated(ctx, sid1, "down", 1); err != errBoom {
		t.Errorf("err = %v", err)
	}
}

func TestCIRelatedPropagatesRelationshipErrors(t *testing.T) {
	ft := cmdbTables()
	inner := ft.listFn
	ft.listFn = func(q usecase.ListQuery) (usecase.ListResult, error) {
		if q.Table == "cmdb_rel_ci" {
			return usecase.ListResult{}, errBoom
		}
		return inner(q)
	}
	if _, err := svc(t, ft, newGuard(t, allowAll)).CIRelated(context.Background(), sid1, "down", 1); err != errBoom {
		t.Errorf("err = %v", err)
	}
}

func TestAppResolvesServiceThenApplicationAndListsRelated(t *testing.T) {
	rels := []domain.Record{rel("r1", sid1, "Checkout", sid2, "db01")}
	ft, g := cmdbTables(rels...), newGuard(t, allowAll)
	s := svc(t, ft, g)
	got, err := s.App(context.Background(), "Checkout", read.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if text(got.Application["owned_by"]) != "Ann" || text(got.Application["support_group"]) != "Platform" || len(got.Related.Nodes) != 1 || got.Related.Depth != 1 {
		t.Errorf("got %+v", got)
	}
	if r := g.Requests[0]; r.Verb != "get" || r.Resource != "cmdb:app" {
		t.Errorf("request = %+v", r)
	}
	if !ft.lists[0].Display {
		t.Error("cmdb app reads display values so owner and support group are names")
	}
	// Falls through to cmdb_ci_appl.
	got, err = s.App(context.Background(), "Payments", read.Options{})
	if err != nil || text(got.Application["name"]) != "Payments" {
		t.Errorf("appl fallback: %+v %v", got, err)
	}
	// By sys_id.
	if got, err = s.App(context.Background(), sid2, read.Options{}); err != nil || text(got.Application["name"]) != "Payments" {
		t.Errorf("by sys_id: %+v %v", got, err)
	}
	if _, err := s.App(context.Background(), "Nope", read.Options{}); exitOf(t, err) != output.ExitNotFound {
		t.Error("unknown app must be not found")
	}
	if _, err := s.App(context.Background(), sid3, read.Options{}); exitOf(t, err) != output.ExitNotFound {
		t.Error("unknown app sys_id must be not found")
	}
}

func TestAppAmbiguousAcrossTables(t *testing.T) {
	ft := cmdbTables()
	ft.records["cmdb_ci_appl"] = append(ft.records["cmdb_ci_appl"], rec("cmdb_ci_appl", "sys_id", sid3, "name", "Checkout"))
	_, err := svc(t, ft, newGuard(t, allowAll)).App(context.Background(), "Checkout", read.Options{})
	if exitOf(t, err) != output.ExitValidation || !strings.Contains(err.Error(), sid3) {
		t.Errorf("err = %v", err)
	}
	if _, err := svc(t, &fakeTables{err: errBoom}, newGuard(t, allowAll)).App(context.Background(), "Checkout", read.Options{}); err != errBoom {
		t.Errorf("err = %v", err)
	}
}

// ASSUMPTION(unverified against a real instance): B-A3, dot-walked fields in sysparm_fields name the related CIs.
func TestAssumptionRelationshipNamesComeFromDotWalkedFields(t *testing.T) {
	ft := cmdbTables(chain()...)
	if _, err := svc(t, ft, newGuard(t, allowAll)).CIRelated(context.Background(), sid1, "down", 1); err != nil {
		t.Fatal(err)
	}
	var fields []string
	for _, q := range ft.lists {
		if q.Table == "cmdb_rel_ci" {
			fields = q.Fields
		}
	}
	got := strings.Join(fields, ",")
	for _, want := range []string{"parent.name", "child.name", "child.sys_class_name", "type.name"} {
		if !strings.Contains(got, want) {
			t.Errorf("relationship read lacks %s: %s", want, got)
		}
	}
}

// ASSUMPTION(unverified against a real instance): B-A4, CI classes are recognised by the cmdb_ci name prefix.
func TestAssumptionCIClassRecognisedByNamePrefix(t *testing.T) {
	s := svc(t, cmdbTables(), newGuard(t, allowAll))
	if _, err := s.CISearch(context.Background(), "cmdb_ci_win_server", read.ListOptions{}); err != nil {
		t.Errorf("cmdb_ci_* must be accepted: %v", err)
	}
	if _, err := s.CISearch(context.Background(), "u_custom_ci", read.ListOptions{}); exitOf(t, err) != output.ExitValidation {
		t.Error("a class without the prefix is refused until the class hierarchy cache exists")
	}
}

// FR-R13: the relationship table read is its own policy-checked action that
// lists every field fetched, dot-walked ones included.
func TestCIRelatedChecksRelTableReadWithAllFetchedFields(t *testing.T) {
	g := newGuard(t, allowAll)
	if _, err := svc(t, cmdbTables(chain()...), g).CIRelated(context.Background(), sid1, "down", 1); err != nil {
		t.Fatal(err)
	}
	if len(g.Requests) != 2 {
		t.Fatalf("requests %v", g.Requests)
	}
	r := g.Requests[1]
	if r.Verb != "list" || r.Resource != "table:cmdb_rel_ci" {
		t.Fatalf("rel read request %+v", r)
	}
	for _, f := range []string{"sys_id", "parent", "child", "type", "parent.name", "child.name", "parent.sys_class_name", "child.sys_class_name", "type.name"} {
		if _, ok := r.Fields[f]; !ok {
			t.Errorf("rel read request lacks fetched field %q: %v", f, r.Fields)
		}
	}
}

func TestCIRelatedDeniedRelTableSendsNoRelationshipRequest(t *testing.T) {
	const pol = `
version: 1
rules:
  - {id: deny-rel, effect: deny, verbs: ["*"], resources: ["table:cmdb_rel_ci"]}
  - {id: all, effect: allow, verbs: ["*"], resources: ["*"]}
`
	ft := cmdbTables(chain()...)
	_, err := svc(t, ft, newGuard(t, pol)).CIRelated(context.Background(), sid1, "down", 2)
	if exitOf(t, err) != output.ExitPolicyDenied {
		t.Fatalf("exit %d", output.ExitOf(err))
	}
	for _, q := range ft.lists {
		if q.Table == "cmdb_rel_ci" {
			t.Fatal("relationship table must not be read when denied")
		}
	}
}

func TestCIRelatedFieldAllowlistOnRelTableApplies(t *testing.T) {
	const pol = `
version: 1
rules:
  - {id: rel, effect: allow, verbs: [list], resources: ["table:cmdb_rel_ci"], fields: [sys_id, parent, child, type]}
  - {id: ci, effect: allow, verbs: [related], resources: ["cmdb:ci"]}
`
	_, err := svc(t, cmdbTables(chain()...), newGuard(t, pol)).CIRelated(context.Background(), sid1, "down", 1)
	if exitOf(t, err) != output.ExitPolicyDenied {
		t.Fatalf("dot-walked fields outside the allowlist must be denied, exit %d", output.ExitOf(err))
	}
}

func TestAppChecksRelTableRead(t *testing.T) {
	g := newGuard(t, allowAll)
	if _, err := svc(t, cmdbTables(), g).App(context.Background(), "Checkout", read.Options{}); err != nil {
		t.Fatal(err)
	}
	last := g.Requests[len(g.Requests)-1]
	if last.Resource != "table:cmdb_rel_ci" || last.Verb != "list" {
		t.Fatalf("requests %v", g.Requests)
	}
}
