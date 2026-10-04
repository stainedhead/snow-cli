package write

import (
	"context"
	"testing"

	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/snow-cli/internal/domain"
	"github.com/stainedhead/snow-cli/internal/usecase/read"
)

// countingCatalog counts raw catalog port calls (each is an HTTP request).
type countingCatalog struct{ calls int }

func (c *countingCatalog) Search(context.Context, string, int, int) ([]domain.CatalogItem, error) {
	c.calls++
	return nil, nil
}

func (c *countingCatalog) Item(context.Context, string) (domain.CatalogItem, error) {
	c.calls++
	return domain.CatalogItem{SysID: itemID, Name: "Laptop"}, nil
}

func (c *countingCatalog) Variables(context.Context, string) ([]domain.CatalogVariable, error) {
	c.calls++
	return laptopVars, nil
}

// FR-R03: a policy that denies `vars` makes order fail with policy_denied and
// not a single catalog request is sent.
func TestOrderDeniedVarsSendsNoCatalogRequest(t *testing.T) {
	const pol = `
version: 1
rules:
  - {id: no-vars, effect: deny, verbs: [vars], resources: ["catalog:item:*"]}
  - {id: all, effect: allow, verbs: ["*"], resources: ["*"]}
`
	g := mustPolicy(t, pol)
	cat := &countingCatalog{}
	o := &fakeOrders{}
	s := OrderService{Base: base(g), Catalog: read.Service{Catalog: cat, Guard: g}, Writer: o}
	_, err := s.Order(context.Background(), itemID, map[string]string{"model": "x"})
	wantCategory(t, err, output.CategoryPolicyDenied)
	if cat.calls != 0 || len(o.orders) != 0 {
		t.Fatalf("catalog calls %d, orders %d; want zero", cat.calls, len(o.orders))
	}
}

// The item and variable reads show up as guarded read actions before the order.
func TestOrderReadsAreGuardedActions(t *testing.T) {
	g := mustPolicy(t, allowAll)
	cat := &countingCatalog{}
	s := OrderService{Base: base(g), Catalog: read.Service{Catalog: cat, Guard: g}, Writer: &fakeOrders{}}
	if _, err := s.Order(context.Background(), itemID, map[string]string{"model": "x"}); err != nil {
		t.Fatal(err)
	}
	if len(g.actions) != 2 || g.actions[0].Request.Verb != "vars" || g.actions[1].Request.Verb != "order" {
		t.Fatalf("%+v", g.actions)
	}
}
