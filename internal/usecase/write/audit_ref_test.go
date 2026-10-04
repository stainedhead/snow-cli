package write

import (
	"context"
	"strings"
	"testing"

	"github.com/stainedhead/snow-cli/internal/usecase"
)

// FR-R10: each write action carries the target reference for the audit
// record while the policy request keeps the base resource.
func TestWriteActionsCarryTargetRefs(t *testing.T) {
	ctx := context.Background()
	g := mustPolicy(t, allowAll)
	w := &fakeIncidents{}
	s := svc(g, w)

	if _, err := s.Update(ctx, "inc0010001", map[string]string{"state": "2"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Resolve(ctx, "INC0010002", "Solved", "ok"); err != nil {
		t.Fatal(err)
	}
	in := goodCreate()
	in.IdempotencyKey = "key-1"
	if _, err := s.Create(ctx, in); err != nil {
		t.Fatal(err)
	}
	ft := &fakeTasks{assigned: "svc.agent"}
	gt := mustPolicy(t, allowAll)
	if _, err := taskSvc(gt, ft).Update(ctx, "SCTASK0000001", map[string]string{"work_notes": "x"}); err != nil {
		t.Fatal(err)
	}
	want := []string{"INC0010001", "INC0010002", "key-1"}
	for i, a := range g.actions {
		if a.Ref != want[i] {
			t.Errorf("action %d ref %q, want %q", i, a.Ref, want[i])
		}
		if a.Request.Resource != "incident" {
			t.Errorf("policy resource %q must stay the base resource", a.Request.Resource)
		}
		if got := usecase.ResourceRef(a.Request.Resource, a.Ref); !strings.HasPrefix(got, "incident:") {
			t.Errorf("audit resource %q", got)
		}
	}
	if gt.actions[0].Ref != "SCTASK0000001" || gt.actions[0].Request.Resource != "task" {
		t.Errorf("task action %+v", gt.actions[0])
	}
}

func TestDefaultCreateRefIsDerivedKey(t *testing.T) {
	g := mustPolicy(t, allowAll)
	if _, err := svc(g, &fakeIncidents{}).Create(context.Background(), goodCreate()); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(g.actions[0].Ref, "snow-") {
		t.Fatalf("ref %q", g.actions[0].Ref)
	}
}

func TestOrderResourceAlreadyNamesItem(t *testing.T) {
	g := mustPolicy(t, allowAll)
	o := &fakeOrders{}
	if _, err := orderSvc(g, o).Order(context.Background(), "Laptop", map[string]string{"model": "x"}); err != nil {
		t.Fatal(err)
	}
	// The item id is part of the resource, so no extra ref is added.
	if g.actions[0].Ref != "" || g.actions[0].Request.Resource != "catalog:item:"+itemID {
		t.Fatalf("%+v", g.actions[0])
	}
}

// FR-R10: previews carry the dry_run outcome label; real sends do not.
func TestPreviewsSetDryRunOutcome(t *testing.T) {
	ctx := context.Background()
	g := mustPolicy(t, allowAll)
	s := svc(g, &fakeIncidents{})
	s.DryRun = true
	_, _ = s.Create(ctx, goodCreate())
	_, _ = s.Update(ctx, "INC0010001", map[string]string{"state": "2"})
	_, _ = s.Resolve(ctx, "INC0010001", "Solved", "ok")
	ft := &fakeTasks{assigned: "svc.agent"}
	ts := taskSvc(g, ft)
	ts.DryRun = true
	_, _ = ts.Update(ctx, "SCTASK0000001", map[string]string{"work_notes": "x"})
	os := orderSvc(g, &fakeOrders{})
	os.DryRun = true
	_, _ = os.Order(ctx, "Laptop", map[string]string{"model": "x"})
	if len(g.outcomes) != 5 {
		t.Fatalf("outcomes %v", g.outcomes)
	}
	for i, o := range g.outcomes {
		if o != usecase.OutcomeDryRun {
			t.Errorf("action %d outcome %q", i, o)
		}
	}
	// A real send keeps the default outcome.
	g.outcomes = nil
	_, _ = svc(g, &fakeIncidents{}).Update(ctx, "INC0010001", map[string]string{"state": "2"})
	if g.outcomes[0] != "" {
		t.Fatalf("outcome %q", g.outcomes[0])
	}
}

func TestPolicyDryRunOnlySetsDryRunOutcome(t *testing.T) {
	const pol = `
version: 1
rules:
  - {id: r, effect: allow, mode: dry_run_only, verbs: ["*"], resources: ["*"]}
`
	g := mustPolicy(t, pol)
	if _, err := svc(g, &fakeIncidents{}).Update(context.Background(), "INC0010001", map[string]string{"state": "2"}); err != nil {
		t.Fatal(err)
	}
	if g.outcomes[0] != usecase.OutcomeDryRun {
		t.Fatalf("outcome %q", g.outcomes[0])
	}
}
