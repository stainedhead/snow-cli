package humanauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"time"
)

// DeviceOptions are the device-flow seams.
type DeviceOptions struct {
	Out   io.Writer
	Sleep func(ctx context.Context, d time.Duration) error // nil waits on the clock
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type deviceAuth struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	VerificationURL string `json:"verification_uri_complete"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// LoginDevice runs the device authorization grant (FR-012): print the
// verification URI and code, then poll honouring interval, slow_down (+5s)
// and expiry.
func LoginDevice(ctx context.Context, cfg Config, opt DeviceOptions) (Credentials, error) {
	if err := cfg.Validate(); err != nil {
		return Credentials{}, err
	}
	out := opt.Out
	if out == nil {
		out = io.Discard
	}
	sleep := opt.Sleep
	if sleep == nil {
		sleep = sleepCtx
	}
	raw, err := cfg.postRaw(ctx, "/v1/device/authorize", url.Values{"client_id": {cfg.ClientID}, "scope": {cfg.scope()}})
	if err != nil {
		return Credentials{}, err
	}
	var da deviceAuth
	if err := json.Unmarshal(raw, &da); err != nil || da.DeviceCode == "" || da.UserCode == "" {
		return Credentials{}, errors.New("okta returned a malformed device authorization response")
	}
	_, _ = fmt.Fprintf(out, "To sign in, visit %s and enter the code: %s\n", da.VerificationURI, da.UserCode)
	if da.VerificationURL != "" {
		_, _ = fmt.Fprintf(out, "Or open: %s\n", da.VerificationURL)
	}
	interval := time.Duration(da.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	expiresIn := time.Duration(da.ExpiresIn) * time.Second
	if expiresIn <= 0 {
		expiresIn = 10 * time.Minute
	}
	deadline := cfg.now().Add(expiresIn)
	for {
		if err := sleep(ctx, interval); err != nil {
			return Credentials{}, err
		}
		if !cfg.now().Before(deadline) {
			return Credentials{}, &LoginRequiredError{Msg: "the device code expired before sign-in completed"}
		}
		tr, err := cfg.post(ctx, "/v1/token", url.Values{
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
			"device_code": {da.DeviceCode}, "client_id": {cfg.ClientID},
		})
		if err == nil {
			return cfg.credentials(tr, Credentials{}), nil
		}
		var oe *OAuthError
		if !errors.As(err, &oe) {
			return Credentials{}, err
		}
		switch oe.Code {
		case "authorization_pending":
		case "slow_down":
			interval += 5 * time.Second
		default: // expired_token, access_denied, anything else
			return Credentials{}, err
		}
	}
}
