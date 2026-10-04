package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stainedhead/agent-cli-core/auth/authtest"
	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/snow-cli/internal/cli"
	"github.com/stainedhead/snow-cli/internal/domain"
	"github.com/stainedhead/snow-cli/internal/sn"
	"github.com/stainedhead/snow-cli/internal/testsupport/snfake"
)

const testPolicy = `
version: 1
rules:
  - id: allow-read
    effect: allow
    verbs: [get, list, whoami]
    resources: ["table:*", whoami]
`

type fixture struct {
	dir, cfgPath string
	fake         *snfake.Fake
	stderr       bytes.Buffer
}

func newFixture(t *testing.T, mode string) *fixture {
	t.Helper()
	f := &fixture{dir: t.TempDir(), fake: snfake.New(t)}
	pol := filepath.Join(f.dir, "agent.policy.yaml")
	if err := os.WriteFile(pol, []byte(testPolicy), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := "default_profile: p\nprofiles:\n  p:\n    mode: " + mode + "\n    agent_id: agent-1\n" +
		"    instance:\n      host: " + f.fake.Host() + "\n" +
		"    daemon:\n      socket: /run/test-daemon.sock\n" +
		"    audit:\n      path: " + filepath.Join(f.dir, "audit.jsonl") + "\n" +
		"    policy:\n      path: " + pol + "\n"
	f.cfgPath = filepath.Join(f.dir, "config.yaml")
	if err := os.WriteFile(f.cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *fixture) opts() Options {
	return Options{
		Getenv: func(string) string { return "" }, HomeDir: f.dir, Stderr: &f.stderr,
		Insecure: true,
	}
}

func TestDaemonStubFailsClosedWithExit3NamingSocket(t *testing.T) {
	f := newFixture(t, "agent")
	b, err := build(f.opts(), cli.GlobalFlags{Config: f.cfgPath})
	if err != nil {
		t.Fatal(err)
	}
	_, err = b.client.Do(context.Background(), sn.Call{Method: "GET", Path: "/api/now/v1/table/incident"})
	if output.ExitOf(err) != output.ExitAuth {
		t.Fatalf("exit = %d, want 3 (%v)", output.ExitOf(err), err)
	}
	if !strings.Contains(err.Error(), "/run/test-daemon.sock") {
		t.Errorf("message must name the configured socket: %v", err)
	}
	if len(f.fake.Requests()) != 0 {
		t.Error("no request may be sent without a token")
	}
}

func TestFakeDaemonClientYieldsBearerToken(t *testing.T) {
	f := newFixture(t, "agent")
	f.fake.On("GET", "/api/now/v1/table/incident", snfake.Records(0))
	o := f.opts()
	fk := authtest.New(authtest.Valid)
	o.DaemonClient = fk
	b, err := build(o, cli.GlobalFlags{Config: f.cfgPath})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.client.Do(context.Background(), sn.Call{Method: "GET", Path: "/api/now/v1/table/incident"}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(f.fake.Requests()[0].Header.Get("Authorization"), "Bearer fake-token-") {
		t.Error("bearer token missing")
	}
	if got := fk.Providers(); len(got) == 0 || got[0] != "snow" {
		t.Errorf("provider = %v", got)
	}
}

func TestFactoryBuildsEnv(t *testing.T) {
	f := newFixture(t, "agent")
	env, err := NewEnvFactory(f.opts())(context.Background(), cli.GlobalFlags{Config: f.cfgPath})
	if err != nil {
		t.Fatal(err)
	}
	if env.Mode != domain.ModeAgent || env.Profile.Host != f.fake.Host() || env.Guard == nil || env.Identity == nil ||
		env.Clock == nil || env.IDs == nil || env.Keychain != nil {
		t.Errorf("env = %+v", env)
	}
	id1, id2 := env.IDs.NewID(), env.IDs.NewID()
	if id1 == id2 || len(id1) < 16 {
		t.Error("IDs must be unique")
	}
	if env.Clock.Now().IsZero() {
		t.Error("clock")
	}
}

func TestHumanProfileGetsKeychainPlaceholderAndFailClosedToken(t *testing.T) {
	f := newFixture(t, "human")
	b, err := build(f.opts(), cli.GlobalFlags{Config: f.cfgPath})
	if err != nil {
		t.Fatal(err)
	}
	if b.env.Keychain == nil {
		t.Error("human mode must carry the keychain placeholder")
	}
	_, err = b.client.Do(context.Background(), sn.Call{Method: "GET", Path: "/api/now/v1/table/incident"})
	if output.ExitOf(err) != output.ExitAuth {
		t.Errorf("human token stub exit = %d (%v)", output.ExitOf(err), err)
	}
}

func TestConfigAndPolicyFailuresAreClean(t *testing.T) {
	f := newFixture(t, "agent")
	// --policy needs the explicit opt-in (FR-R06); the policy section is last.
	cfg, err := os.ReadFile(f.cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.cfgPath, append(cfg, []byte("      allow_override: true\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := NewEnvFactory(f.opts())(ctx, cli.GlobalFlags{Config: filepath.Join(f.dir, "missing.yaml")}); output.ExitOf(err) != output.ExitUsage {
		t.Errorf("missing config exit = %d (%v)", output.ExitOf(err), err)
	}
	bad := filepath.Join(f.dir, "bad.yaml")
	if err := os.WriteFile(bad, []byte("version: 1\nbogus: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = NewEnvFactory(f.opts())(ctx, cli.GlobalFlags{Config: f.cfgPath, Policy: bad})
	if output.ExitOf(err) != output.ExitValidation {
		t.Errorf("invalid policy exit = %d, want 9 (%v)", output.ExitOf(err), err)
	}
	_, err = NewEnvFactory(f.opts())(ctx, cli.GlobalFlags{Config: f.cfgPath, Policy: "agent"})
	if output.ExitOf(err) != output.ExitUsage || !strings.Contains(err.Error(), "agent") {
		t.Errorf("unavailable named policy exit = %d (%v)", output.ExitOf(err), err)
	}
	o := f.opts()
	o.NamedPolicy = func(name string) ([]byte, error) { return []byte(testPolicy), nil }
	if _, err := NewEnvFactory(o)(ctx, cli.GlobalFlags{Config: f.cfgPath, Policy: "human"}); err != nil {
		t.Errorf("named policy via hook: %v", err)
	}
	if _, err := NewEnvFactory(f.opts())(ctx, cli.GlobalFlags{Config: f.cfgPath, Policy: filepath.Join(f.dir, "none.yaml")}); output.ExitOf(err) != output.ExitUsage {
		t.Errorf("missing policy file exit = %d (%v)", output.ExitOf(err), err)
	}
}

func TestNoPolicyConfiguredFailsClosed(t *testing.T) {
	f := newFixture(t, "agent")
	cfg := "profiles:\n  p:\n    mode: agent\n    instance:\n      host: " + f.fake.Host() + "\n"
	p := filepath.Join(f.dir, "nopolicy.yaml")
	if err := os.WriteFile(p, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := NewEnvFactory(f.opts())(context.Background(), cli.GlobalFlags{Config: p})
	if output.ExitOf(err) != output.ExitUsage || !strings.Contains(err.Error(), "policy") {
		t.Errorf("exit = %d (%v)", output.ExitOf(err), err)
	}
}

func TestAuditFailureToOpenIsReported(t *testing.T) {
	f := newFixture(t, "agent")
	blocker := filepath.Join(f.dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := "profiles:\n  p:\n    mode: agent\n    instance:\n      host: " + f.fake.Host() + "\n    audit:\n      path: " + blocker + "/sub/audit.jsonl\n    policy:\n      path: " + filepath.Join(f.dir, "agent.policy.yaml") + "\n"
	p := filepath.Join(f.dir, "c2.yaml")
	if err := os.WriteFile(p, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := NewEnvFactory(f.opts())(context.Background(), cli.GlobalFlags{Config: p})
	if err == nil || output.ExitOf(err) != output.ExitGeneral || !strings.Contains(err.Error(), "audit") {
		t.Errorf("err = %v", err)
	}
}

func TestDefaultAuditPathUnderHome(t *testing.T) {
	if got := defaultAuditPath("/home/u"); got != "/home/u/.local/state/snow/audit.jsonl" {
		t.Errorf("default audit path = %q", got)
	}
}
