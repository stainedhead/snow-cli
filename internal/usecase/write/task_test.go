package write

import (
	"context"
	"strings"
	"testing"

	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/snow-cli/internal/domain"
	"github.com/stainedhead/snow-cli/internal/usecase"
)

type fakeTasks struct {
	assigned  string
	modCount  string
	fetchErr  error
	updates   []usecase.TaskUpdate
	updateErr error
}

func (f *fakeTasks) FetchTask(context.Context, string) (domain.Record, error) {
	return domain.Record{Table: "sc_task", Fields: map[string]string{"number": "SCTASK1", AssignedToUserName: f.assigned, "sys_mod_count": f.modCount}}, f.fetchErr
}
func (f *fakeTasks) UpdateTask(_ context.Context, in usecase.TaskUpdate) (domain.WriteResult, error) {
	f.updates = append(f.updates, in)
	return domain.WriteResult{Record: domain.Record{Table: "sc_task", Fields: map[string]string{"number": in.Ref}}}, f.updateErr
}

type fakeIdentity struct{ err error }

func (f fakeIdentity) Whoami(context.Context) (domain.Identity, error) {
	return domain.Identity{User: "svc.agent"}, f.err
}

func taskSvc(g usecase.Guard, t *fakeTasks) TaskService {
	return TaskService{Base: base(g), Writer: t, Fetcher: t, Identity: fakeIdentity{}}
}

func TestTaskUpdateAssignedToSelf(t *testing.T) {
	ft := &fakeTasks{assigned: "SVC.agent"}
	g := mustPolicy(t, allowAll)
	res, err := taskSvc(g, ft).Update(context.Background(), "sctask1", map[string]string{"work_notes": "done step", "comments": "hello", "state": "2"})
	if err != nil {
		t.Fatal(err)
	}
	u := ft.updates[0]
	if res.Record.Get("number") != "SCTASK1" || u.Ref != "SCTASK1" || !strings.HasPrefix(u.Fields["work_notes"], "[snow-cli agent=agent-1") || u.Fields["comments"] != "hello" {
		t.Fatalf("%+v", u)
	}
	if g.actions[0].Request.Resource != "task" || g.actions[0].Request.Verb != "update" {
		t.Fatalf("%+v", g.actions[0].Request)
	}
}

func TestTaskUpdateNotAssignedToCaller(t *testing.T) {
	ft := &fakeTasks{assigned: "someone.else"}
	_, err := taskSvc(mustPolicy(t, allowAll), ft).Update(context.Background(), "SCTASK1", map[string]string{"work_notes": "x"})
	wantCategory(t, err, output.CategoryPolicyDenied)
	if len(ft.updates) != 0 {
		t.Fatal("must not write")
	}
}

func TestTaskClaimUnassignedForSelfOnly(t *testing.T) {
	ft := &fakeTasks{assigned: ""}
	s := taskSvc(mustPolicy(t, allowAll), ft)
	if _, err := s.Update(context.Background(), "SCTASK1", map[string]string{"assigned_to": "svc.agent"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update(context.Background(), "SCTASK1", map[string]string{"work_notes": "x"}); err == nil {
		t.Fatal("unassigned task: only a claim is allowed")
	}
}

func TestTaskAssignToOtherDenied(t *testing.T) {
	ft := &fakeTasks{assigned: "svc.agent"}
	_, err := taskSvc(mustPolicy(t, allowAll), ft).Update(context.Background(), "SCTASK1", map[string]string{"assigned_to": "bob"})
	wantCategory(t, err, output.CategoryPolicyDenied)
}

func TestTaskUpdateValidation(t *testing.T) {
	cases := map[string]struct {
		ref    string
		fields map[string]string
	}{
		"bad ref":      {"x", map[string]string{"work_notes": "a"}},
		"inc ref":      {"INC1", map[string]string{"work_notes": "a"}},
		"none":         {"SCTASK1", nil},
		"unknown":      {"SCTASK1", map[string]string{"priority": "1"}},
		"closed state": {"SCTASK1", map[string]string{"state": "7"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			ft := &fakeTasks{assigned: "svc.agent"}
			g := mustPolicy(t, allowAll)
			_, err := taskSvc(g, ft).Update(context.Background(), c.ref, c.fields)
			wantCategory(t, err, output.CategoryValidation)
			if len(g.actions) != 0 {
				t.Fatal("validation precedes policy")
			}
		})
	}
}

// Assumption: the limited state set for agents is Work in Progress (2) and
// Closed Complete (3) on the OOB sc_task state model; instances differ.
func TestAssumptionTaskStateSet(t *testing.T) {
	for _, st := range []string{"2", "3"} {
		ft := &fakeTasks{assigned: "svc.agent"}
		if _, err := taskSvc(mustPolicy(t, allowAll), ft).Update(context.Background(), "SCTASK1", map[string]string{"state": st}); err != nil {
			t.Fatalf("state %s: %v", st, err)
		}
	}
}

func TestTaskErrorsAndModes(t *testing.T) {
	ctx := context.Background()
	f := map[string]string{"work_notes": "x"}
	ft := &fakeTasks{assigned: "svc.agent", fetchErr: errBoom}
	if _, err := taskSvc(mustPolicy(t, allowAll), ft).Update(ctx, "SCTASK1", f); err != errBoom {
		t.Fatalf("fetch error: %v", err)
	}
	ft = &fakeTasks{assigned: "svc.agent"}
	s := taskSvc(mustPolicy(t, allowAll), ft)
	s.Identity = fakeIdentity{err: errBoom}
	if _, err := s.Update(ctx, "SCTASK1", f); err != errBoom {
		t.Fatalf("identity error: %v", err)
	}
	s = taskSvc(mustPolicy(t, allowAll), ft)
	s.DryRun = true
	if res, err := s.Update(ctx, "SCTASK1", f); err != nil || !res.DryRun || len(ft.updates) != 0 {
		t.Fatalf("dry-run: %v %+v", err, res)
	}
	s.DryRun = false
	s.Confirm = func(string) error { return errBoom }
	if _, err := s.Update(ctx, "SCTASK1", f); err != errBoom || len(ft.updates) != 0 {
		t.Fatalf("confirm: %v", err)
	}
	_, err := taskSvc(mustPolicy(t, denyAll), ft).Update(ctx, "SCTASK1", f)
	wantCategory(t, err, output.CategoryPolicyDenied)
	ft.updateErr = errBoom
	if _, err := taskSvc(mustPolicy(t, allowAll), ft).Update(ctx, "SCTASK1", f); err != errBoom {
		t.Fatalf("update error: %v", err)
	}
}

// FR-R08: the task update uses the sys_mod_count it fetched for its own
// assignment check as the precondition, unless the caller supplied one.
func TestTaskUpdateUsesFetchedOrSuppliedModCount(t *testing.T) {
	ft := &fakeTasks{assigned: "svc.agent", modCount: "12"}
	s := taskSvc(mustPolicy(t, allowAll), ft)
	if _, err := s.Update(context.Background(), "SCTASK1", map[string]string{"work_notes": "x"}); err != nil {
		t.Fatal(err)
	}
	s.ExpectedModCount = 9
	if _, err := s.Update(context.Background(), "SCTASK1", map[string]string{"work_notes": "x"}); err != nil {
		t.Fatal(err)
	}
	ft.modCount = ""
	s.ExpectedModCount = 0
	if _, err := s.Update(context.Background(), "SCTASK1", map[string]string{"work_notes": "x"}); err != nil {
		t.Fatal(err)
	}
	if ft.updates[0].ExpectedModCount != 12 || ft.updates[1].ExpectedModCount != 9 || ft.updates[2].ExpectedModCount != 0 {
		t.Fatalf("%+v", ft.updates)
	}
}

func TestTaskAppliedConflictSetsAuditOutcome(t *testing.T) {
	ft := &fakeTasks{assigned: "svc.agent", updateErr: appliedErr{}}
	g := mustPolicy(t, allowAll)
	_, err := taskSvc(g, ft).Update(context.Background(), "SCTASK1", map[string]string{"work_notes": "x"})
	wantCategory(t, err, output.CategoryConflict)
	if g.outcomes[0] != usecase.OutcomeAppliedConflict {
		t.Fatalf("outcome %q", g.outcomes[0])
	}
}
