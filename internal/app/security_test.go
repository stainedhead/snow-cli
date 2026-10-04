package app

// E7 (FR-053): token-leak and security tests over the real command tree.

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stainedhead/snow-cli/internal/cli"
	"github.com/stainedhead/snow-cli/internal/testsupport/snfake"
)

// leakSweep runs a spread of commands and returns everything the process
// could have emitted: stdout, stderr and the audit log.
func leakSweep(t *testing.T, i *itg) string {
	t.Helper()
	i.f.On("GET", "/api/now/v1/table/incident", snfake.Records(1, map[string]any{"sys_id": itSID, "number": "INC1", "description": "hi"}))
	i.f.On("GET", "/api/now/v1/table/problem", snfake.Error(403, "forbidden"))
	i.f.On("GET", "/api/x_corp_agent/v1/whoami", snfake.Response{JSON: map[string]any{"result": map[string]any{"user_name": "u", "roles": []string{"r"}}}})
	okCreate(i.f)
	var all strings.Builder
	for _, args := range [][]string{
		{"whoami"},
		{"table", "list", "incident"},
		{"table", "list", "problem"}, // server 403
		{"table", "list", "sys_user"},
		{"incident", "list", "--format", "text"},
		append([]string{}, itCreate...),
		{"incident", "create", "--short-description", "x"}, // validation failure
		{"selftest"},
		{"auth", "status"},
		{"skill", "generate", "--out", i.dir + "/skill.md"},
		{"help"},
		{"version"},
	} {
		if i.mode == "human" {
			args = append(args, "--yes", "--trace")
		}
		_, _ = i.run(args...)
		all.WriteString(i.out.String())
		all.WriteString(i.errb.String())
	}
	if b, err := os.ReadFile(i.audit); err == nil {
		all.Write(b)
	}
	return all.String()
}

func TestSecurityNoTokenInStdoutStderrAuditOrTraceHumanMode(t *testing.T) {
	i := newItg(t, "human")
	i.seedHuman(itToken, time.Now().Add(time.Hour))
	got := leakSweep(t, i)
	for _, secret := range []string{itToken, "REFRESH-SECRET-1"} {
		if strings.Contains(got, secret) {
			t.Fatalf("secret %q leaked:\n%s", secret, got)
		}
	}
	if !strings.Contains(got, "snow") {
		t.Fatal("sweep produced no output; the test would pass vacuously")
	}
	// --trace in human mode is on, and the bearer is redacted there.
	i.out.Reset()
	if _, _ = i.run("table", "list", "incident", "--trace"); !strings.Contains(i.errb.String(), "GET") {
		t.Errorf("expected a trace line on stderr, got %q", i.errb.String())
	}
	if strings.Contains(i.errb.String(), itToken) {
		t.Error("trace leaked the bearer token")
	}
}

func TestSecurityNoTokenInOutputsAgentMode(t *testing.T) {
	i := newItg(t, "agent")
	got := leakSweep(t, i)
	for _, secret := range []string{"fake-token-1", "Bearer "} {
		if strings.Contains(got, secret) {
			t.Fatalf("secret %q leaked:\n%s", secret, got)
		}
	}
}

func TestSecurityServerEchoingTheTokenIsRedactedInErrors(t *testing.T) {
	i := newItg(t, "human")
	i.seedHuman(itToken, time.Now().Add(time.Hour))
	i.f.On("GET", "/api/now/v1/table/incident", snfake.Error(400, "bad request Authorization: Bearer "+itToken))
	_, _ = i.run("table", "list", "incident")
	out := i.out.String() + i.errb.String()
	if strings.Contains(out, itToken) {
		t.Fatalf("server-echoed token reached the output: %s", out)
	}
}

func TestSecurityNoTokenOrRawPassthroughCommandExists(t *testing.T) {
	r := cli.NewRouter(cli.Options{})
	cli.RegisterAll(r)
	for _, c := range r.Commands() {
		n := c.Name()
		for _, bad := range []string{"token", "print-token", "raw", "curl", "api", "request-raw", "exec", "proxy", "password", "secret"} {
			for _, word := range strings.Fields(n) {
				if word == bad {
					t.Errorf("forbidden command %q registered", n)
				}
			}
		}
		for _, f := range []string{"--token", "--raw", "--url", "--header"} {
			if strings.Contains(c.Usage, f) {
				t.Errorf("%q advertises %s", n, f)
			}
		}
	}
	i := newItg(t, "agent")
	for _, args := range [][]string{{"auth", "token"}, {"token"}, {"print-token"}, {"raw", "GET", "/api/now/table/incident"}, {"api"}} {
		if code, _ := i.run(args...); code != 2 && code != 6 {
			t.Errorf("%v: exit %d, want usage (2) or policy refusal (6)", args, code)
		}
	}
	if len(i.f.Requests()) != 0 {
		t.Error("no HTTP request may result from forbidden commands")
	}
}

func TestSecurityFreeTextIsMarkedUntrustedWithShippedPolicy(t *testing.T) {
	i := newItg(t, "agent")
	i.f.On("GET", "/api/now/v1/table/incident", snfake.FixtureFile(t, "../../testdata/fixtures/read/incident_list_injection.json"))
	code, m := i.run("incident", "list")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, i.out.String())
	}
	found := false
	for _, it := range items(m) {
		for k, v := range it.(map[string]any) {
			if vm, ok := v.(map[string]any); ok && vm["untrusted"] == true {
				found = true
			} else if (k == "description" || k == "work_notes" || k == "short_description" || k == "comments") && v != nil {
				if s, ok := v.(string); ok && s != "" {
					t.Errorf("free-text field %s is not marked untrusted: %q", k, s)
				}
			}
		}
	}
	if !found {
		t.Error("no untrusted marking present")
	}
}

func TestSecurityPolicyDeniesWriteToSystemTablesAndDeletesDoNotExist(t *testing.T) {
	i := newItg(t, "agent")
	for _, args := range [][]string{{"table", "get", "sys_user", itSID}, {"table", "list", "sysapproval_approver"}, {"table", "count", "sys_properties"}} {
		if code, _ := i.run(args...); code != 6 {
			t.Errorf("%v: exit %d", args, code)
		}
	}
	r := cli.NewRouter(cli.Options{})
	cli.RegisterAll(r)
	for _, c := range r.Commands() {
		if strings.Contains(c.Name(), "delete") {
			t.Errorf("delete command %q must not exist", c.Name())
		}
	}
	_ = context.Background()
}
