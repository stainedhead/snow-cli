package app

import (
	"strings"
	"testing"

	"github.com/stainedhead/snow-cli/policies"
)

func shippedAgentWithout(t *testing.T, ruleID string) string {
	t.Helper()
	b, err := policies.Named("agent")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	idx := strings.Index(s, "  - id: "+ruleID)
	if idx < 0 {
		t.Fatalf("rule %s not found", ruleID)
	}
	end := strings.Index(s[idx+4:], "\n  - id: ")
	if end < 0 {
		t.Fatal("rule is last")
	}
	return s[:idx] + s[idx+4+end+1:]
}

// FR-R03: catalog order reads the item through the guarded read service.
func TestOrderWithPolicyDenyingCatalogVarsExits6WithNoHTTP(t *testing.T) {
	i := newItg(t, "agent", withPolicyText(shippedAgentWithout(t, "catalog-read")))
	catalogFixtures(i.f)
	code, _ := i.run("catalog", "order", itItem, "--var", "model=x1", "--dry-run")
	if code != 6 {
		t.Fatalf("exit %d, want 6: %s", code, i.out.String())
	}
	if n := len(i.f.Requests()); n != 0 {
		t.Errorf("%d requests sent although vars is denied", n)
	}
	ls := i.auditLines()
	if len(ls) == 0 || ls[len(ls)-1]["outcome"] != "denied" || ls[len(ls)-1]["verb"] != "vars" {
		t.Errorf("the denied vars read must be audited: %v", ls)
	}
}

func TestOrderAuditShowsTheGuardedReadsBeforeTheOrder(t *testing.T) {
	i := newItg(t, "agent")
	catalogFixtures(i.f)
	if code, _ := i.run("catalog", "order", itItem, "--var", "model=x1"); code != 0 {
		t.Fatalf("exit %d: %s", code, i.out.String())
	}
	var verbs []string
	for _, l := range i.auditLines() {
		verbs = append(verbs, l["verb"].(string)+":"+l["outcome"].(string))
	}
	got := strings.Join(verbs, ",")
	if !strings.HasPrefix(got, "vars:ok,order:pending,order:ok") {
		t.Errorf("audit sequence = %s, want the vars read before the order", got)
	}
}
