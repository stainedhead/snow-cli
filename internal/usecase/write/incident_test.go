package write

import (
	"context"
	"strings"
	"testing"

	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/snow-cli/internal/domain"
	"github.com/stainedhead/snow-cli/internal/idempotency"
	"github.com/stainedhead/snow-cli/internal/usecase"
)

type fakeIncidents struct {
	found                *domain.Record
	findErr, createErr   error
	findCalls, createN   int
	updates              []usecase.IncidentUpdate
	resolves             []usecase.IncidentResolve
	creates              []usecase.IncidentCreate
	updateErr            error
	foundKey             string
	order                []string
}

func (f *fakeIncidents) FindByCorrelation(_ context.Context, id string) (*domain.Record, error) {
	f.findCalls++
	f.foundKey = id
	f.order = append(f.order, "find")
	return f.found, f.findErr
}
func (f *fakeIncidents) CreateIncident(_ context.Context, in usecase.IncidentCreate) (domain.WriteResult, error) {
	f.createN++
	f.order = append(f.order, "create")
	f.creates = append(f.creates, in)
	if f.createErr != nil {
		return domain.WriteResult{}, f.createErr
	}
	return domain.WriteResult{Record: rec(map[string]string{"number": "INC0010001"})}, nil
}
func (f *fakeIncidents) UpdateIncident(_ context.Context, in usecase.IncidentUpdate) (domain.WriteResult, error) {
	f.updates = append(f.updates, in)
	return domain.WriteResult{Record: rec(map[string]string{"number": in.Ref})}, f.updateErr
}
func (f *fakeIncidents) ResolveIncident(_ context.Context, in usecase.IncidentResolve) (domain.WriteResult, error) {
	f.resolves = append(f.resolves, in)
	return domain.WriteResult{Record: rec(map[string]string{"number": in.Ref})}, f.updateErr
}

func goodCreate() CreateInput {
	return CreateInput{ShortDescription: "Disk full", Description: "db01 disk 98%", CI: "db01", Impact: 2, Urgency: 3}
}

func svc(g usecase.Guard, w usecase.IncidentWriter) IncidentService {
	return IncidentService{Base: base(g), Writer: w}
}

func TestCreateRequiredFields(t *testing.T) {
	mut := map[string]func(*CreateInput){
		"short":   func(c *CreateInput) { c.ShortDescription = "" },
		"desc":    func(c *CreateInput) { c.Description = " " },
		"ci":      func(c *CreateInput) { c.CI = "" },
		"impact":  func(c *CreateInput) { c.Impact = 0 },
		"urgency": func(c *CreateInput) { c.Urgency = 9 },
	}
	for name, m := range mut {
		t.Run(name, func(t *testing.T) {
			w := &fakeIncidents{}
			g := mustPolicy(t, allowAll)
			in := goodCreate()
			m(&in)
			_, err := svc(g, w).Create(context.Background(), in)
			wantCategory(t, err, output.CategoryValidation)
			if len(g.actions) != 0 || w.findCalls+w.createN != 0 {
				t.Fatal("validation must run before policy and before any request")
			}
		})
	}
}

func TestCreateDedupeMissThenCreate(t *testing.T) {
	w := &fakeIncidents{}
	res, err := svc(mustPolicy(t, allowAll), w).Create(context.Background(), goodCreate())
	if err != nil {
		t.Fatal(err)
	}
	if res.Deduplicated || res.Record.Get("number") != "INC0010001" {
		t.Fatalf("%+v", res)
	}
	if strings.Join(w.order, ",") != "find,create" {
		t.Fatalf("dedupe must precede create: %v", w.order)
	}
	c := w.creates[0]
	want := idempotency.Key("agent-1", "db01", "Disk full", fixedClock{}.Now())
	if c.CorrelationID != want || w.foundKey != want {
		t.Fatalf("correlation id %q / lookup %q, want %q", c.CorrelationID, w.foundKey, want)
	}
	if c.CorrelationDisplay != "agent:agent-1" {
		t.Fatalf("correlation_display %q", c.CorrelationDisplay)
	}
	if !strings.HasPrefix(c.WorkNote, "[snow-cli agent=agent-1 run=run-1]") {
		t.Fatalf("work note %q", c.WorkNote)
	}
}

func TestCreateDedupeHitSendsNothing(t *testing.T) {
	w := &fakeIncidents{found: &domain.Record{Table: "incident", Fields: map[string]string{"number": "INC0000007"}}}
	res, err := svc(mustPolicy(t, allowAll), w).Create(context.Background(), goodCreate())
	if err != nil {
		t.Fatal(err)
	}
	if !res.Deduplicated || res.Record.Get("number") != "INC0000007" || w.createN != 0 {
		t.Fatalf("%+v creates=%d", res, w.createN)
	}
}

func TestCreateExplicitIdempotencyKey(t *testing.T) {
	w := &fakeIncidents{}
	in := goodCreate()
	in.IdempotencyKey = "my-key"
	if _, err := svc(mustPolicy(t, allowAll), w).Create(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if w.foundKey != "my-key" || w.creates[0].CorrelationID != "my-key" {
		t.Fatalf("explicit key not used: %q", w.foundKey)
	}
}

func TestCreateDedupeLookupErrorAbortsCreate(t *testing.T) {
	w := &fakeIncidents{findErr: errBoom}
	_, err := svc(mustPolicy(t, allowAll), w).Create(context.Background(), goodCreate())
	if err == nil || w.createN != 0 {
		t.Fatalf("lookup failure must abort create: %v creates=%d", err, w.createN)
	}
}

func TestCreateNeverWritesPriority(t *testing.T) {
	// Assumption A-01: priority is derived by instance rules; the CLI never writes it.
	w := &fakeIncidents{}
	g := mustPolicy(t, allowAll)
	if _, err := svc(g, w).Create(context.Background(), goodCreate()); err != nil {
		t.Fatal(err)
	}
	if _, ok := g.actions[0].Request.Fields["priority"]; ok {
		t.Fatal("priority must not appear in the policy request")
	}
	res, _ := IncidentService{Base: Base{Guard: g, Clock: fixedClock{}, DryRun: true}, Writer: w}.Create(context.Background(), goodCreate())
	if _, ok := res.Record.Fields["priority"]; ok {
		t.Fatal("priority must not appear in the payload preview")
	}
}

// Assumption A-01 (spec D-d): with the agent constraint min 2 max 3, impact or
// urgency 1 (High on the OOB scale) is denied, 2 and 3 allowed.
func TestAssumptionA01ImpactUrgencyConstraint(t *testing.T) {
	const pol = `
version: 1
rules:
  - id: create
    effect: allow
    verbs: [create]
    resources: [incident]
    constraints:
      impact: {min: 2, max: 3}
      urgency: {min: 2, max: 3}
`
	for _, field := range []string{"impact", "urgency"} {
		for v, wantDenied := range map[int]bool{1: true, 2: false, 3: false} {
			in := goodCreate()
			in.Impact, in.Urgency = 2, 2
			if field == "impact" {
				in.Impact = v
			} else {
				in.Urgency = v
			}
			_, err := svc(mustPolicy(t, pol), &fakeIncidents{}).Create(context.Background(), in)
			if wantDenied {
				wantCategory(t, err, output.CategoryPolicyDenied)
			} else if err != nil {
				t.Fatalf("%s=%d: %v", field, v, err)
			}
		}
	}
}

func TestCreateImpactUsesConfiguredScale(t *testing.T) {
	s := svc(mustPolicy(t, allowAll), &fakeIncidents{})
	s.Scale = domain.Scale{High: 10, Medium: 20, Low: 30}
	in := goodCreate()
	in.Impact, in.Urgency = 20, 30
	if _, err := s.Create(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	in.Impact = 2
	_, err := s.Create(context.Background(), in)
	wantCategory(t, err, output.CategoryValidation)
}

func TestCreateDryRunSendsNothing(t *testing.T) {
	w := &fakeIncidents{}
	s := svc(mustPolicy(t, allowAll), w)
	s.DryRun = true
	res, err := s.Create(context.Background(), goodCreate())
	if err != nil {
		t.Fatal(err)
	}
	if !res.DryRun || w.findCalls+w.createN != 0 {
		t.Fatalf("dry run must make no requests: %+v", res)
	}
	f := res.Record.Fields
	if f["short_description"] != "Disk full" || f["impact"] != "2" || f["correlation_id"] == "" || f["correlation_display"] != "agent:agent-1" {
		t.Fatalf("preview payload: %v", f)
	}
}

func TestCreateDryRunOnlyPolicyPreviews(t *testing.T) {
	const pol = `
version: 1
rules:
  - {id: dry, effect: allow, mode: dry_run_only, verbs: [create], resources: [incident]}
`
	w := &fakeIncidents{}
	res, err := svc(mustPolicy(t, pol), w).Create(context.Background(), goodCreate())
	if err != nil || !res.DryRun || w.createN+w.findCalls != 0 {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestCreatePolicyDenied(t *testing.T) {
	w := &fakeIncidents{}
	_, err := svc(mustPolicy(t, "version: 1\nrules: []\n"), w).Create(context.Background(), goodCreate())
	wantCategory(t, err, output.CategoryPolicyDenied)
	if w.createN+w.findCalls != 0 {
		t.Fatal("denied create must not touch the writer")
	}
}

func TestCreateConfirmation(t *testing.T) {
	w := &fakeIncidents{}
	s := svc(mustPolicy(t, allowAll), w)
	var prompts []string
	s.Confirm = func(p string) error { prompts = append(prompts, p); return errBoom }
	if _, err := s.Create(context.Background(), goodCreate()); err != errBoom {
		t.Fatalf("declined confirm must abort: %v", err)
	}
	if w.createN+w.findCalls != 0 || len(prompts) != 1 || !strings.Contains(prompts[0], "Disk full") {
		t.Fatalf("prompts=%v creates=%d", prompts, w.createN)
	}
	// dry-run never prompts
	s.DryRun = true
	prompts = nil
	if _, err := s.Create(context.Background(), goodCreate()); err != nil || len(prompts) != 0 {
		t.Fatalf("dry-run must not prompt: %v %v", err, prompts)
	}
}

func TestUpdate(t *testing.T) {
	w := &fakeIncidents{}
	g := mustPolicy(t, allowAll)
	res, err := svc(g, w).Update(context.Background(), "inc0010001", map[string]string{"work_notes": "looking", "state": "2"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Record.Get("number") != "INC0010001" {
		t.Fatalf("%+v", res)
	}
	u := w.updates[0]
	if u.Ref != "INC0010001" || u.ExpectedModCount != 0 || u.Fields["state"] != "2" {
		t.Fatalf("%+v", u)
	}
	if !strings.HasPrefix(u.Fields["work_notes"], "[snow-cli agent=agent-1 run=run-1] looking") {
		t.Fatalf("provenance missing: %q", u.Fields["work_notes"])
	}
	// policy sees the user's own value, not the prefix
	if g.actions[0].Request.Fields["work_notes"] != "looking" || g.actions[0].Request.Verb != "update" {
		t.Fatalf("%+v", g.actions[0].Request)
	}
}

func TestUpdateValidation(t *testing.T) {
	cases := map[string]struct {
		ref    string
		fields map[string]string
	}{
		"bad ref":  {"bogus", map[string]string{"state": "2"}},
		"not inc":  {"CHG0000001", map[string]string{"state": "2"}},
		"no field": {"INC1", nil},
		"priority": {"INC1", map[string]string{"priority": "1"}},
		"empty k":  {"INC1", map[string]string{"": "x"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			w := &fakeIncidents{}
			_, err := svc(mustPolicy(t, allowAll), w).Update(context.Background(), c.ref, c.fields)
			wantCategory(t, err, output.CategoryValidation)
			if len(w.updates) != 0 {
				t.Fatal("must not write")
			}
		})
	}
}

func TestUpdateSysIDRefAndDryRunAndError(t *testing.T) {
	w := &fakeIncidents{}
	s := svc(mustPolicy(t, allowAll), w)
	id := strings.Repeat("ab", 16)
	if _, err := s.Update(context.Background(), id, map[string]string{"state": "2"}); err != nil || w.updates[0].Ref != id {
		t.Fatalf("sys_id ref: %v %+v", err, w.updates)
	}
	s.DryRun = true
	res, err := s.Update(context.Background(), "INC1", map[string]string{"work_notes": "x"})
	if err != nil || !res.DryRun || len(w.updates) != 1 {
		t.Fatalf("dry-run: %+v %v", res, err)
	}
	if !strings.HasPrefix(res.Record.Fields["work_notes"], "[snow-cli") {
		t.Fatalf("preview should show the prefixed note: %v", res.Record.Fields)
	}
	s.DryRun = false
	w.updateErr = errBoom
	if _, err := s.Update(context.Background(), "INC1", map[string]string{"state": "2"}); err != errBoom {
		t.Fatalf("adapter error must propagate: %v", err)
	}
}

func TestUpdateConfirmAndDenied(t *testing.T) {
	w := &fakeIncidents{}
	s := svc(mustPolicy(t, allowAll), w)
	s.Confirm = func(string) error { return errBoom }
	if _, err := s.Update(context.Background(), "INC1", map[string]string{"state": "2"}); err != errBoom || len(w.updates) != 0 {
		t.Fatalf("%v", err)
	}
	_, err := svc(mustPolicy(t, "version: 1\nrules: []\n"), w).Update(context.Background(), "INC1", map[string]string{"state": "2"})
	wantCategory(t, err, output.CategoryPolicyDenied)
}

func TestResolve(t *testing.T) {
	w := &fakeIncidents{}
	g := mustPolicy(t, allowAll)
	res, err := svc(g, w).Resolve(context.Background(), "INC1", "Solved (Permanently)", "restarted")
	if err != nil {
		t.Fatal(err)
	}
	r := w.resolves[0]
	if res.Record.Get("number") != "INC1" || r.CloseCode != "Solved (Permanently)" || !strings.HasSuffix(r.CloseNote, "restarted") || !strings.HasPrefix(r.CloseNote, "[snow-cli agent=agent-1") {
		t.Fatalf("%+v", r)
	}
	if g.actions[0].Request.Verb != "resolve" || g.actions[0].Request.Resource != "incident" {
		t.Fatalf("%+v", g.actions[0].Request)
	}
}

func TestResolveValidationDeniedDryRunConfirm(t *testing.T) {
	w := &fakeIncidents{}
	s := svc(mustPolicy(t, allowAll), w)
	_, err := s.Resolve(context.Background(), "INC1", "", "n")
	wantCategory(t, err, output.CategoryValidation)
	_, err = s.Resolve(context.Background(), "INC1", "c", " ")
	wantCategory(t, err, output.CategoryValidation)
	_, err = s.Resolve(context.Background(), "nope", "c", "n")
	wantCategory(t, err, output.CategoryValidation)

	// FR-042: agents are default-deny for resolve.
	_, err = svc(mustPolicy(t, "version: 1\nrules:\n  - {id: u, effect: allow, verbs: [update], resources: [incident]}\n"), w).Resolve(context.Background(), "INC1", "c", "n")
	wantCategory(t, err, output.CategoryPolicyDenied)

	s.DryRun = true
	if res, err := s.Resolve(context.Background(), "INC1", "c", "n"); err != nil || !res.DryRun || len(w.resolves) != 0 {
		t.Fatalf("%v %+v", err, res)
	}
	s.DryRun = false
	s.Confirm = func(string) error { return errBoom }
	if _, err := s.Resolve(context.Background(), "INC1", "c", "n"); err != errBoom || len(w.resolves) != 0 {
		t.Fatalf("%v", err)
	}
}
