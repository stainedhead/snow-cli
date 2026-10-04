package write

import (
	"context"
	"strings"

	"github.com/stainedhead/agent-cli-core/policy"
	"github.com/stainedhead/snow-cli/internal/domain"
	"github.com/stainedhead/snow-cli/internal/idempotency"
	"github.com/stainedhead/snow-cli/internal/policymap"
	"github.com/stainedhead/snow-cli/internal/provenance"
	"github.com/stainedhead/snow-cli/internal/usecase"
)

const incidentTable = "incident"

// IncidentService creates, updates and resolves incidents.
type IncidentService struct {
	Base
	Writer usecase.IncidentWriter
	// ExpectedModCount is a sys_mod_count the caller read earlier. When set,
	// update and resolve refuse to write (exit 7, nothing sent) if the record
	// moved; 0 leaves only the post-write advance check (FR-R08).
	ExpectedModCount int
	// Scale is the instance impact/urgency scale; zero means the OOB scale.
	Scale domain.Scale
}

// CreateInput is the validated-on-entry payload of `incident create`.
type CreateInput struct {
	ShortDescription string
	Description      string
	CI               string // --ci or --app (a CMDB CI either way)
	AssignmentGroup  string // optional override
	Impact, Urgency  int
	IdempotencyKey   string // --idempotency-key; empty selects the default
	WorkNote         string // optional initial work note
}

func (s IncidentService) scale() domain.Scale {
	if s.Scale == (domain.Scale{}) {
		return domain.DefaultScale()
	}
	return s.Scale
}

func (s IncidentService) validateCreate(in CreateInput) error {
	switch {
	case blank(in.ShortDescription):
		return invalid("--short-description is required")
	case blank(in.Description):
		return invalid("--description is required")
	case blank(in.CI):
		return invalid("--ci or --app is required")
	}
	sc := s.scale()
	if in.Impact == 0 {
		return invalid("--impact is required (%d high, %d medium, %d low)", sc.High, sc.Medium, sc.Low)
	}
	if in.Urgency == 0 {
		return invalid("--urgency is required (%d high, %d medium, %d low)", sc.High, sc.Medium, sc.Low)
	}
	if !sc.Contains(in.Impact) {
		return invalid("--impact %d is not on the instance scale (%d high, %d medium, %d low)", in.Impact, sc.High, sc.Medium, sc.Low)
	}
	if !sc.Contains(in.Urgency) {
		return invalid("--urgency %d is not on the instance scale (%d high, %d medium, %d low)", in.Urgency, sc.High, sc.Medium, sc.Low)
	}
	return nil
}

// Create implements FR-040/043/044. Priority is never sent (A-01). The
// dedupe lookup runs inside the guarded action; the adapter re-runs it before
// every re-send of the create POST (FR-R07).
func (s IncidentService) Create(ctx context.Context, in CreateInput) (domain.WriteResult, error) {
	if err := s.validateCreate(in); err != nil {
		return domain.WriteResult{}, err
	}
	key := idempotency.Resolve(in.IdempotencyKey, s.AgentID, in.CI, in.ShortDescription, s.Clock.Now())
	note := provenance.WorkNote(s.AgentID, s.RunID, in.WorkNote)
	call := usecase.IncidentCreate{
		ShortDescription: in.ShortDescription, Description: in.Description, CI: in.CI,
		AssignmentGroup: in.AssignmentGroup, Impact: in.Impact, Urgency: in.Urgency,
		CorrelationID: key, CorrelationDisplay: provenance.CorrelationDisplay(s.AgentID), WorkNote: note,
	}

	// The policy sees only caller-chosen fields; system-added correlation and
	// provenance fields are not subject to the caller's field allowlist.
	vals := map[string]any{
		"short_description": in.ShortDescription, "description": in.Description,
		"cmdb_ci": in.CI, "impact": in.Impact, "urgency": in.Urgency,
	}
	if in.AssignmentGroup != "" {
		vals["assignment_group"] = in.AssignmentGroup
	}
	if in.WorkNote != "" {
		vals["work_notes"] = in.WorkNote
	}
	req := policymap.WithValues(policymap.NewRequest(policymap.VerbCreate, policymap.ResIncident), vals)

	var out domain.WriteResult
	err := s.Guard.Run(ctx, usecase.Action{Kind: usecase.Write, Request: req}, func(ctx context.Context, d policy.Decision) (int, error) {
		if s.preview(d) {
			out = previewResult(incidentTable, createPayload(call))
			return 0, nil
		}
		if err := s.confirm("Create incident \"" + in.ShortDescription + "\" on " + in.CI + "?"); err != nil {
			return 0, err
		}
		hit, err := idempotency.Check(ctx, s.Writer, key)
		if err != nil {
			return 0, err
		}
		if hit != nil {
			out = domain.WriteResult{Record: *hit, Deduplicated: true}
			return statusOK, nil
		}
		res, err := s.Writer.CreateIncident(ctx, call)
		if err != nil {
			return 0, err
		}
		out = res
		return statusOK, nil
	})
	if err != nil {
		return domain.WriteResult{}, err
	}
	return out, nil
}

func createPayload(c usecase.IncidentCreate) map[string]string {
	f := map[string]string{
		"short_description": c.ShortDescription, "description": c.Description, "cmdb_ci": c.CI,
		"impact": itoa(c.Impact), "urgency": itoa(c.Urgency),
		"correlation_id": c.CorrelationID, "correlation_display": c.CorrelationDisplay, "work_notes": c.WorkNote,
	}
	if c.AssignmentGroup != "" {
		f["assignment_group"] = c.AssignmentGroup
	}
	return f
}

// parseIncidentRef accepts an INC number or a sys_id.
func parseIncidentRef(ref string) (string, error) {
	if domain.LooksLikeSysID(ref) {
		return strings.ToLower(ref), nil
	}
	n, err := domain.ParseNumber(ref)
	if err != nil || n.Kind() != domain.KindIncident {
		return "", invalid("%q is not an incident number (INC...) or sys_id", ref)
	}
	return n.String(), nil
}

// readOnlyFields can never be written through update.
var readOnlyFields = map[string]bool{"priority": true, "number": true, "sys_id": true, "sys_mod_count": true}

func validateFields(fields map[string]string) error {
	if len(fields) == 0 {
		return invalid("at least one --set field=value is required")
	}
	for k := range fields {
		switch {
		case blank(k):
			return invalid("empty field name")
		case readOnlyFields[k]:
			return invalid("field %q cannot be written (priority is derived by the instance, A-01)", k)
		}
	}
	return nil
}

// withProvenance returns a copy of fields whose work_notes carry the prefix.
func (b Base) withProvenance(fields map[string]string) map[string]string {
	out := make(map[string]string, len(fields))
	for k, v := range fields {
		out[k] = v
	}
	if v, ok := out["work_notes"]; ok {
		out["work_notes"] = provenance.WorkNote(b.AgentID, b.RunID, v)
	}
	return out
}

// Update implements FR-041. The adapter reads sys_mod_count before the PATCH
// and re-reads it after (D-j); a caller-supplied ExpectedModCount makes the
// pre-write check a hard precondition (FR-R08).
func (s IncidentService) Update(ctx context.Context, ref string, fields map[string]string) (domain.WriteResult, error) {
	id, err := parseIncidentRef(ref)
	if err != nil {
		return domain.WriteResult{}, err
	}
	if err := validateFields(fields); err != nil {
		return domain.WriteResult{}, err
	}
	req := policymap.WithValues(policymap.NewRequest(policymap.VerbUpdate, policymap.ResIncident), policyValues(fields))
	send := s.withProvenance(fields)
	var out domain.WriteResult
	err = s.Guard.Run(ctx, usecase.Action{Kind: usecase.Write, Request: req}, func(ctx context.Context, d policy.Decision) (int, error) {
		if s.preview(d) {
			out = previewResult(incidentTable, withRef(send, id))
			return 0, nil
		}
		if err := s.confirm("Update incident " + id + " (" + strings.Join(sortedKeys(fields), ", ") + ")?"); err != nil {
			return 0, err
		}
		res, err := s.Writer.UpdateIncident(ctx, usecase.IncidentUpdate{Ref: id, Fields: send, ExpectedModCount: s.ExpectedModCount})
		if err != nil {
			noteOutcome(ctx, err)
			return 0, err
		}
		out = res
		return statusOK, nil
	})
	if err != nil {
		return domain.WriteResult{}, err
	}
	return out, nil
}

func withRef(fields map[string]string, ref string) map[string]string {
	out := make(map[string]string, len(fields)+1)
	for k, v := range fields {
		out[k] = v
	}
	out["ref"] = ref
	return out
}

// Resolve implements FR-042. The close notes carry the provenance prefix.
func (s IncidentService) Resolve(ctx context.Context, ref, closeCode, closeNotes string) (domain.WriteResult, error) {
	id, err := parseIncidentRef(ref)
	if err != nil {
		return domain.WriteResult{}, err
	}
	if blank(closeCode) {
		return domain.WriteResult{}, invalid("--close-code is required")
	}
	if blank(closeNotes) {
		return domain.WriteResult{}, invalid("--close-notes is required")
	}
	req := policymap.WithValues(policymap.NewRequest(policymap.VerbResolve, policymap.ResIncident),
		map[string]any{"close_code": closeCode, "close_notes": closeNotes})
	notes := provenance.WorkNote(s.AgentID, s.RunID, closeNotes)
	var out domain.WriteResult
	err = s.Guard.Run(ctx, usecase.Action{Kind: usecase.Write, Request: req}, func(ctx context.Context, d policy.Decision) (int, error) {
		if s.preview(d) {
			out = previewResult(incidentTable, map[string]string{"ref": id, "close_code": closeCode, "close_notes": notes})
			return 0, nil
		}
		if err := s.confirm("Resolve incident " + id + " with close code \"" + closeCode + "\"?"); err != nil {
			return 0, err
		}
		res, err := s.Writer.ResolveIncident(ctx, usecase.IncidentResolve{Ref: id, CloseCode: closeCode, CloseNote: notes, ExpectedModCount: s.ExpectedModCount})
		if err != nil {
			noteOutcome(ctx, err)
			return 0, err
		}
		out = res
		return statusOK, nil
	})
	if err != nil {
		return domain.WriteResult{}, err
	}
	return out, nil
}
