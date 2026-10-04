package humanauth_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stainedhead/agent-cli-core/httpx"
	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/snow-cli/internal/humanauth"
)

// redirecting starts an "issuer" that answers every request with the given
// redirect status to a second server, and counts hits on the second server.
func redirecting(t *testing.T, status int, target func(second string) string) (issuer string, secondHits *atomic.Int32) {
	t.Helper()
	hits := &atomic.Int32{}
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(second.Close)
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target(second.URL), status)
	}))
	t.Cleanup(first.Close)
	return first.URL, hits
}

func assertForbiddenHost(t *testing.T, err error) {
	t.Helper()
	var fh *httpx.ForbiddenHostError
	if !errors.As(err, &fh) {
		t.Fatalf("want *httpx.ForbiddenHostError, got %T: %v", err, err)
	}
	if got := output.ExitOf(err); got != 4 {
		t.Errorf("exit = %d, want 4", got)
	}
}

func TestOktaRedirectToOtherHostRefused(t *testing.T) {
	for _, status := range []int{301, 302, 303, 307, 308} {
		issuer, hits := redirecting(t, status, func(s string) string { return s + "/steal" })
		cfg := humanauth.Config{Issuer: issuer, ClientID: "cid"}

		err := cfg.Revoke(context.Background(), secretRefresh, "refresh_token")
		assertForbiddenHost(t, err)
		if n := hits.Load(); n != 0 {
			t.Errorf("status %d: second server got %d requests, want 0", status, n)
		}
	}
}

func TestOktaRedirectDowngradeRefused(t *testing.T) {
	// A same-host redirect to plain http on a non-loopback name is a downgrade.
	issuer, hits := redirecting(t, 307, func(string) string { return "http://okta.example.test/v1/token" })
	cfg := humanauth.Config{Issuer: issuer, ClientID: "cid"}
	err := cfg.Revoke(context.Background(), secretRefresh, "refresh_token")
	assertForbiddenHost(t, err)
	if hits.Load() != 0 {
		t.Error("downgrade target was contacted")
	}
}

func TestOktaRedirectToSameHostAllowed(t *testing.T) {
	var final atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/revoke" {
			http.Redirect(w, r, "/v1/revoke2", http.StatusTemporaryRedirect)
			return
		}
		final.Add(1)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	cfg := humanauth.Config{Issuer: srv.URL, ClientID: "cid"}
	if err := cfg.Revoke(context.Background(), "t", "access_token"); err != nil {
		t.Fatal(err)
	}
	if final.Load() != 1 {
		t.Errorf("same-host redirect not followed")
	}
}

func TestOktaRedirectRefusedOnRefreshIsExit4(t *testing.T) {
	issuer, hits := redirecting(t, 308, func(s string) string { return s })
	clk := fixedNow()
	st := humanauth.NewMemoryStore()
	cfg := humanauth.Config{Issuer: issuer, ClientID: "cid", Now: clk.now}
	must(t, st.Save("dev", humanauth.Credentials{Issuer: issuer, ClientID: "cid", AccessToken: secretAccess,
		RefreshToken: secretRefresh, Expiry: clk.t.Add(-time.Hour)}))
	_, err := humanauth.NewSource(cfg, st, "dev").Token(context.Background())
	assertForbiddenHost(t, err)
	if hits.Load() != 0 {
		t.Error("refresh body reached the redirect target")
	}
}

func TestOktaRedirectRefusedOnLoginTokenExchange(t *testing.T) {
	issuer, hits := redirecting(t, 307, func(s string) string { return s })
	b := &browser{t: t}
	_, err := humanauth.LoginPKCE(context.Background(), humanauth.Config{Issuer: issuer, ClientID: "cid"},
		humanauth.PKCEOptions{Launch: b.launch, Timeout: 5 * time.Second})
	assertForbiddenHost(t, err)
	if hits.Load() != 0 {
		t.Error("code/verifier reached the redirect target")
	}
}

func TestOktaInjectedClientStillGuarded(t *testing.T) {
	issuer, hits := redirecting(t, 307, func(s string) string { return s })
	cfg := humanauth.Config{Issuer: issuer, ClientID: "cid", HTTP: &http.Client{Timeout: time.Second}}
	assertForbiddenHost(t, cfg.Revoke(context.Background(), "t", "access_token"))
	if hits.Load() != 0 {
		t.Error("injected client followed redirect")
	}
}

func TestOktaIssuerHostLiteralComparison(t *testing.T) {
	// Redirect to the same hostname on a different port is a different origin.
	issuer, hits := redirecting(t, 307, func(s string) string { return s })
	cfg := humanauth.Config{Issuer: issuer, ClientID: "cid"}
	assertForbiddenHost(t, cfg.Revoke(context.Background(), "t", "access_token"))
	_ = hits
}
