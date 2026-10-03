package read

import "github.com/stainedhead/agent-cli-core/output"

// ValidationError is a bad input the use case refuses before any request
// (exit 9).
type ValidationError struct {
	Msg string
	Hnt string
}

func (e *ValidationError) Error() string { return e.Msg }

// Category is validation.
func (*ValidationError) Category() output.Category { return output.CategoryValidation }

// Hint returns remediation text.
func (e *ValidationError) Hint() string { return e.Hnt }

// NotFoundError is a lookup that matched nothing (exit 5).
type NotFoundError struct {
	Msg string
}

func (e *NotFoundError) Error() string { return e.Msg }

// Category is not_found.
func (*NotFoundError) Category() output.Category { return output.CategoryNotFound }

// Hint explains that ACLs look the same as absence (A-03).
func (*NotFoundError) Hint() string {
	return "The record does not exist or your ServiceNow roles cannot see it."
}
