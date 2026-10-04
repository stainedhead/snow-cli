package humanauth_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stainedhead/agent-cli-core/auth"
	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/snow-cli/internal/humanauth"
	"github.com/stainedhead/snow-cli/internal/testsupport/oktafake"
)

type env struct {
	f     *oktafake.Fake
	st    *humanauth.MemoryStore
	clk   *clockT
	cfg   humanauth.Config
	src   *humanauth.Source
	creds humanauth.Credentials
}

func newEnv(t *testing.T, tokenType string, expiresIn time.Duration) *env {
	t.Helper()
	f := oktafake.New(t)
	clk := fixedNow()
	cfg := cfgFor(f)
	cfg.Now = clk.now
	cfg.TokenType = tokenType
	e := &env{f: f, st: humanauth.NewMemoryStore(), clk: clk, cfg: cfg}
	e.creds = humanauth.Credentials{Issuer: f.Issuer(), ClientID: "cid", Subject: "00u1", Scope: "openid",
		AccessToken: secretAccess, IDToken: secretID, RefreshToken: secretRefresh, Expiry: clk.t.Add(expiresIn)}
	must(t, e.st.Save("dev", e.creds))
	e.src = humanauth.NewSource(cfg, e.st, "dev")
	return e
}

// authHeader extracts the bearer value through the only sanctioned path.
func authHeader(t *testing.T, s auth.TokenSource) (string, error) {
	t.Helper()
	req, _ := http.NewRequest("GET", "http://x.invalid", nil)
	err := auth.NewAuthorizer(s).Authorize(context.Background(), req)
	return req.Header.Get("Authorization"), err
}

func TestSourceImplementsRefresher(t *testing.T) {
	var s auth.TokenSource = humanauth.NewSource(humanauth.Config{}, humanauth.NewMemoryStore(), "p")
	if _, ok := s.(auth.Refresher); !ok {
		t.Fatal("human source must implement auth.Refresher")
	}
}

func TestSourceServesValidTokenWithoutNetwork(t *testing.T) {
	e := newEnv(t, "", time.Hour)
	h, err := authHeader(t, e.src)
	must(t, err)
	if h != "Bearer "+secretAccess {
		t.Errorf("header %q", h)
	}
	if len(e.f.Requests()) != 0 {
		t.Error("no refresh expected")
	}
}

func TestSourceIDTokenType(t *testing.T) {
	e := newEnv(t, "id", time.Hour)
	h, err := authHeader(t, e.src)
	must(t, err)
	if h != "Bearer "+secretID {
		t.Errorf("header %q", h)
	}
}

func TestSourceRefreshesWithRotationAndPersists(t *testing.T) {
	e := newEnv(t, "", 30*time.Second) // inside the skew
	e.f.QueueToken(200, tokenBody("NEW-ACCESS-9999", "NEW-REFRESH-8888", map[string]any{"sub": "00u1"}, 3600))
	h, err := authHeader(t, e.src)
	must(t, err)
	if h != "Bearer NEW-ACCESS-9999" {
		t.Errorf("header %q", h)
	}
	req := e.f.Requests()[0]
	if req.Form.Get("grant_type") != "refresh_token" || req.Form.Get("refresh_token") != secretRefresh || req.Form.Get("client_id") != "cid" {
		t.Errorf("refresh form %v", req.Form)
	}
	got, err := e.st.Load("dev")
	must(t, err)
	if got.RefreshToken != "NEW-REFRESH-8888" || got.AccessToken != "NEW-ACCESS-9999" || !got.Expiry.Equal(e.clk.t.Add(time.Hour)) {
		t.Errorf("not rotated/persisted: %v", got)
	}
	// second call is served from the store without another refresh
	if _, err := authHeader(t, e.src); err != nil || e.f.Count("/v1/token") != 1 {
		t.Errorf("second call: %v count %d", err, e.f.Count("/v1/token"))
	}
}

func TestSourceKeepsRefreshTokenWhenNotRotated(t *testing.T) {
	e := newEnv(t, "", -time.Minute)
	e.f.QueueToken(200, tokenBody("NEW-ACCESS-9999", "", nil, 3600))
	_, err := e.src.Refresh(context.Background())
	must(t, err)
	got, _ := e.st.Load("dev")
	if got.RefreshToken != secretRefresh || got.IDToken != secretID || got.Subject != "00u1" {
		t.Errorf("lost fields: %v", got)
	}
}

func TestSourceForcedRefreshIgnoresExpiry(t *testing.T) {
	e := newEnv(t, "", time.Hour)
	e.f.QueueToken(200, tokenBody("FORCED-1", "R2", nil, 3600))
	_, err := e.src.Refresh(context.Background())
	must(t, err)
	if e.f.Count("/v1/token") != 1 {
		t.Error("forced refresh must call Okta")
	}
	// Authorizer.Refresh drives the same path (core's single-401 retry).
	e.f.QueueToken(200, tokenBody("FORCED-2", "R3", nil, 3600))
	must(t, auth.NewAuthorizer(e.src).Refresh(context.Background()))
	got, _ := e.st.Load("dev")
	if got.RefreshToken != "R3" {
		t.Error("rotation not persisted")
	}
}

func TestSourceRefreshRejectedShowsOktaErrorExit3(t *testing.T) {
	e := newEnv(t, "", -time.Minute)
	e.f.QueueToken(400, `{"error":"invalid_grant","error_description":"The refresh token is invalid or expired."}`)
	_, err := authHeader(t, e.src)
	if err == nil || output.ExitOf(err) != 3 {
		t.Fatalf("err = %v exit %d", err, output.ExitOf(err))
	}
	if !strings.Contains(err.Error(), "invalid_grant") || !strings.Contains(err.Error(), "invalid or expired") {
		t.Errorf("Okta error not shown: %v", err)
	}
	env := output.FromError(err)
	if !strings.Contains(env.Error.Hint, "snow auth login") {
		t.Errorf("hint %q", env.Error.Hint)
	}
	notContains(t, "error", err.Error())
	if _, e2 := e.st.Load("dev"); e2 != nil {
		t.Error("credentials must be kept on refresh rejection (user may retry)")
	}
}

func TestSourceRefreshOtherFailures(t *testing.T) {
	// transport failure -> TokenError, exit 3
	e := newEnv(t, "", -time.Minute)
	e.src = humanauth.NewSource(func() humanauth.Config {
		c := e.cfg
		c.HTTP = &http.Client{Transport: failAfter{rt: http.DefaultTransport}}
		return c
	}(), e.st, "dev")
	_, err := e.src.Token(context.Background())
	var te *auth.TokenError
	if !errors.As(err, &te) || output.ExitOf(err) != 3 {
		t.Errorf("transport: %v", err)
	}
	// 200 with no access token
	e2 := newEnv(t, "", -time.Minute)
	e2.f.QueueToken(200, `{"expires_in":10}`)
	if _, err := e2.src.Token(context.Background()); !errors.As(err, &te) {
		t.Errorf("empty: %v", err)
	}
	// 200 but id token requested and absent
	e3 := newEnv(t, "id", -time.Minute)
	e3.creds.IDToken = ""
	must(t, e3.st.Save("dev", e3.creds))
	e3.f.QueueToken(200, tokenBody("A", "R", nil, 60))
	if _, err := e3.src.Token(context.Background()); !errors.As(err, &te) {
		t.Errorf("no id token: %v", err)
	}
	// persist failure is an error, not a silent loss
	e4 := newEnv(t, "", -time.Minute)
	e4.st.SaveErr = errors.New("disk full")
	e4.f.QueueToken(200, tokenBody("A", "R", nil, 60))
	if _, err := e4.src.Token(context.Background()); !errors.As(err, &te) || !strings.Contains(err.Error(), "persist") {
		t.Errorf("persist: %v", err)
	}
	// no refresh token
	e5 := newEnv(t, "", -time.Minute)
	e5.creds.RefreshToken = ""
	must(t, e5.st.Save("dev", e5.creds))
	var lr *humanauth.LoginRequiredError
	if _, err := e5.src.Token(context.Background()); !errors.As(err, &lr) {
		t.Errorf("no refresh token: %v", err)
	}
}

func TestSourceNotLoggedInAndMisconfigured(t *testing.T) {
	f := oktafake.New(t)
	src := humanauth.NewSource(cfgFor(f), humanauth.NewMemoryStore(), "dev")
	_, err := src.Token(context.Background())
	var lr *humanauth.LoginRequiredError
	if !errors.As(err, &lr) || output.ExitOf(err) != 3 || !strings.Contains(err.Error(), "not logged in") {
		t.Errorf("not logged in: %v", err)
	}
	if _, err := humanauth.NewSource(humanauth.Config{}, humanauth.NewMemoryStore(), "dev").Token(context.Background()); !errors.As(err, &lr) {
		t.Errorf("misconfigured: %v", err)
	}
	// issuer mismatch
	e := newEnv(t, "", time.Hour)
	other := e.cfg
	other.Issuer = "https://other.example"
	if _, err := humanauth.NewSource(other, e.st, "dev").Token(context.Background()); !errors.As(err, &lr) {
		t.Errorf("mismatch: %v", err)
	}
	// store backend error passes through
	stub := humanauth.StubStore{Backend: "b", Reason: "r"}
	var su *humanauth.StoreUnavailableError
	if _, err := humanauth.NewSource(cfgFor(f), stub, "dev").Token(context.Background()); !errors.As(err, &su) {
		t.Errorf("stub: %v", err)
	}
}

func TestSourceConcurrentRefreshRotatesOnce(t *testing.T) {
	e := newEnv(t, "", -time.Minute)
	e.f.QueueToken(200, tokenBody("A2", "R2", nil, 3600))
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := e.src.Token(context.Background()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if e.f.Count("/v1/token") != 1 {
		t.Errorf("refresh calls = %d", e.f.Count("/v1/token"))
	}
}

func TestStatusOf(t *testing.T) {
	e := newEnv(t, "", time.Hour)
	st, err := humanauth.StatusOf(e.st, "dev", e.clk.t)
	must(t, err)
	if st.Subject != "00u1" || st.Expired || st.Store != "memory" || st.Issuer != e.f.Issuer() {
		t.Errorf("%+v", st)
	}
	if st, _ = humanauth.StatusOf(e.st, "dev", e.clk.t.Add(2*time.Hour)); !st.Expired {
		t.Error("expired")
	}
	notContains(t, "status", fmt.Sprintf("%+v", st))
	_, err = humanauth.StatusOf(e.st, "nope", e.clk.t)
	var lr *humanauth.LoginRequiredError
	if !errors.As(err, &lr) || output.ExitOf(err) != 3 {
		t.Errorf("not logged in: %v", err)
	}
	if _, err := humanauth.StatusOf(humanauth.StubStore{Backend: "b"}, "dev", e.clk.t); err == nil {
		t.Error("stub")
	}
}

func TestLogout(t *testing.T) {
	e := newEnv(t, "", time.Hour)
	revoked, err := humanauth.Logout(context.Background(), e.cfg, e.st, "dev")
	must(t, err)
	if !revoked || e.f.Count("/v1/revoke") != 2 {
		t.Errorf("revoked=%v calls=%d", revoked, e.f.Count("/v1/revoke"))
	}
	seen := map[string]string{}
	for _, r := range e.f.Requests() {
		seen[r.Form.Get("token_type_hint")] = r.Form.Get("token")
	}
	if seen["refresh_token"] != secretRefresh || seen["access_token"] != secretAccess {
		t.Errorf("revoked tokens %v", seen)
	}
	if _, err := e.st.Load("dev"); !errors.Is(err, humanauth.ErrNotFound) {
		t.Error("credentials not deleted")
	}
	// logging out again
	if _, err := humanauth.Logout(context.Background(), e.cfg, e.st, "dev"); err == nil {
		t.Error("second logout must say not logged in")
	}
}

func TestLogoutPartialFailure(t *testing.T) {
	e := newEnv(t, "", time.Hour)
	e.f.SetRevoke(500)
	revoked, err := humanauth.Logout(context.Background(), e.cfg, e.st, "dev")
	var pe *humanauth.PartialError
	if !errors.As(err, &pe) || revoked || !strings.Contains(err.Error(), "revoking the refresh token failed") {
		t.Fatalf("err = %v revoked=%v", err, revoked)
	}
	if _, lerr := e.st.Load("dev"); !errors.Is(lerr, humanauth.ErrNotFound) {
		t.Error("local credentials must still be deleted")
	}
	notContains(t, "error", err.Error())

	e2 := newEnv(t, "", time.Hour)
	e2.st.DelErr = errors.New("locked")
	revoked, err = humanauth.Logout(context.Background(), e2.cfg, e2.st, "dev")
	if !errors.As(err, &pe) || !revoked || !strings.Contains(err.Error(), "deleting the stored credentials failed") {
		t.Errorf("delete failure: %v revoked=%v", err, revoked)
	}
	// store load error
	if _, err := humanauth.Logout(context.Background(), e2.cfg, humanauth.StubStore{Backend: "b"}, "dev"); err == nil {
		t.Error("stub load")
	}
	// only a refresh token (no access token)
	e3 := newEnv(t, "", time.Hour)
	e3.creds.AccessToken = ""
	must(t, e3.st.Save("dev", e3.creds))
	if _, err := humanauth.Logout(context.Background(), e3.cfg, e3.st, "dev"); err != nil || e3.f.Count("/v1/revoke") != 1 {
		t.Errorf("refresh-only logout: %v", err)
	}
}

func TestNoTokenInAnyOutput(t *testing.T) {
	// Run login, refresh failure and logout and make sure nothing printed
	// or returned as text contains a token value.
	f := oktafake.New(t)
	var prompts strings.Builder
	br := &browser{t: t}
	br.queueEchoToken(f, secretAccess, secretRefresh, map[string]any{"sub": "u"})
	cr, err := humanauth.LoginPKCE(context.Background(), cfgFor(f), humanauth.PKCEOptions{Launch: br.launch, Out: &prompts})
	must(t, err)
	st := humanauth.NewMemoryStore()
	must(t, st.Save("dev", cr))
	status, _ := humanauth.StatusOf(st, "dev", time.Now())
	f.QueueToken(400, `{"error":"invalid_grant","error_description":"expired"}`)
	cfg := cfgFor(f)
	cfg.Now = func() time.Time { return time.Now().Add(48 * time.Hour) }
	_, rerr := humanauth.NewSource(cfg, st, "dev").Token(context.Background())
	_, lerr := humanauth.Logout(context.Background(), cfg, st, "dev")
	notContains(t, "all output", fmt.Sprint(prompts.String(), cr, status, rerr, lerr), "auth-code")
}
