package sn_test

import (
	"context"
	"net/url"
	"testing"

	"github.com/stainedhead/agent-cli-core/auth/authtest"
	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/snow-cli/internal/sn"
	"github.com/stainedhead/snow-cli/internal/testsupport/snfake"
)

const catBase = "/api/sn_sc/servicecatalog/items"
const itemID = "0123456789abcdef0123456789abcdef"

func newCatalog(t *testing.T, f *snfake.Fake) *sn.Catalog {
	t.Helper()
	return sn.NewCatalog(newClient(t, f, authtest.Valid))
}

// ASSUMPTION(unverified against a real instance): catalog response shapes (result arrays/objects with
// sys_id, name, short_description, category as string or {title}); parsing is deliberately tolerant.
func TestAssumptionCatalogSearchParsesTolerantShapes(t *testing.T) {
	f := snfake.New(t)
	f.On("GET", catBase, snfake.Response{JSON: map[string]any{"result": []any{
		map[string]any{"sys_id": itemID, "name": "Laptop", "short_description": "A laptop", "category": map[string]any{"title": "Hardware", "sys_id": "c1"}},
		map[string]any{"sys_id": "x2", "name": "VPN", "category": "Network"},
	}}})
	items, err := newCatalog(t, f).Search(context.Background(), "lap top", 5, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Name != "Laptop" || items[0].Category != "Hardware" || items[1].Category != "Network" {
		t.Errorf("items = %+v", items)
	}
	q, _ := url.ParseQuery(f.Requests()[0].Query)
	if q.Get("sysparm_text") != "lap top" || q.Get("sysparm_limit") != "5" || q.Get("sysparm_offset") != "10" {
		t.Errorf("query = %v", q)
	}
}

func TestAssumptionCatalogItemAndVariablesShapes(t *testing.T) {
	f := snfake.New(t)
	f.On("GET", catBase+"/"+itemID, snfake.Response{JSON: map[string]any{"result": map[string]any{
		"sys_id": itemID, "name": "Laptop", "short_description": "A laptop", "category": map[string]any{"title": "Hardware"},
	}}})
	f.On("GET", catBase+"/"+itemID+"/variables", snfake.Response{JSON: map[string]any{"result": []any{
		map[string]any{"name": "model", "label": "Model", "type": 5, "mandatory": true,
			"choices": []any{map[string]any{"value": "mbp", "label": "MacBook"}, "thinkpad"}},
		map[string]any{"name": "note", "label": "Note", "type": "6", "mandatory": "false"},
		map[string]any{"name": "urgent", "mandatory": "true"},
	}}})
	c := newCatalog(t, f)
	it, err := c.Item(context.Background(), itemID)
	if err != nil || it.SysID != itemID || it.Category != "Hardware" {
		t.Fatalf("item = %+v %v", it, err)
	}
	vars, err := c.Variables(context.Background(), itemID)
	if err != nil || len(vars) != 3 {
		t.Fatalf("vars = %+v %v", vars, err)
	}
	if !vars[0].Mandatory || vars[0].Type != "5" || len(vars[0].Choices) != 2 || vars[0].Choices[0] != "mbp" || vars[0].Choices[1] != "thinkpad" {
		t.Errorf("var0 = %+v", vars[0])
	}
	if vars[1].Mandatory || !vars[2].Mandatory {
		t.Errorf("mandatory parsing: %+v %+v", vars[1], vars[2])
	}
}

func TestCatalogErrorsAndBadBodies(t *testing.T) {
	f := snfake.New(t)
	c := newCatalog(t, f)
	ctx := context.Background()
	if _, err := c.Item(ctx, itemID); output.ExitOf(err) != output.ExitNotFound {
		t.Errorf("404 exit = %d", output.ExitOf(err))
	}
	f.On("GET", catBase, snfake.Error(403, "no"))
	if _, err := c.Search(ctx, "x", 1, 0); output.ExitOf(err) != output.ExitForbidden {
		t.Errorf("403 exit = %d", output.ExitOf(err))
	}
	f.On("GET", catBase, snfake.Response{Body: []byte("nope")})
	if _, err := c.Search(ctx, "x", 1, 0); err == nil {
		t.Error("bad search body")
	}
	f.On("GET", catBase+"/"+itemID, snfake.Response{Body: []byte(`{"result":[]}`)})
	if _, err := c.Item(ctx, itemID); err == nil {
		t.Error("bad item body")
	}
	f.On("GET", catBase+"/"+itemID+"/variables", snfake.Response{Body: []byte(`{"result":{}}`)})
	if _, err := c.Variables(ctx, itemID); err == nil {
		t.Error("bad variables body")
	}
}

func TestCatalogRefusesNonSysIDItem(t *testing.T) {
	f := snfake.New(t)
	c := newCatalog(t, f)
	if _, err := c.Item(context.Background(), "../etc"); err == nil {
		t.Error("item must be a sys_id")
	}
	if _, err := c.Variables(context.Background(), "a/b"); err == nil {
		t.Error("item must be a sys_id")
	}
	if len(f.Requests()) != 0 {
		t.Error("no request for an invalid item")
	}
}
