// Package usecase defines the application ports (frozen at A-GATE) that the
// read, write, selftest and identity use cases depend on. Adapters (internal/sn
// and others) implement them. This package imports no net/http, keychain or
// filesystem code.
package usecase

import (
	"context"
	"sync"
	"time"

	"github.com/stainedhead/agent-cli-core/policy"
	"github.com/stainedhead/snow-cli/internal/domain"
)

// GetOptions tunes a single-record read.
type GetOptions struct {
	Fields  []string
	Display bool
}

// ListQuery describes a table list read (spec D-h).
type ListQuery struct {
	Table   string
	Query   string // encoded query
	Fields  []string
	Limit   int
	Offset  int
	OrderBy string // caller order; empty selects the deterministic default
	Display bool
}

// ListResult is one page of records. Total is nil when X-Total-Count is absent.
type ListResult struct {
	Items []domain.Record
	Total *int
}

// TableReader reads table records.
type TableReader interface {
	Get(ctx context.Context, table, sysID string, opts GetOptions) (domain.Record, error)
	List(ctx context.Context, q ListQuery) (ListResult, error)
	Count(ctx context.Context, table, encodedQuery string) (int, error)
}

// CatalogReader reads the service catalog.
type CatalogReader interface {
	Search(ctx context.Context, text string, limit, offset int) ([]domain.CatalogItem, error)
	Item(ctx context.Context, item string) (domain.CatalogItem, error)
	Variables(ctx context.Context, item string) ([]domain.CatalogVariable, error)
}

// IncidentCreate is the payload of an incident create. Priority is never part
// of it (A-01).
type IncidentCreate struct {
	ShortDescription   string
	Description        string
	CI                 string
	AssignmentGroup    string
	Impact, Urgency    int
	CorrelationID      string
	CorrelationDisplay string
	WorkNote           string
}

// IncidentUpdate patches an incident. ExpectedModCount is the sys_mod_count
// read before the write (D-j); the adapter reports a conflict when it moved.
type IncidentUpdate struct {
	Ref              string // number or sys_id
	Fields           map[string]string
	ExpectedModCount int
}

// IncidentResolve resolves an incident.
type IncidentResolve struct {
	Ref                  string
	CloseCode, CloseNote string
	ExpectedModCount     int
}

// IncidentWriter writes incidents.
type IncidentWriter interface {
	FindByCorrelation(ctx context.Context, correlationID string) (*domain.Record, error)
	CreateIncident(ctx context.Context, in IncidentCreate) (domain.WriteResult, error)
	UpdateIncident(ctx context.Context, in IncidentUpdate) (domain.WriteResult, error)
	ResolveIncident(ctx context.Context, in IncidentResolve) (domain.WriteResult, error)
}

// TaskUpdate patches a catalog task (sc_task).
type TaskUpdate struct {
	Ref              string
	Fields           map[string]string
	ExpectedModCount int
}

// TaskWriter writes catalog tasks.
type TaskWriter interface {
	UpdateTask(ctx context.Context, in TaskUpdate) (domain.WriteResult, error)
}

// OrderRequest orders a catalog item.
type OrderRequest struct {
	Item          string
	Variables     map[string]string
	CorrelationID string
}

// OrderWriter orders catalog items.
type OrderWriter interface {
	Order(ctx context.Context, in OrderRequest) (domain.WriteResult, error)
}

// Identity resolves the caller (the scripted whoami endpoint, A-02).
type Identity interface {
	Whoami(ctx context.Context) (domain.Identity, error)
}

// Clock supplies time.
type Clock interface{ Now() time.Time }

// IDGen supplies unique identifiers (run ids, request ids).
type IDGen interface{ NewID() string }

// ActionKind separates reads (audit Warn) from writes (audit Block, D-c).
type ActionKind int

// Action kinds.
const (
	Read ActionKind = iota
	Write
)

// String names the kind.
func (k ActionKind) String() string {
	switch k {
	case Read:
		return "read"
	case Write:
		return "write"
	}
	return "unknown"
}

// Action is one guarded operation: its kind and the policy request.
type Action struct {
	Kind    ActionKind
	Request policy.Request
	// Ref is the target record reference (number or sys_id) for the audit
	// record only; the guard logs ResourceRef(Request.Resource, Ref) while
	// policy keeps matching on Request.Resource. Empty means no suffix.
	Ref string
}

// ActionFunc performs the operation after policy allowed (or dry-run-only
// downgraded) it, and returns the upstream HTTP status (0 if none) and error.
type ActionFunc func(ctx context.Context, d policy.Decision) (httpStatus int, err error)

// Guard implements FR-005: policy check, then audit record(s), then the
// action. A denial returns a policy_denied error and runs nothing. A decision
// that is dry_run_only is passed to fn (Allowed=false, DryRunOnly()=true) so
// the use case previews instead of sending.
type Guard interface {
	Run(ctx context.Context, a Action, fn ActionFunc) error
	// AllowedFields reports the field allowlist of the policy rule that would
	// decide verb on resource (nil when unrestricted), so an omitted --fields
	// is replaced by the allowlist (spec D-f).
	AllowedFields(verb, resource string) []string
}

// PolicyErrorAdapter turns a policy denial found in err into the
// policy_denied (exit 6) error; other errors pass through unchanged. It keeps
// adapters such as internal/sn out of the CLI layer.
type PolicyErrorAdapter interface {
	AdaptPolicyError(err error) error
}

// PolicyErrorFunc adapts a function to PolicyErrorAdapter.
type PolicyErrorFunc func(err error) error

// AdaptPolicyError calls f.
func (f PolicyErrorFunc) AdaptPolicyError(err error) error { return f(err) }

// ResourceRef forms the audit resource for a target record: base alone when
// ref is empty, else "base:ref" (for example incident:INC0010001).
func ResourceRef(base, ref string) string {
	if ref == "" {
		return base
	}
	return base + ":" + ref
}

// Outcome is an audit outcome label a use case may set for the action it ran,
// overriding the guard's default ("ok"/"error").
type Outcome string

// Audit outcome labels (the guard maps them onto the audit record).
const (
	// OutcomeDryRun marks a preview: nothing was sent.
	OutcomeDryRun Outcome = "dry_run"
	// OutcomeAppliedConflict marks a write that was applied but whose
	// sys_mod_count moved more than expected.
	OutcomeAppliedConflict Outcome = "applied_conflict"
)

// OutcomeSink records the Outcome set by the running action.
type OutcomeSink struct {
	mu sync.Mutex
	o  Outcome
}

// Outcome returns the recorded outcome ("" when none was set).
func (s *OutcomeSink) Outcome() Outcome {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.o
}

type outcomeKey struct{}

// WithOutcomeSink returns a context carrying a fresh sink. A Guard calls it
// before running the action and reads the sink afterwards.
func WithOutcomeSink(ctx context.Context) (context.Context, *OutcomeSink) {
	s := &OutcomeSink{}
	return context.WithValue(ctx, outcomeKey{}, s), s
}

// SetOutcome records o on the sink in ctx; without a sink it does nothing.
func SetOutcome(ctx context.Context, o Outcome) {
	if s, ok := ctx.Value(outcomeKey{}).(*OutcomeSink); ok {
		s.mu.Lock()
		s.o = o
		s.mu.Unlock()
	}
}

// CatalogVars is the data of `catalog vars`: the item (sys_id, name) and its
// variables.
type CatalogVars struct {
	Item      map[string]any           `json:"item"`
	Variables []domain.CatalogVariable `json:"variables"`
}

// GuardedCatalog is the policy-checked, audited catalog read surface (the read
// service). Write use cases that need catalog data (order) take this, never the
// raw CatalogReader.
type GuardedCatalog interface {
	CatalogGet(ctx context.Context, ref string) (map[string]any, error)
	CatalogVars(ctx context.Context, ref string) (CatalogVars, error)
}
