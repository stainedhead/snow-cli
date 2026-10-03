package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/snow-cli/internal/domain"
)

const validYAML = `
default_profile: dev
profiles:
  dev:
    mode: agent
    agent_id: agent-7
    instance:
      host: acme.service-now.com
      release: xanadu
    daemon:
      socket: /run/agent-okta-d.sock
      provider: snow
    audit:
      path: /tmp/snow-audit.jsonl
    policy:
      path: /etc/snow/agent.policy.yaml
    incident:
      create_via: table
      categories: [software, hardware]
      states:
        resolved: "6"
      scale:
        high: 1
        medium: 2
        low: 3
  me:
    mode: human
    instance:
      host: acme.service-now.com
    okta:
      issuer: https://acme.okta.com/oauth2/default
      client_id: abc
      token_type: id
`

func noEnv(string) string { return "" }

func TestParseValid(t *testing.T) {
	c, err := Parse([]byte(validYAML))
	if err != nil {
		t.Fatal(err)
	}
	p, err := c.Resolve("", noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "dev" || p.Mode != domain.ModeAgent || p.Host != "acme.service-now.com" {
		t.Errorf("resolved = %+v", p)
	}
	if p.Daemon.Socket != "/run/agent-okta-d.sock" || p.Daemon.Provider != "snow" || p.AgentID != "agent-7" {
		t.Errorf("daemon/agent = %+v", p)
	}
	if p.Incident.CreateVia != "table" || len(p.Incident.Categories) != 2 || p.Incident.States["resolved"] != "6" {
		t.Errorf("incident = %+v", p.Incident)
	}
	h, err := c.Resolve("me", noEnv)
	if err != nil || h.Mode != domain.ModeHuman || h.Okta.TokenType != "id" {
		t.Errorf("human = %+v %v", h, err)
	}
}

func TestDefaults(t *testing.T) {
	c, err := Parse([]byte("profiles:\n  only:\n    mode: agent\n    instance:\n      host: a.example.com\n"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := c.Resolve("", noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "only" {
		t.Errorf("single profile should be the default, got %q", p.Name)
	}
	if p.Incident.CreateVia != "producer" || p.Incident.Scale != domain.DefaultScale() {
		t.Errorf("incident defaults = %+v", p.Incident)
	}
	if p.Okta.TokenType != "access" {
		t.Errorf("token type default = %q (A-05)", p.Okta.TokenType)
	}
	if p.Daemon.Provider != "snow" || p.Daemon.Socket == "" {
		t.Errorf("daemon defaults = %+v", p.Daemon)
	}
}

func TestStrictParseRejects(t *testing.T) {
	tests := []struct{ name, yaml string }{
		{"unknown top-level key", "bogus: 1\nprofiles:\n  a:\n    mode: agent\n"},
		{"unknown profile key", "profiles:\n  a:\n    mode: agent\n    wat: 1\n"},
		{"unknown nested key", "profiles:\n  a:\n    mode: agent\n    instance:\n      host: a.example.com\n      port: 1\n"},
		{"signature key (D-g)", "profiles:\n  a:\n    mode: agent\n    policy:\n      signature: x\n"},
		{"empty", ""},
		{"not yaml", "profiles: [unterminated"},
		{"no profiles", "default_profile: x\n"},
		{"bad mode", "profiles:\n  a:\n    mode: root\n"},
		{"missing mode", "profiles:\n  a:\n    instance:\n      host: a.example.com\n"},
		{"bad create_via", "profiles:\n  a:\n    mode: agent\n    incident:\n      create_via: raw\n"},
		{"bad token type", "profiles:\n  a:\n    mode: human\n    okta:\n      token_type: refresh\n"},
		{"bad scale", "profiles:\n  a:\n    mode: agent\n    incident:\n      scale:\n        high: 1\n        medium: 1\n        low: 3\n"},
		{"default profile missing", "default_profile: zz\nprofiles:\n  a:\n    mode: agent\n"},
		{"duplicate key", "profiles:\n  a:\n    mode: agent\n    mode: human\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.yaml))
			if err == nil {
				t.Fatal("expected error")
			}
			if output.ExitOf(err) != output.ExitUsage {
				t.Errorf("exit = %d, want 2 (%v)", output.ExitOf(err), err)
			}
		})
	}
}

func TestHostValidation(t *testing.T) {
	good := []string{"acme.service-now.com", "ACME.service-now.com", "dev123456.service-now.com", "localhost", "127.0.0.1:8443", "a.b-c.example.org:443"}
	for _, h := range good {
		if _, err := ValidateHost(h); err != nil {
			t.Errorf("ValidateHost(%q) = %v, want ok", h, err)
		}
	}
	bad := []string{
		"", "https://acme.service-now.com", "http://acme.service-now.com", "acme.service-now.com/path",
		"user@acme.service-now.com", "user:pw@acme.service-now.com", "*.service-now.com", "acme.*.com",
		"acme..com", "-acme.com", "acme-.com", "acme.com.", "acme.com:", "acme.com:99999", "acme.com:abc",
		"acme com", "acme.com?x=1", "acme.com#frag", "[::1]", "acme_x.com", "ac\nme.com",
	}
	for _, h := range bad {
		if _, err := ValidateHost(h); err == nil {
			t.Errorf("ValidateHost(%q) accepted, want error", h)
		} else if output.ExitOf(err) != output.ExitUsage {
			t.Errorf("ValidateHost(%q) exit = %d", h, output.ExitOf(err))
		}
	}
	if h, _ := ValidateHost("ACME.Service-Now.com"); h != "acme.service-now.com" {
		t.Errorf("host not lowercased: %q", h)
	}
}

func TestParseRejectsBadHost(t *testing.T) {
	_, err := Parse([]byte("profiles:\n  a:\n    mode: agent\n    instance:\n      host: https://evil.example.com\n"))
	if err == nil || output.ExitOf(err) != output.ExitUsage {
		t.Fatalf("err = %v", err)
	}
}

func TestEnvHostPrecedence(t *testing.T) {
	env := func(k string) string {
		if k == "SNOW_INSTANCE_HOST" {
			return "env.service-now.com"
		}
		return ""
	}
	c, _ := Parse([]byte(validYAML))
	p, err := c.Resolve("dev", env)
	if err != nil || p.Host != "acme.service-now.com" {
		t.Errorf("configured host must win over env: %q %v", p.Host, err)
	}
	c2, _ := Parse([]byte("profiles:\n  a:\n    mode: agent\n"))
	p2, err := c2.Resolve("a", env)
	if err != nil || p2.Host != "env.service-now.com" {
		t.Errorf("env fills a missing host: %q %v", p2.Host, err)
	}
	badEnv := func(string) string { return "https://x.example.com" }
	if _, err := c2.Resolve("a", badEnv); err == nil || output.ExitOf(err) != output.ExitUsage {
		t.Errorf("env host is validated too: %v", err)
	}
	if _, err := c2.Resolve("a", noEnv); err == nil || !strings.Contains(err.Error(), "instance.host") {
		t.Errorf("no host anywhere must fail naming instance.host: %v", err)
	}
}

func TestResolveProfileSelection(t *testing.T) {
	c, _ := Parse([]byte(validYAML))
	if _, err := c.Resolve("nope", noEnv); err == nil || output.ExitOf(err) != output.ExitUsage {
		t.Errorf("unknown profile: %v", err)
	}
	two, _ := Parse([]byte("profiles:\n  a:\n    mode: agent\n    instance:\n      host: a.example.com\n  b:\n    mode: agent\n    instance:\n      host: b.example.com\n"))
	if _, err := two.Resolve("", noEnv); err == nil {
		t.Error("ambiguous profile must fail without default_profile")
	}
}

func TestLoadAndPathResolution(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(validYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(filepath.Join(dir, "missing.yaml")); err == nil || output.ExitOf(err) != output.ExitUsage {
		t.Errorf("missing file: %v", err)
	}
	env := func(k string) string {
		if k == "SNOW_CONFIG" {
			return "/env/config.yaml"
		}
		return ""
	}
	if got := ResolvePath("/flag.yaml", env, "/home/u"); got != "/flag.yaml" {
		t.Errorf("flag wins: %q", got)
	}
	if got := ResolvePath("", env, "/home/u"); got != "/env/config.yaml" {
		t.Errorf("env: %q", got)
	}
	if got := ResolvePath("", noEnv, "/home/u"); got != "/home/u/.config/snow/config.yaml" {
		t.Errorf("default: %q", got)
	}
}

// ASSUMPTION A-05: the human token sent to ServiceNow defaults to the access token.
func TestAssumptionA05TokenTypeDefaultsToAccess(t *testing.T) {
	c, err := Parse([]byte("profiles:\n  h:\n    mode: human\n    instance:\n      host: a.example.com\n"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := c.Resolve("h", noEnv)
	if err != nil || p.Okta.TokenType != "access" {
		t.Errorf("token type = %q err=%v", p.Okta.TokenType, err)
	}
}

// ASSUMPTION A-02: the whoami path is configurable with a documented default.
func TestAssumptionA02WhoamiPathDefault(t *testing.T) {
	c, _ := Parse([]byte("profiles:\n  a:\n    mode: agent\n    instance:\n      host: a.example.com\n    whoami:\n      path: /api/custom/whoami\n  b:\n    mode: agent\n    instance:\n      host: a.example.com\n"))
	a, _ := c.Resolve("a", noEnv)
	b, _ := c.Resolve("b", noEnv)
	if a.Whoami.Path != "/api/custom/whoami" || b.Whoami.Path != DefaultWhoamiPath {
		t.Errorf("paths = %q %q", a.Whoami.Path, b.Whoami.Path)
	}
}
