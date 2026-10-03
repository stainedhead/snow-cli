// Package app is the composition root: it turns parsed flags into a cli.Env
// by wiring config, policy, audit, the token source, the sn client and the
// ports. Stream-specific wiring lives in wire_*.go and human.go.
package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/stainedhead/agent-cli-core/audit"
	"github.com/stainedhead/agent-cli-core/auth"
	"github.com/stainedhead/agent-cli-core/httpx"
	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/agent-cli-core/policy"
	"github.com/stainedhead/snow-cli/internal/agentauth"
	"github.com/stainedhead/snow-cli/internal/auditx"
	"github.com/stainedhead/snow-cli/internal/cli"
	"github.com/stainedhead/snow-cli/internal/config"
	"github.com/stainedhead/snow-cli/internal/domain"
	"github.com/stainedhead/snow-cli/internal/sn"
)

// Options carries process-level inputs and test seams.
type Options struct {
	Getenv  func(string) string
	HomeDir string
	Stdin   io.Reader
	Stderr  io.Writer

	// NamedPolicy resolves --policy agent|human to policy bytes. The shipped
	// policies arrive with WS-E; until then named policies are unavailable.
	NamedPolicy func(name string) ([]byte, error)

	// Test seams.
	Insecure     bool              // http to a loopback instance (httptest)
	DaemonClient auth.DaemonClient // overrides the stub daemon client
	Base         http.RoundTripper // underlying transport
	Transport    httpx.Config      // retry tuning
}

// Wiring is what stream wire_*.go files receive to attach their ports.
type Wiring struct {
	Env     *cli.Env
	Client  *sn.Client
	Profile config.Resolved
}

type built struct {
	env    *cli.Env
	client *sn.Client
}

func newDaemonClient(socket string) auth.DaemonClient {
	return agentauth.NewUnavailableClient(socket)
}

// NewEnvFactory returns the cli.EnvFactory for the process.
func NewEnvFactory(o Options) cli.EnvFactory {
	return func(_ context.Context, g cli.GlobalFlags) (*cli.Env, error) {
		b, err := build(o, g)
		if err != nil {
			return nil, err
		}
		return b.env, nil
	}
}

type clock struct{}

func (clock) Now() time.Time { return time.Now() }

type idGen struct{}

func (idGen) NewID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func defaultAuditPath(home string) string {
	return filepath.Join(home, ".local", "state", "snow", "audit.jsonl")
}

func build(o Options, g cli.GlobalFlags) (*built, error) {
	if o.Getenv == nil {
		o.Getenv = os.Getenv
	}
	if o.Stderr == nil {
		o.Stderr = io.Discard
	}
	home := o.HomeDir
	if home == "" {
		home, _ = os.UserHomeDir()
	}

	cfg, err := config.Load(config.ResolvePath(g.Config, o.Getenv, home))
	if err != nil {
		return nil, err
	}
	prof, err := cfg.Resolve(g.Profile, o.Getenv)
	if err != nil {
		return nil, err
	}
	pol, err := loadPolicy(o, g, prof)
	if err != nil {
		return nil, err
	}

	src, err := tokenSource(o, prof)
	if err != nil {
		return nil, err
	}
	tcfg := o.Transport
	if g.Trace && prof.Mode == domain.ModeHuman {
		tcfg.Trace = o.Stderr
	}
	client, err := sn.New(sn.Options{
		Host: prof.Host, Insecure: o.Insecure, Auth: auth.NewAuthorizer(src), Base: o.Base, Transport: tcfg,
	})
	if err != nil {
		return nil, err
	}

	auditPath := prof.Audit.Path
	if auditPath == "" {
		auditPath = defaultAuditPath(home)
	}
	lg, err := audit.Open(audit.Config{Path: auditPath, OnFailure: audit.Block})
	if err != nil {
		return nil, &auditOpenError{path: auditPath, err: err}
	}

	ids := idGen{}
	agentID := prof.AgentID
	if agentID == "" {
		agentID = o.Getenv("SNOW_AGENT_ID")
	}
	runID := o.Getenv("SNOW_RUN_ID")
	if runID == "" {
		runID = ids.NewID()
	}
	env := &cli.Env{
		Mode: prof.Mode, Profile: prof, AgentID: agentID, RunID: runID,
		Identity: sn.NewIdentity(client, prof.Whoami.Path),
		Clock:    clock{}, IDs: ids,
		Guard: &auditx.Guard{
			Engine: policy.NewEngine(pol, nil), Sink: lg, Tool: "snow", AgentID: agentID, RunID: runID, Path: auditPath,
			OnWarn: func(err error) { _, _ = fmt.Fprintf(o.Stderr, "warning: %v\n", err) },
		},
		Limits: pol.Limits,
		In:     o.Stdin, Err: o.Stderr,
		Extra: map[string]any{},
	}
	if prof.Mode == domain.ModeHuman {
		env.Keychain = newKeychain()
	}
	w := &Wiring{Env: env, Client: client, Profile: prof}
	wireRead(w)
	wireWrite(w)
	return &built{env: env, client: client}, nil
}

func tokenSource(o Options, prof config.Resolved) (auth.TokenSource, error) {
	if prof.Mode == domain.ModeHuman {
		return newHumanTokenSource(prof)
	}
	dc := o.DaemonClient
	if dc == nil {
		dc = newDaemonClient(prof.Daemon.Socket)
	}
	return auth.NewDaemonTokenSource(dc, prof.Daemon.Provider,
		auth.WithRemediation("Re-enroll the agent credential with the agent-okta-d operator."))
}

func loadPolicy(o Options, g cli.GlobalFlags, prof config.Resolved) (*policy.Policy, error) {
	sel := g.Policy
	if sel == "" {
		sel = prof.Policy.Path
	}
	if sel == "" {
		return nil, &config.Error{
			Msg: "no policy configured (fail closed)",
			Hnt: "set policy.path in the profile or pass --policy agent|human|<file>",
		}
	}
	if sel == "agent" || sel == "human" {
		if o.NamedPolicy == nil {
			return nil, &config.Error{
				Msg: fmt.Sprintf("built-in policy %q is not available in this build", sel),
				Hnt: "pass --policy <file> or set policy.path",
			}
		}
		data, err := o.NamedPolicy(sel)
		if err != nil {
			return nil, &config.Error{Msg: fmt.Sprintf("built-in policy %q", sel), Wrap: err}
		}
		p, err := policy.Parse(data)
		if err != nil {
			return nil, invalidPolicy(err)
		}
		return p, nil
	}
	if _, err := os.Stat(sel); err != nil {
		return nil, &config.Error{Msg: "cannot read policy file " + sel, Wrap: errors.Unwrap(err), Hnt: "check policy.path / --policy"}
	}
	p, err := policy.Load(sel)
	if err != nil {
		return nil, invalidPolicy(err)
	}
	return p, nil
}

func invalidPolicy(err error) error { return &validationError{err: err} }

// validationError reports an invalid policy file (exit 9, fail closed).
type validationError struct{ err error }

func (e *validationError) Error() string           { return "invalid policy: " + e.err.Error() }
func (e *validationError) Unwrap() error           { return e.err }
func (*validationError) Category() output.Category { return output.CategoryValidation }
func (*validationError) Hint() string {
	return "Fix the policy file; the tool refuses to run with an invalid policy."
}

type auditOpenError struct {
	path string
	err  error
}

func (e *auditOpenError) Error() string {
	return fmt.Sprintf("cannot open audit log %s: %v", e.path, e.err)
}
func (e *auditOpenError) Unwrap() error { return e.err }
