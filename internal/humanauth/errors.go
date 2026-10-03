package humanauth

import (
	"errors"
	"fmt"

	"github.com/stainedhead/agent-cli-core/output"
)

// ErrNotFound is returned by a Store when no credentials exist for a profile.
var ErrNotFound = errors.New("no stored credentials")

// LoginRequiredError means the human session is missing, expired beyond
// refresh, or was rejected by Okta. It maps to exit 3 (AUTH-H5). Err may carry
// the Okta error that explains why; it never carries a token.
type LoginRequiredError struct {
	Msg string
	Err error
}

// Error includes the Okta error when there is one.
func (e *LoginRequiredError) Error() string {
	if e.Err != nil {
		return e.Msg + ": " + e.Err.Error()
	}
	return e.Msg
}

// Unwrap returns the underlying cause.
func (e *LoginRequiredError) Unwrap() error { return e.Err }

// Category is auth (exit 3).
func (*LoginRequiredError) Category() output.Category { return output.CategoryAuth }

// Hint tells the human what to run.
func (*LoginRequiredError) Hint() string {
	return "Run `snow auth login` (or `snow auth login --device`) to sign in again."
}

// StoreUnavailableError is the fail-closed answer of a credential store
// backend that is not built into this binary (FR-016, D-l).
type StoreUnavailableError struct {
	Backend string
	Reason  string
}

// Error names the backend and the reason.
func (e *StoreUnavailableError) Error() string {
	return fmt.Sprintf("credential store %q is unavailable: %s", e.Backend, e.Reason)
}

// Category is auth (exit 3).
func (*StoreUnavailableError) Category() output.Category { return output.CategoryAuth }

// Hint points to the explicit insecure fallback.
func (*StoreUnavailableError) Hint() string {
	return "No secure credential store is available in this build. To store tokens in a 0600 file instead (not recommended), pass --insecure-store or set SNOW_INSECURE_STORE=1."
}

// OAuthError is an error response from an Okta endpoint.
type OAuthError struct {
	Status      int
	Code        string
	Description string
}

// Error shows the Okta error code and description (never a token).
func (e *OAuthError) Error() string {
	s := fmt.Sprintf("okta error %s", e.Code)
	if e.Code == "" {
		s = fmt.Sprintf("okta http %d", e.Status)
	}
	if e.Description != "" {
		s += ": " + e.Description
	}
	return s
}

// Category is auth (exit 3).
func (*OAuthError) Category() output.Category { return output.CategoryAuth }

// PartialError reports a logout (or similar) that only partly succeeded.
type PartialError struct{ Msg string }

// Error returns the message.
func (e *PartialError) Error() string { return e.Msg }

// Category is general (exit 1).
func (*PartialError) Category() output.Category { return output.CategoryGeneral }
