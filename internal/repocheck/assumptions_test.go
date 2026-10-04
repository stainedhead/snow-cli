package repocheck

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Every A-xx row of docs/assumptions.md names at least one test, each named
// test exists and contains "Assumption" (I3 gate; PRD warning-sign items).
func TestAssumptionRegisterMatchesTests(t *testing.T) {
	root := filepath.Join("..", "..")
	reg, err := os.ReadFile(filepath.Join(root, "docs", "assumptions.md"))
	if err != nil {
		t.Fatal(err)
	}
	var src strings.Builder
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.HasSuffix(p, "_test.go") {
			b, _ := os.ReadFile(p)
			src.Write(b)
		}
		return nil
	})
	row := regexp.MustCompile(`^\| (A-\d\d) \|`)
	name := regexp.MustCompile("`(Test[A-Za-z0-9_]+)`")
	rows := 0
	for _, line := range strings.Split(string(reg), "\n") {
		m := row.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		rows++
		names := name.FindAllStringSubmatch(line, -1)
		if len(names) == 0 {
			t.Errorf("%s names no test", m[1])
		}
		for _, n := range names {
			if !strings.Contains(n[1], "Assumption") {
				t.Errorf("%s: test %s does not contain Assumption", m[1], n[1])
			}
			if !strings.Contains(src.String(), "func "+n[1]+"(") {
				t.Errorf("%s: test %s does not exist", m[1], n[1])
			}
		}
	}
	if rows != 14 {
		t.Errorf("register has %d A-rows, want 14 (A-01..A-14)", rows)
	}
}
