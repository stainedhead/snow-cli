package cli_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stainedhead/snow-cli/internal/cli"
	"github.com/stainedhead/snow-cli/internal/domain"
	"github.com/stainedhead/snow-cli/internal/humanauth"
	"github.com/stainedhead/snow-cli/internal/sn"
	"github.com/stainedhead/snow-cli/internal/testsupport/oktafake"
	"github.com/stainedhead/snow-cli/internal/usecase"
)

const (
	tAccess  = "ACCESS-SECRET-aaaa1111"
	tRefresh = "REFRESH-SECRET-bbbb2222"
)

type authFixture struct {
	h    *harness
	f    *oktafake.Fake
	st   *humanauth.MemoryStore
	deps *cli.HumanAuthDeps
	errW *bytes.Buffer
}

func newAuthFixture(t *testing.T, mode domain.Mode) *authFixture {
	t.Helper()
	h := newHarness(t)
	cli.RegisterAuth(h.r)
	f := oktafake.New(t)
	st := humanauth.NewMemoryStore()
	errW := &bytes.Buffer{}
	deps := &cli.HumanAuthDeps{
		Launch: func(raw string) error {
			u, _ := url.Parse(raw)
			q := u.Query()
			go func() {
				r, err := http.Get(q.Get("redirect_uri") + "?" + url.Values{"code": {"c1"}, "state": {q.Get("state")}}.Encode())
				if err == nil {
					_, _ = io.Copy(io.Discard, r.Body)
					_ = r.Body.Close()
				}
			}()
			return nil
		},
		Sleep:        func(context.Context, time.Duration) error { return nil },
		InsecurePath: filepath.Join(t.TempDir(), "creds.json"),
	}
	h.env = &cli.Env{Mode: mode, Keychain: st, Err: errW, HumanAuth: deps, PolicyErrors: usecase.PolicyErrorFunc(sn.AdaptPolicyError)}
	h.env.Profile.Name = "dev"
	h.env.Profile.Okta.Issuer = f.Issuer()
	h.env.Profile.Okta.ClientID = "cid"
	return &authFixture{h: h, f: f, st: st, deps: deps, errW: errW}
}

func (a *authFixture) tokenOK() {
	a.f.QueueToken(200, `{"access_token":"`+tAccess+`","refresh_token":"`+tRefresh+`","expires_in":3600,"scope":"openid snow.user"}`)
}

func (a *authFixture) noLeak(t *testing.T) {
	t.Helper()
	for _, s := range []string{a.h.out.String(), a.h.err.String(), a.errW.String()} {
		if strings.Contains(s, tAccess) || strings.Contains(s, tRefresh) {
			t.Fatalf("token leaked: %s", s)
		}
	}
}

func TestAuthLoginPKCEStatusLogout(t *testing.T) {
	a := newAuthFixture(t, domain.ModeHuman)
	a.tokenOK()
	if code := a.h.run("auth", "login"); code != 0 {
		t.Fatalf("login exit %d: %s", code, a.h.out.String())
	}
	d := decode(t, a.h)["data"].(map[string]any)
	if d["flow"] != "pkce" || d["logged_in"] != true || d["mode"] != "human" || d["store"] != "memory" {
		t.Errorf("data %v", d)
	}
	a.noLeak(t)
	if _, err := a.st.Load("dev"); err != nil {
		t.Fatal("credentials not stored under the profile")
	}

	if code := a.h.run("auth", "status"); code != 0 {
		t.Fatalf("status exit %d", code)
	}
	d = decode(t, a.h)["data"].(map[string]any)
	if d["issuer"] != a.f.Issuer() || d["expired"] != false || d["expires_at"] == "" {
		t.Errorf("status %v", d)
	}
	a.noLeak(t)

	if code := a.h.run("auth", "logout"); code != 0 {
		t.Fatalf("logout exit %d: %s", code, a.h.out.String())
	}
	d = decode(t, a.h)["data"].(map[string]any)
	if d["revoked"] != true || d["logged_out"] != true {
		t.Errorf("logout %v", d)
	}
	a.noLeak(t)
	if code := a.h.run("auth", "status"); code != 3 {
		t.Errorf("status after logout exit %d, want 3", code)
	}
}

func TestAuthLoginDevice(t *testing.T) {
	a := newAuthFixture(t, domain.ModeHuman)
	a.f.QueueDevice(200, `{"device_code":"dc","user_code":"WXYZ-1234","verification_uri":"https://okta.example/activate","expires_in":600,"interval":1}`)
	a.f.QueueToken(400, `{"error":"authorization_pending"}`)
	a.tokenOK()
	if code := a.h.run("auth", "login", "--device"); code != 0 {
		t.Fatalf("exit %d: %s", code, a.h.out.String())
	}
	if d := decode(t, a.h)["data"].(map[string]any); d["flow"] != "device" {
		t.Errorf("flow %v", d["flow"])
	}
	if !strings.Contains(a.errW.String(), "WXYZ-1234") || !strings.Contains(a.errW.String(), "https://okta.example/activate") {
		t.Errorf("prompt on stderr: %q", a.errW.String())
	}
	a.noLeak(t)
}

func TestAuthAgentModeRefused(t *testing.T) {
	for _, args := range [][]string{{"auth", "login"}, {"auth", "login", "--device"}, {"auth", "status"}, {"auth", "logout"}} {
		a := newAuthFixture(t, domain.ModeAgent)
		if code := a.h.run(args...); code != 6 {
			t.Errorf("%v: exit %d, want 6", args, code)
		}
		if a.f.Count("/v1/token") != 0 || len(a.f.Requests()) != 0 {
			t.Errorf("%v: Okta must not be contacted", args)
		}
		if e := decode(t, a.h)["error"].(map[string]any); e["code"] != "policy_denied" {
			t.Errorf("%v: %v", args, e)
		}
	}
}

func TestAuthLoginFailuresExit3(t *testing.T) {
	a := newAuthFixture(t, domain.ModeHuman)
	a.f.QueueToken(400, `{"error":"invalid_grant","error_description":"bad code"}`)
	if code := a.h.run("auth", "login"); code != 3 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(a.h.out.String(), "invalid_grant") {
		t.Error("Okta error not shown")
	}
	// misconfigured profile -> wrapped as login failure, exit 3
	b := newAuthFixture(t, domain.ModeHuman)
	b.h.env.Profile.Okta.ClientID = ""
	if code := b.h.run("auth", "login"); code != 3 || !strings.Contains(b.h.out.String(), "okta.client_id") {
		t.Errorf("misconfigured exit %d: %s", code, b.h.out.String())
	}
	// stub keychain fails closed and names the escape hatch
	c := newAuthFixture(t, domain.ModeHuman)
	c.h.env.Keychain = humanauth.SelectStore("darwin", false, false, "")
	c.tokenOK()
	if code := c.h.run("auth", "login"); code != 3 || !strings.Contains(c.h.out.String(), "--insecure-store") {
		t.Errorf("stub exit %d: %s", code, c.h.out.String())
	}
	c.noLeak(t)
	// no keychain at all
	d := newAuthFixture(t, domain.ModeHuman)
	d.h.env.Keychain = nil
	if code := d.h.run("auth", "status"); code != 3 {
		t.Errorf("nil keychain exit %d", code)
	}
}

func TestAuthInsecureStoreFlag(t *testing.T) {
	a := newAuthFixture(t, domain.ModeHuman)
	a.h.env.Keychain = humanauth.SelectStore("darwin", false, false, "")
	a.tokenOK()
	if code := a.h.run("auth", "login", "--insecure-store"); code != 0 {
		t.Fatalf("exit %d: %s", code, a.h.out.String())
	}
	if d := decode(t, a.h)["data"].(map[string]any); d["store"] != "insecure-file" {
		t.Errorf("store %v", d["store"])
	}
	if !strings.Contains(a.errW.String(), "warning") {
		t.Error("insecure store must warn")
	}
	fi, err := os.Stat(a.deps.InsecurePath)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("file: %v %v", fi, err)
	}
	if code := a.h.run("auth", "status", "--insecure-store"); code != 0 {
		t.Errorf("status exit %d", code)
	}
	if code := a.h.run("auth", "logout", "--insecure-store"); code != 0 {
		t.Errorf("logout exit %d", code)
	}
	a.noLeak(t)
}

func TestAuthLogoutPartialFailure(t *testing.T) {
	a := newAuthFixture(t, domain.ModeHuman)
	a.tokenOK()
	if code := a.h.run("auth", "login"); code != 0 {
		t.Fatal(code)
	}
	a.f.SetRevoke(500)
	if code := a.h.run("auth", "logout"); code != 1 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(a.h.out.String(), "partially failed") {
		t.Errorf("out %s", a.h.out.String())
	}
	if _, err := a.st.Load("dev"); err == nil {
		t.Error("local credentials should be gone")
	}
	a.noLeak(t)
}

func TestAuthDefaultsWithoutExtra(t *testing.T) {
	a := newAuthFixture(t, domain.ModeHuman)
	a.h.env.HumanAuth = nil
	a.h.env.Err = nil
	a.f.QueueToken(200, `{"access_token":"x","expires_in":60}`)
	_ = a.st.Save("dev", humanauth.Credentials{Issuer: a.f.Issuer(), ClientID: "cid", Subject: "s", AccessToken: "x", Expiry: time.Now().Add(time.Hour)})
	if code := a.h.run("auth", "status"); code != 0 {
		t.Errorf("status exit %d", code)
	}
}
