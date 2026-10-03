package humanauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strconv"
	"time"
)

// Launcher opens a URL in the user's browser.
type Launcher func(rawURL string) error

// SystemLauncher opens the URL with the platform opener.
func SystemLauncher(rawURL string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", rawURL)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL)
	default:
		cmd = exec.Command("xdg-open", rawURL)
	}
	return cmd.Start()
}

// PKCEOptions are the login seams.
type PKCEOptions struct {
	Launch  Launcher      // nil means SystemLauncher
	Out     io.Writer     // prompts (stderr); nil discards
	Port    int           // loopback port; 0 picks a free one
	Timeout time.Duration // 0 means 5 minutes
}

func randString(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

type callback struct {
	code string
	err  error
}

// LoginPKCE runs the authorization code flow with PKCE on a loopback
// listener (FR-011). ASSUMPTION(unverified against a real instance): the Okta
// app registration allows the http://127.0.0.1:<port>/callback redirect
// (a fixed Port may be required) and the authorization server issues the
// snow.user scope (A-05).
func LoginPKCE(ctx context.Context, cfg Config, opt PKCEOptions) (Credentials, error) {
	if err := cfg.Validate(); err != nil {
		return Credentials{}, err
	}
	out := opt.Out
	if out == nil {
		out = io.Discard
	}
	launch := opt.Launch
	if launch == nil {
		launch = SystemLauncher
	}
	timeout := opt.Timeout
	if timeout == 0 {
		timeout = 5 * time.Minute
	}
	verifier, err := randString(48)
	if err != nil {
		return Credentials{}, err
	}
	state, err := randString(24)
	if err != nil {
		return Credentials{}, err
	}
	nonce, err := randString(24)
	if err != nil {
		return Credentials{}, err
	}
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(opt.Port)))
	if err != nil {
		return Credentials{}, fmt.Errorf("cannot start the loopback listener: %w", err)
	}
	redirect := "http://" + ln.Addr().String() + "/callback"
	ch := make(chan callback, 1)
	send := func(cb callback) {
		select {
		case ch <- cb:
		default:
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case q.Get("state") != state:
			http.Error(w, "state mismatch", http.StatusBadRequest)
			send(callback{err: errors.New("login aborted: the callback state did not match (possible forged request)")})
		case q.Get("error") != "":
			http.Error(w, "login failed", http.StatusBadRequest)
			send(callback{err: &OAuthError{Status: http.StatusBadRequest, Code: q.Get("error"), Description: truncate(q.Get("error_description"), 300)}})
		case q.Get("code") == "":
			http.Error(w, "missing code", http.StatusBadRequest)
			send(callback{err: errors.New("login aborted: the callback carried no authorization code")})
		default:
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = io.WriteString(w, "snow: login complete. You can close this tab.")
			send(callback{code: q.Get("code")})
		}
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	defer func() {
		sctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()

	authURL := cfg.endpoint("/v1/authorize") + "?" + url.Values{
		"client_id": {cfg.ClientID}, "response_type": {"code"}, "scope": {cfg.scope()},
		"redirect_uri": {redirect}, "state": {state}, "nonce": {nonce},
		"code_challenge": {challenge}, "code_challenge_method": {"S256"},
	}.Encode()
	if err := launch(authURL); err != nil {
		_, _ = fmt.Fprintf(out, "Could not open a browser (%v). Open this URL to sign in:\n%s\n", err, authURL)
	} else {
		_, _ = fmt.Fprintf(out, "Opening your browser to sign in. If nothing opens, visit:\n%s\n", authURL)
	}

	wctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var cb callback
	select {
	case cb = <-ch:
	case <-wctx.Done():
		return Credentials{}, &LoginRequiredError{Msg: "login timed out waiting for the browser callback", Err: wctx.Err()}
	}
	if cb.err != nil {
		return Credentials{}, cb.err
	}
	tr, err := cfg.post(ctx, "/v1/token", url.Values{
		"grant_type": {"authorization_code"}, "code": {cb.code}, "redirect_uri": {redirect},
		"client_id": {cfg.ClientID}, "code_verifier": {verifier},
	})
	if err != nil {
		return Credentials{}, err
	}
	if cl := idClaims(tr.IDToken); tr.IDToken != "" && cl.Nonce != "" && cl.Nonce != nonce {
		return Credentials{}, errors.New("login aborted: the id_token nonce did not match")
	}
	return cfg.credentials(tr, Credentials{}), nil
}
