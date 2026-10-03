package sn

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/stainedhead/snow-cli/internal/domain"
	"github.com/stainedhead/snow-cli/internal/usecase"
)

const incidentTable = "incident"

// DefaultResolvedState is the OOB incident state value for Resolved.
const DefaultResolvedState = "6"

// IncidentOptions configures the incident write adapter.
type IncidentOptions struct {
	// CreateVia is "producer" (default) or "table" (config incident.create_via).
	CreateVia string
	// Producer is the record producer sys_id or name (config incident.producer).
	Producer string
	// ResolvedState is the state value written by resolve; empty means "6".
	ResolvedState string
}

// IncidentAdapter implements usecase.IncidentWriter.
type IncidentAdapter struct {
	c    *Client
	opts IncidentOptions
}

var _ usecase.IncidentWriter = (*IncidentAdapter)(nil)

// NewIncidentAdapter binds the incident write endpoints to a client.
func NewIncidentAdapter(c *Client, o IncidentOptions) *IncidentAdapter {
	if o.CreateVia == "" {
		o.CreateVia = "producer"
	}
	if o.ResolvedState == "" {
		o.ResolvedState = DefaultResolvedState
	}
	return &IncidentAdapter{c: c, opts: o}
}

// FindByCorrelation returns the active incident carrying correlation_id, or
// nil when none exists (the dedupe query, D-c).
func (a *IncidentAdapter) FindByCorrelation(ctx context.Context, correlationID string) (*domain.Record, error) {
	if !wSafeRef(correlationID) {
		return nil, fmt.Errorf("sn: refusing correlation id %q: only letters, digits and . _ - : are allowed", correlationID)
	}
	p := TableParams{
		Fields: []string{"sys_id", "number", "state", "short_description", "correlation_id", "sys_mod_count"},
		Limit:  1, Query: wJoin("active=true", "correlation_id="+correlationID), NoOrder: true,
	}
	resp, err := a.c.Do(ctx, Call{Method: http.MethodGet, Path: TablePath(incidentTable), Query: p.Values()})
	if err != nil {
		return nil, err
	}
	return wDecodeFirst(incidentTable, resp.Body)
}

// CreateIncident creates the incident. The POST is marked safe to retry
// because the only caller (the write use case) runs FindByCorrelation first
// and calls this only after a miss, and the correlation_id is stored
// server-side (D-c). Priority is never sent (A-01).
//
// ASSUMPTION(unverified against a real instance): correlation_id and
// correlation_display exist on the incident (A-04) and the record producer
// stores the correlation_id variable (A-08); the producer response shape is
// {"result":{"sys_id","number","table"}} (A-08).
func (a *IncidentAdapter) CreateIncident(ctx context.Context, in usecase.IncidentCreate) (domain.WriteResult, error) {
	f := map[string]string{
		"short_description": in.ShortDescription, "description": in.Description, "cmdb_ci": in.CI,
		"impact": fmt.Sprint(in.Impact), "urgency": fmt.Sprint(in.Urgency),
		"correlation_id": in.CorrelationID, "correlation_display": in.CorrelationDisplay,
	}
	if in.AssignmentGroup != "" {
		f["assignment_group"] = in.AssignmentGroup
	}
	if in.WorkNote != "" {
		f["work_notes"] = in.WorkNote
	}
	if a.opts.CreateVia == "table" {
		resp, err := a.c.Do(ctx, Call{
			Method: http.MethodPost, Path: TablePath(incidentTable), Body: f, SafeToRetry: true,
			Query: url.Values{"sysparm_exclude_reference_link": {"true"}},
		})
		if err != nil {
			return domain.WriteResult{}, err
		}
		rec, err := wDecodeOne(incidentTable, resp.Body)
		if err != nil {
			return domain.WriteResult{}, err
		}
		return domain.WriteResult{Record: rec}, nil
	}
	if !wSafeRef(a.opts.Producer) {
		return domain.WriteResult{}, &ProducerConfigError{}
	}
	resp, err := a.c.Do(ctx, Call{
		Method: http.MethodPost, Path: "/api/sn_sc/servicecatalog/items/" + a.opts.Producer + "/submit_producer",
		Body: map[string]any{"variables": f}, SafeToRetry: true,
	})
	if err != nil {
		return domain.WriteResult{}, err
	}
	rec, err := wDecodeOne(incidentTable, resp.Body)
	if err != nil {
		return domain.WriteResult{}, err
	}
	if rec.Get("number") == "" && rec.Get("sys_id") == "" {
		return domain.WriteResult{}, fmt.Errorf("record producer response has neither number nor sys_id (the producer response shape is unverified, A-08)")
	}
	if t := rec.Get("table"); t != "" {
		rec.Table = t
	}
	return domain.WriteResult{Record: rec}, nil
}

// UpdateIncident patches an incident with the sys_mod_count guard (D-j).
func (a *IncidentAdapter) UpdateIncident(ctx context.Context, in usecase.IncidentUpdate) (domain.WriteResult, error) {
	return a.c.wGuardedPatch(ctx, incidentTable, in.Ref, in.Fields, in.ExpectedModCount)
}

// ResolveIncident moves the incident to the resolved state with a close code
// and notes.
func (a *IncidentAdapter) ResolveIncident(ctx context.Context, in usecase.IncidentResolve) (domain.WriteResult, error) {
	return a.c.wGuardedPatch(ctx, incidentTable, in.Ref, map[string]string{
		"state": a.opts.ResolvedState, "close_code": in.CloseCode, "close_notes": in.CloseNote,
	}, in.ExpectedModCount)
}
