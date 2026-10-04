package humanauth

import (
	"context"
	"errors"
	"net/url"
	"sync"
	"time"

	"github.com/stainedhead/agent-cli-core/auth"
	"github.com/stainedhead/agent-cli-core/httpx"
)

// refreshSkew refreshes slightly before expiry.
const refreshSkew = 60 * time.Second

// Source is the human-mode auth.TokenSource and auth.Refresher: it serves the
// stored token, refreshes silently with rotation (FR-015) and persists the
// result atomically through the Store.
type Source struct {
	cfg     Config
	store   Store
	profile string
	mu      sync.Mutex
}

var (
	_ auth.TokenSource = (*Source)(nil)
	_ auth.Refresher   = (*Source)(nil)
)

// NewSource returns the token source for a profile.
func NewSource(cfg Config, store Store, profile string) *Source {
	return &Source{cfg: cfg, store: store, profile: profile}
}

func (s *Source) load() (Credentials, error) {
	if err := s.cfg.Validate(); err != nil {
		return Credentials{}, &LoginRequiredError{Msg: "human auth is not configured", Err: err}
	}
	c, err := s.store.Load(s.profile)
	switch {
	case errors.Is(err, ErrNotFound):
		return Credentials{}, &LoginRequiredError{Msg: "not logged in"}
	case err != nil:
		return Credentials{}, err
	case c.Issuer != s.cfg.Issuer || c.ClientID != s.cfg.ClientID:
		return Credentials{}, &LoginRequiredError{Msg: "stored login belongs to a different Okta issuer or client"}
	}
	return c, nil
}

func (s *Source) pick(c Credentials) auth.Token {
	if s.cfg.TokenType == "id" {
		return auth.NewToken(c.IDToken) // FR-017 okta.token_type: id
	}
	return auth.NewToken(c.AccessToken)
}

// Token returns a valid token, refreshing first when it is (nearly) expired.
func (s *Source) Token(ctx context.Context) (auth.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.load()
	if err != nil {
		return auth.Token{}, err
	}
	if s.cfg.now().Add(refreshSkew).Before(c.Expiry) {
		if t := s.pick(c); !t.IsZero() {
			return t, nil
		}
	}
	return s.refreshLocked(ctx, c)
}

// Refresh forces a refresh (used after a 401).
func (s *Source) Refresh(ctx context.Context) (auth.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.load()
	if err != nil {
		return auth.Token{}, err
	}
	return s.refreshLocked(ctx, c)
}

func (s *Source) refreshLocked(ctx context.Context, c Credentials) (auth.Token, error) {
	if c.RefreshToken == "" {
		return auth.Token{}, &LoginRequiredError{Msg: "the session expired and has no refresh token"}
	}
	tr, err := s.cfg.post(ctx, "/v1/token", url.Values{
		"grant_type": {"refresh_token"}, "refresh_token": {c.RefreshToken},
		"client_id": {s.cfg.ClientID}, "scope": {s.cfg.scope()},
	})
	if err != nil {
		var fh *httpx.ForbiddenHostError
		if errors.As(err, &fh) {
			return auth.Token{}, err // exit 4: keep the category (FR-R01)
		}
		var oe *OAuthError
		if errors.As(err, &oe) {
			return auth.Token{}, &LoginRequiredError{Msg: "token refresh was rejected", Err: oe}
		}
		return auth.Token{}, &auth.TokenError{Provider: "okta", Op: "refresh", Err: err}
	}
	nc := s.cfg.credentials(tr, c)
	if nc.AccessToken == "" {
		return auth.Token{}, &auth.TokenError{Provider: "okta", Op: "refresh", Err: errors.New("okta returned no access token")}
	}
	// Rotation: the old refresh token is dead once Okta answers, so a failed
	// persist is an error rather than a silently lost session.
	if err := s.store.Save(s.profile, nc); err != nil {
		return auth.Token{}, &auth.TokenError{Provider: "okta", Op: "refresh", Err: errors.New("could not persist the rotated credentials: " + err.Error())}
	}
	t := s.pick(nc)
	if t.IsZero() {
		return auth.Token{}, &auth.TokenError{Provider: "okta", Op: "refresh", Err: errors.New("okta returned no token of the configured type")}
	}
	return t, nil
}

// Status is the non-secret view of a stored session (FR-013).
type Status struct {
	Issuer    string    `json:"issuer"`
	Subject   string    `json:"subject"`
	Scope     string    `json:"scope"`
	ExpiresAt time.Time `json:"expires_at"`
	Expired   bool      `json:"expired"`
	Store     string    `json:"store"`
}

// StatusOf reports the stored session without touching the network.
func StatusOf(store Store, profile string, now time.Time) (Status, error) {
	c, err := store.Load(profile)
	if errors.Is(err, ErrNotFound) {
		return Status{}, &LoginRequiredError{Msg: "not logged in"}
	}
	if err != nil {
		return Status{}, err
	}
	return Status{Issuer: c.Issuer, Subject: c.Subject, Scope: c.Scope, ExpiresAt: c.Expiry, Expired: !now.Before(c.Expiry), Store: store.Kind()}, nil
}

// Logout revokes the refresh and access tokens at Okta and deletes the stored
// credentials. Deletion is attempted even when revocation fails; any partial
// failure is reported as *PartialError (FR-014).
func Logout(ctx context.Context, cfg Config, store Store, profile string) (revoked bool, err error) {
	c, lerr := store.Load(profile)
	if errors.Is(lerr, ErrNotFound) {
		return false, &LoginRequiredError{Msg: "not logged in"}
	}
	if lerr != nil {
		return false, lerr
	}
	var msgs []string
	revoked = true
	if c.RefreshToken != "" {
		if e := cfg.Revoke(ctx, c.RefreshToken, "refresh_token"); e != nil {
			revoked = false
			msgs = append(msgs, "revoking the refresh token failed: "+e.Error())
		}
	}
	if c.AccessToken != "" {
		if e := cfg.Revoke(ctx, c.AccessToken, "access_token"); e != nil {
			revoked = false
			msgs = append(msgs, "revoking the access token failed: "+e.Error())
		}
	}
	if e := store.Delete(profile); e != nil {
		msgs = append(msgs, "deleting the stored credentials failed: "+e.Error())
	}
	if len(msgs) == 0 {
		return true, nil
	}
	out := "logout partially failed"
	for _, m := range msgs {
		out += "; " + m
	}
	return revoked, &PartialError{Msg: out}
}
