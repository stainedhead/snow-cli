package cli_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stainedhead/agent-cli-core/policy"
	"github.com/stainedhead/snow-cli/internal/cli"
	"github.com/stainedhead/snow-cli/internal/domain"
	"github.com/stainedhead/snow-cli/internal/usecase"
)

func TestVersionCommand(t *testing.T) {
	h := newHarness(t)
	cli.RegisterCore(h.r)
	if code := h.run("version"); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if h.envCalls != 0 {
		t.Error("version must not build the environment")
	}
	m := decode(t, h)
	d := m["data"].(map[string]any)
	if d["version"] != "1.2.3" || d["commit"] != "abc" || d["date"] != "2026-10-03" {
		t.Errorf("data = %v", d)
	}
	golden(t, "version.golden.json", h.out.String())
}

type fakeIdentity struct {
	id  domain.Identity
	err error
}

func (f fakeIdentity) Whoami(context.Context) (domain.Identity, error) { return f.id, f.err }

type allowGuard struct{}

func (allowGuard) Run(ctx context.Context, _ usecase.Action, fn usecase.ActionFunc) error {
	_, err := fn(ctx, policy.Decision{Allowed: true})
	return err
}

func TestWhoamiCommand(t *testing.T) {
	h := newHarness(t)
	cli.RegisterCore(h.r)
	h.env = &cli.Env{
		Mode: domain.ModeAgent, AgentID: "a1", RunID: "r1",
		Identity: fakeIdentity{id: domain.Identity{User: "svc", Roles: []string{"itil"}}},
		Guard:    allowGuard{},
	}
	h.env.Profile.Name = "dev"
	h.env.Profile.Host = "acme.service-now.com"
	if code := h.run("whoami"); code != 0 {
		t.Fatalf("exit = %d: %s", code, h.out.String())
	}
	d := decode(t, h)["data"].(map[string]any)
	if d["user"] != "svc" || d["mode"] != "agent" || d["profile"] != "dev" || d["instance"] != "acme.service-now.com" || d["agent_id"] != "a1" {
		t.Errorf("data = %v", d)
	}
}

func TestWhoamiErrorExit(t *testing.T) {
	h := newHarness(t)
	cli.RegisterCore(h.r)
	h.env = &cli.Env{Mode: domain.ModeAgent, Identity: fakeIdentity{err: errors.New("boom")}, Guard: allowGuard{}}
	if code := h.run("whoami"); code != 1 {
		t.Errorf("exit = %d", code)
	}
}

func TestCoreCommandsAreDocumented(t *testing.T) {
	h := newHarness(t)
	cli.RegisterAll(h.r)
	got := map[string]cli.Command{}
	for _, c := range h.r.Commands() {
		got[c.Name()] = c
	}
	for _, name := range []string{"version", "whoami"} {
		c, ok := got[name]
		if !ok {
			t.Fatalf("%s not registered", name)
		}
		if c.Summary == "" || c.Usage == "" || len(c.Examples) == 0 {
			t.Errorf("%s needs summary, usage and examples for the generated skill", name)
		}
	}
	if !strings.Contains(strings.Join(got["whoami"].Forbidden, " "), "token") {
		t.Error("whoami should list the forbidden token handling")
	}
}
