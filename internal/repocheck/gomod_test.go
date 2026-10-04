package repocheck

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func readGoMod(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("../../go.mod")
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	return string(b)
}

func TestGoModModuleAndGoVersion(t *testing.T) {
	mod := readGoMod(t)
	if !strings.Contains(mod, "module github.com/stainedhead/snow-cli\n") {
		t.Errorf("go.mod has wrong module path:\n%s", mod)
	}
	if !regexp.MustCompile(`(?m)^go 1\.27(\.\d+)?$`).MatchString(mod) {
		t.Errorf("go.mod must declare go 1.27:\n%s", mod)
	}
}

func TestGoModRequiresCoreV021(t *testing.T) {
	mod := readGoMod(t)
	if !regexp.MustCompile(`github\.com/stainedhead/agent-cli-core v0\.2\.1\b`).MatchString(mod) {
		t.Errorf("go.mod must require agent-cli-core v0.2.1:\n%s", mod)
	}
}

func TestGoModNoReplaceNoPseudoVersion(t *testing.T) {
	mod := readGoMod(t)
	if regexp.MustCompile(`(?m)^\s*replace\b`).MatchString(mod) {
		t.Error("go.mod must not contain replace directives")
	}
	if regexp.MustCompile(`v\d+\.\d+\.\d+-(\d+\.)?\d{14}-[0-9a-f]{12}`).MatchString(mod) {
		t.Error("go.mod must not contain pseudo-versions")
	}
}
