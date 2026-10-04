package sn_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stainedhead/agent-cli-core/auth/authtest"
	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/snow-cli/internal/sn"
	"github.com/stainedhead/snow-cli/internal/testsupport/snfake"
	"github.com/stainedhead/snow-cli/internal/usecase"
)

const (
	taskPath = "/api/now/v1/table/sc_task"
	taskID   = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	itemPath = "/api/sn_sc/servicecatalog/items/0123456789abcdef0123456789abcdef/order_now"
)

func TestFetchTaskDotWalksAssignee(t *testing.T) {
	f := snfake.New(t)
	f.On("GET", taskPath, snfake.Records(1, map[string]any{"sys_id": taskID, "number": "SCTASK1", "assigned_to.user_name": "svc.agent"}))
	a := sn.NewTaskAdapter(newClient(t, f, authtest.Valid))
	rec, err := a.FetchTask(context.Background(), "SCTASK1")
	if err != nil || rec.Get("assigned_to.user_name") != "svc.agent" || rec.Table != "sc_task" {
		t.Fatalf("%v %+v", err, rec)
	}
	if q := f.Requests()[0].Query; !contains(q, "assigned_to.user_name") {
		t.Fatalf("fields must request the dot-walk: %q", q)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestUpdateTaskGuarded(t *testing.T) {
	f := snfake.New(t)
	i := 0
	f.OnFunc("GET", taskPath+"/"+taskID, func(w http.ResponseWriter, _ *http.Request) {
		i++
		_ = json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"sys_id": taskID, "number": "SCTASK1", "sys_mod_count": map[bool]string{false: "1", true: "2"}[i > 1]}})
	})
	f.On("PATCH", taskPath+"/"+taskID, snfake.Response{Status: 200, JSON: map[string]any{"result": map[string]any{"sys_id": taskID, "number": "SCTASK1"}}})
	a := sn.NewTaskAdapter(newClient(t, f, authtest.Valid))
	res, err := a.UpdateTask(context.Background(), usecase.TaskUpdate{Ref: taskID, Fields: map[string]string{"work_notes": "n"}})
	if err != nil || res.Record.Table != "sc_task" || res.Record.Get("number") != "SCTASK1" {
		t.Fatalf("%v %+v", err, res)
	}
}

func TestUpdateTaskConflict(t *testing.T) {
	f := snfake.New(t)
	n := 0
	f.OnFunc("GET", taskPath+"/"+taskID, func(w http.ResponseWriter, _ *http.Request) {
		n += 2
		_ = json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"sys_id": taskID, "sys_mod_count": string(rune('0' + n))}})
	})
	f.On("PATCH", taskPath+"/"+taskID, snfake.Response{Status: 200, JSON: map[string]any{"result": map[string]any{"sys_id": taskID}}})
	_, err := sn.NewTaskAdapter(newClient(t, f, authtest.Valid)).UpdateTask(context.Background(), usecase.TaskUpdate{Ref: taskID, Fields: map[string]string{"state": "2"}})
	if output.ExitOf(err) != output.ExitConflict {
		t.Fatalf("exit %d", output.ExitOf(err))
	}
}

// Assumption A-07: no dedupe key is retrievable after order_now, so the order
// POST is NOT marked safe to retry: a 503 yields exactly one POST and exit 8.
func TestAssumptionA07OrderPostNotRetried(t *testing.T) {
	f := snfake.New(t)
	f.On("POST", itemPath, snfake.Error(503, "busy"))
	a := sn.NewOrderAdapter(newClient(t, f, authtest.Valid))
	_, err := a.Order(context.Background(), usecase.OrderRequest{Item: "0123456789abcdef0123456789abcdef", Variables: map[string]string{"model": "x"}})
	if output.ExitOf(err) != output.ExitRateLimited {
		t.Fatalf("exit %d: %v", output.ExitOf(err), err)
	}
	if f.Posts() != 1 {
		t.Fatalf("posts=%d, want 1 (no retry)", f.Posts())
	}
	if h := err.Error(); h == "" {
		t.Fatal("empty error")
	}
}

// Assumption A-08: order_now returns {"result":{"sys_id","number","request_number"...}}.
func TestAssumptionA08OrderResponse(t *testing.T) {
	f := snfake.New(t)
	f.On("POST", itemPath, snfake.Response{Status: 200, JSON: map[string]any{"result": map[string]any{"sys_id": "r1", "number": "REQ0001", "request_number": "REQ0001", "request_id": "r1"}}})
	a := sn.NewOrderAdapter(newClient(t, f, authtest.Valid))
	res, err := a.Order(context.Background(), usecase.OrderRequest{Item: "0123456789abcdef0123456789abcdef", Variables: map[string]string{"model": "x"}})
	if err != nil || res.Record.Get("number") != "REQ0001" || res.Record.Table != "sc_request" {
		t.Fatalf("%v %+v", err, res)
	}
	var body struct {
		Qty  int               `json:"sysparm_quantity"`
		Vars map[string]string `json:"variables"`
	}
	_ = json.Unmarshal(f.Requests()[0].Body, &body)
	if body.Qty != 1 || body.Vars["model"] != "x" {
		t.Fatalf("%s", f.Requests()[0].Body)
	}
	f.On("POST", itemPath, snfake.Response{Status: 200, JSON: map[string]any{"result": map[string]any{}}})
	if _, err := a.Order(context.Background(), usecase.OrderRequest{Item: "0123456789abcdef0123456789abcdef"}); err == nil {
		t.Fatal("an order response without a number must fail loudly")
	}
}

func TestOrderItemMustBeSysID(t *testing.T) {
	f := snfake.New(t)
	a := sn.NewOrderAdapter(newClient(t, f, authtest.Valid))
	if _, err := a.Order(context.Background(), usecase.OrderRequest{Item: "../../evil"}); err == nil || len(f.Requests()) != 0 {
		t.Fatal("non-sys_id item must be refused without a request")
	}
}
