package sn_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/stainedhead/agent-cli-core/auth/authtest"
	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/snow-cli/internal/sn"
	"github.com/stainedhead/snow-cli/internal/testsupport/snfake"
	"github.com/stainedhead/snow-cli/internal/usecase"
)

const (
	incPath = "/api/now/v1/table/incident"
	incID   = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

func createIn() usecase.IncidentCreate {
	return usecase.IncidentCreate{
		ShortDescription: "Disk full", Description: "98%", CI: "db01", Impact: 2, Urgency: 3,
		CorrelationID: "snow-abc", CorrelationDisplay: "agent:a1", WorkNote: "[snow-cli agent=a1 run=r1] created",
	}
}

func tableAdapter(t *testing.T, f *snfake.Fake) *sn.IncidentAdapter {
	return sn.NewIncidentAdapter(newClient(t, f, authtest.Valid), sn.IncidentOptions{CreateVia: "table"})
}

func TestFindByCorrelationHitAndMiss(t *testing.T) {
	f := snfake.New(t)
	f.On("GET", incPath, snfake.Records(1, map[string]any{"sys_id": incID, "number": "INC7", "correlation_id": "snow-abc"}))
	a := tableAdapter(t, f)
	rec, err := a.FindByCorrelation(context.Background(), "snow-abc")
	if err != nil || rec == nil || rec.Get("number") != "INC7" || rec.Table != "incident" {
		t.Fatalf("%v %+v", err, rec)
	}
	q := f.Requests()[0].Query
	if !strings.Contains(q, "active%3Dtrue%5Ecorrelation_id%3Dsnow-abc") && !strings.Contains(q, "active=true^correlation_id=snow-abc") {
		t.Fatalf("dedupe query %q", q)
	}
	f.On("GET", incPath, snfake.Records(0))
	rec, err = a.FindByCorrelation(context.Background(), "snow-abc")
	if err != nil || rec != nil {
		t.Fatalf("miss: %v %+v", err, rec)
	}
}

func TestFindByCorrelationRejectsInjection(t *testing.T) {
	f := snfake.New(t)
	a := tableAdapter(t, f)
	if _, err := a.FindByCorrelation(context.Background(), "x^ORactive=false"); err == nil {
		t.Fatal("encoded-query metacharacters in the key must be refused")
	}
	if len(f.Requests()) != 0 {
		t.Fatal("no request")
	}
}

func TestCreateViaTable(t *testing.T) {
	f := snfake.New(t)
	f.On("POST", incPath, snfake.Response{Status: 201, JSON: map[string]any{"result": map[string]any{"sys_id": incID, "number": "INC1"}}})
	res, err := tableAdapter(t, f).CreateIncident(context.Background(), createIn())
	if err != nil {
		t.Fatal(err)
	}
	if res.Record.Get("number") != "INC1" || res.Record.Table != "incident" || res.Deduplicated {
		t.Fatalf("%+v", res)
	}
	var body map[string]any
	_ = json.Unmarshal(f.Requests()[0].Body, &body)
	if body["correlation_id"] != "snow-abc" || body["cmdb_ci"] != "db01" || body["impact"] != "2" || body["correlation_display"] != "agent:a1" {
		t.Fatalf("body %v", body)
	}
	if _, ok := body["priority"]; ok {
		t.Fatal("priority must never be sent (A-01)")
	}
}

// Assumption A-08: the record producer response shape is
// {"result":{"sys_id","number","table"}} and its id comes from config.
func TestAssumptionA08CreateViaProducer(t *testing.T) {
	f := snfake.New(t)
	f.On("POST", "/api/sn_sc/servicecatalog/items/prod1/submit_producer", snfake.Response{Status: 200, JSON: map[string]any{
		"result": map[string]any{"sys_id": incID, "number": "INC2", "table": "incident"}}})
	a := sn.NewIncidentAdapter(newClient(t, f, authtest.Valid), sn.IncidentOptions{CreateVia: "producer", Producer: "prod1"})
	res, err := a.CreateIncident(context.Background(), createIn())
	if err != nil || res.Record.Get("number") != "INC2" {
		t.Fatalf("%v %+v", err, res)
	}
	var body struct {
		Variables map[string]any `json:"variables"`
	}
	_ = json.Unmarshal(f.Requests()[0].Body, &body)
	if body.Variables["short_description"] != "Disk full" || body.Variables["correlation_id"] != "snow-abc" {
		t.Fatalf("body %s", f.Requests()[0].Body)
	}
}

func TestProducerRequiresConfiguredID(t *testing.T) {
	f := snfake.New(t)
	a := sn.NewIncidentAdapter(newClient(t, f, authtest.Valid), sn.IncidentOptions{CreateVia: "producer"})
	_, err := a.CreateIncident(context.Background(), createIn())
	if output.CategoryOf(err) != output.CategoryValidation {
		t.Fatalf("want validation error: %v", err)
	}
	if len(f.Requests()) != 0 {
		t.Fatal("no request")
	}
	f.On("POST", "/api/sn_sc/servicecatalog/items/prod1/submit_producer", snfake.Response{Status: 200, JSON: map[string]any{"result": map[string]any{}}})
	a = sn.NewIncidentAdapter(newClient(t, f, authtest.Valid), sn.IncidentOptions{CreateVia: "producer", Producer: "prod1"})
	if _, err := a.CreateIncident(context.Background(), createIn()); err == nil {
		t.Fatal("a producer response without number/sys_id must fail loudly (A-08)")
	}
}

// C3 acceptance: POST-count under injected 503. The create POST is marked safe
// to retry (the use case ran the dedupe miss first), so 503s are retried and
// the single logical create ends with exactly one successful POST.
func TestCreateRetriesOn503AfterDedupeMiss(t *testing.T) {
	f := snfake.New(t)
	f.Fail("POST", incPath, 2, snfake.Error(503, "busy"))
	f.On("POST", incPath, snfake.Response{Status: 201, JSON: map[string]any{"result": map[string]any{"sys_id": incID, "number": "INC1"}}})
	if _, err := tableAdapter(t, f).CreateIncident(context.Background(), createIn()); err != nil {
		t.Fatal(err)
	}
	if f.Posts() != 3 {
		t.Fatalf("posts=%d, want 3 (2 injected 503 + 1 success)", f.Posts())
	}
}

func TestCreatePersistent503IsExit8(t *testing.T) {
	f := snfake.New(t)
	f.On("POST", incPath, snfake.Error(503, "busy"))
	_, err := tableAdapter(t, f).CreateIncident(context.Background(), createIn())
	if output.ExitOf(err) != output.ExitRateLimited {
		t.Fatalf("exit %d: %v", output.ExitOf(err), err)
	}
}

func modRoute(f *snfake.Fake, mods ...string) {
	i := 0
	f.OnFunc("GET", incPath+"/"+incID, func(w http.ResponseWriter, _ *http.Request) {
		m := mods[min(i, len(mods)-1)]
		i++
		_ = json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"sys_id": incID, "number": "INC1", "sys_mod_count": m}})
	})
	f.On("PATCH", incPath+"/"+incID, snfake.Response{Status: 200, JSON: map[string]any{"result": map[string]any{"sys_id": incID, "number": "INC1", "sys_mod_count": "x"}}})
}

func TestUpdateReadPatchReread(t *testing.T) {
	f := snfake.New(t)
	modRoute(f, "4", "5")
	res, err := tableAdapter(t, f).UpdateIncident(context.Background(), usecase.IncidentUpdate{Ref: incID, Fields: map[string]string{"state": "2"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Record.Get("sys_mod_count") != "5" || res.Record.Table != "incident" {
		t.Fatalf("%+v", res)
	}
	var seq []string
	for _, r := range f.Requests() {
		seq = append(seq, r.Method)
	}
	if strings.Join(seq, ",") != "GET,PATCH,GET" {
		t.Fatalf("sequence %v", seq)
	}
	if string(f.Requests()[1].Body) != `{"state":"2"}` {
		t.Fatalf("patch body %s", f.Requests()[1].Body)
	}
}

func TestUpdateByNumberResolvesSysID(t *testing.T) {
	f := snfake.New(t)
	f.On("GET", incPath, snfake.Records(1, map[string]any{"sys_id": incID, "number": "INC1", "sys_mod_count": "1"}))
	modRoute(f, "1", "2")
	if _, err := tableAdapter(t, f).UpdateIncident(context.Background(), usecase.IncidentUpdate{Ref: "INC1", Fields: map[string]string{"state": "2"}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.Requests()[0].Query, "number") {
		t.Fatalf("lookup query %q", f.Requests()[0].Query)
	}
}

func TestUpdateNumberNotFound(t *testing.T) {
	f := snfake.New(t)
	f.On("GET", incPath, snfake.Records(0))
	_, err := tableAdapter(t, f).UpdateIncident(context.Background(), usecase.IncidentUpdate{Ref: "INC9", Fields: map[string]string{"state": "2"}})
	var nf *sn.NotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("want NotFoundError: %v", err)
	}
}

// Assumption A-09: sys_mod_count exists and increments by one per update. A
// jump of more than one between our read and re-read means someone else wrote
// concurrently -> conflict (exit 7).
func TestAssumptionA09ModCountConflict(t *testing.T) {
	f := snfake.New(t)
	modRoute(f, "4", "6")
	_, err := tableAdapter(t, f).UpdateIncident(context.Background(), usecase.IncidentUpdate{Ref: incID, Fields: map[string]string{"state": "2"}})
	if output.ExitOf(err) != output.ExitConflict {
		t.Fatalf("exit %d: %v", output.ExitOf(err), err)
	}
}

func TestAssumptionA09ExpectedModCountMismatchSendsNoPatch(t *testing.T) {
	f := snfake.New(t)
	modRoute(f, "4", "5")
	_, err := tableAdapter(t, f).UpdateIncident(context.Background(), usecase.IncidentUpdate{Ref: incID, Fields: map[string]string{"state": "2"}, ExpectedModCount: 3})
	if output.ExitOf(err) != output.ExitConflict || f.Count("PATCH", incPath+"/"+incID) != 0 {
		t.Fatalf("exit %d patches=%d", output.ExitOf(err), f.Count("PATCH", incPath+"/"+incID))
	}
}

// Assumption A-09: when sys_mod_count is absent the guard degrades to no
// detection (documented), the write still proceeds.
func TestAssumptionA09MissingModCountDegrades(t *testing.T) {
	f := snfake.New(t)
	f.On("GET", incPath+"/"+incID, snfake.Response{Status: 200, JSON: map[string]any{"result": map[string]any{"sys_id": incID, "number": "INC1"}}})
	f.On("PATCH", incPath+"/"+incID, snfake.Response{Status: 200, JSON: map[string]any{"result": map[string]any{"sys_id": incID, "number": "INC1"}}})
	if _, err := tableAdapter(t, f).UpdateIncident(context.Background(), usecase.IncidentUpdate{Ref: incID, Fields: map[string]string{"state": "2"}}); err != nil {
		t.Fatal(err)
	}
}

func TestPatchIsNotRetriedOn503(t *testing.T) {
	// A retried PATCH could duplicate journal entries (work_notes), so PATCH
	// is never marked safe to retry; the guard only detects conflicts.
	f := snfake.New(t)
	modRoute(f, "4", "5")
	f.Fail("PATCH", incPath+"/"+incID, 5, snfake.Error(503, "busy"))
	_, err := tableAdapter(t, f).UpdateIncident(context.Background(), usecase.IncidentUpdate{Ref: incID, Fields: map[string]string{"state": "2"}})
	if output.ExitOf(err) != output.ExitRateLimited || f.Count("PATCH", incPath+"/"+incID) != 1 {
		t.Fatalf("exit %d patches=%d", output.ExitOf(err), f.Count("PATCH", incPath+"/"+incID))
	}
}

func TestResolveSetsStateCloseCodeAndNotes(t *testing.T) {
	f := snfake.New(t)
	modRoute(f, "4", "5")
	a := sn.NewIncidentAdapter(newClient(t, f, authtest.Valid), sn.IncidentOptions{CreateVia: "table", ResolvedState: "9"})
	if _, err := a.ResolveIncident(context.Background(), usecase.IncidentResolve{Ref: incID, CloseCode: "Solved", CloseNote: "fixed"}); err != nil {
		t.Fatal(err)
	}
	var body map[string]string
	_ = json.Unmarshal(f.Requests()[1].Body, &body)
	if body["state"] != "9" || body["close_code"] != "Solved" || body["close_notes"] != "fixed" {
		t.Fatalf("%v", body)
	}
	// default resolved state is 6
	f2 := snfake.New(t)
	modRoute(f2, "1", "2")
	a = sn.NewIncidentAdapter(newClient(t, f2, authtest.Valid), sn.IncidentOptions{CreateVia: "table"})
	if _, err := a.ResolveIncident(context.Background(), usecase.IncidentResolve{Ref: incID, CloseCode: "Solved", CloseNote: "fixed"}); err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(f2.Requests()[1].Body, &body)
	if body["state"] != "6" {
		t.Fatalf("default state %v", body)
	}
}

func TestWriteErrorsMapped(t *testing.T) {
	f := snfake.New(t)
	f.On("POST", incPath, snfake.Error(400, "Mandatory field missing"))
	_, err := tableAdapter(t, f).CreateIncident(context.Background(), createIn())
	if output.ExitOf(err) != output.ExitValidation {
		t.Fatalf("exit %d: %v", output.ExitOf(err), err)
	}
	f.On("POST", incPath, snfake.Response{Status: 201, Body: []byte("not json")})
	if _, err := tableAdapter(t, f).CreateIncident(context.Background(), createIn()); err == nil {
		t.Fatal("unparseable success body must fail")
	}
}
