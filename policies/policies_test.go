package policies_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stainedhead/agent-cli-core/policy"
	"github.com/stainedhead/snow-cli/internal/policymap"
	"github.com/stainedhead/snow-cli/policies"
)

func mustParse(t *testing.T, name string) *policy.Policy {
	t.Helper()
	data, err := policies.Named(name)
	if err != nil {
		t.Fatalf("Named(%q): %v", name, err)
	}
	p, err := policy.Parse(data)
	if err != nil {
		t.Fatalf("policy.Parse(%s): %v", name, err)
	}
	return p
}

// FR-052: each shipped file parses strictly and loads through policy.Load.
func TestShippedPoliciesParseAndLoad(t *testing.T) {
	for _, name := range []string{"agent", "human"} {
		mustParse(t, name)
		path := name + ".policy.yaml"
		if _, err := policy.Load(path, policy.WithWritable(policy.WritableIgnore)); err != nil {
			t.Errorf("policy.Load(%s): %v", path, err)
		}
	}
}

func TestEmbeddedMatchesFiles(t *testing.T) {
	for _, name := range []string{"agent", "human"} {
		disk, err := os.ReadFile(filepath.Join(name + ".policy.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		emb, _ := policies.Named(name)
		if string(disk) != string(emb) {
			t.Errorf("%s: embedded bytes differ from file", name)
		}
	}
}

func TestNamedUnknown(t *testing.T) {
	for _, n := range []string{"", "root", "../agent", "agent.policy.yaml"} {
		if _, err := policies.Named(n); err == nil {
			t.Errorf("Named(%q) should fail", n)
		}
	}
}

// Strict schema: an unknown key in either shipped file must fail Parse.
func TestUnknownKeysRejected(t *testing.T) {
	for _, name := range []string{"agent", "human"} {
		data, _ := policies.Named(name)
		bad := append([]byte("signature: abc\n"), data...)
		if _, err := policy.Parse(bad); err == nil {
			t.Errorf("%s with unknown top-level key parsed", name)
		}
		bad = []byte(strings.Replace(string(data), "effect: allow", "effect: allow\n    surprise: 1", 1))
		if _, err := policy.Parse(bad); err == nil {
			t.Errorf("%s with unknown rule key parsed", name)
		}
	}
}

func TestEveryRuleUsesVocabulary(t *testing.T) {
	for _, name := range []string{"agent", "human"} {
		p := mustParse(t, name)
		for _, r := range p.Rules {
			for _, v := range r.Verbs {
				if v != "*" && !policymap.IsVerb(v) {
					t.Errorf("%s/%s: verb %q not in vocabulary", name, r.ID, v)
				}
			}
			for _, res := range r.Resources {
				if !policymap.IsResource(res) && !strings.Contains(res, "*") {
					t.Errorf("%s/%s: resource %q not in vocabulary", name, r.ID, res)
				}
			}
		}
	}
}

func decide(p *policy.Policy, verb, res string, vals map[string]any) policy.Decision {
	req := policymap.NewRequest(verb, res)
	if vals != nil {
		req = policymap.WithValues(req, vals)
	}
	return p.Evaluate(req)
}

// D-d (A-01): impact and urgency 1 (High) denied for agents, 2 and 3 allowed.
func TestAgentPolicyAssumptionImpactUrgencyScale(t *testing.T) {
	p := mustParse(t, "agent")
	for _, f := range []string{"impact", "urgency"} {
		for v, want := range map[int]bool{1: false, 2: true, 3: true} {
			vals := map[string]any{"short_description": "x", f: v}
			if got := decide(p, "create", "incident", vals).Allowed; got != want {
				t.Errorf("create %s=%d allowed=%v want %v", f, v, got, want)
			}
			if got := decide(p, "update", "incident", map[string]any{f: v}).Allowed; got != want {
				t.Errorf("update %s=%d allowed=%v want %v", f, v, got, want)
			}
		}
	}
	// priority is never writable
	if decide(p, "create", "incident", map[string]any{"priority": 1}).Allowed {
		t.Error("priority must not be writable")
	}
}

func TestHumanPolicyAllowsEscalation(t *testing.T) {
	p := mustParse(t, "human")
	if !decide(p, "create", "incident", map[string]any{"impact": 1, "urgency": 1}).Allowed {
		t.Error("human may set impact/urgency 1")
	}
	if decide(p, "create", "incident", map[string]any{"impact": 4}).Allowed {
		t.Error("impact 4 is outside the scale")
	}
}

func TestResolveAgentDeniedHumanAllowed(t *testing.T) {
	vals := map[string]any{"close_code": "Solved", "close_notes": "done"}
	if d := decide(mustParse(t, "agent"), "resolve", "incident", vals); d.Allowed || d.Mode != policy.ModeDeny {
		t.Errorf("agent resolve = %+v", d)
	}
	if d := decide(mustParse(t, "human"), "resolve", "incident", vals); !d.Allowed {
		t.Errorf("human resolve = %+v", d)
	}
}

func TestSysTablesDeniedForBoth(t *testing.T) {
	for _, name := range []string{"agent", "human"} {
		p := mustParse(t, name)
		for _, tbl := range []string{"sys_user", "sys_user_has_role", "sys_properties", "sysapproval_approver", "sys_db_object"} {
			if d := decide(p, "list", "table:"+tbl, nil); d.Allowed {
				t.Errorf("%s: table:%s allowed", name, tbl)
			}
		}
	}
}

func TestAgentTableAndFieldAllowlists(t *testing.T) {
	p := mustParse(t, "agent")
	read := func(res string, fields ...string) policy.Decision {
		return p.Evaluate(policymap.WithFields(policymap.NewRequest("list", res), fields...))
	}
	if !read("table:incident", "number", "state").Allowed {
		t.Error("incident number,state should be allowed")
	}
	if read("table:incident", "number", "u_secret").Allowed {
		t.Error("field outside allowlist must be denied")
	}
	if read("table:u_custom", "number").Allowed {
		t.Error("table outside allowlist must be denied")
	}
	if !read("table:cmdb_ci_server", "name").Allowed {
		t.Error("cmdb_ci child class should be allowed")
	}
	if !read("table:cmdb_rel_ci", "parent", "child").Allowed {
		t.Error("cmdb_rel_ci should be allowed")
	}
	if !p.Evaluate(policymap.NewRequest("get", "table:sc_req_item")).Allowed {
		t.Error("sc_req_item get should be allowed")
	}
}

func TestAgentWritesRestricted(t *testing.T) {
	p := mustParse(t, "agent")
	if decide(p, "update", "incident", map[string]any{"state": 6}).Allowed {
		t.Error("agent may not set incident state")
	}
	if !decide(p, "update", "incident", map[string]any{"work_notes": "n"}).Allowed {
		t.Error("work_notes update should be allowed")
	}
	if !decide(p, "update", "task", map[string]any{"state": 3, "work_notes": "n"}).Allowed {
		t.Error("task update of state/work_notes should be allowed")
	}
	if decide(p, "update", "task", map[string]any{"short_description": "n"}).Allowed {
		t.Error("task short_description is not updatable")
	}
	if decide(p, "create", "change", nil).Allowed {
		t.Error("change create is not built nor allowed")
	}
}

func TestAgentCatalogOrderDryRunOnlyAndOptIn(t *testing.T) {
	p := mustParse(t, "agent")
	d := decide(p, "order", "catalog:item:abc123", map[string]any{"v": "1"})
	if d.Allowed || !d.DryRunOnly() {
		t.Errorf("order = %+v want dry_run_only", d)
	}
	// An opt-in rule placed before the default makes the item live.
	data, _ := policies.Named("agent")
	optIn := strings.Replace(string(data), "  - id: catalog-order-dry-run",
		"  - id: order-abc\n    effect: allow\n    mode: allow\n    verbs: [order]\n    resources: [\"catalog:item:abc123\"]\n  - id: catalog-order-dry-run", 1)
	p2, err := policy.Parse([]byte(optIn))
	if err != nil {
		t.Fatal(err)
	}
	if !decide(p2, "order", "catalog:item:abc123", nil).Allowed {
		t.Error("opted-in item should be allowed")
	}
	if !decide(p2, "order", "catalog:item:other", nil).DryRunOnly() {
		t.Error("other items stay dry-run-only")
	}
}

func TestAgentRateLimits(t *testing.T) {
	p := mustParse(t, "agent")
	e := policy.NewEngine(p, nil)
	vals := map[string]any{"impact": 2, "urgency": 2}
	allowed := 0
	for i := 0; i < 8; i++ {
		d := e.Check(policymap.WithValues(policymap.NewRequest("create", "incident"), vals))
		if d.Allowed {
			allowed++
		}
	}
	if allowed != 5 {
		t.Errorf("allowed %d creates, want 5 per hour", allowed)
	}
	if p.Limits.MaxResults != 200 || p.Limits.MaxBytes != 32768 {
		t.Errorf("limits = %+v", p.Limits)
	}
	for _, id := range []string{"incident-update", "task-update", "catalog-order-dry-run"} {
		found := false
		for _, r := range p.Rules {
			if r.ID == id {
				found = true
				if r.RateLimit.PerRun != 10 {
					t.Errorf("%s per_run = %d, want 10", id, r.RateLimit.PerRun)
				}
			}
		}
		if !found {
			t.Errorf("rule %s missing", id)
		}
	}
}

func TestIdentityVerbsAllowed(t *testing.T) {
	for _, name := range []string{"agent", "human"} {
		p := mustParse(t, name)
		if !decide(p, "whoami", "whoami", nil).Allowed || !decide(p, "selftest", "selftest", nil).Allowed {
			t.Errorf("%s: whoami/selftest must be allowed", name)
		}
	}
}
