package app

import (
	"os"
	"strings"
	"testing"
)

func limitedWhoamiPolicy(rate string) string {
	return "version: 1\nrules:\n  - id: who\n    effect: allow\n    verbs: [whoami]\n    resources: [whoami]\n    rate_limit: " + rate + "\n"
}

// FR-R02: each invocation builds a fresh engine; the limits must still hold
// across invocations.
func TestRateLimitPerRunHoldsAcrossInvocations(t *testing.T) {
	i := newItg(t, "agent", withPolicyText(limitedWhoamiPolicy("{ per_run: 2 }")))
	i.f.On("GET", "/api/x_corp_agent/v1/whoami", okWhoami())
	for n := 1; n <= 2; n++ {
		if code, _ := i.run("whoami"); code != 0 {
			t.Fatalf("invocation %d: exit %d: %s", n, code, i.out.String())
		}
	}
	before := len(i.f.Requests())
	code, m := i.run("whoami")
	if code != 6 {
		t.Fatalf("invocation 3 (N+1): exit %d, want 6: %s", code, i.out.String())
	}
	if !strings.Contains(errMsg(m), "per-run rate limit") {
		t.Errorf("message = %q", errMsg(m))
	}
	if len(i.f.Requests()) != before {
		t.Error("the denied invocation sent a request")
	}
	// The denial is audited as denied.
	ls := i.auditLines()
	if last := ls[len(ls)-1]; last["outcome"] != "denied" {
		t.Errorf("last audit line = %v", last)
	}
	// A new run id starts a new per-run budget.
	i.opts.Getenv = func(k string) string {
		if k == "SNOW_RUN_ID" {
			return "run-2"
		}
		return ""
	}
	if code, _ := i.run("whoami"); code != 0 {
		t.Errorf("new run id: exit %d", code)
	}
}

func TestRateLimitPerHourHoldsAcrossRunsWithRetryHint(t *testing.T) {
	i := newItg(t, "agent", withPolicyText(limitedWhoamiPolicy("{ per_hour: 2 }")))
	i.f.On("GET", "/api/x_corp_agent/v1/whoami", okWhoami())
	for n := 1; n <= 3; n++ {
		run := "run-" + string(rune('a'+n))
		i.opts.Getenv = func(k string) string {
			if k == "SNOW_RUN_ID" {
				return run
			}
			return ""
		}
		code, m := i.run("whoami")
		if n < 3 && code != 0 {
			t.Fatalf("invocation %d: exit %d", n, code)
		}
		if n == 3 && (code != 6 || !strings.Contains(errMsg(m), "retry in")) {
			t.Fatalf("N+1: exit %d msg %q", code, errMsg(m))
		}
	}
}

func TestRateLimitStateFailureRefusesLimitedAction(t *testing.T) {
	i := newItg(t, "agent", withPolicyText(limitedWhoamiPolicy("{ per_run: 5 }")))
	i.f.On("GET", "/api/x_corp_agent/v1/whoami", okWhoami())
	if code, _ := i.run("whoami"); code != 0 {
		t.Fatal(code)
	}
	if err := writeFile(rateStatePath(i.audit), "{broken"); err != nil {
		t.Fatal(err)
	}
	n := len(i.f.Requests())
	if code, _ := i.run("whoami"); code != 1 || len(i.f.Requests()) != n {
		t.Errorf("corrupt state: exit %d, requests %d->%d", code, n, len(i.f.Requests()))
	}
}

func writeFile(p, s string) error { return os.WriteFile(p, []byte(s), 0o600) }
