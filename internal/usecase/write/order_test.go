package write

import (
	"context"
	"strings"
	"testing"

	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/snow-cli/internal/domain"
	"github.com/stainedhead/snow-cli/internal/usecase"
)

const itemID = "0123456789abcdef0123456789abcdef"

type fakeCatalog struct {
	vars    []domain.CatalogVariable
	itemErr error
	varsErr error
}

func (f fakeCatalog) Search(context.Context, string, int, int) ([]domain.CatalogItem, error) {
	return nil, nil
}
func (f fakeCatalog) Item(_ context.Context, item string) (domain.CatalogItem, error) {
	return domain.CatalogItem{SysID: itemID, Name: item}, f.itemErr
}
func (f fakeCatalog) Variables(context.Context, string) ([]domain.CatalogVariable, error) {
	return f.vars, f.varsErr
}

type fakeOrders struct {
	orders []usecase.OrderRequest
	err    error
}

func (f *fakeOrders) Order(_ context.Context, in usecase.OrderRequest) (domain.WriteResult, error) {
	f.orders = append(f.orders, in)
	return domain.WriteResult{Record: domain.Record{Table: "sc_request", Fields: map[string]string{"number": "REQ1"}}}, f.err
}

var laptopVars = []domain.CatalogVariable{{Name: "model", Mandatory: true}, {Name: "note"}}

func orderSvc(g usecase.Guard, o *fakeOrders) OrderService {
	return OrderService{Base: base(g), Catalog: fakeCatalog{vars: laptopVars}, Writer: o}
}

func TestOrderOptedInItem(t *testing.T) {
	const pol = `
version: 1
rules:
  - {id: laptop, effect: allow, verbs: [order], resources: ["catalog:item:0123456789abcdef0123456789abcdef"]}
  - {id: rest, effect: allow, mode: dry_run_only, verbs: [order], resources: ["catalog:item:*"]}
`
	o := &fakeOrders{}
	g := mustPolicy(t, pol)
	res, err := orderSvc(g, o).Order(context.Background(), "Laptop", map[string]string{"model": "x1"})
	if err != nil {
		t.Fatal(err)
	}
	if res.DryRun || len(o.orders) != 1 || o.orders[0].Item != itemID || o.orders[0].Variables["model"] != "x1" {
		t.Fatalf("%+v %+v", res, o.orders)
	}
	if g.actions[0].Request.Resource != "catalog:item:"+itemID || g.actions[0].Request.Verb != "order" {
		t.Fatalf("%+v", g.actions[0].Request)
	}
}

func TestOrderDefaultDryRunOnly(t *testing.T) {
	const pol = `
version: 1
rules:
  - {id: rest, effect: allow, mode: dry_run_only, verbs: [order], resources: ["catalog:item:*"]}
`
	o := &fakeOrders{}
	res, err := orderSvc(mustPolicy(t, pol), o).Order(context.Background(), "Laptop", map[string]string{"model": "x1"})
	if err != nil || !res.DryRun || len(o.orders) != 0 {
		t.Fatalf("%+v %v", res, err)
	}
	if res.Record.Fields["item"] != itemID || res.Record.Fields["var.model"] != "x1" {
		t.Fatalf("%v", res.Record.Fields)
	}
}

func TestOrderVariableValidation(t *testing.T) {
	cases := map[string]map[string]string{
		"missing mandatory": {"note": "n"},
		"unknown":           {"model": "x", "bogus": "y"},
	}
	for name, vars := range cases {
		t.Run(name, func(t *testing.T) {
			o := &fakeOrders{}
			g := mustPolicy(t, allowAll)
			_, err := orderSvc(g, o).Order(context.Background(), "Laptop", vars)
			wantCategory(t, err, output.CategoryValidation)
			if len(g.actions) != 0 || len(o.orders) != 0 {
				t.Fatal("must stop before policy")
			}
		})
	}
	_, err := orderSvc(mustPolicy(t, allowAll), &fakeOrders{}).Order(context.Background(), " ", nil)
	wantCategory(t, err, output.CategoryValidation)
}

func TestOrderLookupErrors(t *testing.T) {
	s := orderSvc(mustPolicy(t, allowAll), &fakeOrders{})
	s.Catalog = fakeCatalog{itemErr: errBoom}
	if _, err := s.Order(context.Background(), "x", nil); err != errBoom {
		t.Fatal(err)
	}
	s.Catalog = fakeCatalog{varsErr: errBoom}
	if _, err := s.Order(context.Background(), "x", nil); err != errBoom {
		t.Fatal(err)
	}
}

func TestOrderDeniedConfirmDryRunExplicitKey(t *testing.T) {
	o := &fakeOrders{}
	vars := map[string]string{"model": "x1"}
	_, err := orderSvc(mustPolicy(t, denyAll), o).Order(context.Background(), "L", vars)
	wantCategory(t, err, output.CategoryPolicyDenied)

	s := orderSvc(mustPolicy(t, allowAll), o)
	s.Confirm = func(p string) error {
		if !strings.Contains(p, "L") {
			t.Errorf("prompt %q", p)
		}
		return errBoom
	}
	if _, err := s.Order(context.Background(), "L", vars); err != errBoom || len(o.orders) != 0 {
		t.Fatal(err)
	}
	s.Confirm = nil
	s.IdempotencyKey = "k1"
	if _, err := s.Order(context.Background(), "L", vars); err != nil || o.orders[0].CorrelationID != "k1" {
		t.Fatalf("%v %+v", err, o.orders)
	}
	o.err = errBoom
	if _, err := s.Order(context.Background(), "L", vars); err != errBoom {
		t.Fatal(err)
	}
	s.DryRun = true
	n := len(o.orders)
	if res, err := s.Order(context.Background(), "L", vars); err != nil || !res.DryRun || len(o.orders) != n {
		t.Fatalf("%v", err)
	}
}
