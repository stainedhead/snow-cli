package cli

import (
	"context"
	"flag"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/stainedhead/agent-cli-core/policy"
	"github.com/stainedhead/snow-cli/internal/config"
	"github.com/stainedhead/snow-cli/internal/domain"
	"github.com/stainedhead/snow-cli/internal/humanauth"
)

// HumanAuthDeps are the injectable seams of the auth commands.
type HumanAuthDeps struct {
	HTTP         *http.Client
	Launch       humanauth.Launcher
	Sleep        func(ctx context.Context, d time.Duration) error
	Now          func() time.Time
	Port         int
	InsecurePath string // file store used by --insecure-store
}

func depsOf(e *Env) HumanAuthDeps {
	if e.HumanAuth != nil {
		return *e.HumanAuth
	}
	return HumanAuthDeps{}
}

func requireHuman(e *Env, cmd string) error {
	if e.Mode == domain.ModeHuman {
		return nil
	}
	return e.adaptPolicy(&policy.DeniedError{Decision: policy.Decision{
		RuleID: "auth-human-only",
		Reason: "`snow " + cmd + "` is a human-mode command; agent profiles authenticate through the credential daemon (FR-017)",
	}})
}

func (d HumanAuthDeps) config(e *Env) humanauth.Config {
	return humanauth.Config{
		Issuer: e.Profile.Okta.Issuer, ClientID: e.Profile.Okta.ClientID,
		TokenType: e.Profile.Okta.TokenType, HTTP: d.HTTP, Now: d.Now,
	}
}

func (d HumanAuthDeps) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

// storeFor returns the Env's keychain, or the insecure file store when the
// flag asks for it. The flag only affects the invoking command; later
// commands need SNOW_INSECURE_STORE=1 to read the same file.
func storeFor(e *Env, d HumanAuthDeps, insecure bool) (humanauth.Store, error) {
	if insecure {
		p := d.InsecurePath
		if p == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return nil, &config.Error{Msg: "cannot locate the home directory for --insecure-store", Wrap: err}
			}
			p = humanauth.DefaultInsecurePath(home)
		}
		if e.Err != nil {
			_, _ = io.WriteString(e.Err, "warning: --insecure-store keeps tokens in a plain 0600 file; set SNOW_INSECURE_STORE=1 so other commands read it\n")
		}
		return humanauth.NewFileStore(p), nil
	}
	if e.Keychain != nil {
		return e.Keychain, nil
	}
	return humanauth.StubStore{Backend: "none", Reason: "no credential store is wired for this profile"}, nil
}

func errWriter(e *Env) io.Writer {
	if e.Err != nil {
		return e.Err
	}
	return io.Discard
}

func statusData(st humanauth.Status, mode domain.Mode, extra map[string]any) map[string]any {
	m := map[string]any{
		"mode": string(mode), "logged_in": true, "issuer": st.Issuer, "subject": st.Subject,
		"scope": st.Scope, "expires_at": st.ExpiresAt.UTC().Format(time.RFC3339), "expired": st.Expired, "store": st.Store,
	}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

// RegisterAuth registers the human-mode auth commands (FR-011..FR-015, FR-017).
func RegisterAuth(r *Router) {
	var insecure, device bool
	insecureFlag := func(fs *flag.FlagSet) {
		fs.BoolVar(&insecure, "insecure-store", false, "store tokens in a plain 0600 file instead of the OS keychain (not recommended)")
	}
	r.Register(Command{
		Path:    []string{"auth", "login"},
		Summary: "Sign in with Okta (human mode): browser PKCE, or --device for the device code flow.",
		Usage:   "snow auth login [--device] [--insecure-store] [--profile <name>]",
		Examples: []string{"snow auth login", "snow auth login --device",
			"snow auth login --insecure-store"},
		Forbidden: []string{"Agent profiles must not run `auth login`; it is refused with exit 6. Never ask for or print tokens."},
		Flags: func(fs *flag.FlagSet) {
			fs.BoolVar(&device, "device", false, "use the device authorization grant")
			insecureFlag(fs)
		},
		Run: func(ctx context.Context, c *Call) (Result, error) {
			e := c.Env
			if err := requireHuman(e, "auth login"); err != nil {
				return Result{}, err
			}
			d := depsOf(e)
			st, err := storeFor(e, d, insecure)
			if err != nil {
				return Result{}, err
			}
			cfg := d.config(e)
			var cr humanauth.Credentials
			flow := "pkce"
			if device {
				flow = "device"
				cr, err = humanauth.LoginDevice(ctx, cfg, humanauth.DeviceOptions{Out: errWriter(e), Sleep: d.Sleep})
			} else {
				cr, err = humanauth.LoginPKCE(ctx, cfg, humanauth.PKCEOptions{Launch: d.Launch, Out: errWriter(e), Port: d.Port})
			}
			if err != nil {
				return Result{}, wrapLoginErr(err)
			}
			if err := st.Save(e.Profile.Name, cr); err != nil {
				return Result{}, err
			}
			s, _ := humanauth.StatusOf(st, e.Profile.Name, d.now())
			return Result{Data: statusData(s, e.Mode, map[string]any{"flow": flow})}, nil
		},
	})
	r.Register(Command{
		Path:     []string{"auth", "status"},
		Summary:  "Show the human login state: mode, issuer, subject and token expiry (never token values).",
		Usage:    "snow auth status [--insecure-store] [--profile <name>]",
		Examples: []string{"snow auth status"},
		Flags:    insecureFlag,
		Run: func(_ context.Context, c *Call) (Result, error) {
			e := c.Env
			if err := requireHuman(e, "auth status"); err != nil {
				return Result{}, err
			}
			d := depsOf(e)
			st, err := storeFor(e, d, insecure)
			if err != nil {
				return Result{}, err
			}
			s, err := humanauth.StatusOf(st, e.Profile.Name, d.now())
			if err != nil {
				return Result{}, err
			}
			return Result{Data: statusData(s, e.Mode, nil)}, nil
		},
	})
	r.Register(Command{
		Path:     []string{"auth", "logout"},
		Summary:  "Revoke the tokens at Okta and delete the stored credentials; partial failure is reported.",
		Usage:    "snow auth logout [--insecure-store] [--profile <name>]",
		Examples: []string{"snow auth logout"},
		Flags:    insecureFlag,
		Run: func(ctx context.Context, c *Call) (Result, error) {
			e := c.Env
			if err := requireHuman(e, "auth logout"); err != nil {
				return Result{}, err
			}
			d := depsOf(e)
			st, err := storeFor(e, d, insecure)
			if err != nil {
				return Result{}, err
			}
			revoked, err := humanauth.Logout(ctx, d.config(e), st, e.Profile.Name)
			if err != nil {
				return Result{}, err
			}
			return Result{Data: map[string]any{"logged_out": true, "revoked": revoked, "store_cleared": true}}, nil
		},
	})
}

// wrapLoginErr keeps Okta/login failures in the auth category (exit 3).
func wrapLoginErr(err error) error {
	switch err.(type) {
	case *humanauth.OAuthError, *humanauth.LoginRequiredError, *humanauth.StoreUnavailableError:
		return err
	}
	return &humanauth.LoginRequiredError{Msg: "login failed", Err: err}
}
