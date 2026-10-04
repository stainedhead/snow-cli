package humanauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/stainedhead/agent-cli-core/httpx"
)

// DefaultScopes are requested at login (FR-011).
var DefaultScopes = []string{"openid", "profile", "email", "offline_access", "snow.user"}

// Config describes the Okta client. HTTP and Now are test seams.
type Config struct {
	Issuer    string
	ClientID  string
	Scopes    []string
	TokenType string // "access" (default) or "id" (FR-017)
	HTTP      *http.Client
	Now       func() time.Time
}

func (c Config) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// client returns the Okta HTTP client. Whatever transport is injected, the
// client refuses any redirect to a host other than the issuer host and any
// https to http downgrade (FR-R01): Go would otherwise re-send a 307/308 form
// body (refresh_token, code, code_verifier) to the redirect target. Refusals
// are *httpx.ForbiddenHostError (exit 4) and nothing is sent to the target.
func (c Config) client() *http.Client {
	var hc http.Client
	if c.HTTP != nil {
		hc = *c.HTTP
	} else {
		hc = http.Client{Timeout: 30 * time.Second}
	}
	hc.CheckRedirect = c.checkRedirect
	return &hc
}

func (c Config) checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("stopped after 10 redirects")
	}
	return c.checkTarget(req.URL)
}

// checkTarget allows only the issuer host (host:port), over https, or over
// http when the issuer itself is plain-http loopback (Validate rules).
func (c Config) checkTarget(u *url.URL) error {
	iss, err := url.Parse(c.Issuer)
	if err != nil {
		return &httpx.ForbiddenHostError{Host: u.Host}
	}
	if !strings.EqualFold(u.Host, iss.Host) {
		return &httpx.ForbiddenHostError{Host: u.Host}
	}
	if u.Scheme != iss.Scheme {
		return &httpx.ForbiddenHostError{Host: u.Host, Insecure: u.Scheme == "http"}
	}
	return nil
}

func (c Config) scope() string {
	if len(c.Scopes) == 0 {
		return strings.Join(DefaultScopes, " ")
	}
	return strings.Join(c.Scopes, " ")
}

// Validate checks the Okta settings. Issuers must be https, except loopback
// hosts (httptest).
func (c Config) Validate() error {
	if c.Issuer == "" || c.ClientID == "" {
		return errors.New("okta.issuer and okta.client_id must be set in the profile")
	}
	u, err := url.Parse(c.Issuer)
	if err != nil || u.Host == "" {
		return fmt.Errorf("okta.issuer %q is not a valid URL", c.Issuer)
	}
	host := u.Hostname()
	loop := host == "localhost" || net.ParseIP(host).IsLoopback()
	if u.Scheme != "https" && (u.Scheme != "http" || !loop) {
		return fmt.Errorf("okta.issuer %q must use https", c.Issuer)
	}
	return nil
}

func (c Config) endpoint(p string) string { return strings.TrimRight(c.Issuer, "/") + p }

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	IDToken      string `json:"id_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
	Error        string `json:"error"`
	Description  string `json:"error_description"`
}

// post sends a form and decodes a token-style JSON response.
func (c Config) post(ctx context.Context, path string, form url.Values) (tokenResponse, error) {
	body, err := c.postRaw(ctx, path, form)
	if err != nil {
		return tokenResponse{}, err
	}
	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return tokenResponse{}, errors.New("okta returned a malformed response")
	}
	if tr.Error != "" { // some servers answer 200 with an error body
		return tokenResponse{}, &OAuthError{Status: http.StatusOK, Code: tr.Error, Description: truncate(tr.Description, 300)}
	}
	return tr, nil
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// credentials builds stored credentials from a token response. A refresh
// response without a refresh token keeps prev's (rotation is optional).
func (c Config) credentials(tr tokenResponse, prev Credentials) Credentials {
	exp := tr.ExpiresIn
	if exp <= 0 {
		exp = 3600 // Okta default access token lifetime
	}
	cr := Credentials{
		Issuer: c.Issuer, ClientID: c.ClientID, Scope: tr.Scope,
		AccessToken: tr.AccessToken, IDToken: tr.IDToken, RefreshToken: tr.RefreshToken,
		Expiry:  c.now().Add(time.Duration(exp) * time.Second),
		Subject: prev.Subject,
	}
	if cr.Scope == "" {
		cr.Scope = prev.Scope
	}
	if cr.RefreshToken == "" {
		cr.RefreshToken = prev.RefreshToken
	}
	if cr.IDToken == "" {
		cr.IDToken = prev.IDToken
	}
	if claims := idClaims(tr.IDToken); claims.Sub != "" {
		cr.Subject = claims.Sub
	}
	return cr
}

type claims struct {
	Sub   string `json:"sub"`
	Nonce string `json:"nonce"`
}

// idClaims decodes the id_token payload. The token came straight from the
// token endpoint over TLS, so the signature is not re-verified here (OIDC
// Core 3.1.3.7 allows this); claims are used for display and nonce only.
func idClaims(idToken string) claims {
	parts := strings.Split(idToken, ".")
	if len(parts) < 2 {
		return claims{}
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return claims{}
	}
	var cl claims
	_ = json.Unmarshal(b, &cl)
	return cl
}

// Revoke revokes a token at Okta (RFC 7009).
func (c Config) Revoke(ctx context.Context, token, hint string) error {
	_, err := c.post(ctx, "/v1/revoke", url.Values{
		"client_id": {c.ClientID}, "token": {token}, "token_type_hint": {hint},
	})
	return err
}

// postRaw is post for endpoints whose success body is not a token response.
func (c Config) postRaw(ctx context.Context, path string, form url.Values) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint(path), strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := c.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		var tr tokenResponse
		oe := &OAuthError{Status: resp.StatusCode}
		if json.Unmarshal(body, &tr) == nil {
			oe.Code, oe.Description = tr.Error, truncate(tr.Description, 300)
		}
		return nil, oe
	}
	return body, nil
}
