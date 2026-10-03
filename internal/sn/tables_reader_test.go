package sn_test

import (
	"context"
	"net/url"
	"testing"

	"github.com/stainedhead/agent-cli-core/auth/authtest"
	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/snow-cli/internal/sn"
	"github.com/stainedhead/snow-cli/internal/testsupport/snfake"
	"github.com/stainedhead/snow-cli/internal/usecase"
)

const incPath = "/api/now/v1/table/incident"

func newTables(t *testing.T, f *snfake.Fake) *sn.Tables {
	t.Helper()
	return sn.NewTables(newClient(t, f, authtest.Valid))
}

func TestTablesGetSendsFieldsAndNoOrder(t *testing.T) {
	f := snfake.New(t)
	id := "0123456789abcdef0123456789abcdef"
	f.On("GET", incPath+"/"+id, snfake.Response{JSON: map[string]any{"result": map[string]any{
		"sys_id": id, "number": "INC1", "impact": 2, "active": true, "nothing": nil,
		"caller_id": map[string]any{"display_value": "Ann", "value": "u1"},
	}}})
	rec, err := newTables(t, f).Get(context.Background(), "incident", id, usecase.GetOptions{Fields: []string{"number", "impact"}, Display: true})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Table != "incident" || rec.Get("number") != "INC1" || rec.Get("impact") != "2" || rec.Get("active") != "true" ||
		rec.Get("nothing") != "" || rec.Get("caller_id") != "Ann" {
		t.Errorf("record = %+v", rec)
	}
	q, _ := url.ParseQuery(f.Requests()[0].Query)
	if q.Get("sysparm_fields") != "number,impact" || q.Get("sysparm_exclude_reference_link") != "true" ||
		q.Get("sysparm_display_value") != "true" || q.Get("sysparm_query") != "" {
		t.Errorf("query = %v", q)
	}
}

func TestTablesListPaginationOrderAndTotal(t *testing.T) {
	f := snfake.New(t)
	f.On("GET", incPath, snfake.Records(42, map[string]any{"number": "INC1"}, map[string]any{"number": "INC2"}))
	res, err := newTables(t, f).List(context.Background(), usecase.ListQuery{
		Table: "incident", Query: "active=true", Fields: []string{"number"}, Limit: 2, Offset: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 2 || res.Total == nil || *res.Total != 42 || res.Items[1].Get("number") != "INC2" {
		t.Errorf("res = %+v", res)
	}
	q, _ := url.ParseQuery(f.Requests()[0].Query)
	if q.Get("sysparm_limit") != "2" || q.Get("sysparm_offset") != "4" ||
		q.Get("sysparm_query") != "active=true^"+sn.DefaultOrder {
		t.Errorf("query = %v", q)
	}
}

func TestTablesListCallerOrderAndMissingTotal(t *testing.T) {
	f := snfake.New(t)
	f.On("GET", incPath, snfake.Response{JSON: map[string]any{"result": []any{}}})
	res, err := newTables(t, f).List(context.Background(), usecase.ListQuery{Table: "incident", OrderBy: "ORDERBYname^ORDERBYsys_id"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != nil || len(res.Items) != 0 {
		t.Errorf("res = %+v", res)
	}
	q, _ := url.ParseQuery(f.Requests()[0].Query)
	if q.Get("sysparm_query") != "ORDERBYname^ORDERBYsys_id" {
		t.Errorf("order = %q", q.Get("sysparm_query"))
	}
}

func TestTablesCountUsesAggregateAPI(t *testing.T) {
	f := snfake.New(t)
	// ASSUMPTION(unverified against a real instance): A-11 count response {"result":{"stats":{"count":"7"}}}.
	f.On("GET", "/api/now/v1/stats/incident", snfake.Response{JSON: map[string]any{"result": map[string]any{"stats": map[string]any{"count": "7"}}}})
	n, err := newTables(t, f).Count(context.Background(), "incident", "active=true")
	if err != nil || n != 7 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	q, _ := url.ParseQuery(f.Requests()[0].Query)
	if q.Get("sysparm_count") != "true" || q.Get("sysparm_query") != "active=true" {
		t.Errorf("query = %v", q)
	}
}

func TestAssumptionCountResponseShapeTolerance(t *testing.T) {
	f := snfake.New(t)
	f.On("GET", "/api/now/v1/stats/incident", snfake.Response{JSON: map[string]any{"result": map[string]any{"stats": map[string]any{"count": 3}}}})
	if n, err := newTables(t, f).Count(context.Background(), "incident", ""); err != nil || n != 3 {
		t.Fatalf("numeric count: %d %v", n, err)
	}
	f.On("GET", "/api/now/v1/stats/incident", snfake.Response{JSON: map[string]any{"result": map[string]any{}}})
	if _, err := newTables(t, f).Count(context.Background(), "incident", ""); err == nil {
		t.Fatal("missing count must error")
	}
}

func TestTablesErrorsMapAndBadBodies(t *testing.T) {
	f := snfake.New(t)
	tb := newTables(t, f)
	ctx := context.Background()
	if _, err := tb.Get(ctx, "incident", "0123456789abcdef0123456789abcdef", usecase.GetOptions{}); output.ExitOf(err) != output.ExitNotFound {
		t.Errorf("404 exit = %d", output.ExitOf(err))
	}
	f.On("GET", incPath, snfake.Error(403, "no"))
	if _, err := tb.List(ctx, usecase.ListQuery{Table: "incident"}); output.ExitOf(err) != output.ExitForbidden {
		t.Errorf("403 exit = %d", output.ExitOf(err))
	}
	f.On("GET", incPath, snfake.Response{Body: []byte("not json")})
	if _, err := tb.List(ctx, usecase.ListQuery{Table: "incident"}); err == nil {
		t.Error("bad list body must error")
	}
	id := "0123456789abcdef0123456789abcdef"
	f.On("GET", incPath+"/"+id, snfake.Response{Body: []byte(`{"result": 5}`)})
	if _, err := tb.Get(ctx, "incident", id, usecase.GetOptions{}); err == nil {
		t.Error("bad get body must error")
	}
	f.On("GET", "/api/now/v1/stats/incident", snfake.Response{Body: []byte("x")})
	if _, err := tb.Count(ctx, "incident", ""); err == nil {
		t.Error("bad count body must error")
	}
}
