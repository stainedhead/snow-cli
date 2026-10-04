// Package auditx wires the core audit logger into the use-case Guard (FR-005,
// spec D-c): policy check, then audit record(s), then the action. Writes run
// in Block mode (a "pending" record before the request, an outcome record
// after); reads run in Warn mode.
package auditx

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/stainedhead/agent-cli-core/audit"
	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/agent-cli-core/policy"
	"github.com/stainedhead/snow-cli/internal/sn"
	"github.com/stainedhead/snow-cli/internal/usecase"
)

// Outcome labels. "pending" is an audit.Record.Outcome label: the core Record
// has no free-form fields.
const (
	OutcomePending = "pending"
	OutcomeOK      = "ok"
	OutcomeError   = "error"
	OutcomeDenied  = "denied"
)

// Sink is the part of *audit.Logger the guard needs.
type Sink interface {
	Log(audit.Record) error
}

// Guard implements usecase.Guard.
type Guard struct {
	Engine  *policy.Engine
	Sink    Sink
	Tool    string
	AgentID string
	RunID   string
	// Path names the audit log in error messages.
	Path string
	// OnWarn receives audit write failures for reads (Warn mode). May be nil.
	OnWarn func(error)
	// Now supplies elapsed-time measurement; nil uses time.Now.
	Now func() time.Time
}

var _ usecase.Guard = (*Guard)(nil)

// Error is an audit failure on a write (exit 1).
type Error struct {
	Path string
	// MayHaveHappened is set when the failure came after the request was sent.
	MayHaveHappened bool
	Err             error
}

func (e *Error) Error() string {
	if e.MayHaveHappened {
		return fmt.Sprintf("audit outcome record could not be written to %s; the write may have happened (check the record before retrying): %v", e.Path, e.Err)
	}
	return fmt.Sprintf("audit record could not be written to %s; the request was not sent: %v", e.Path, e.Err)
}

// Unwrap returns the underlying audit error.
func (e *Error) Unwrap() error { return e.Err }

// Category is general (exit 1).
func (*Error) Category() output.Category { return output.CategoryGeneral }

// Hint advises on the audit path.
func (e *Error) Hint() string {
	return "Fix the audit log path or disk space (audit.path in the profile); writes are refused while auditing fails."
}

func decisionLabel(d policy.Decision) string {
	switch {
	case d.Allowed:
		return "allow"
	case d.DryRunOnly():
		return "dry_run_only"
	}
	return "deny"
}

// hasStatus is implemented by errors that know their HTTP status.
type hasStatus interface{ HTTPStatus() int }

// DecisionProbeBypass is the policy_decision label of a selftest probe: the
// client policy is skipped on purpose so the server ACL answers (FR-R04).
const DecisionProbeBypass = "probe_bypass"

// AuditFailed marks an audit write failure so callers (selftest) can abort
// without importing this package.
func (*Error) AuditFailed() {}

// Run implements usecase.Guard.
func (g *Guard) Run(ctx context.Context, a usecase.Action, fn usecase.ActionFunc) error {
	return g.run(ctx, a, fn, false)
}

// RunProbe runs a selftest server probe (FR-R04): the client policy is
// deliberately skipped, but the request is audited like a write in Block
// mode whatever the action kind: a pending record first, no request if it
// cannot be written, then the outcome record.
func (g *Guard) RunProbe(ctx context.Context, a usecase.Action, fn usecase.ActionFunc) error {
	a.Kind = usecase.Write
	return g.run(ctx, a, fn, true)
}

func (g *Guard) run(ctx context.Context, a usecase.Action, fn usecase.ActionFunc, probe bool) error {
	now := g.Now
	if now == nil {
		now = time.Now
	}
	var d policy.Decision
	label := DecisionProbeBypass
	switch {
	case probe:
		d = policy.Decision{Allowed: true}
	case g.Engine == nil:
		d = (*policy.Policy)(nil).Evaluate(a.Request)
	default:
		d = g.Engine.Check(a.Request)
	}
	if !probe {
		label = decisionLabel(d)
	}
	rec := audit.Record{
		Tool: g.Tool, AgentID: g.AgentID, RunID: g.RunID,
		Verb: a.Request.Verb, Resource: a.Request.Resource,
		PolicyDecision: label,
	}
	write := a.Kind == usecase.Write

	if !d.Allowed && !d.DryRunOnly() {
		rec.Outcome = OutcomeDenied
		// A denial is reported as such even when the audit write fails.
		g.logDenial(rec)
		return sn.AdaptPolicyError(d.Err())
	}
	if write {
		p := rec
		p.Outcome = OutcomePending
		if err := g.Sink.Log(p); err != nil {
			return &Error{Path: g.Path, Err: err}
		}
	}
	start := now()
	status, err := fn(ctx, d)
	rec.Duration = now().Sub(start)
	rec.HTTPStatus = status
	var se hasStatus
	if status == 0 && errors.As(err, &se) {
		rec.HTTPStatus = se.HTTPStatus()
	}
	rec.Outcome = OutcomeOK
	if err != nil {
		rec.Outcome = OutcomeError
	}
	if lerr := g.Sink.Log(rec); lerr != nil {
		if write {
			return errors.Join(err, &Error{Path: g.Path, MayHaveHappened: true, Err: lerr})
		}
		g.warn(lerr)
	}
	return err
}

func (g *Guard) logDenial(rec audit.Record) {
	if err := g.Sink.Log(rec); err != nil {
		g.warn(err)
	}
}

func (g *Guard) warn(err error) {
	if g.OnWarn != nil {
		g.OnWarn(err)
	}
}
