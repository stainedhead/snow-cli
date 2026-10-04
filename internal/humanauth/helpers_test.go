package humanauth_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stainedhead/snow-cli/internal/humanauth"
	"github.com/stainedhead/snow-cli/internal/testsupport/oktafake"
)

const (
	secretAccess  = "ACCESS-SECRET-aaaa1111"
	secretRefresh = "REFRESH-SECRET-bbbb2222"
	secretID      = "ID-SECRET-cccc3333"
)

func jwt(claims map[string]any) string {
	b, _ := json.Marshal(claims)
	return "e30." + base64.RawURLEncoding.EncodeToString(b) + ".sig"
}

func tokenBody(access, refresh string, claims map[string]any, expires int) string {
	m := map[string]any{"access_token": access, "token_type": "Bearer", "expires_in": expires, "scope": "openid snow.user"}
	if refresh != "" {
		m["refresh_token"] = refresh
	}
	if claims != nil {
		m["id_token"] = jwt(claims)
	}
	b, _ := json.Marshal(m)
	return string(b)
}

func cfgFor(f *oktafake.Fake) humanauth.Config {
	return humanauth.Config{Issuer: f.Issuer(), ClientID: "cid"}
}

// browser plays the user's browser: it follows the authorize URL's redirect.
type browser struct {
	t        *testing.T
	override url.Values // replaces callback params
	gotURL   string
	mu       sync.Mutex
	nonce    string
}

// Nonce returns the nonce of the authorize URL the browser was sent to.
func (b *browser) Nonce() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.nonce
}

// queueEchoToken queues a token response whose id_token echoes the nonce.
func (b *browser) queueEchoToken(f *oktafake.Fake, access, refresh string, claims map[string]any) {
	f.QueueTokenFunc(func() (int, string) {
		c := map[string]any{"nonce": b.Nonce()}
		for k, v := range claims {
			c[k] = v
		}
		return 200, tokenBody(access, refresh, c, 3600)
	})
}

func (b *browser) launch(raw string) error {
	b.gotURL = raw
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	q := u.Query()
	b.mu.Lock()
	b.nonce = q.Get("nonce")
	b.mu.Unlock()
	cb := q.Get("redirect_uri")
	params := url.Values{"code": {"auth-code"}, "state": {q.Get("state")}}
	for k, v := range b.override {
		if len(v) == 0 || v[0] == "" {
			params.Del(k)
		} else {
			params[k] = v
		}
	}
	go func() {
		resp, err := http.Get(cb + "?" + params.Encode())
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
	}()
	return nil
}

type clockT struct{ t time.Time }

func (c *clockT) now() time.Time { return c.t }

func fixedNow() *clockT { return &clockT{t: time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)} }

func noSleep(d *[]time.Duration) func(context.Context, time.Duration) error {
	return func(_ context.Context, x time.Duration) error { *d = append(*d, x); return nil }
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func notContains(t *testing.T, what, s string, secrets ...string) {
	t.Helper()
	for _, sec := range append(secrets, secretAccess, secretRefresh, secretID) {
		if strings.Contains(s, sec) {
			t.Errorf("%s leaks a secret: %q", what, s)
		}
	}
}

var _ = fmt.Sprint
