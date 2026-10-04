package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// FR-R06: --policy cannot widen the profile's policy in agent mode.

func TestPolicyFlagHumanOnAgentProfileIsDeniedWithNoHTTP(t *testing.T) {
	i := newItg(t, "agent")
	code, m := i.run("incident", "resolve", "INC0010001", "--close-code", "x", "--close-notes", "y", "--yes", "--policy", "human")
	if code != 6 {
		t.Fatalf("exit = %d, want 6: %s", code, i.out.String())
	}
	if !strings.Contains(errMsg(m), "allow_override") {
		t.Errorf("message should name policy.allow_override: %q", errMsg(m))
	}
	if n := len(i.f.Requests()); n != 0 {
		t.Errorf("%d requests sent despite refused --policy", n)
	}
}

func TestPolicyFlagFileOnAgentProfileIsDenied(t *testing.T) {
	i := newItg(t, "agent")
	p := filepath.Join(i.dir, "wide.yaml")
	if err := os.WriteFile(p, []byte("version: 1\nrules:\n  - id: all\n    effect: allow\n    verbs: [\"*\"]\n    resources: [\"*\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _ := i.run("whoami", "--policy", p); code != 6 {
		t.Errorf("exit = %d, want 6", code)
	}
	if n := len(i.f.Requests()); n != 0 {
		t.Errorf("%d requests", n)
	}
}

func TestPolicyFlagSameAsConfiguredIsNotAnOverride(t *testing.T) {
	i := newItg(t, "agent")
	i.f.On("GET", "/api/x_corp_agent/v1/whoami", okWhoami())
	if code, _ := i.run("whoami", "--policy", "agent"); code != 0 {
		t.Errorf("--policy equal to configured policy exit = %d", code)
	}
}

func TestPolicyAllowOverrideKeyPermitsMatchingNamedPolicy(t *testing.T) {
	i := newItg(t, "agent", withPolicyOverride())
	i.f.On("GET", "/api/x_corp_agent/v1/whoami", okWhoami())
	if code, _ := i.run("whoami", "--policy", "agent"); code != 0 {
		t.Errorf("override allowed, matching named policy: exit %d: %s", code, i.out.String())
	}
}

func TestPolicyNamedMustMatchModeByDefaultInHumanMode(t *testing.T) {
	i := newItg(t, "human")
	if code, _ := i.run("whoami", "--policy", "agent"); code != 6 {
		t.Errorf("human profile selecting the agent policy by name: exit %d, want 6 (must match mode)", code)
	}
}

func TestPolicyAllowOverrideLiftsPinForOtherNamedPolicy(t *testing.T) {
	i := newItg(t, "agent", withPolicyOverride())
	i.f.On("GET", "/api/x_corp_agent/v1/whoami", okWhoami())
	if code, _ := i.run("whoami", "--policy", "human"); code != 0 {
		t.Errorf("explicit allow_override: exit %d: %s", code, i.out.String())
	}
}
