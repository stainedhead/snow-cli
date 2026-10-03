package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stainedhead/agent-cli-core/docgen"
	"github.com/stainedhead/snow-cli/internal/cli"
)

func skillHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	cli.RegisterAll(h.r)
	return h
}

func generated(t *testing.T, h *harness) []byte {
	t.Helper()
	b, err := docgen.Generate(h.r.CommandTree("snow", cli.SkillDescription))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// FR-051: the skill document is derived from the command tree and pinned by a
// golden file. Regenerate with: go test ./internal/cli -run TestSkillGolden -update
func TestSkillGolden(t *testing.T) {
	h := skillHarness(t)
	golden(t, "skill.golden.md", string(generated(t, h)))
}

func TestSkillGenerateWritesFile(t *testing.T) {
	h := skillHarness(t)
	out := filepath.Join(t.TempDir(), "nested", "snow-cli.md")
	if code := h.run("skill", "generate", "--out", out); code != 0 {
		t.Fatalf("exit %d: %s", code, h.out.String())
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(generated(t, h)) {
		t.Error("file differs from docgen output")
	}
	s := string(got)
	for _, want := range []string{"### skill generate", "### whoami", "## Exit codes", "## Untrusted content", "Do not try to obtain or print the access token"} {
		if !strings.Contains(s, want) {
			t.Errorf("skill lacks %q", want)
		}
	}
	env := decode(t, h)
	data := env["data"].(map[string]any)
	if data["path"] != out {
		t.Errorf("data = %v", data)
	}
}

func TestSkillGenerateDefaultsToDist(t *testing.T) {
	h := skillHarness(t)
	wd, _ := os.Getwd()
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(wd) }()
	if code := h.run("skill", "generate"); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if _, err := os.Stat(filepath.Join(dir, "dist", "snow-cli.md")); err != nil {
		t.Errorf("default output missing: %v", err)
	}
}

func TestSkillGenerateIsDeterministic(t *testing.T) {
	dir := t.TempDir()
	var prev []byte
	for i := 0; i < 2; i++ {
		h := skillHarness(t)
		p := filepath.Join(dir, "a.md")
		if code := h.run("skill", "generate", "--out", p); code != 0 {
			t.Fatal(code)
		}
		b, _ := os.ReadFile(p)
		if prev != nil && string(prev) != string(b) {
			t.Error("two runs differ")
		}
		prev = b
	}
}

func TestSkillCheckDetectsDrift(t *testing.T) {
	h := skillHarness(t)
	p := filepath.Join(t.TempDir(), "s.md")
	if code := h.run("skill", "generate", "--out", p); code != 0 {
		t.Fatal(code)
	}
	if code := h.run("skill", "generate", "--check", p); code != 0 {
		t.Errorf("fresh file reported as drifted: exit %d %s", code, h.out.String())
	}
	if err := os.WriteFile(p, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := h.run("skill", "generate", "--check", p); code != 1 {
		t.Errorf("drift exit = %d, want 1", code)
	}
	if !strings.Contains(h.out.String(), "make skill") {
		t.Errorf("drift hint missing: %s", h.out.String())
	}
	if code := h.run("skill", "generate", "--check", filepath.Join(t.TempDir(), "missing.md")); code != 1 {
		t.Errorf("missing file exit = %d", code)
	}
}

func TestSkillGenerateBadOutPath(t *testing.T) {
	h := skillHarness(t)
	f := filepath.Join(t.TempDir(), "file")
	_ = os.WriteFile(f, nil, 0o644)
	if code := h.run("skill", "generate", "--out", filepath.Join(f, "x.md")); code == 0 {
		t.Error("writing under a regular file must fail")
	}
}

// The skill must list a forbidden-actions line for the identity command and
// must not advertise a token or raw-passthrough command (FR-053).
func TestSkillHasNoTokenOrPassthroughCommand(t *testing.T) {
	h := skillHarness(t)
	s := string(generated(t, h))
	for _, bad := range []string{"### token", "### print-token", "### raw", "### api"} {
		if strings.Contains(s, bad) {
			t.Errorf("skill advertises %q", bad)
		}
	}
}
