package whoami_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stainedhead/agent-cli-core/policy"
	"github.com/stainedhead/snow-cli/internal/domain"
	"github.com/stainedhead/snow-cli/internal/usecase"
	"github.com/stainedhead/snow-cli/internal/usecase/whoami"
)

type fakeID struct {
	id  domain.Identity
	err error
	n   int
}

func (f *fakeID) Whoami(context.Context) (domain.Identity, error) { f.n++; return f.id, f.err }

type recGuard struct {
	got   usecase.Action
	deny  error
	ranFn bool
}

func (g *recGuard) Run(ctx context.Context, a usecase.Action, fn usecase.ActionFunc) error {
	g.got = a
	if g.deny != nil {
		return g.deny
	}
	g.ranFn = true
	_, err := fn(ctx, policy.Decision{Allowed: true})
	return err
}

func svc(id *fakeID, g *recGuard) whoami.Service {
	return whoami.Service{Identity: id, Guard: g, Mode: domain.ModeAgent, Profile: "dev", Instance: "acme.service-now.com", AgentID: "a1", RunID: "r1"}
}

func TestExecuteEnrichesIdentityAndGuardsAsRead(t *testing.T) {
	id := &fakeID{id: domain.Identity{User: "svc", Roles: []string{"itil"}}}
	g := &recGuard{}
	out, err := svc(id, g).Execute(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if out.User != "svc" || out.Mode != domain.ModeAgent || out.Profile != "dev" || out.Instance != "acme.service-now.com" || out.AgentID != "a1" || out.RunID != "r1" {
		t.Errorf("identity = %+v", out)
	}
	if g.got.Kind != usecase.Read || g.got.Request.Verb != "whoami" || g.got.Request.Resource != "whoami" {
		t.Errorf("action = %+v", g.got)
	}
}

func TestPolicyDenialPreventsRemoteCall(t *testing.T) {
	id := &fakeID{}
	denied := errors.New("denied")
	_, err := svc(id, &recGuard{deny: denied}).Execute(context.Background())
	if !errors.Is(err, denied) || id.n != 0 {
		t.Errorf("err=%v calls=%d", err, id.n)
	}
}

func TestIdentityErrorPropagates(t *testing.T) {
	boom := errors.New("boom")
	_, err := svc(&fakeID{err: boom}, &recGuard{}).Execute(context.Background())
	if !errors.Is(err, boom) {
		t.Errorf("err = %v", err)
	}
}
