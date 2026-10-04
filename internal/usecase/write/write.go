// Package write holds the write-path use cases: incident create, update and
// resolve, task update and catalog order (FR-040..048). Each runs inside the
// Guard (policy check, audit pending/outcome, action); validation happens
// before the Guard so malformed input exits 9 without touching policy,
// audit or the network.
package write

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/agent-cli-core/policy"
	"github.com/stainedhead/snow-cli/internal/domain"
	"github.com/stainedhead/snow-cli/internal/usecase"
)

// statusOK is the audit status recorded when a write succeeded.
const statusOK = 200

// Base carries what every write use case shares.
type Base struct {
	Guard   usecase.Guard
	Clock   usecase.Clock
	AgentID string
	RunID   string
	// DryRun previews instead of sending (--dry-run, FR-048).
	DryRun bool
	// Confirm, when non-nil, is asked before a mutating request is sent
	// (human mode without --yes, FR-047). A non-nil error aborts the write.
	Confirm func(prompt string) error
}

func (b Base) confirm(prompt string) error {
	if b.Confirm == nil {
		return nil
	}
	return b.Confirm(prompt)
}

// preview reports whether the action must not send: --dry-run, or a
// dry_run_only policy decision.
// A preview records the dry_run audit outcome (FR-R10).
func (b Base) preview(ctx context.Context, d policy.Decision) bool {
	if b.DryRun || d.DryRunOnly() {
		usecase.SetOutcome(ctx, usecase.OutcomeDryRun)
		return true
	}
	return false
}

// ValidationError is invalid input (exit 9).
type ValidationError struct {
	Msg string
	Hnt string
}

func (e *ValidationError) Error() string { return e.Msg }

// Category is validation.
func (*ValidationError) Category() output.Category { return output.CategoryValidation }

// Hint returns remediation text.
func (e *ValidationError) Hint() string { return e.Hnt }

func invalid(format string, args ...any) error {
	return &ValidationError{Msg: fmt.Sprintf(format, args...)}
}

// DeniedError is a client-side refusal that is not a policy-file rule, such
// as writing a task assigned to someone else (exit 6).
type DeniedError struct{ Msg string }

func (e *DeniedError) Error() string { return e.Msg }

// Category is policy_denied.
func (*DeniedError) Category() output.Category { return output.CategoryPolicyDenied }

// Hint explains the guardrail.
func (*DeniedError) Hint() string {
	return "The client-side guardrail refused this write. ServiceNow roles remain the real boundary."
}

// appliedConflict is implemented by an error that reports a write that was
// applied before a conflict was noticed (sn.ConflictError).
type appliedConflict interface{ Applied() bool }

// noteOutcome records the applied_conflict audit outcome when err says the
// write was applied (FR-R08, FR-R10 outcome label).
func noteOutcome(ctx context.Context, err error) {
	var ac appliedConflict
	if errors.As(err, &ac) && ac.Applied() {
		usecase.SetOutcome(ctx, usecase.OutcomeAppliedConflict)
	}
}

// previewResult builds a dry-run result carrying the intended payload.
func previewResult(table string, fields map[string]string) domain.WriteResult {
	return domain.WriteResult{DryRun: true, Record: domain.Record{Table: table, Fields: fields}}
}

// policyValues copies string fields as policy request values.
func policyValues(m map[string]string) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func sortedKeys(m map[string]string) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func itoa(n int) string { return strconv.Itoa(n) }

func blank(s string) bool { return strings.TrimSpace(s) == "" }
