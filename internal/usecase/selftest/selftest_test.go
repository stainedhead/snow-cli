package selftest_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	core "github.com/stainedhead/agent-cli-core/selftest"

	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/agent-cli-core/policy"
	"github.com/stainedhead/snow-cli/internal/domain"
	"github.com/stainedhead/snow-cli/internal/usecase"
	"github.com/stainedhead/snow-cli/internal/usecase/selftest"
)

const agentLike = `
version: 1
rules:
  - {id: deny-sys, effect: deny, verbs: ["*"], resources: ["table:sys_*"]}
  - {id: deny-resolve, effect: deny, verbs: [resolve], resources: [incident]}
  - {id: who, effect: allow, verbs: [whoami, selftest], resources: [whoami, selftest]}
  - {id: reads, effect: allow, verbs: [get, list, count, search, related], resources: [incident, request, ritm, task, change, problem, "cmdb:*", "table:*", "catalog:search"]}
  - {id: create, effect: allow, verbs: [create], resources: [incident], constraints: {impact: {min: 2, max: 3}}}
  - {id: order, effect: allow, mode: dry_run_only, verbs: [order], resources: ["catalog:item:*"]}
`

type denied struct{ error }

func (denied) Category() output.Category { return output.CategoryPolicyDenied }

type guard struct {
	eng   *policy.Engine
	calls []policy.Request
}

func newGuard(t *testing.T, y string) *guard {
	t.Helper()
	p, err := policy.Parse([]byte(y))
	if err != nil {
		t.Fatal(err)
	}
	return &guard{eng: policy.NewEngine(p, nil)}
}

func (g *guard) AllowedFields(string, string) []string { return nil }

func (g *guard) Run(ctx context.Context, a usecase.Action, fn usecase.ActionFunc) error {
	g.calls = append(g.calls, a.Request)
	d := g.eng.Check(a.Request)
	if !d.Allowed && !d.DryRunOnly() {
		return denied{d.Err()}
	}
	_, err := fn(ctx, d)
	return err
}

type status struct {
	code int
	cat  output.Category
}

func (s status) Error() string             { return fmt.Sprintf("http %d", s.code) }
func (s status) Category() output.Category { return s.cat }

type tables struct {
	denyTables map[string]error // table -> error returned
	listed     []string
	counted    []string
}

func (f *tables) Get(context.Context, string, string, usecase.GetOptions) (domain.Record, error) {
	return domain.Record{}, errors.New("unused")
}
func (f *tables) List(_ context.Context, q usecase.ListQuery) (usecase.ListResult, error) {
	f.listed = append(f.listed, q.Table)
	if err := f.denyTables[q.Table]; err != nil {
		return usecase.ListResult{}, err
	}
	return usecase.ListResult{}, nil
}
func (f *tables) Count(_ context.Context, table, _ string) (int, error) {
	f.counted = append(f.counted, table)
	return 1, f.denyTables[table]
}

type catalog struct{ err error }

func (c catalog) Search(context.Context, string, int, int) ([]domain.CatalogItem, error) {
	return nil, c.err
}
func (catalog) Item(context.Context, string) (domain.CatalogItem, error) {
	return domain.CatalogItem{}, nil
}
func (catalog) Variables(context.Context, string) ([]domain.CatalogVariable, error) { return nil, nil }

type ident struct{ err error }

func (i ident) Whoami(context.Context) (domain.Identity, error) { return domain.Identity{}, i.err }

type incidents struct {
	resolveErr, updateErr error
	resolved, updated     []string
}

func (*incidents) FindByCorrelation(context.Context, string) (*domain.Record, error) {
	return nil, nil
}
func (*incidents) CreateIncident(context.Context, usecase.IncidentCreate) (domain.WriteResult, error) {
	return domain.WriteResult{}, errors.New("selftest must never create")
}
func (i *incidents) UpdateIncident(_ context.Context, in usecase.IncidentUpdate) (domain.WriteResult, error) {
	i.updated = append(i.updated, in.Ref)
	return domain.WriteResult{}, i.updateErr
}
func (i *incidents) ResolveIncident(_ context.Context, in usecase.IncidentResolve) (domain.WriteResult, error) {
	i.resolved = append(i.resolved, in.Ref)
	return domain.WriteResult{}, i.resolveErr
}

var (
	forbidden = status{403, output.CategoryForbidden}
	notFound  = status{404, output.CategoryNotFound}
)

// probeGuard is the audited, policy-skipping guard for the server probes.
type probeGuard struct {
	actions []usecase.Action
	failAt  int // 1-based call number whose audit write fails (0 = never)
}

type auditFailure struct{}

func (auditFailure) Error() string { return "audit record could not be written" }
func (auditFailure) AuditFailed()  {}

func (p *probeGuard) RunProbe(ctx context.Context, a usecase.Action, fn usecase.ActionFunc) error {
	p.actions = append(p.actions, a)
	if p.failAt != 0 && len(p.actions) == p.failAt {
		return auditFailure{} // pending record failed: fn must not run
	}
	_, err := fn(ctx, policy.Decision{Allowed: true})
	return err
}

func svc(t *testing.T, g *guard, tb *tables, inc *incidents) selftest.Service {
	t.Helper()
	return selftest.Service{
		Guard: g, Probes: &probeGuard{}, Tables: tb, Catalog: catalog{}, Identity: ident{}, Incidents: inc,
		Mode: domain.ModeAgent,
	}
}

func TestRowsAreNamedAndReadOnlyDefault(t *testing.T) {
	s := svc(t, newGuard(t, agentLike), &tables{}, &incidents{})
	rows := s.Rows()
	if len(rows) < 10 {
		t.Fatalf("only %d rows", len(rows))
	}
	for i, r := range rows {
		if r.Name != fmt.Sprintf("m8-3-row-%d", i+1) {
			t.Errorf("row %d name %q", i, r.Name)
		}
		if r.Verb == "" || r.Resource == "" {
			t.Errorf("row %v lacks verb/resource", r)
		}
	}
	// Write rows are present but not read-only.
	w := 0
	for _, r := range rows {
		if !r.ReadOnly {
			w++
		}
	}
	if w != 2 {
		t.Errorf("want 2 write rows, got %d", w)
	}
}

func TestRunReadOnlyPassesAndNeverWrites(t *testing.T) {
	inc := &incidents{}
	tb := &tables{denyTables: map[string]error{"sys_user": forbidden, "sys_properties": notFound}}
	res, err := svc(t, newGuard(t, agentLike), tb, inc).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK() || res.Skipped != 2 || res.Passed < 10 {
		t.Fatalf("%+v", res)
	}
	if len(inc.resolved)+len(inc.updated) != 0 {
		t.Error("read-only run wrote")
	}
}

func TestOverGrantFailsTheMatrix(t *testing.T) {
	// sys_user answers 200: the server over-grants, the deny row must fail.
	tb := &tables{denyTables: map[string]error{"sys_properties": forbidden}}
	res, err := svc(t, newGuard(t, agentLike), tb, &incidents{}).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.OK() || res.Failed != 1 || res.ExitCode() != output.ExitGeneral {
		t.Fatalf("%+v", res)
	}
	var detail string
	for _, r := range res.Rows {
		if r.Status == core.StatusFail {
			detail = r.Detail + " " + r.Resource
		}
	}
	if !strings.Contains(detail, "expected deny, got allow") || !strings.Contains(detail, "sys_user") {
		t.Errorf("detail %q", detail)
	}
}

func TestClientPolicyDriftFailsTheMatrix(t *testing.T) {
	// A policy that lets resolve through contradicts the agent matrix.
	loose := strings.Replace(agentLike, "{id: deny-resolve, effect: deny, verbs: [resolve], resources: [incident]}", "{id: ok-resolve, effect: allow, verbs: [resolve], resources: [incident]}", 1)
	tb := &tables{denyTables: map[string]error{"sys_user": forbidden, "sys_properties": forbidden}}
	res, _ := svc(t, newGuard(t, loose), tb, &incidents{}).Run(context.Background())
	if res.OK() {
		t.Fatal("drifted policy must fail")
	}
}

func TestProbeErrorOtherStatusFailsWithDetail(t *testing.T) {
	tb := &tables{denyTables: map[string]error{"incident": status{500, output.CategoryRateLimited}, "sys_user": forbidden, "sys_properties": forbidden}}
	res, _ := svc(t, newGuard(t, agentLike), tb, &incidents{}).Run(context.Background())
	found := false
	for _, r := range res.Rows {
		if strings.HasPrefix(r.Detail, "probe error:") {
			found = true
		}
	}
	if res.OK() || !found {
		t.Fatalf("%+v", res)
	}
}

func TestAllowRowDeniedByServerFails(t *testing.T) {
	tb := &tables{denyTables: map[string]error{"problem": forbidden, "sys_user": forbidden, "sys_properties": forbidden}}
	res, _ := svc(t, newGuard(t, agentLike), tb, &incidents{}).Run(context.Background())
	if res.OK() || res.Failed != 1 {
		t.Fatalf("%+v", res)
	}
}

func TestIncludeWritesNeedsFixtureConfig(t *testing.T) {
	s := svc(t, newGuard(t, agentLike), &tables{}, &incidents{})
	s.IncludeWrites = true
	_, err := s.Run(context.Background())
	if output.ExitOf(err) != output.ExitUsage {
		t.Fatalf("want usage error, got %v", err)
	}
}

func TestIncludeWritesRunsServerDenyProbes(t *testing.T) {
	inc := &incidents{resolveErr: forbidden, updateErr: forbidden}
	tb := &tables{denyTables: map[string]error{"sys_user": forbidden, "sys_properties": forbidden}}
	s := svc(t, newGuard(t, agentLike), tb, inc)
	s.IncludeWrites = true
	s.Fixture = selftest.Fixture{Own: "INC0000001", Foreign: "INC0000002"}
	res, err := s.Run(context.Background())
	if err != nil || !res.OK() || res.Skipped != 0 {
		t.Fatalf("%+v %v", res, err)
	}
	if len(inc.resolved) != 1 || inc.resolved[0] != "INC0000001" || len(inc.updated) != 1 || inc.updated[0] != "INC0000002" {
		t.Errorf("resolved=%v updated=%v", inc.resolved, inc.updated)
	}
	// Over-grant on the write rows fails.
	inc2 := &incidents{}
	s.Incidents = inc2
	res, _ = s.Run(context.Background())
	if res.OK() || res.Failed != 2 {
		t.Fatalf("over-granted writes must fail: %+v", res)
	}
}

func TestSelftestVerbDeniedByPolicyExits6(t *testing.T) {
	g := newGuard(t, strings.Replace(agentLike, "[whoami, selftest]", "[whoami]", 1))
	_, err := svc(t, g, &tables{}, &incidents{}).Run(context.Background())
	if output.ExitOf(err) != output.ExitPolicyDenied {
		t.Fatalf("got %v", err)
	}
}

func TestHumanMatrixDiffersFromAgent(t *testing.T) {
	a := svc(t, newGuard(t, agentLike), &tables{}, &incidents{})
	h := a
	h.Mode = domain.ModeHuman
	resolve := func(rs []core.Row) core.Outcome {
		for _, r := range rs {
			if r.Verb == "resolve" && r.Resource == "incident" && r.ReadOnly {
				return r.Expect
			}
		}
		return ""
	}
	if resolve(a.Rows()) != core.Deny || resolve(h.Rows()) != core.Allow {
		t.Errorf("agent=%q human=%q", resolve(a.Rows()), resolve(h.Rows()))
	}
}

func TestWhoamiAndCatalogProbeErrors(t *testing.T) {
	s := svc(t, newGuard(t, agentLike), &tables{}, &incidents{})
	s.Identity = ident{err: status{500, output.CategoryRateLimited}}
	s.Catalog = catalog{err: forbidden}
	res, _ := s.Run(context.Background())
	if res.OK() || res.Failed < 2 {
		t.Fatalf("%+v", res)
	}
}

func TestAuthFailureAbortsWithExit3InsteadOfAMatrixOfProbeErrors(t *testing.T) {
	s := svc(t, newGuard(t, agentLike), &tables{}, &incidents{})
	s.Identity = ident{err: status{401, output.CategoryAuth}}
	_, err := s.Run(context.Background())
	if output.ExitOf(err) != output.ExitAuth {
		t.Fatalf("got %v", err)
	}
}

func TestServerProbesAreAuditedWithDistinctVerbsAndSkipClientPolicy(t *testing.T) {
	inc := &incidents{resolveErr: forbidden, updateErr: forbidden}
	tb := &tables{denyTables: map[string]error{"sys_user": forbidden, "sys_properties": forbidden}}
	g := newGuard(t, agentLike)
	pg := &probeGuard{}
	s := svc(t, g, tb, inc)
	s.Probes = pg
	s.IncludeWrites = true
	s.Fixture = selftest.Fixture{Own: "INC0000001", Foreign: "INC0000002"}
	if res, err := s.Run(context.Background()); err != nil || !res.OK() {
		t.Fatalf("%+v %v", res, err)
	}
	if len(pg.actions) != 4 {
		t.Fatalf("want 4 audited probes (2 server reads, resolve, update), got %d: %+v", len(pg.actions), pg.actions)
	}
	seen := map[string]usecase.Action{}
	for _, a := range pg.actions {
		if _, dup := seen[a.Request.Verb+" "+a.Request.Resource]; dup {
			t.Errorf("probe %v not distinct", a.Request)
		}
		seen[a.Request.Verb+" "+a.Request.Resource] = a
		if !strings.HasPrefix(a.Request.Verb, "selftest:probe-") {
			t.Errorf("verb %q must be a selftest:probe-* verb", a.Request.Verb)
		}
	}
	rv := seen["selftest:probe-resolve incident"]
	if rv.Kind != usecase.Write || rv.Ref != "INC0000001" {
		t.Errorf("resolve probe = %+v", rv)
	}
	up := seen["selftest:probe-update incident"]
	if up.Kind != usecase.Write || up.Ref != "INC0000002" {
		t.Errorf("update probe = %+v", up)
	}
	// The probes never went through the client policy guard.
	for _, r := range g.calls {
		if strings.HasPrefix(r.Verb, "selftest:probe-") {
			t.Errorf("probe %v went through the policy guard", r)
		}
	}
}

func TestFailedAuditOnWriteProbeSendsNoRequestAndAborts(t *testing.T) {
	inc := &incidents{resolveErr: forbidden, updateErr: forbidden}
	tb := &tables{denyTables: map[string]error{"sys_user": forbidden, "sys_properties": forbidden}}
	pg := &probeGuard{failAt: 3} // 1,2 = server reads; 3 = resolve
	s := svc(t, newGuard(t, agentLike), tb, inc)
	s.Probes = pg
	s.IncludeWrites = true
	s.Fixture = selftest.Fixture{Own: "INC0000001", Foreign: "INC0000002"}
	_, err := s.Run(context.Background())
	if err == nil || output.ExitOf(err) != output.ExitGeneral {
		t.Fatalf("audit failure must abort the selftest with a general error, got %v", err)
	}
	if len(inc.resolved)+len(inc.updated) != 0 {
		t.Errorf("a write probe was sent after the audit failure: resolved=%v updated=%v", inc.resolved, inc.updated)
	}
	if len(pg.actions) != 3 {
		t.Errorf("probes after the audit failure must not run, got %d actions", len(pg.actions))
	}
}

func TestFailedAuditOnServerReadProbeSendsNoRequest(t *testing.T) {
	tb := &tables{}
	pg := &probeGuard{failAt: 1}
	s := svc(t, newGuard(t, agentLike), tb, &incidents{})
	s.Probes = pg
	_, err := s.Run(context.Background())
	if err == nil {
		t.Fatal("audit failure must surface")
	}
	for _, tbl := range tb.listed {
		if tbl == "sys_user" {
			t.Error("the sys_user probe request was sent although its audit record failed")
		}
	}
}

func TestMissingProbeGuardFailsClosedWithoutRequests(t *testing.T) {
	tb := &tables{}
	s := svc(t, newGuard(t, agentLike), tb, &incidents{})
	s.Probes = nil
	res, _ := s.Run(context.Background())
	if res.OK() {
		t.Fatal("a missing probe guard must fail the server-probe rows")
	}
	for _, tbl := range tb.listed {
		if tbl == "sys_user" || tbl == "sys_properties" {
			t.Errorf("unaudited probe request sent to %s", tbl)
		}
	}
}
