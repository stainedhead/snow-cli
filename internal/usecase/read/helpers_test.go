package read_test

import (
	"context"
	"errors"
	"sort"
	"testing"

	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/agent-cli-core/policy"
	"github.com/stainedhead/snow-cli/internal/domain"
	"github.com/stainedhead/snow-cli/internal/usecase"
	"github.com/stainedhead/snow-cli/internal/usecase/read"
)

// allowAll lets every verb on every resource.
const allowAll = `
version: 1
rules:
  - {id: all, effect: allow, verbs: ["*"], resources: ["*"]}
`

// policyGuard evaluates a real core policy and records each request.
type policyGuard struct {
	t        *testing.T
	eng      *policy.Engine
	Requests []policy.Request
	Refs     []string
}

var _ usecase.Guard = (*policyGuard)(nil)

func newGuard(t *testing.T, yaml string) *policyGuard {
	t.Helper()
	p, err := policy.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	return &policyGuard{t: t, eng: policy.NewEngine(p, nil)}
}

func (g *policyGuard) AllowedFields(string, string) []string { return nil }

func (g *policyGuard) Run(ctx context.Context, a usecase.Action, fn usecase.ActionFunc) error {
	g.Requests = append(g.Requests, a.Request)
	g.Refs = append(g.Refs, a.Ref)
	if a.Kind != usecase.Read {
		g.t.Errorf("read path issued a %v action", a.Kind)
	}
	d := g.eng.Check(a.Request)
	if !d.Allowed {
		return denied{d.Err()}
	}
	_, err := fn(ctx, d)
	return err
}

// allowlistGuard adds the optional FieldAllowlister hook.
type allowlistGuard struct {
	*policyGuard
	fields []string
}

func (g allowlistGuard) AllowedFields(verb, resource string) []string { return g.fields }

// fakeTables is an in-memory usecase.TableReader.
type fakeTables struct {
	records map[string][]domain.Record // table -> rows
	gets    []getCall
	lists   []usecase.ListQuery
	counts  []string
	total   *int
	listFn  func(usecase.ListQuery) (usecase.ListResult, error)
	err     error
}

type getCall struct {
	Table, SysID string
	Opts         usecase.GetOptions
}

var _ usecase.TableReader = (*fakeTables)(nil)

func (f *fakeTables) Get(_ context.Context, table, sysID string, o usecase.GetOptions) (domain.Record, error) {
	f.gets = append(f.gets, getCall{table, sysID, o})
	if f.err != nil {
		return domain.Record{}, f.err
	}
	for _, r := range f.records[table] {
		if r.Get("sys_id") == sysID {
			return r, nil
		}
	}
	return domain.Record{}, notFound{}
}

func (f *fakeTables) List(_ context.Context, q usecase.ListQuery) (usecase.ListResult, error) {
	f.lists = append(f.lists, q)
	if f.listFn != nil {
		return f.listFn(q)
	}
	if f.err != nil {
		return usecase.ListResult{}, f.err
	}
	rows := f.records[q.Table]
	return usecase.ListResult{Items: rows, Total: f.total}, nil
}

func (f *fakeTables) Count(_ context.Context, table, q string) (int, error) {
	f.counts = append(f.counts, table+"|"+q)
	if f.err != nil {
		return 0, f.err
	}
	return 42, nil
}

type notFound struct{}

func (notFound) Error() string             { return "not found" }
func (notFound) Category() output.Category { return output.CategoryNotFound }

func rec(table string, kv ...string) domain.Record {
	m := map[string]string{}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	return domain.Record{Table: table, Fields: m}
}

type fakeIdentity struct {
	user string
	err  error
}

func (f fakeIdentity) Whoami(context.Context) (domain.Identity, error) {
	return domain.Identity{User: f.user}, f.err
}

func intp(n int) *int { return &n }

func svc(t *testing.T, tb usecase.TableReader, g usecase.Guard) read.Service {
	t.Helper()
	return read.Service{Tables: tb, Guard: g, Identity: fakeIdentity{user: "agent.bot"}}
}

func exitOf(t *testing.T, err error) output.ExitCode {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error")
	}
	return output.ExitOf(err)
}

func sorted(s []string) []string {
	out := append([]string(nil), s...)
	sort.Strings(out)
	return out
}

var errBoom = errors.New("boom")

const sid1 = "0123456789abcdef0123456789abcdef"
const sid2 = "fedcba9876543210fedcba9876543210"
const sid3 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

// denied mimics the adapter the real guard applies (sn.AdaptPolicyError).
type denied struct{ error }

func (denied) Category() output.Category { return output.CategoryPolicyDenied }

// text returns the string of a plain or output.Untrusted value.
func text(v any) string {
	if u, ok := v.(output.Untrusted); ok {
		return u.Value
	}
	s, _ := v.(string)
	return s
}
