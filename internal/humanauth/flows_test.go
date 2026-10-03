package humanauth_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stainedhead/snow-cli/internal/humanauth"
	"github.com/stainedhead/snow-cli/internal/testsupport/oktafake"
)

func TestPKCESuccess(t *testing.T) {
	f := oktafake.New(t)
	b := &browser{t: t}
	var out bytes.Buffer
	// The id_token nonce must echo the one sent; patch after launch.
	f.QueueToken(200, tokenBody(secretAccess, secretRefresh, map[string]any{"sub": "00u1"}, 3600))
	cr, err := humanauth.LoginPKCE(context.Background(), cfgFor(f), humanauth.PKCEOptions{Launch: b.launch, Out: &out})
	must(t, err)
	if cr.Subject != "00u1" || cr.AccessToken != secretAccess || cr.RefreshToken != secretRefresh {
		t.Fatalf("credentials: %v", cr)
	}
	for _, want := range []string{"/v1/authorize", "code_challenge_method=S256", "scope=openid+profile+email+offline_access+snow.user", "client_id=cid"} {
		if !strings.Contains(b.gotURL, want) {
			t.Errorf("authorize URL lacks %s: %s", want, b.gotURL)
		}
	}
	if !strings.Contains(out.String(), "/v1/authorize") {
		t.Error("URL not printed")
	}
	req := f.Requests()[0]
	if req.Form.Get("grant_type") != "authorization_code" || req.Form.Get("code") != "auth-code" ||
		!strings.HasPrefix(req.Form.Get("redirect_uri"), "http://127.0.0.1:") {
		t.Errorf("token request %v", req.Form)
	}
	sum := sha256.Sum256([]byte(req.Form.Get("code_verifier")))
	if !strings.Contains(b.gotURL, "code_challenge="+base64.RawURLEncoding.EncodeToString(sum[:])) {
		t.Error("verifier does not match challenge")
	}
	notContains(t, "output", out.String())
}

func TestPKCEFailures(t *testing.T) {
	cases := []struct {
		name     string
		override map[string][]string
		want     string
	}{
		{"state mismatch", map[string][]string{"state": {"forged"}}, "state did not match"},
		{"okta error", map[string][]string{"error": {"access_denied"}, "error_description": {"nope"}}, "access_denied"},
		{"missing code", map[string][]string{"code": {""}}, "no authorization code"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := oktafake.New(t)
			b := &browser{t: t, override: tc.override}
			_, err := humanauth.LoginPKCE(context.Background(), cfgFor(f), humanauth.PKCEOptions{Launch: b.launch})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v", err)
			}
			if f.Count("/v1/token") != 0 {
				t.Error("code must not be exchanged")
			}
		})
	}
}

func TestPKCELauncherFailurePrintsURL(t *testing.T) {
	f := oktafake.New(t)
	var out bytes.Buffer
	b := &browser{t: t}
	launch := func(u string) error { _ = b.launch(u); return errors.New("no browser") }
	f.QueueToken(200, tokenBody("a", "r", nil, 60))
	_, err := humanauth.LoginPKCE(context.Background(), cfgFor(f), humanauth.PKCEOptions{Launch: launch, Out: &out})
	must(t, err)
	if !strings.Contains(out.String(), "Could not open a browser") || !strings.Contains(out.String(), "/v1/authorize") {
		t.Errorf("out = %q", out.String())
	}
}

func TestPKCETokenErrorsAndTimeout(t *testing.T) {
	f := oktafake.New(t)
	f.QueueToken(400, `{"error":"invalid_grant","error_description":"code used"}`)
	_, err := humanauth.LoginPKCE(context.Background(), cfgFor(f), humanauth.PKCEOptions{Launch: (&browser{t: t}).launch})
	var oe *humanauth.OAuthError
	if !errors.As(err, &oe) || oe.Code != "invalid_grant" {
		t.Fatalf("err = %v", err)
	}
	_, err = humanauth.LoginPKCE(context.Background(), cfgFor(f), humanauth.PKCEOptions{Launch: func(string) error { return nil }, Timeout: 50 * time.Millisecond})
	var lr *humanauth.LoginRequiredError
	if !errors.As(err, &lr) || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("timeout err = %v", err)
	}
	if _, err = humanauth.LoginPKCE(context.Background(), humanauth.Config{}, humanauth.PKCEOptions{}); err == nil {
		t.Fatal("config must be validated")
	}
	if _, err = humanauth.LoginPKCE(context.Background(), cfgFor(f), humanauth.PKCEOptions{Port: 1, Launch: func(string) error { return nil }}); err == nil {
		t.Fatal("privileged port must fail")
	}
}

func TestPKCENonceMismatch(t *testing.T) {
	f := oktafake.New(t)
	f.QueueToken(200, tokenBody("a", "r", map[string]any{"sub": "s", "nonce": "someone-elses"}, 60))
	_, err := humanauth.LoginPKCE(context.Background(), cfgFor(f), humanauth.PKCEOptions{Launch: (&browser{t: t}).launch})
	if err == nil || !strings.Contains(err.Error(), "nonce") {
		t.Fatalf("err = %v", err)
	}
}

func TestConfigValidate(t *testing.T) {
	for issuer, ok := range map[string]bool{
		"https://acme.okta.com/oauth2/default": true, "http://127.0.0.1:9": true, "http://localhost:9": true,
		"http://acme.okta.com": false, "": false, "not a url": false, "https://": false,
	} {
		err := (humanauth.Config{Issuer: issuer, ClientID: "c"}).Validate()
		if (err == nil) != ok {
			t.Errorf("%q: %v", issuer, err)
		}
	}
	if (humanauth.Config{Issuer: "https://a.b"}).Validate() == nil {
		t.Error("client id required")
	}
}

const deviceOK = `{"device_code":"dev-code","user_code":"ABCD-EFGH","verification_uri":"https://okta.example/activate","verification_uri_complete":"https://okta.example/activate?user_code=ABCD-EFGH","expires_in":600,"interval":5}`

func TestDeviceFlowPollingRules(t *testing.T) {
	f := oktafake.New(t)
	f.QueueDevice(200, deviceOK)
	f.QueueToken(400, `{"error":"authorization_pending"}`)
	f.QueueToken(400, `{"error":"slow_down"}`)
	f.QueueToken(400, `{"error":"authorization_pending"}`)
	f.QueueToken(200, tokenBody(secretAccess, secretRefresh, map[string]any{"sub": "00u2"}, 3600))
	var slept []time.Duration
	var out bytes.Buffer
	cr, err := humanauth.LoginDevice(context.Background(), cfgFor(f), humanauth.DeviceOptions{Out: &out, Sleep: noSleep(&slept)})
	must(t, err)
	if cr.Subject != "00u2" {
		t.Error("subject")
	}
	want := []time.Duration{5 * time.Second, 5 * time.Second, 10 * time.Second, 10 * time.Second}
	if len(slept) != len(want) {
		t.Fatalf("slept %v", slept)
	}
	for i := range want {
		if slept[i] != want[i] {
			t.Errorf("sleep[%d] = %v want %v", i, slept[i], want[i])
		}
	}
	for _, s := range []string{"https://okta.example/activate", "ABCD-EFGH"} {
		if !strings.Contains(out.String(), s) {
			t.Errorf("prompt lacks %s: %q", s, out.String())
		}
	}
	notContains(t, "prompt", out.String(), "dev-code")
	last := f.Requests()[len(f.Requests())-1]
	if last.Form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:device_code" || last.Form.Get("device_code") != "dev-code" {
		t.Errorf("poll form %v", last.Form)
	}
}

func TestDeviceFlowTerminalErrors(t *testing.T) {
	for _, code := range []string{"access_denied", "expired_token"} {
		f := oktafake.New(t)
		f.QueueDevice(200, deviceOK)
		f.QueueToken(400, `{"error":"`+code+`"}`)
		var s []time.Duration
		_, err := humanauth.LoginDevice(context.Background(), cfgFor(f), humanauth.DeviceOptions{Sleep: noSleep(&s)})
		var oe *humanauth.OAuthError
		if !errors.As(err, &oe) || oe.Code != code {
			t.Errorf("%s: %v", code, err)
		}
	}
}

func TestDeviceFlowExpiry(t *testing.T) {
	f := oktafake.New(t)
	f.QueueDevice(200, `{"device_code":"d","user_code":"U","verification_uri":"https://v","expires_in":12,"interval":5}`)
	f.QueueToken(400, `{"error":"authorization_pending"}`)
	f.QueueToken(400, `{"error":"authorization_pending"}`)
	clk := fixedNow()
	cfg := cfgFor(f)
	cfg.Now = clk.now
	sleep := func(_ context.Context, d time.Duration) error { clk.t = clk.t.Add(d); return nil }
	_, err := humanauth.LoginDevice(context.Background(), cfg, humanauth.DeviceOptions{Sleep: sleep})
	var lr *humanauth.LoginRequiredError
	if !errors.As(err, &lr) || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("err = %v", err)
	}
	if f.Count("/v1/token") != 2 {
		t.Errorf("polled %d times", f.Count("/v1/token"))
	}
}

func TestDeviceFlowOtherFailures(t *testing.T) {
	f := oktafake.New(t)
	f.QueueDevice(400, `{"error":"invalid_client"}`)
	if _, err := humanauth.LoginDevice(context.Background(), cfgFor(f), humanauth.DeviceOptions{}); err == nil {
		t.Error("authorize failure")
	}
	f.QueueDevice(200, `{"nope":1}`)
	if _, err := humanauth.LoginDevice(context.Background(), cfgFor(f), humanauth.DeviceOptions{}); err == nil {
		t.Error("malformed")
	}
	// context cancel during the wait
	f.QueueDevice(200, deviceOK)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := humanauth.LoginDevice(ctx, cfgFor(f), humanauth.DeviceOptions{}); !errors.Is(err, context.Canceled) {
		t.Errorf("cancel: %v", err)
	}
	// transport failure while polling is not retried
	f.QueueDevice(200, deviceOK)
	cfg := cfgFor(f)
	cfg.HTTP = &http.Client{Transport: failAfter{n: 1, rt: http.DefaultTransport}}
	var s []time.Duration
	if _, err := humanauth.LoginDevice(context.Background(), cfg, humanauth.DeviceOptions{Sleep: noSleep(&s)}); err == nil {
		t.Error("transport error")
	}
	if _, err := humanauth.LoginDevice(context.Background(), humanauth.Config{}, humanauth.DeviceOptions{}); err == nil {
		t.Error("config")
	}
	// default interval/expiry when absent, default sleep with a short real interval is avoided via Sleep
	f.QueueDevice(200, `{"device_code":"d","user_code":"U","verification_uri":"https://v"}`)
	f.QueueToken(200, tokenBody("a", "r", nil, 0))
	cr, err := humanauth.LoginDevice(context.Background(), cfgFor(f), humanauth.DeviceOptions{Sleep: noSleep(&s)})
	must(t, err)
	if s[len(s)-1] != 5*time.Second || cr.Expiry.IsZero() {
		t.Errorf("defaults: %v", s)
	}
}

type failAfter struct {
	n  int
	rt http.RoundTripper
}

func (f failAfter) RoundTrip(r *http.Request) (*http.Response, error) {
	if strings.HasSuffix(r.URL.Path, "/token") {
		return nil, errors.New("connection reset")
	}
	return f.rt.RoundTrip(r)
}

// TestAssumptionLoopbackRedirect documents ASSUMPTION(unverified against a
// real instance): the redirect URI is http://127.0.0.1:<port>/callback and
// the requested scopes include snow.user.
func TestAssumptionLoopbackRedirect(t *testing.T) {
	f := oktafake.New(t)
	b := &browser{t: t}
	f.QueueToken(200, tokenBody("a", "r", nil, 60))
	_, err := humanauth.LoginPKCE(context.Background(), cfgFor(f), humanauth.PKCEOptions{Launch: b.launch})
	must(t, err)
	if !strings.Contains(b.gotURL, "redirect_uri=http%3A%2F%2F127.0.0.1%3A") || !strings.Contains(b.gotURL, "snow.user") {
		t.Errorf("url %s", b.gotURL)
	}
}
