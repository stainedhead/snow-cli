package humanauth_test

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stainedhead/snow-cli/internal/humanauth"
	"github.com/stainedhead/snow-cli/internal/testsupport/oktafake"
)

// probe sends a request to the loopback callback and returns the status.
func probe(t *testing.T, rawURL, host string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	must(t, err)
	if host != "" {
		req.Host = host
	}
	resp, err := http.DefaultClient.Do(req)
	must(t, err)
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	return resp.StatusCode
}

func TestPKCEBadStateIgnoredLoginContinues(t *testing.T) {
	f := oktafake.New(t)
	b := &browser{t: t}
	b.queueEchoToken(f, secretAccess, secretRefresh, map[string]any{"sub": "00u1"})
	var statuses []int
	done := make(chan struct{})
	launch := func(raw string) error {
		u, _ := url.Parse(raw)
		cb := u.Query().Get("redirect_uri")
		go func() {
			defer close(done)
			statuses = append(statuses,
				probe(t, cb+"?state=wrong&code=evil", ""),
				probe(t, cb+"?code=evil", ""),
				probe(t, cb+"?error=access_denied&state=wrong", ""),
			)
			_ = b.launch(raw) // the genuine callback arrives after the noise
		}()
		return nil
	}
	cr, err := humanauth.LoginPKCE(context.Background(), cfgFor(f), humanauth.PKCEOptions{Launch: launch, Timeout: 5 * time.Second})
	must(t, err)
	<-done
	if cr.AccessToken != secretAccess {
		t.Fatalf("creds %v", cr)
	}
	for i, s := range statuses {
		if s != http.StatusBadRequest {
			t.Errorf("hostile request %d answered %d, want 400", i, s)
		}
	}
	if got := f.Requests()[0].Form.Get("code"); got != "auth-code" {
		t.Errorf("exchanged code %q, want the genuine one", got)
	}
}

func TestPKCEBadStateOnlyKeepsWaitingUntilTimeout(t *testing.T) {
	f := oktafake.New(t)
	launch := func(raw string) error {
		u, _ := url.Parse(raw)
		go probe(t, u.Query().Get("redirect_uri")+"?state=wrong&code=evil", "")
		return nil
	}
	_, err := humanauth.LoginPKCE(context.Background(), cfgFor(f), humanauth.PKCEOptions{Launch: launch, Timeout: 300 * time.Millisecond})
	var lr *humanauth.LoginRequiredError
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v, want timeout", err)
	}
	_ = lr
	if f.Count("/v1/token") != 0 {
		t.Error("token endpoint contacted after a forged callback")
	}
}

func TestPKCEForeignHostHeaderRefused(t *testing.T) {
	f := oktafake.New(t)
	b := &browser{t: t}
	b.queueEchoToken(f, secretAccess, secretRefresh, map[string]any{"sub": "00u1"})
	var rebind int
	done := make(chan struct{})
	launch := func(raw string) error {
		u, _ := url.Parse(raw)
		q := u.Query()
		cb := q.Get("redirect_uri")
		go func() {
			defer close(done)
			// Valid state and code, but a rebound DNS name in Host.
			rebind = probe(t, cb+"?code=evil&state="+url.QueryEscape(q.Get("state")), "attacker.example")
			_ = b.launch(raw)
		}()
		return nil
	}
	_, err := humanauth.LoginPKCE(context.Background(), cfgFor(f), humanauth.PKCEOptions{Launch: launch, Timeout: 5 * time.Second})
	must(t, err)
	<-done
	if rebind != http.StatusBadRequest && rebind != http.StatusForbidden {
		t.Errorf("foreign Host answered %d, want 400/403", rebind)
	}
	if got := f.Requests()[0].Form.Get("code"); got != "auth-code" {
		t.Errorf("rebound request's code was used: %q", got)
	}
}

func TestPKCEMissingNonceInIDTokenFails(t *testing.T) {
	f := oktafake.New(t)
	f.QueueToken(200, tokenBody("a", "r", map[string]any{"sub": "s"}, 60)) // id_token without nonce
	_, err := humanauth.LoginPKCE(context.Background(), cfgFor(f), humanauth.PKCEOptions{Launch: (&browser{t: t}).launch})
	if err == nil || !strings.Contains(err.Error(), "nonce") {
		t.Fatalf("err = %v, want nonce failure", err)
	}
}

func TestPKCENoIDTokenNoNonceCheck(t *testing.T) {
	f := oktafake.New(t)
	f.QueueToken(200, tokenBody("a", "r", nil, 60)) // no id_token at all
	cr, err := humanauth.LoginPKCE(context.Background(), cfgFor(f), humanauth.PKCEOptions{Launch: (&browser{t: t}).launch})
	must(t, err)
	if cr.AccessToken != "a" {
		t.Fatal("access token missing")
	}
}
