package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stainedhead/agent-cli-core/auth/authtest"
	"github.com/stainedhead/snow-cli/internal/app"
	"github.com/stainedhead/snow-cli/internal/testsupport/snfake"
	"github.com/stainedhead/snow-cli/policies"
)

const policyAllowWhoami = `
version: 1
rules:
  - id: allow-whoami
    effect: allow
    verbs: [whoami]
    resources: [whoami]
`

const policyDenyAll = `
version: 1
rules:
  - id: only-get
    effect: allow
    verbs: [get]
    resources: ["table:incident"]
`

type env struct {
	dir, cfg, audit string
	fake            *snfake.Fake
}

func setup(t *testing.T, policyYAML string) env {
	t.Helper()
	e := env{dir: t.TempDir(), fake: snfake.New(t)}
	pol := filepath.Join(e.dir, "p.yaml")
	e.audit = filepath.Join(e.dir, "audit.jsonl")
	mustWrite(t, pol, policyYAML)
	e.cfg = filepath.Join(e.dir, "c.yaml")
	mustWrite(t, e.cfg, "profiles:\n  dev:\n    mode: agent\n    agent_id: agent-9\n    instance:\n      host: "+e.fake.Host()+
		"\n    daemon:\n      socket: /run/stub-daemon.sock\n    audit:\n      path: "+e.audit+"\n    policy:\n      path: "+pol+"\n")
	return e
}

func mustWrite(t *testing.T, p, s string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
}

func exec(t *testing.T, o app.Options, args ...string) (int, map[string]any, string) {
	t.Helper()
	var out, errb bytes.Buffer
	o.Stderr = &errb
	o.Getenv = func(string) string { return "" }
	o.HomeDir = t.TempDir()
	code := run(context.Background(), args, &out, &errb, o)
	var m map[string]any
	if err := json.Unmarshal(out.Bytes(), &m); err != nil {
		t.Fatalf("stdout not a JSON envelope: %v\n%s", err, out.String())
	}
	return code, m, errb.String()
}

func TestVersionNeedsNoConfig(t *testing.T) {
	code, m, _ := exec(t, app.Options{}, "version")
	d := m["data"].(map[string]any)
	if code != 0 || d["version"] != "dev" || d["commit"] == "" {
		t.Errorf("code=%d data=%v", code, d)
	}
}

func TestWhoamiAgentModeDaemonStubIsExit3NamingSocket(t *testing.T) {
	e := setup(t, policyAllowWhoami)
	code, m, _ := exec(t, app.Options{Insecure: true}, "whoami", "--config", e.cfg)
	if code != 3 {
		t.Fatalf("exit = %d, want 3: %v", code, m)
	}
	msg := m["error"].(map[string]any)["message"].(string)
	if !strings.Contains(msg, "/run/stub-daemon.sock") {
		t.Errorf("message must name the socket: %s", msg)
	}
	if len(e.fake.Requests()) != 0 {
		t.Error("no HTTP request without a token")
	}
}

func TestWhoamiWithFakeDaemonReturnsIdentityAndAudits(t *testing.T) {
	e := setup(t, policyAllowWhoami)
	e.fake.On("GET", "/api/x_corp_agent/v1/whoami", snfake.Response{Status: 200, JSON: map[string]any{
		"result": map[string]any{"user_name": "svc.agent", "name": "Svc", "roles": []string{"itil"}},
	}})
	code, m, _ := exec(t, app.Options{Insecure: true, DaemonClient: authtest.New(authtest.Valid)}, "whoami", "--config", e.cfg)
	if code != 0 {
		t.Fatalf("exit = %d: %v", code, m)
	}
	d := m["data"].(map[string]any)
	if d["user"] != "svc.agent" || d["mode"] != "agent" || d["profile"] != "dev" || d["agent_id"] != "agent-9" {
		t.Errorf("data = %v", d)
	}
	b, err := os.ReadFile(e.audit)
	if err != nil || !strings.Contains(string(b), `"verb":"whoami"`) || strings.Contains(string(b), "fake-token") {
		t.Errorf("audit = %q err=%v", b, err)
	}
}

func TestWhoamiPolicyDenialIsExit6WithoutHTTP(t *testing.T) {
	e := setup(t, policyDenyAll)
	code, _, _ := exec(t, app.Options{Insecure: true, DaemonClient: authtest.New(authtest.Valid)}, "whoami", "--config", e.cfg)
	if code != 6 || len(e.fake.Requests()) != 0 {
		t.Errorf("exit=%d requests=%d", code, len(e.fake.Requests()))
	}
}

func TestUsageAndTraceGuards(t *testing.T) {
	e := setup(t, policyAllowWhoami)
	if code, _, _ := exec(t, app.Options{}, "nope"); code != 2 {
		t.Errorf("unknown command exit = %d", code)
	}
	code, _, _ := exec(t, app.Options{Insecure: true, DaemonClient: authtest.New(authtest.Valid)}, "whoami", "--config", e.cfg, "--trace")
	if code != 6 {
		t.Errorf("--trace in agent mode exit = %d, want 6", code)
	}
}

func TestBuiltInPolicyNamesResolveThroughMain(t *testing.T) {
	// main() passes policies.Named; the run() helper mirrors the options.
	for _, name := range []string{"agent", "human"} {
		if b, err := policies.Named(name); err != nil || len(b) == 0 {
			t.Fatalf("%s: %v", name, err)
		}
	}
	e := setup(t, policyAllowWhoami)
	cfg, err := os.ReadFile(e.cfg)
	if err != nil {
		t.Fatal(err)
	}
	// Select the built-in policy through config (the --policy flag is pinned, FR-R06).
	mustWrite(t, e.cfg, strings.Replace(string(cfg), filepath.Join(e.dir, "p.yaml"), "agent", 1))
	o := app.Options{Insecure: true, NamedPolicy: policies.Named}
	code, m, _ := exec(t, o, "whoami", "--config", e.cfg)
	if code != 3 { // policy loaded; the daemon stub is the next failure
		t.Fatalf("exit %d: %v", code, m)
	}
}
