package sn

import (
	"fmt"

	"github.com/stainedhead/agent-cli-core/output"
)

// NotFoundError is a 404 (also how ACL-hidden records appear, A-03).
// ASSUMPTION(unverified against a real instance): ACL-hidden records appear as 404 / empty pages.
type NotFoundError struct {
	Status  int
	Message string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("not found (HTTP %d): %s", e.Status, e.Message)
}
func (*NotFoundError) Category() output.Category { return output.CategoryNotFound }
func (*NotFoundError) Hint() string {
	return "The record does not exist or your ServiceNow roles cannot see it."
}

// HTTPStatus returns the upstream status.
func (e *NotFoundError) HTTPStatus() int { return e.Status }

// ConflictError is a 409 or 412, or a sys_mod_count conflict found by the
// write guard.
type ConflictError struct {
	Status  int
	Message string
	// AppliedChange is set when the write was sent and applied before the
	// conflict was noticed (FR-R08): the caller must not simply retry it.
	AppliedChange bool
}

// Applied reports that the write was applied despite the conflict; the use
// case records the applied_conflict audit outcome from it.
func (e *ConflictError) Applied() bool { return e.AppliedChange }

func (e *ConflictError) Error() string {
	return fmt.Sprintf("conflict (HTTP %d): %s", e.Status, e.Message)
}
func (*ConflictError) Category() output.Category { return output.CategoryConflict }
func (e *ConflictError) Hint() string {
	if e.AppliedChange {
		return "The change was already applied; do not repeat it (a retry would duplicate work notes). Re-read the record and reconcile with the other writer."
	}
	return "The record changed or conflicts with the request. Re-read it and retry deliberately."
}

// HTTPStatus returns the upstream status.
func (e *ConflictError) HTTPStatus() int { return e.Status }

// ValidationError is a 400 or 422.
type ValidationError struct {
	Status  int
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation failed (HTTP %d): %s", e.Status, e.Message)
}
func (*ValidationError) Category() output.Category { return output.CategoryValidation }
func (*ValidationError) Hint() string {
	return "ServiceNow rejected the request as invalid. Check field names and values."
}

// HTTPStatus returns the upstream status.
func (e *ValidationError) HTTPStatus() int { return e.Status }

// APIError is any other unexpected status (exit 1).
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("ServiceNow returned HTTP %d: %s", e.Status, e.Message)
}
func (*APIError) Category() output.Category { return output.CategoryGeneral }
func (*APIError) Hint() string              { return "Unexpected response from ServiceNow." }

// HTTPStatus returns the upstream status.
func (e *APIError) HTTPStatus() int { return e.Status }
