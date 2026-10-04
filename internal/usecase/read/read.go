// Package read holds the read-path use cases (FR-020..037): table, CMDB, work
// item and catalog reads. Every operation is one policy-checked, audited
// guard action; the ports do the HTTP.
package read

import (
	"context"
	"regexp"
	"strings"

	"github.com/stainedhead/agent-cli-core/policy"
	"github.com/stainedhead/snow-cli/internal/domain"
	"github.com/stainedhead/snow-cli/internal/policymap"
	"github.com/stainedhead/snow-cli/internal/usecase"
)

// DefaultLimit is the page size when --limit is not given (before the
// policy's limits.max_results clamp).
const DefaultLimit = 25

// statusOK is the HTTP status recorded in the audit outcome on success.
const statusOK = 200

// Service runs the read use cases over the ports.
type Service struct {
	Tables   usecase.TableReader
	Catalog  usecase.CatalogReader
	Identity usecase.Identity
	Guard    usecase.Guard
	// Limits are the policy's result caps (limits.max_results).
	Limits policy.Limits
	// RelatedNodeCap bounds `cmdb ci related` output nodes; 0 selects
	// DefaultRelatedNodeCap.
	RelatedNodeCap int
}

// Options tunes a single-record read.
type Options struct {
	Fields  []string
	Display bool
}

// ListOptions tunes a list read.
type ListOptions struct {
	Fields  []string
	Display bool
	Limit   int
	Offset  int
	// OrderBy is a field name, "-field" for descending; empty selects the
	// deterministic default (spec D-h).
	OrderBy string
	// Query is a caller encoded query, validated before use.
	Query string
}

// ListData is the data of every list command (spec D-e).
type ListData struct {
	Items               []map[string]any `json:"items"`
	Page                domain.Page      `json:"page"`
	ACLFilteredPossible bool             `json:"acl_filtered_possible"`
	// Truncated is set by Fit when items were dropped to honour --max-bytes.
	Truncated bool `json:"truncated,omitempty"`
}

var tableName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// guarded runs fn as one read action: policy check, audit, then the work.
func (s Service) guarded(ctx context.Context, verb, resource string, fields []string, fn func(ctx context.Context) error) error {
	return s.guardedRef(ctx, "", verb, resource, fields, fn)
}

// guardedRef is guarded with a target reference for the audit resource suffix
// (FR-R10); policy still matches on resource alone.
func (s Service) guardedRef(ctx context.Context, ref, verb, resource string, fields []string, fn func(ctx context.Context) error) error {
	req := policymap.NewRequest(verb, resource)
	if len(fields) > 0 {
		req = policymap.WithFields(req, fields...)
	}
	return s.Guard.Run(ctx, usecase.Action{Kind: usecase.Read, Request: req, Ref: domain.AuditRef(ref)},
		func(ctx context.Context, _ policy.Decision) (int, error) {
			if err := fn(ctx); err != nil {
				return 0, err
			}
			return statusOK, nil
		})
}

// effectiveFields applies field substitution: the caller's fields when given,
// else the policy allowlist (when the guard exposes it), else the defaults.
func (s Service) effectiveFields(verb, resource string, requested, defaults []string) []string {
	if len(requested) > 0 {
		return dedupe(requested)
	}
	if f := s.Guard.AllowedFields(verb, resource); len(f) > 0 {
		return dedupe(f)
	}
	return dedupe(defaults)
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, f := range in {
		if f = strings.TrimSpace(f); f != "" && !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}
	return out
}

func union(a []string, extra ...string) []string {
	return dedupe(append(append([]string(nil), a...), extra...))
}

func (s Service) limit(n int) int {
	if n <= 0 {
		n = DefaultLimit
	}
	return s.Limits.ClampResults(n)
}

// list is the shared paged read behind every list command.
// build produces the use case's own encoded query inside the guarded action
// (it may need the caller identity); the caller's --query is ANDed after it.
func (s Service) list(ctx context.Context, verb, resource, table string, build func(context.Context) (string, error), fields []string, o ListOptions) (ListData, error) {
	if err := validTable(table); err != nil {
		return ListData{}, err
	}
	info, err := ParseQuery(o.Query)
	if err != nil {
		return ListData{}, err
	}
	order, orderField, err := orderClause(o.OrderBy)
	if err != nil {
		return ListData{}, err
	}
	// The caller's query and order fields are requested fields too, so a
	// policy allowlist applies to them (FR-R05).
	policyFields := fields
	if len(fields) > 0 {
		policyFields = union(fields, info.Fields...)
		if orderField != "" {
			policyFields = union(policyFields, orderField)
		}
	}
	limit := s.limit(o.Limit)
	var out ListData
	err = s.guarded(ctx, verb, resource, policyFields, func(ctx context.Context) error {
		own, err := build(ctx)
		if err != nil {
			return err
		}
		q := joinQuery(own, o.Query)
		res, err := s.Tables.List(ctx, usecase.ListQuery{
			Table: table, Query: q, Fields: fields, Limit: limit, Offset: o.Offset, OrderBy: order, Display: o.Display,
		})
		if err != nil {
			return err
		}
		out = buildList(res, o.Offset, limit)
		return nil
	})
	if err != nil {
		return ListData{}, err
	}
	return out, nil
}

func buildList(res usecase.ListResult, offset, limit int) ListData {
	items := make([]map[string]any, 0, len(res.Items))
	for _, r := range res.Items {
		items = append(items, Present(r))
	}
	page := domain.NewPage(offset, len(items), limit, res.Total)
	return ListData{Items: items, Page: page, ACLFilteredPossible: page.ACLFilteredPossible(limit)}
}

// noQuery is the build function of lists without use-case conditions.
func noQuery(context.Context) (string, error) { return "", nil }
