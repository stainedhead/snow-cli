// Package selftest is the selftest use case (FR-050, spec D-k): it builds the
// allow/deny matrix of PRD section 8.3 as core selftest rows and a Probe that
// exercises each row against the real ports.
//
// Probe semantics (D-k):
//   - a server read that succeeds is Allow;
//   - a client policy denial (exit 6), a server refusal (403, exit 4) or a
//     404-on-ACL (exit 5) is Deny;
//   - anything else is a probe error and the row fails with detail.
//
// Rows that cannot mutate (reads, policy-only checks) are ReadOnly. The two
// write rows run only with IncludeWrites, against configured fixture records.
// A failing matrix is a general-category failure: `snow selftest` exits 1
// (not 4 or 6); the per-row detail is in the failure message.
package selftest

import (
	"context"
	"errors"
	"fmt"

	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/agent-cli-core/policy"
	core "github.com/stainedhead/agent-cli-core/selftest"
	"github.com/stainedhead/snow-cli/internal/domain"
	"github.com/stainedhead/snow-cli/internal/policymap"
	"github.com/stainedhead/snow-cli/internal/usecase"
)

const statusOK = 200

// Fixture names the records the write rows run against (config
// selftest.fixture_incident and selftest.foreign_incident). Own is an
// incident the identity may touch but never resolve; Foreign is one it must
// not update.
type Fixture struct {
	Own, Foreign string
}

// ProbeGuard audits the server probes (FR-R04). Unlike usecase.Guard it does
// not apply the client policy (the probe exists to ask the server ACL) but it
// writes the pending audit record before the request and refuses to send the
// request when that record cannot be written. The verbs are distinct
// (selftest:probe-*), so the policy bypass is visible in the audit log.
type ProbeGuard interface {
	RunProbe(ctx context.Context, a usecase.Action, fn usecase.ActionFunc) error
}

// auditFailure is implemented by audit write errors (auditx.Error).
type auditFailure interface{ AuditFailed() }

// Probe verbs.
const (
	verbProbeList    = "selftest:probe-list"
	verbProbeResolve = "selftest:probe-resolve"
	verbProbeUpdate  = "selftest:probe-update"
)

// Service runs the matrix.
type Service struct {
	Guard usecase.Guard
	// Probes audits the server-ACL and write probes; required for them.
	Probes    ProbeGuard
	Tables    usecase.TableReader
	Catalog   usecase.CatalogReader
	Identity  usecase.Identity
	Incidents usecase.IncidentWriter
	Mode      domain.Mode

	IncludeWrites bool
	Fixture       Fixture
}

type op int

const (
	opPolicy        op = iota // client policy decision only, no request
	opWhoami                  // guarded identity call
	opList                    // guarded one-row table read
	opCount                   // guarded aggregate count
	opCatalog                 // guarded catalog search
	opServerList              // unguarded one-row read: asks the server ACL
	opResolveServer           // unguarded resolve of the fixture (write)
	opUpdateServer            // unguarded update of the foreign record (write)
)

type spec struct {
	verb, resource string
	table          string
	expect         core.Outcome
	op             op
	fields         map[string]any
	readOnly       bool
}

func read(verb, res, table string, o op) spec {
	return spec{verb: verb, resource: res, table: table, expect: core.Allow, op: o, readOnly: true}
}

func policyRow(verb, res string, expect core.Outcome, fields map[string]any) spec {
	return spec{verb: verb, resource: res, expect: expect, op: opPolicy, fields: fields, readOnly: true}
}

// matrix is the PRD 8.3 matrix for a mode, in a stable order (row names are
// derived from the position).
func matrix(mode domain.Mode) []spec {
	m := []spec{
		read(policymap.VerbWhoami, policymap.ResWhoami, "", opWhoami),
		read(policymap.VerbList, policymap.ResIncident, "incident", opList),
		read(policymap.VerbCount, policymap.Table("incident"), "incident", opCount),
		read(policymap.VerbList, policymap.ResCMDBCI, "cmdb_ci", opList),
		read(policymap.VerbRelated, policymap.ResCMDBCI, "cmdb_rel_ci", opList),
		read(policymap.VerbList, policymap.ResCMDBApp, "cmdb_ci_service", opList),
		read(policymap.VerbList, policymap.ResRequest, "sc_request", opList),
		read(policymap.VerbList, policymap.ResRITM, "sc_req_item", opList),
		read(policymap.VerbList, policymap.ResTask, "task", opList),
		read(policymap.VerbSearch, policymap.ResCatalogSearch, "", opCatalog),
		read(policymap.VerbList, policymap.ResChange, "change_request", opList),
		read(policymap.VerbList, policymap.ResProblem, "problem", opList),
		policyRow(policymap.VerbList, policymap.Table("sys_user"), core.Deny, nil),
	}
	if mode == domain.ModeHuman {
		m = append(m, policyRow(policymap.VerbResolve, policymap.ResIncident, core.Allow, nil))
	} else {
		m = append(m,
			policyRow(policymap.VerbResolve, policymap.ResIncident, core.Deny, nil),
			policyRow(policymap.VerbCreate, policymap.ResIncident, core.Deny, map[string]any{"impact": 1}),
			policyRow(policymap.VerbOrder, policymap.CatalogItem("selftest"), core.Deny, nil),
			// Server over-grant probes: the client policy is bypassed so the
			// ServiceNow ACL answers (8.1 rules 4 and 5).
			spec{verb: policymap.VerbList, resource: policymap.Table("sys_user"), table: "sys_user", expect: core.Deny, op: opServerList, readOnly: true},
			spec{verb: policymap.VerbList, resource: policymap.Table("sys_properties"), table: "sys_properties", expect: core.Deny, op: opServerList, readOnly: true},
		)
	}
	m = append(m,
		spec{verb: policymap.VerbResolve, resource: policymap.ResIncident, expect: core.Deny, op: opResolveServer},
		spec{verb: policymap.VerbUpdate, resource: policymap.ResIncident, expect: core.Deny, op: opUpdateServer},
	)
	return m
}

func rowName(i int) string { return fmt.Sprintf("m8-3-row-%d", i+1) }

// Rows returns the matrix as core rows.
func (s Service) Rows() []core.Row {
	specs := matrix(s.Mode)
	rows := make([]core.Row, len(specs))
	for i, sp := range specs {
		rows[i] = core.Row{Name: rowName(i), Verb: sp.verb, Resource: sp.resource, Expect: sp.expect, ReadOnly: sp.readOnly}
	}
	return rows
}

// UsageError reports a selftest configuration problem (exit 2).
type UsageError struct{ Msg, Hnt string }

func (e *UsageError) Error() string           { return e.Msg }
func (*UsageError) Category() output.Category { return output.CategoryUsage }
func (e *UsageError) Hint() string            { return e.Hnt }

// Run checks the selftest verb against the policy, then executes the matrix
// (read-only unless IncludeWrites).
func (s Service) Run(ctx context.Context) (core.Result, error) {
	if s.IncludeWrites && (s.Fixture.Own == "" || s.Fixture.Foreign == "") {
		return core.Result{}, &UsageError{
			Msg: "--include-writes needs selftest.fixture_incident and selftest.foreign_incident in the profile",
			Hnt: "Configure the two fixture incident numbers or run without --include-writes.",
		}
	}
	var res core.Result
	var runErr, authErr, auditErr error
	probe := func(ctx context.Context, r core.Row) (core.Outcome, error) {
		if auditErr != nil { // audit failed earlier: send nothing more
			return "", auditErr
		}
		o, err := s.Probe(ctx, r)
		var af auditFailure
		switch {
		case err != nil && errors.As(err, &af):
			auditErr = err
		case err != nil && authErr == nil && output.ExitOf(err) == output.ExitAuth:
			authErr = err
		}
		return o, err
	}
	err := s.Guard.Run(ctx, usecase.Action{Kind: usecase.Read, Request: policymap.Selftest()},
		func(ctx context.Context, _ policy.Decision) (int, error) {
			res, runErr = core.Runner{Rows: s.Rows(), Probe: probe, ReadOnly: !s.IncludeWrites}.Run(ctx)
			return statusOK, runErr
		})
	if err != nil {
		return core.Result{}, err
	}
	if auditErr != nil {
		return core.Result{}, auditErr
	}
	if authErr != nil {
		// No credentials: report that (exit 3) instead of a matrix of
		// identical probe errors.
		return core.Result{}, authErr
	}
	return res, runErr
}

// Probe exercises one row and maps the observation to Allow or Deny.
func (s Service) Probe(ctx context.Context, row core.Row) (core.Outcome, error) {
	specs := matrix(s.Mode)
	for i, sp := range specs {
		if rowName(i) == row.Name {
			return s.probe(ctx, sp)
		}
	}
	return "", fmt.Errorf("unknown selftest row %q", row.Name)
}

func (s Service) probe(ctx context.Context, sp spec) (core.Outcome, error) {
	switch sp.op {
	case opServerList:
		return s.serverProbe(ctx, verbProbeList, sp.resource, "", usecase.Read, func(ctx context.Context) error {
			_, err := s.Tables.List(ctx, usecase.ListQuery{Table: sp.table, Limit: 1, Fields: []string{"sys_id"}})
			return err
		})
	case opResolveServer:
		return s.serverProbe(ctx, verbProbeResolve, policymap.ResIncident, s.Fixture.Own, usecase.Write, func(ctx context.Context) error {
			_, err := s.Incidents.ResolveIncident(ctx, usecase.IncidentResolve{
				Ref: s.Fixture.Own, CloseCode: "Solved (Permanently)", CloseNote: "snow selftest probe",
			})
			return err
		})
	case opUpdateServer:
		return s.serverProbe(ctx, verbProbeUpdate, policymap.ResIncident, s.Fixture.Foreign, usecase.Write, func(ctx context.Context) error {
			_, err := s.Incidents.UpdateIncident(ctx, usecase.IncidentUpdate{
				Ref: s.Fixture.Foreign, Fields: map[string]string{"work_notes": "snow selftest probe"},
			})
			return err
		})
	}
	var out core.Outcome
	req := policymap.NewRequest(sp.verb, sp.resource)
	if len(sp.fields) > 0 {
		req = policymap.WithValues(req, sp.fields)
	}
	err := s.Guard.Run(ctx, usecase.Action{Kind: usecase.Read, Request: req},
		func(ctx context.Context, d policy.Decision) (int, error) {
			if !d.Allowed { // dry-run-only: the real action would not run
				out = core.Deny
				return 0, nil
			}
			if err := s.guarded(ctx, sp); err != nil {
				return 0, err
			}
			out = core.Allow
			return statusOK, nil
		})
	if err != nil {
		return classify(err)
	}
	return out, nil
}

// serverProbe sends one server-ACL probe through the audited, policy-skipping
// probe guard. Without a probe guard nothing is sent (fail closed).
func (s Service) serverProbe(ctx context.Context, verb, resource, ref string, kind usecase.ActionKind, do func(context.Context) error) (core.Outcome, error) {
	if s.Probes == nil {
		return "", errors.New("selftest probe guard is not wired; refusing to send an unaudited probe")
	}
	err := s.Probes.RunProbe(ctx, usecase.Action{Kind: kind, Request: policymap.NewRequest(verb, resource), Ref: ref},
		func(ctx context.Context, _ policy.Decision) (int, error) {
			if err := do(ctx); err != nil {
				return 0, err
			}
			return statusOK, nil
		})
	return classify(err)
}

func (s Service) guarded(ctx context.Context, sp spec) error {
	switch sp.op {
	case opWhoami:
		_, err := s.Identity.Whoami(ctx)
		return err
	case opList:
		_, err := s.Tables.List(ctx, usecase.ListQuery{Table: sp.table, Limit: 1, Fields: []string{"sys_id"}})
		return err
	case opCount:
		_, err := s.Tables.Count(ctx, sp.table, "")
		return err
	case opCatalog:
		_, err := s.Catalog.Search(ctx, "", 1, 0)
		return err
	}
	return nil // opPolicy: the allow decision is the observation
}

// classify applies D-k: success is Allow; policy denial, 403 and 404-on-ACL
// are Deny; anything else is a probe error.
func classify(err error) (core.Outcome, error) {
	if err == nil {
		return core.Allow, nil
	}
	switch output.ExitOf(err) {
	case output.ExitPolicyDenied, output.ExitForbidden, output.ExitNotFound:
		return core.Deny, nil
	}
	return "", err
}
