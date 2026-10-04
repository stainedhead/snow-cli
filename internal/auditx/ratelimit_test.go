package auditx

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/agent-cli-core/policy"
)

const limitedPolicy = `
version: 1
rules:
  - id: create
    effect: allow
    verbs: [create]
    resources: [incident]
    rate_limit: { per_hour: 3, per_run: 2 }
  - id: unlimited
    effect: allow
    verbs: [get]
    resources: ["table:*"]
`

func limPolicy(t *testing.T) *policy.Policy {
	t.Helper()
	p, err := policy.Parse([]byte(limitedPolicy))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func allowedFor(t *testing.T, p *policy.Policy, verb, res string) policy.Decision {
	t.Helper()
	d := p.Evaluate(policy.Request{Verb: verb, Resource: res})
	if !d.Allowed {
		t.Fatalf("setup: %+v", d)
	}
	return d
}

func newLimiter(path, agent, run string, now *time.Time) *StateLimiter {
	return &StateLimiter{Path: path, AgentID: agent, RunID: run, Now: func() time.Time { return *now }}
}

func TestStateLimiterPerRunAcrossInstances(t *testing.T) {
	p := limPolicy(t)
	path := filepath.Join(t.TempDir(), "state", "rl.json")
	now := time.Unix(1_700_000_000, 0)
	d := allowedFor(t, p, "create", "incident")
	for i := 0; i < 2; i++ { // each iteration is a new "process"
		got, err := newLimiter(path, "a1", "run-1", &now).Admit(p, d)
		if err != nil || !got.Allowed {
			t.Fatalf("invocation %d: %+v %v", i+1, got, err)
		}
	}
	got, err := newLimiter(path, "a1", "run-1", &now).Admit(p, d)
	if err != nil || got.Allowed || got.Mode != policy.ModeDeny {
		t.Fatalf("N+1 must be denied: %+v %v", got, err)
	}
	if got.RuleID != "create" || !strings.Contains(got.Reason, "per-run") {
		t.Errorf("decision = %+v", got)
	}
	// A different run id has its own per-run budget.
	if got, _ := newLimiter(path, "a1", "run-2", &now).Admit(p, d); !got.Allowed {
		t.Errorf("new run id must start fresh: %+v", got)
	}
}

func TestStateLimiterPerHourSlidesAndCarriesRetryAfter(t *testing.T) {
	p := limPolicy(t)
	path := filepath.Join(t.TempDir(), "rl.json")
	now := time.Unix(1_700_000_000, 0)
	d := allowedFor(t, p, "create", "incident")
	for i := 0; i < 3; i++ {
		now = now.Add(10 * time.Minute)
		if got, err := newLimiter(path, "a1", "run-"+string(rune('a'+i)), &now).Admit(p, d); err != nil || !got.Allowed {
			t.Fatalf("hit %d: %+v %v", i, got, err)
		}
	}
	got, err := newLimiter(path, "a1", "run-z", &now).Admit(p, d)
	if err != nil || got.Allowed {
		t.Fatalf("4th in the hour must be denied: %+v %v", got, err)
	}
	if got.RetryAfter <= 0 || got.RetryAfter > time.Hour || !strings.Contains(got.Reason, "retry") {
		t.Errorf("retry hint missing: %+v", got)
	}
	now = now.Add(got.RetryAfter + time.Second)
	if got, _ := newLimiter(path, "a1", "run-y", &now).Admit(p, d); !got.Allowed {
		t.Errorf("must admit after the window slides: %+v", got)
	}
	// Another agent is independent.
	if got, _ := newLimiter(path, "other", "r", &now).Admit(p, d); !got.Allowed {
		t.Errorf("agents must not share budget: %+v", got)
	}
}

func TestStateLimiterIgnoresUnlimitedRulesAndNonAllow(t *testing.T) {
	p := limPolicy(t)
	path := filepath.Join(t.TempDir(), "rl.json")
	now := time.Unix(1_700_000_000, 0)
	l := newLimiter(path, "a", "r", &now)
	d := allowedFor(t, p, "get", "table:incident")
	for i := 0; i < 20; i++ {
		if got, err := l.Admit(p, d); err != nil || !got.Allowed {
			t.Fatal(got, err)
		}
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("an unlimited rule must not touch the state file")
	}
	deny := policy.Decision{Mode: policy.ModeDeny, RuleID: "x"}
	if got, _ := l.Admit(p, deny); got.Allowed || got.RuleID != "x" {
		t.Errorf("non-allow decisions pass through: %+v", got)
	}
}

func TestStateLimiterGlobalRateLimit(t *testing.T) {
	p, err := policy.Parse([]byte("version: 1\nrate_limit: { per_run: 1 }\nrules:\n  - {id: r, effect: allow, verbs: [get], resources: [\"*\"]}\n"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "rl.json")
	now := time.Unix(1_700_000_000, 0)
	d := allowedFor(t, p, "get", "x")
	if got, _ := newLimiter(path, "a", "r", &now).Admit(p, d); !got.Allowed {
		t.Fatal("first")
	}
	if got, _ := newLimiter(path, "a", "r", &now).Admit(p, d); got.Allowed || !strings.Contains(got.Reason, "policy") {
		t.Errorf("global limit: %+v", got)
	}
}

func TestStateLimiterConcurrentProcessesNeverExceedTheLimit(t *testing.T) {
	p := limPolicy(t)
	path := filepath.Join(t.TempDir(), "rl.json")
	now := time.Unix(1_700_000_000, 0)
	d := allowedFor(t, p, "create", "incident")
	var admitted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			l := &StateLimiter{Path: path, AgentID: "a", RunID: "run-" + string(rune('a'+i)), Now: func() time.Time { return now }}
			if got, err := l.Admit(p, d); err == nil && got.Allowed {
				admitted.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if admitted.Load() != 3 { // per_hour: 3 across 12 distinct runs
		t.Errorf("admitted %d, want exactly 3", admitted.Load())
	}
}

func TestStateLimiterFailsClosedOnCorruptOrUnwritableState(t *testing.T) {
	p := limPolicy(t)
	dir := t.TempDir()
	now := time.Unix(1_700_000_000, 0)
	d := allowedFor(t, p, "create", "incident")

	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := newLimiter(bad, "a", "r", &now).Admit(p, d)
	if err == nil || output.ExitOf(err) != output.ExitGeneral || !strings.Contains(err.Error(), bad) {
		t.Errorf("corrupt state must fail closed naming the path: %v", err)
	}

	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = newLimiter(filepath.Join(blocker, "sub", "rl.json"), "a", "r", &now).Admit(p, d)
	if err == nil {
		t.Error("unwritable state location must fail closed")
	}
}

func TestStateLimiterPrunesStaleRunEntries(t *testing.T) {
	p := limPolicy(t)
	path := filepath.Join(t.TempDir(), "rl.json")
	now := time.Unix(1_700_000_000, 0)
	d := allowedFor(t, p, "create", "incident")
	if _, err := newLimiter(path, "a", "old-run", &now).Admit(p, d); err != nil {
		t.Fatal(err)
	}
	now = now.Add(10 * 24 * time.Hour)
	if _, err := newLimiter(path, "a", "new-run", &now).Admit(p, d); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if strings.Contains(string(b), "old-run") {
		t.Errorf("stale run entry kept: %s", b)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("state file mode = %v err %v", fi, err)
	}
}
