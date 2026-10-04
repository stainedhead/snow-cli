package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stainedhead/agent-cli-core/policy"
	"github.com/stainedhead/snow-cli/internal/domain"
	"github.com/stainedhead/snow-cli/internal/usecase"
)

// fakes document the port contracts and fail to compile if a port drifts.
type fakeAll struct{}

func (fakeAll) Get(context.Context, string, string, usecase.GetOptions) (domain.Record, error) {
	return domain.Record{}, nil
}
func (fakeAll) List(context.Context, usecase.ListQuery) (usecase.ListResult, error) {
	return usecase.ListResult{}, nil
}
func (fakeAll) Count(context.Context, string, string) (int, error) { return 0, nil }
func (fakeAll) Search(context.Context, string, int, int) ([]domain.CatalogItem, error) {
	return nil, nil
}
func (fakeAll) Item(context.Context, string) (domain.CatalogItem, error) {
	return domain.CatalogItem{}, nil
}
func (fakeAll) Variables(context.Context, string) ([]domain.CatalogVariable, error) {
	return nil, nil
}
func (fakeAll) FindByCorrelation(context.Context, string) (*domain.Record, error) { return nil, nil }
func (fakeAll) CreateIncident(context.Context, usecase.IncidentCreate) (domain.WriteResult, error) {
	return domain.WriteResult{}, nil
}
func (fakeAll) UpdateIncident(context.Context, usecase.IncidentUpdate) (domain.WriteResult, error) {
	return domain.WriteResult{}, nil
}
func (fakeAll) ResolveIncident(context.Context, usecase.IncidentResolve) (domain.WriteResult, error) {
	return domain.WriteResult{}, nil
}
func (fakeAll) UpdateTask(context.Context, usecase.TaskUpdate) (domain.WriteResult, error) {
	return domain.WriteResult{}, nil
}
func (fakeAll) Order(context.Context, usecase.OrderRequest) (domain.WriteResult, error) {
	return domain.WriteResult{}, nil
}
func (fakeAll) Whoami(context.Context) (domain.Identity, error) { return domain.Identity{}, nil }
func (fakeAll) Now() time.Time                                  { return time.Time{} }
func (fakeAll) NewID() string                                   { return "id" }

var (
	_ usecase.TableReader        = fakeAll{}
	_ usecase.CatalogReader      = fakeAll{}
	_ usecase.IncidentWriter     = fakeAll{}
	_ usecase.TaskWriter         = fakeAll{}
	_ usecase.OrderWriter        = fakeAll{}
	_ usecase.Identity           = fakeAll{}
	_ usecase.Clock              = fakeAll{}
	_ usecase.IDGen              = fakeAll{}
	_ usecase.Guard              = fakeGuard{}
	_ usecase.GuardedCatalog     = fakeCatalogReads{}
	_ usecase.PolicyErrorAdapter = usecase.PolicyErrorFunc(nil)
)

type fakeGuard struct{ ran *bool }

func (g fakeGuard) AllowedFields(string, string) []string { return nil }

func (g fakeGuard) Run(ctx context.Context, a usecase.Action, fn usecase.ActionFunc) error {
	*g.ran = true
	_, err := fn(ctx, policy.Decision{Allowed: true})
	return err
}

func TestActionKindString(t *testing.T) {
	if usecase.Read.String() != "read" || usecase.Write.String() != "write" {
		t.Error("kind strings")
	}
	if usecase.ActionKind(9).String() != "unknown" {
		t.Error("unknown kind string")
	}
}

func TestGuardContract(t *testing.T) {
	ran := false
	want := errors.New("boom")
	err := usecase.Guard(fakeGuard{&ran}).Run(context.Background(), usecase.Action{Kind: usecase.Write},
		func(context.Context, policy.Decision) (int, error) { return 500, want })
	if !ran || !errors.Is(err, want) {
		t.Errorf("ran=%v err=%v", ran, err)
	}
}

type fakeCatalogReads struct{}

func (fakeCatalogReads) CatalogGet(context.Context, string) (map[string]any, error) { return nil, nil }
func (fakeCatalogReads) CatalogVars(context.Context, string) (usecase.CatalogVars, error) {
	return usecase.CatalogVars{}, nil
}

func TestPolicyErrorFunc(t *testing.T) {
	wrapped := errors.New("adapted")
	f := usecase.PolicyErrorFunc(func(error) error { return wrapped })
	if !errors.Is(f.AdaptPolicyError(errors.New("x")), wrapped) {
		t.Error("func adapter must delegate")
	}
}

func TestResourceRef(t *testing.T) {
	if got := usecase.ResourceRef("incident", "INC0010001"); got != "incident:INC0010001" {
		t.Errorf("got %q", got)
	}
	if got := usecase.ResourceRef("incident", ""); got != "incident" {
		t.Errorf("empty ref keeps base, got %q", got)
	}
	a := usecase.Action{Kind: usecase.Write, Ref: "INC1"}
	if a.Ref != "INC1" {
		t.Error("Action.Ref carries the target reference; policy matches Request.Resource only")
	}
}

func TestOutcomeSink(t *testing.T) {
	ctx, sink := usecase.WithOutcomeSink(context.Background())
	if sink.Outcome() != "" {
		t.Error("default outcome empty")
	}
	usecase.SetOutcome(ctx, usecase.OutcomeDryRun)
	if sink.Outcome() != usecase.OutcomeDryRun {
		t.Errorf("got %q", sink.Outcome())
	}
	usecase.SetOutcome(context.Background(), usecase.OutcomeAppliedConflict) // no sink: no-op
	if usecase.OutcomeDryRun != "dry_run" || usecase.OutcomeAppliedConflict != "applied_conflict" {
		t.Error("outcome labels are part of the audit contract")
	}
}
