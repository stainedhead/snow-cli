package agentauth_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stainedhead/agent-cli-core/auth"
	"github.com/stainedhead/agent-cli-core/auth/authtest"
	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/snow-cli/internal/agentauth"
)

func TestStubReportsDaemonUnavailableNamingSocket(t *testing.T) {
	c := agentauth.NewUnavailableClient("/run/test/agent-okta-d.sock")
	for name, call := range map[string]func() (auth.Token, error){
		"fetch":   func() (auth.Token, error) { return c.Fetch(context.Background(), "snow") },
		"refresh": func() (auth.Token, error) { return c.Refresh(context.Background(), "snow") },
	} {
		t.Run(name, func(t *testing.T) {
			tok, err := call()
			if !tok.IsZero() {
				t.Error("stub must never return a token")
			}
			var ue *auth.UnreachableError
			if !errors.As(err, &ue) || ue.Socket != "/run/test/agent-okta-d.sock" {
				t.Fatalf("err = %v", err)
			}
			if output.ExitOf(err) != output.ExitAuth {
				t.Errorf("exit = %d, want 3", output.ExitOf(err))
			}
			if !strings.Contains(err.Error(), "/run/test/agent-okta-d.sock") {
				t.Errorf("message must name the socket: %v", err)
			}
		})
	}
}

// The stub behaves like the core's own Unreachable scenario when wrapped in
// the real token source, so the exit code and message shape are the core's.
func TestStubMatchesCoreUnreachableScenario(t *testing.T) {
	sock := "/run/x.sock"
	fake := authtest.New(authtest.Unreachable, authtest.WithSocket(sock))
	stub := agentauth.NewUnavailableClient(sock)
	for name, c := range map[string]auth.DaemonClient{"fake": fake, "stub": stub} {
		src, err := auth.NewDaemonTokenSource(c, "snow")
		if err != nil {
			t.Fatal(err)
		}
		_, err = src.Token(context.Background())
		if output.ExitOf(err) != output.ExitAuth || !strings.Contains(err.Error(), sock) {
			t.Errorf("%s: exit=%d err=%v", name, output.ExitOf(err), err)
		}
		if h, ok := err.(output.Hinter); !ok || !strings.Contains(h.Hint(), sock) {
			t.Errorf("%s: hint must name the socket", name)
		}
	}
}

func TestStubReasonDocumentsDeferral(t *testing.T) {
	_, err := agentauth.NewUnavailableClient("/s").Fetch(context.Background(), "p")
	if !errors.Is(err, agentauth.ErrAdapterNotBuilt) {
		t.Errorf("err should wrap ErrAdapterNotBuilt: %v", err)
	}
}
