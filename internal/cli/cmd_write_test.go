package cli_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stainedhead/snow-cli/internal/cli"
	"github.com/stainedhead/snow-cli/internal/domain"
	"github.com/stainedhead/snow-cli/internal/usecase"
	"github.com/stainedhead/snow-cli/internal/usecase/write"
)

type wIncidents struct {
	creates  []usecase.IncidentCreate
	updates  []usecase.IncidentUpdate
	resolves []usecase.IncidentResolve
}

func (w *wIncidents) FindByCorrelation(context.Context, string) (*domain.Record, error) {
	return nil, nil
}
func (w *wIncidents) CreateIncident(_ context.Context, in usecase.IncidentCreate) (domain.WriteResult, error) {
	w.creates = append(w.creates, in)
	return domain.WriteResult{Record: domain.Record{Table: "incident", Fields: map[string]string{"number": "INC1"}}}, nil
}
func (w *wIncidents) UpdateIncident(_ context.Context, in usecase.IncidentUpdate) (domain.WriteResult, error) {
	w.updates = append(w.updates, in)
	return domain.WriteResult{Record: domain.Record{Table: "incident", Fields: map[string]string{"number": in.Ref}}}, nil
}
func (w *wIncidents) ResolveIncident(_ context.Context, in usecase.IncidentResolve) (domain.WriteResult, error) {
	w.resolves = append(w.resolves, in)
	return domain.WriteResult{Record: domain.Record{Table: "incident", Fields: map[string]string{"number": in.Ref}}}, nil
}

type wTasks struct{ updates []usecase.TaskUpdate }

func (w *wTasks) UpdateTask(_ context.Context, in usecase.TaskUpdate) (domain.WriteResult, error) {
	w.updates = append(w.updates, in)
	return domain.WriteResult{Record: domain.Record{Table: "sc_task", Fields: map[string]string{"number": in.Ref}}}, nil
}
func (w *wTasks) FetchTask(context.Context, string) (domain.Record, error) {
	return domain.Record{Fields: map[string]string{write.AssignedToUserName: "svc"}}, nil
}

type wCatalog struct{}

func (wCatalog) Search(context.Context, string, int, int) ([]domain.CatalogItem, error) {
	return nil, nil
}
func (wCatalog) Item(context.Context, string) (domain.CatalogItem, error) {
	return domain.CatalogItem{SysID: strings.Repeat("c", 32), Name: "Laptop"}, nil
}
func (wCatalog) Variables(context.Context, string) ([]domain.CatalogVariable, error) {
	return []domain.CatalogVariable{{Name: "model", Mandatory: true}}, nil
}

type wOrders struct{ n int }

func (w *wOrders) Order(context.Context, usecase.OrderRequest) (domain.WriteResult, error) {
	w.n++
	return domain.WriteResult{Record: domain.Record{Table: "sc_request", Fields: map[string]string{"number": "REQ1"}}}, nil
}

type wFixedClock struct{}

func (wFixedClock) Now() time.Time { return time.Date(2026, 10, 3, 10, 15, 0, 0, time.UTC) }

func writeHarness(t *testing.T) (*harness, *wIncidents, *wTasks, *wOrders) {
	t.Helper()
	h := newHarness(t)
	cli.RegisterWrite(h.r)
	inc, tk, ord := &wIncidents{}, &wTasks{}, &wOrders{}
	h.env = &cli.Env{
		Mode: domain.ModeAgent, AgentID: "a1", RunID: "r1",
		Incidents: inc, Tasks: tk, Orders: ord, Catalog: wCatalog{},
		Identity: fakeIdentity{id: domain.Identity{User: "svc"}},
		Guard:    allowGuard{}, Clock: wFixedClock{},
		Extra: map[string]any{cli.ExtraTaskFetcher: tk},
	}
	return h, inc, tk, ord
}

var createArgs = []string{"incident", "create", "--short-description", "Disk full", "--description", "98%", "--ci", "db01", "--impact", "2", "--urgency", "3"}

func TestIncidentCreateCommand(t *testing.T) {
	h, inc, _, _ := writeHarness(t)
	if code := h.run(createArgs...); code != 0 {
		t.Fatalf("exit %d: %s", code, h.out.String())
	}
	d := decode(t, h)["data"].(map[string]any)
	if d["deduplicated"] != false || d["dry_run"] != false || d["record"].(map[string]any)["number"] != "INC1" {
		t.Fatalf("%v", d)
	}
	if len(inc.creates) != 1 || inc.creates[0].CI != "db01" || inc.creates[0].Impact != 2 {
		t.Fatalf("%+v", inc.creates)
	}
}

func TestIncidentCreateAppAndIdempotencyKeyAndGroup(t *testing.T) {
	h, inc, _, _ := writeHarness(t)
	args := []string{"incident", "create", "--short-description", "s", "--description", "d", "--app", "billing", "--impact", "2", "--urgency", "2",
		"--idempotency-key", "k9", "--assignment-group", "Net Ops", "--note", "hello"}
	if code := h.run(args...); code != 0 {
		t.Fatalf("exit %d: %s", code, h.out.String())
	}
	c := inc.creates[0]
	if c.CI != "billing" || c.CorrelationID != "k9" || c.AssignmentGroup != "Net Ops" || !strings.HasSuffix(c.WorkNote, "hello") {
		t.Fatalf("%+v", c)
	}
}

func TestIncidentCreateMissingFieldsExit9(t *testing.T) {
	for _, drop := range []string{"--short-description", "--description", "--impact", "--urgency", "--ci"} {
		h, inc, _, _ := writeHarness(t)
		var args []string
		for i := 0; i < len(createArgs); i++ {
			if createArgs[i] == drop {
				i++
				continue
			}
			args = append(args, createArgs[i])
		}
		if code := h.run(args...); code != 9 {
			t.Errorf("without %s: exit %d (%s)", drop, code, h.out.String())
		}
		if len(inc.creates) != 0 {
			t.Errorf("without %s: wrote", drop)
		}
	}
	h, _, _, _ := writeHarness(t)
	if code := h.run(append(createArgs, "--app", "x")...); code != 9 {
		t.Errorf("--ci with --app: exit %d", code)
	}
}

func TestIncidentCreateDryRunFlag(t *testing.T) {
	h, inc, _, _ := writeHarness(t)
	if code := h.run(append(createArgs, "--dry-run")...); code != 0 {
		t.Fatalf("exit %d", code)
	}
	d := decode(t, h)["data"].(map[string]any)
	if d["dry_run"] != true || len(inc.creates) != 0 {
		t.Fatalf("%v", d)
	}
}

func humanHarness(t *testing.T, in *bytes.Buffer) (*harness, *wIncidents) {
	h, inc, _, _ := writeHarness(t)
	h.env.Mode = domain.ModeHuman
	var errBuf bytes.Buffer
	h.env.Err = &errBuf
	if in != nil {
		h.env.In = in
	}
	return h, inc
}

func TestHumanConfirmationYes(t *testing.T) {
	h, inc := humanHarness(t, bytes.NewBufferString("y\n"))
	if code := h.run(createArgs...); code != 0 || len(inc.creates) != 1 {
		t.Fatalf("exit %d creates=%d %s", code, len(inc.creates), h.out.String())
	}
	if !strings.Contains(h.env.Err.(*bytes.Buffer).String(), "Disk full") {
		t.Fatalf("prompt on stderr: %q", h.env.Err.(*bytes.Buffer).String())
	}
}

func TestHumanConfirmationNoAborts(t *testing.T) {
	h, inc := humanHarness(t, bytes.NewBufferString("n\n"))
	if code := h.run(createArgs...); code != 1 || len(inc.creates) != 0 {
		t.Fatalf("exit %d creates=%d", code, len(inc.creates))
	}
	h, inc = humanHarness(t, bytes.NewBufferString(""))
	if code := h.run(createArgs...); code != 1 || len(inc.creates) != 0 {
		t.Fatalf("EOF must decline: exit %d", code)
	}
}

func TestHumanNonInteractiveWithoutYesIsExit2(t *testing.T) {
	h, inc := humanHarness(t, nil)
	if code := h.run(createArgs...); code != 2 || len(inc.creates) != 0 {
		t.Fatalf("exit %d creates=%d", code, len(inc.creates))
	}
}

func TestHumanYesSkipsPromptAndDryRunNeverPrompts(t *testing.T) {
	h, inc := humanHarness(t, nil)
	if code := h.run(append(createArgs, "--yes")...); code != 0 || len(inc.creates) != 1 {
		t.Fatalf("--yes: exit %d", code)
	}
	h, inc = humanHarness(t, nil)
	if code := h.run(append(createArgs, "--dry-run")...); code != 0 || len(inc.creates) != 0 {
		t.Fatalf("--dry-run without --yes must not prompt: exit %d", code)
	}
}

func TestAgentModeNeverPrompts(t *testing.T) {
	h, inc, _, _ := writeHarness(t)
	h.env.In = bytes.NewBufferString("n\n")
	if code := h.run(createArgs...); code != 0 || len(inc.creates) != 1 {
		t.Fatalf("exit %d", code)
	}
}

func TestIncidentUpdateAndResolveCommands(t *testing.T) {
	h, inc, _, _ := writeHarness(t)
	if code := h.run("incident", "update", "INC1", "--set", "state=2", "--set", "short_description=a=b", "--work-note", "n"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.out.String())
	}
	f := inc.updates[0].Fields
	if f["state"] != "2" || f["short_description"] != "a=b" || !strings.HasPrefix(f["work_notes"], "[snow-cli agent=a1 run=r1] n") {
		t.Fatalf("%v", f)
	}
	if code := h.run("incident", "update", "INC1", "--set", "novalue"); code != 2 {
		t.Fatalf("bad --set: exit %d", code)
	}
	if code := h.run("incident", "update"); code != 2 {
		t.Fatalf("missing ref: exit %d", code)
	}
	if code := h.run("incident", "resolve", "INC1", "--close-code", "Solved", "--close-notes", "ok"); code != 0 || len(inc.resolves) != 1 {
		t.Fatalf("resolve exit %d", code)
	}
	if code := h.run("incident", "resolve", "INC1"); code != 9 {
		t.Fatalf("resolve without flags: exit %d", code)
	}
	if code := h.run("incident", "resolve"); code != 2 {
		t.Fatalf("resolve without ref: exit %d", code)
	}
}

func TestTaskUpdateCommand(t *testing.T) {
	h, _, tk, _ := writeHarness(t)
	if code := h.run("task", "update", "SCTASK1", "--work-note", "n", "--comment", "c", "--state", "2", "--assigned-to", "svc"); code != 0 {
		t.Fatalf("exit %d: %s", code, h.out.String())
	}
	f := tk.updates[0].Fields
	if f["comments"] != "c" || f["state"] != "2" || f["assigned_to"] != "svc" || f["work_notes"] == "" {
		t.Fatalf("%v", f)
	}
	if code := h.run("task", "update"); code != 2 {
		t.Fatalf("exit %d", code)
	}
	if code := h.run("task", "update", "SCTASK1"); code != 9 {
		t.Fatalf("no fields: exit %d", code)
	}
}

func TestCatalogOrderCommand(t *testing.T) {
	h, _, _, ord := writeHarness(t)
	if code := h.run("catalog", "order", "Laptop", "--var", "model=x1"); code != 0 || ord.n != 1 {
		t.Fatalf("exit %d: %s", code, h.out.String())
	}
	if code := h.run("catalog", "order", "Laptop"); code != 9 {
		t.Fatalf("missing mandatory var: exit %d", code)
	}
	if code := h.run("catalog", "order", "Laptop", "--var", "bad"); code != 2 {
		t.Fatalf("bad --var: exit %d", code)
	}
	if code := h.run("catalog", "order"); code != 2 {
		t.Fatalf("missing item: exit %d", code)
	}
	if code := h.run("catalog", "order", "Laptop", "--var", "model=x1", "--dry-run"); code != 0 || ord.n != 1 {
		t.Fatalf("dry-run must not order: exit %d n=%d", code, ord.n)
	}
}

func TestWriteCommandsNeedWiredPorts(t *testing.T) {
	h := newHarness(t)
	cli.RegisterWrite(h.r)
	h.env = &cli.Env{Mode: domain.ModeAgent, Guard: allowGuard{}}
	for _, args := range [][]string{createArgs, {"incident", "update", "INC1", "--set", "a=b"}, {"incident", "resolve", "INC1", "--close-code", "c", "--close-notes", "n"},
		{"task", "update", "SCTASK1", "--state", "2"}, {"catalog", "order", "x"}} {
		if code := h.run(args...); code != 1 {
			t.Errorf("%v: exit %d, want 1 (port not wired)", args, code)
		}
	}
}

func TestWriteCommandsRegistered(t *testing.T) {
	h := newHarness(t)
	cli.RegisterWrite(h.r)
	got := map[string]bool{}
	for _, c := range h.r.Commands() {
		got[c.Name()] = true
		if c.Summary == "" || c.Usage == "" || len(c.Examples) == 0 {
			t.Errorf("%s lacks docs", c.Name())
		}
	}
	for _, n := range []string{"incident create", "incident update", "incident resolve", "task update", "catalog order"} {
		if !got[n] {
			t.Errorf("missing %s", n)
		}
	}
	if got["change create"] {
		t.Error("change create must be absent (M4)")
	}
}
