package humanauth

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestSleepCtx(t *testing.T) {
	if err := sleepCtx(context.Background(), time.Millisecond); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sleepCtx(ctx, time.Hour); err == nil {
		t.Fatal("canceled context must stop the sleep")
	}
}

func TestScopeTruncateClaims(t *testing.T) {
	if got := (Config{}).scope(); got != strings.Join(DefaultScopes, " ") {
		t.Errorf("default scope %q", got)
	}
	if got := (Config{Scopes: []string{"a", "b"}}).scope(); got != "a b" {
		t.Errorf("custom scope %q", got)
	}
	if truncate("abcdef", 3) != "abc" || truncate("ab", 3) != "ab" {
		t.Error("truncate")
	}
	for _, bad := range []string{"", "onlyone", "a.!!!.c", "a." + base64.RawURLEncoding.EncodeToString([]byte("not json")) + ".c"} {
		if cl := idClaims(bad); cl.Sub != "" || cl.Nonce != "" {
			t.Errorf("idClaims(%q) = %+v", bad, cl)
		}
	}
}

func TestCredentialsGoStringRedacts(t *testing.T) {
	c := Credentials{Issuer: "i", Subject: "s", AccessToken: "SECRET", RefreshToken: "SECRET2"}
	for _, s := range []string{c.GoString(), fmt.Sprintf("%#v", c), fmt.Sprintf("%+v", c)} {
		if strings.Contains(s, "SECRET") {
			t.Errorf("leak: %s", s)
		}
	}
}

func TestRandStringAndDetectWSL(t *testing.T) {
	a, err := randString(16)
	if err != nil || len(a) == 0 {
		t.Fatalf("randString: %q %v", a, err)
	}
	b, _ := randString(16)
	if a == b {
		t.Error("randString repeated")
	}
	_ = DetectWSL() // platform dependent; must not panic
}

func TestSystemLauncherReturnsStartError(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // no opener binary can be found
	if err := SystemLauncher("http://127.0.0.1:1/"); err == nil {
		t.Skip("platform opener resolved without PATH")
	}
}

func TestCheckTargetUnparsableIssuer(t *testing.T) {
	c := Config{Issuer: "http://[::1"}
	u := mustParse(t, "https://okta.example.test/x")
	if err := c.checkTarget(u); err == nil {
		t.Fatal("unparsable issuer must refuse")
	}
}

func TestCheckTargetDowngradeAndLoop(t *testing.T) {
	c := Config{Issuer: "https://okta.example.test"}
	if err := c.checkTarget(mustParse(t, "http://okta.example.test/v1/token")); err == nil || !strings.Contains(err.Error(), "plain http") {
		t.Errorf("downgrade: %v", err)
	}
	if err := c.checkTarget(mustParse(t, "https://OKTA.example.test/v1/token")); err != nil {
		t.Errorf("same host case-insensitive: %v", err)
	}
	if err := c.checkRedirect(nil, make([]*httpRequest, 10)); err == nil {
		t.Error("redirect limit")
	}
}

type httpRequest = http.Request

func mustParse(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
