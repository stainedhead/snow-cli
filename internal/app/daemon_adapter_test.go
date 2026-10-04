package app

// The agent-mode daemon wiring: the composition root returns the core's
// agent-okta-d adapter, and the CLI command path drives it end to end against
// the daemon's own fake on a real unix socket.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stainedhead/agent-cli-core/auth/oktad"
	"github.com/stainedhead/agent-okta-d/pkg/client/clienttest"
	"github.com/stainedhead/snow-cli/internal/cli"
	"github.com/stainedhead/snow-cli/internal/testsupport/snfake"
)

const daemonToken = "DAEMON-ACCESS-TOKEN-must-not-leak-7c1e"

func TestNewDaemonClientIsOktadAdapter(t *testing.T) {
	c, ok := newDaemonClient("/s/explicit.sock").(*oktad.Client)
	if !ok {
		t.Fatalf("newDaemonClient returned %T, want *oktad.Client", newDaemonClient(""))
	}
	if got := c.SocketPath(); got != "/s/explicit.sock" {
		t.Errorf("socket = %q", got)
	}
}

func TestNewDaemonClientHonorsEnvSocketWhenProfileHasNone(t *testing.T) {
	t.Setenv("AGENT_OKTA_D_SOCKET", "/s/from-env.sock")
	c := newDaemonClient("").(*oktad.Client)
	if got := c.SocketPath(); got != "/s/from-env.sock" {
		t.Errorf("socket = %q, want the AGENT_OKTA_D_SOCKET value", got)
	}
	if got := newDaemonClient("/s/profile.sock").(*oktad.Client).SocketPath(); got != "/s/profile.sock" {
		t.Errorf("an explicit profile socket must win, got %q", got)
	}
}

// shortDir returns a short temp dir: unix socket paths are limited to about
// 104 bytes on macOS.
func shortDir(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("", "sn")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(d) })
	return d
}

// agentItg builds an agent-mode integration fixture using the production
// daemon wiring, with daemon.socket set to sock (empty leaves it unset).
func agentItg(t *testing.T, sock string) *itg {
	t.Helper()
	i := newItg(t, "agent")
	i.opts.DaemonClient = nil
	if sock != "" {
		b, err := os.ReadFile(i.cfg)
		if err != nil {
			t.Fatal(err)
		}
		b = append(b, []byte("    daemon:\n      socket: "+sock+"\n")...)
		if err := os.WriteFile(i.cfg, b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return i
}

func serveToken(t *testing.T) *clienttest.Server {
	t.Helper()
	d := clienttest.New(t)
	now := time.Now()
	d.SetCredential("snow", clienttest.Credential{
		TokenType: "Bearer", AccessToken: daemonToken,
		IssuedAt: now, ExpiresAt: now.Add(time.Hour), Audience: "servicenow",
	})
	return d
}

func TestDaemonAdapterEndToEndReadUsesDaemonToken(t *testing.T) {
	d := serveToken(t)
	i := agentItg(t, d.SocketPath())
	i.f.On("GET", "/api/now/v1/table/incident", snfake.Records(1, map[string]any{"sys_id": itSID, "number": "INC1", "short_description": "disk"}))
	code, m := i.run("incident", "list", "--fields", "number")
	if code != 0 {
		t.Fatalf("exit %d: %s %s", code, i.out.String(), i.errb.String())
	}
	if m["ok"] != true {
		t.Errorf("envelope = %v", m)
	}
	reqs := i.f.Requests()
	if len(reqs) == 0 {
		t.Fatal("no request reached ServiceNow")
	}
	if got := reqs[0].Header.Get("Authorization"); got != "Bearer "+daemonToken {
		t.Errorf("ServiceNow saw Authorization %q, want the daemon's token", got)
	}
	audit, _ := os.ReadFile(i.audit)
	if len(audit) == 0 {
		t.Error("expected audit records")
	}
	for name, s := range map[string]string{"stdout": i.out.String(), "stderr": i.errb.String(), "audit": string(audit)} {
		if strings.Contains(s, daemonToken) {
			t.Errorf("token leaked into %s", name)
		}
	}
}

func TestDaemonAdapterEnvSocketReachesDaemon(t *testing.T) {
	d := serveToken(t)
	t.Setenv("AGENT_OKTA_D_SOCKET", d.SocketPath())
	i := agentItg(t, "") // no daemon.socket in the profile
	i.f.On("GET", "/api/now/v1/table/incident", snfake.Records(0))
	if code, _ := i.run("incident", "list", "--fields", "number"); code != 0 {
		t.Fatalf("exit %d: %s", code, i.out.String())
	}
	if got := i.f.Requests()[0].Header.Get("Authorization"); got != "Bearer "+daemonToken {
		t.Errorf("Authorization = %q", got)
	}
}

func TestDaemonAdapterNoDaemonExit3NamingSocket(t *testing.T) {
	sock := filepath.Join(shortDir(t), "none.sock")
	i := agentItg(t, sock)
	code, m := i.run("incident", "list", "--fields", "number")
	if code != 3 {
		t.Fatalf("exit %d, want 3: %s", code, i.out.String())
	}
	if !strings.Contains(errMsg(m), sock) {
		t.Errorf("message must name the socket %s: %q", sock, errMsg(m))
	}
	if len(i.f.Requests()) != 0 {
		t.Error("no request may be sent without a token")
	}
}

func TestDaemonAdapterErrorsMapToExitCodes(t *testing.T) {
	cases := []struct {
		name string
		err  clienttest.Error
		exit int
		want string // substring of message or hint
	}{
		{"reauth_required", clienttest.Error{Code: clienttest.CodeReauthRequired}, 3, "Re-enroll the agent credential"},
		{"revoked", clienttest.Error{Code: clienttest.CodeRevoked}, 3, "Re-enroll the agent credential"},
		{"degraded", clienttest.Error{Code: clienttest.CodeDegraded, State: "degraded", RetryAfter: 30 * time.Second}, 8, "30"},
		{"not_configured", clienttest.Error{Code: clienttest.CodeNotConfigured}, 3, "Check that the provider name is right"},
		{"unauthorized", clienttest.Error{Code: clienttest.CodeUnauthorized}, 3, "administrator to authorize this agent"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := clienttest.New(t)
			d.SetProviderError("snow", tc.err)
			i := agentItg(t, d.SocketPath())
			code, m := i.run("incident", "list", "--fields", "number")
			if code != tc.exit {
				t.Fatalf("exit %d, want %d: %s", code, tc.exit, i.out.String())
			}
			e, _ := m["error"].(map[string]any)
			text := errMsg(m)
			if h, _ := e["hint"].(string); h != "" {
				text += " " + h
			}
			if !strings.Contains(text, tc.want) {
				t.Errorf("error %v lacks %q", e, tc.want)
			}
			if len(i.f.Requests()) != 0 {
				t.Error("no request may be sent without a token")
			}
		})
	}
}

func TestDaemonAdapterCancelledContextIsGeneralExit1(t *testing.T) {
	d := serveToken(t)
	i := agentItg(t, d.SocketPath())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := cli.NewRouter(cli.Options{EnvFactory: NewEnvFactory(i.opts), Stdout: &i.out, Stderr: &i.errb})
	cli.RegisterAll(r)
	if code := r.Execute(ctx, []string{"incident", "list", "--fields", "number", "--config", i.cfg}); code != 1 {
		t.Fatalf("exit %d, want 1: %s", code, i.out.String())
	}
	if len(i.f.Requests()) != 0 {
		t.Error("no request may be sent")
	}
}
