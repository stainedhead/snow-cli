package sn

import (
	"errors"

	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/agent-cli-core/policy"
)

// DeniedError adapts the core's *policy.DeniedError (which does not implement
// output.CategoryError) to category policy_denied, exit 6 (spec D-b).
type DeniedError struct{ Err *policy.DeniedError }

// Error returns the policy denial message.
func (e *DeniedError) Error() string { return e.Err.Error() }

// Unwrap exposes the core error.
func (e *DeniedError) Unwrap() error { return e.Err }

// Category is policy_denied.
func (*DeniedError) Category() output.Category { return output.CategoryPolicyDenied }

// Hint explains the client policy is a guardrail layer.
func (*DeniedError) Hint() string {
	return "The client-side policy denied this action. Use a profile or policy that allows it, or ask a human."
}

// AdaptPolicyError wraps a *policy.DeniedError found in err; other errors
// (including nil) are returned unchanged.
func AdaptPolicyError(err error) error {
	var de *policy.DeniedError
	if errors.As(err, &de) {
		return &DeniedError{Err: de}
	}
	return err
}
