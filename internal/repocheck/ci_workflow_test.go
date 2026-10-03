package repocheck

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// FR-054 / BLD-1..6: the CI workflow keeps its required shape.
func TestCIWorkflowShape(t *testing.T) {
	b, err := os.ReadFile("../../.github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		"contents: read", "packages: read", "GOPRIVATE: github.com/stainedhead/*",
		"x-access-token:${GH_TOKEN}@github.com/", "go mod tidy", "gofmt -l .", "go vet ./...",
		"golangci-lint-action", "GOLANGCI_LINT_VERSION: v", "go test -race ./...",
		"govulncheck@${{ env.GOVULNCHECK_VERSION }}", "GOVULNCHECK_VERSION: v",
		"make skill", "skill.golden.md", "pull_request:", "workflow_dispatch:",
		"darwin", "arm64", "linux", "amd64", "go-version-file: go.mod",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("ci.yml lacks %q", want)
		}
	}
	// No secrets other than the job token; no windows target.
	for _, m := range regexp.MustCompile(`secrets\.([A-Za-z0-9_]+)`).FindAllStringSubmatch(s, -1) {
		if m[1] != "GITHUB_TOKEN" {
			t.Errorf("ci.yml uses secret %s", m[1])
		}
	}
	if strings.Contains(s, "windows") || strings.Contains(s, "write") && strings.Contains(s, ": write") {
		t.Error("ci.yml must not build windows or request write permissions")
	}
	// Every third-party action is pinned to a version.
	for _, m := range regexp.MustCompile(`uses:\s*(\S+)`).FindAllStringSubmatch(s, -1) {
		if !regexp.MustCompile(`@v\d+(\.\d+)*$|@[0-9a-f]{40}$`).MatchString(m[1]) {
			t.Errorf("action %q is not pinned to a version", m[1])
		}
	}
}
