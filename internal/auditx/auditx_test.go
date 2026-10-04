package auditx

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stainedhead/agent-cli-core/audit"
	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/agent-cli-core/policy"
	"github.com/stainedhead/snow-cli/internal/usecase"
)

type failWriter struct{ after, n int }

func (w *failWriter) Write(p []byte) (int, error) {
	w.n++
	if w.n > w.after {
		return 0, errors.New("disk full")
	}
	return len(p), nil
}

const allowAll = `
version: 1
rules:
  - id: allow-get
    effect: allow
    verbs: [get, update]
    resources: ["table:incident"]
  - id: dry
    effect: allow
    mode: dry_run_only
    verbs: [order]
    resources: ["catalog:item:*"]
`

func engine(t *testing.T) *policy.Engine {
	t.Helper()
	p, err := policy.Parse([]byte(allowAll))
	if err != nil {
		t.Fatal(err)
	}
	return policy.NewEngine(p, nil)
}

func newGuard(t *testing.T, w interface{ Write([]byte) (int, error) }, warn func(error)) *Guard {
	t.Helper()
	lg := audit.NewLogger(w)
	return &Guard{Engine: engine(t), Sink: lg, Tool: "snow", AgentID: "a1", RunID: "r1", Path: "/var/log/snow-audit.jsonl", OnWarn: warn}
}

func get() usecase.Action {
	return usecase.Action{Kind: usecase.Read, Request: policy.Request{Verb: "get", Resource: "table:incident"}}
}
func write() usecase.Action {
	return usecase.Action{Kind: usecase.Write, Request: policy.Request{Verb: "update", Resource: "table:incident"}}
}

func ok(_ context.Context, _ policy.Decision) (int, error) { return 200, nil }

func records(t *testing.T, buf *bytes.Buffer) []audit.Record {
	t.Helper()
	var out []audit.Record
	for _, l := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if l == "" {
			continue
		}
		var r audit.Record
		if err := r.UnmarshalJSON([]byte(l)); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func TestReadWritesOutcomeOnly(t *testing.T) {
	var buf bytes.Buffer
	g := newGuard(t, &buf, nil)
	if err := g.Run(context.Background(), get(), ok); err != nil {
		t.Fatal(err)
	}
	rs := records(t, &buf)
	if len(rs) != 1 || rs[0].Outcome != "ok" || rs[0].HTTPStatus != 200 || rs[0].PolicyDecision != "allow" {
		t.Errorf("records = %+v", rs)
	}
	if rs[0].Tool != "snow" || rs[0].AgentID != "a1" || rs[0].RunID != "r1" || rs[0].Verb != "get" || rs[0].Resource != "table:incident" {
		t.Errorf("record fields = %+v", rs[0])
	}
}

func TestWriteWritesPendingThenOutcome(t *testing.T) {
	var buf bytes.Buffer
	g := newGuard(t, &buf, nil)
	if err := g.Run(context.Background(), write(), ok); err != nil {
		t.Fatal(err)
	}
	rs := records(t, &buf)
	if len(rs) != 2 || rs[0].Outcome != OutcomePending || rs[1].Outcome != OutcomeOK {
		t.Errorf("records = %+v", rs)
	}
}

func TestBlockAbortsBeforeAction(t *testing.T) {
	g := newGuard(t, &failWriter{after: 0}, nil)
	called := false
	err := g.Run(context.Background(), write(), func(context.Context, policy.Decision) (int, error) { called = true; return 200, nil })
	if called {
		t.Fatal("action ran although the pending audit record could not be written")
	}
	if err == nil || output.ExitOf(err) != output.ExitGeneral {
		t.Fatalf("err = %v exit=%d, want general", err, output.ExitOf(err))
	}
	if !strings.Contains(err.Error(), "/var/log/snow-audit.jsonl") {
		t.Errorf("message must name the audit path: %v", err)
	}
	if !errors.Is(err, audit.ErrWrite) {
		t.Errorf("must wrap audit.ErrWrite: %v", err)
	}
}

func TestOutcomeFailureJoinedWithResult(t *testing.T) {
	g := newGuard(t, &failWriter{after: 1}, nil) // pending ok, outcome fails
	err := g.Run(context.Background(), write(), ok)
	if err == nil {
		t.Fatal("outcome write failure must surface")
	}
	if output.ExitOf(err) != output.ExitGeneral {
		t.Errorf("exit = %d", output.ExitOf(err))
	}
	if !strings.Contains(err.Error(), "may have") {
		t.Errorf("message must say the write may have happened: %v", err)
	}
	if !errors.Is(err, audit.ErrWrite) {
		t.Errorf("must wrap audit.ErrWrite: %v", err)
	}
}

func TestOutcomeFailureJoinsActionError(t *testing.T) {
	g := newGuard(t, &failWriter{after: 1}, nil)
	actionErr := errors.New("conflict happened")
	err := g.Run(context.Background(), write(), func(context.Context, policy.Decision) (int, error) { return 409, actionErr })
	if !errors.Is(err, actionErr) || !errors.Is(err, audit.ErrWrite) {
		t.Errorf("must join both: %v", err)
	}
}

func TestWarnModeNeverBlocksReads(t *testing.T) {
	var warned []error
	g := newGuard(t, &failWriter{after: 0}, func(e error) { warned = append(warned, e) })
	if err := g.Run(context.Background(), get(), ok); err != nil {
		t.Fatalf("read must succeed when audit fails in warn mode: %v", err)
	}
	if len(warned) != 1 {
		t.Errorf("warnings = %v", warned)
	}
	// nil OnWarn must not panic
	g2 := newGuard(t, &failWriter{after: 0}, nil)
	if err := g2.Run(context.Background(), get(), ok); err != nil {
		t.Fatal(err)
	}
}

func TestDenialIsAuditedAndSkipsAction(t *testing.T) {
	var buf bytes.Buffer
	g := newGuard(t, &buf, nil)
	called := false
	a := usecase.Action{Kind: usecase.Write, Request: policy.Request{Verb: "resolve", Resource: "incident"}}
	err := g.Run(context.Background(), a, func(context.Context, policy.Decision) (int, error) { called = true; return 200, nil })
	if called {
		t.Fatal("denied action ran")
	}
	if output.ExitOf(err) != output.ExitPolicyDenied {
		t.Errorf("exit = %d, want 6 (%v)", output.ExitOf(err), err)
	}
	rs := records(t, &buf)
	if len(rs) != 1 || rs[0].Outcome != OutcomeDenied || rs[0].PolicyDecision != "deny" {
		t.Errorf("records = %+v", rs)
	}
}

func TestDenialAuditFailureInBlockModeStillDenies(t *testing.T) {
	g := newGuard(t, &failWriter{after: 0}, nil)
	a := usecase.Action{Kind: usecase.Write, Request: policy.Request{Verb: "resolve", Resource: "incident"}}
	err := g.Run(context.Background(), a, ok)
	if output.ExitOf(err) != output.ExitPolicyDenied {
		t.Errorf("exit = %d (%v)", output.ExitOf(err), err)
	}
}

func TestDryRunOnlyPassesDecision(t *testing.T) {
	var buf bytes.Buffer
	g := newGuard(t, &buf, nil)
	var got policy.Decision
	a := usecase.Action{Kind: usecase.Write, Request: policy.Request{Verb: "order", Resource: "catalog:item:abc"}}
	err := g.Run(context.Background(), a, func(_ context.Context, d policy.Decision) (int, error) { got = d; return 0, nil })
	if err != nil {
		t.Fatal(err)
	}
	if !got.DryRunOnly() || got.Allowed {
		t.Errorf("decision = %+v", got)
	}
	rs := records(t, &buf)
	if len(rs) != 2 || rs[0].PolicyDecision != "dry_run_only" {
		t.Errorf("records = %+v", rs)
	}
}

func TestActionErrorOutcomeLabelAndStatus(t *testing.T) {
	var buf bytes.Buffer
	g := newGuard(t, &buf, nil)
	boom := statusErr{404}
	err := g.Run(context.Background(), get(), func(context.Context, policy.Decision) (int, error) { return 0, boom })
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	rs := records(t, &buf)
	if rs[0].Outcome != OutcomeError || rs[0].HTTPStatus != 404 {
		t.Errorf("record = %+v", rs[0])
	}
}

type statusErr struct{ s int }

func (e statusErr) Error() string   { return "status" }
func (e statusErr) HTTPStatus() int { return e.s }

func TestNilEngineDenies(t *testing.T) {
	var buf bytes.Buffer
	g := &Guard{Sink: audit.NewLogger(&buf), Tool: "snow"}
	err := g.Run(context.Background(), get(), ok)
	if output.ExitOf(err) != output.ExitPolicyDenied {
		t.Errorf("exit = %d", output.ExitOf(err))
	}
}

func probe() usecase.Action {
	return usecase.Action{Kind: usecase.Write, Request: policy.Request{Verb: "selftest:probe-resolve", Resource: "incident"}}
}

func TestRunProbeSkipsPolicyAndAuditsPendingThenOutcome(t *testing.T) {
	var buf bytes.Buffer
	g := newGuard(t, &buf, nil) // allowAll has no rule for the probe verb: Run would deny
	called := false
	err := g.RunProbe(context.Background(), probe(), func(context.Context, policy.Decision) (int, error) { called = true; return 200, nil })
	if err != nil || !called {
		t.Fatalf("err=%v called=%v", err, called)
	}
	rs := records(t, &buf)
	if len(rs) != 2 || rs[0].Outcome != OutcomePending || rs[1].Outcome != OutcomeOK {
		t.Fatalf("records = %+v", rs)
	}
	if rs[0].Verb != "selftest:probe-resolve" || rs[0].PolicyDecision != DecisionProbeBypass || rs[1].HTTPStatus != 200 {
		t.Errorf("record = %+v", rs[0])
	}
}

func TestRunProbeReadKindIsStillBlockMode(t *testing.T) {
	g := newGuard(t, &failWriter{after: 0}, nil)
	called := false
	a := probe()
	a.Kind = usecase.Read
	err := g.RunProbe(context.Background(), a, func(context.Context, policy.Decision) (int, error) { called = true; return 200, nil })
	if called {
		t.Fatal("probe request ran although its pending audit record failed")
	}
	var af interface{ AuditFailed() }
	if !errors.As(err, &af) {
		t.Fatalf("want an audit failure, got %v", err)
	}
}

func TestRunProbeActionErrorIsAuditedAsError(t *testing.T) {
	var buf bytes.Buffer
	g := newGuard(t, &buf, nil)
	boom := statusErr{403}
	err := g.RunProbe(context.Background(), probe(), func(context.Context, policy.Decision) (int, error) { return 0, boom })
	if !errors.Is(err, boom) {
		t.Fatal(err)
	}
	rs := records(t, &buf)
	if rs[1].Outcome != OutcomeError || rs[1].HTTPStatus != 403 {
		t.Errorf("records = %+v", rs)
	}
}

type stubLimiter struct {
	d   policy.Decision
	err error
	n   int
}

func (s *stubLimiter) Admit(_ *policy.Policy, d policy.Decision) (policy.Decision, error) {
	s.n++
	if s.err != nil || !s.d.Allowed && s.d.Mode == policy.ModeDeny {
		return s.d, s.err
	}
	return d, nil
}

func TestLimiterDenialIsAuditedAsDeniedAndSkipsAction(t *testing.T) {
	var buf bytes.Buffer
	g := newGuard(t, &buf, nil)
	g.Limiter = &stubLimiter{d: policy.Decision{Mode: policy.ModeDeny, RuleID: "allow-get", Reason: "hourly rate limit reached across invocations; retry in 5m0s"}}
	called := false
	err := g.Run(context.Background(), write(), func(context.Context, policy.Decision) (int, error) { called = true; return 200, nil })
	if called || output.ExitOf(err) != output.ExitPolicyDenied {
		t.Fatalf("called=%v err=%v", called, err)
	}
	rs := records(t, &buf)
	if len(rs) != 1 || rs[0].Outcome != OutcomeDenied || rs[0].PolicyDecision != "deny" {
		t.Errorf("records = %+v", rs)
	}
}

func TestLimiterStateErrorRefusesTheRequest(t *testing.T) {
	var buf bytes.Buffer
	g := newGuard(t, &buf, nil)
	g.Limiter = &stubLimiter{err: errors.New("state unavailable")}
	called := false
	err := g.Run(context.Background(), write(), func(context.Context, policy.Decision) (int, error) { called = true; return 200, nil })
	if called || err == nil || output.ExitOf(err) != output.ExitGeneral {
		t.Fatalf("called=%v err=%v", called, err)
	}
	if buf.Len() != 0 {
		t.Errorf("no audit record is expected before the request is admitted: %s", buf.String())
	}
}

func TestLimiterNotConsultedForDeniedOrProbe(t *testing.T) {
	var buf bytes.Buffer
	g := newGuard(t, &buf, nil)
	l := &stubLimiter{}
	g.Limiter = l
	a := usecase.Action{Kind: usecase.Write, Request: policy.Request{Verb: "resolve", Resource: "incident"}}
	_ = g.Run(context.Background(), a, ok) // denied by policy
	_ = g.RunProbe(context.Background(), probe(), ok)
	if l.n != 0 {
		t.Errorf("limiter consulted %d times", l.n)
	}
	_ = g.Run(context.Background(), get(), ok)
	if l.n != 1 {
		t.Errorf("limiter must see allowed requests, n=%d", l.n)
	}
}
