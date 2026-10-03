package repocheck

import (
	"os/exec"
	"strings"
	"testing"
)

// NFR-004: domain and use-case packages (direct imports) import neither net/http, the
// filesystem/OS, nor any keychain code.
func TestDomainAndUsecaseAreIOFree(t *testing.T) {
	out, err := exec.Command("go", "list", "-f", `{{join .Imports "\n"}}`, "../domain/...", "../usecase/...").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	for _, dep := range strings.Fields(string(out)) {
		switch {
		case dep == "net/http", dep == "net", dep == "os", dep == "os/exec", dep == "io/ioutil":
			t.Errorf("domain/usecase must not depend on %s", dep)
		case strings.Contains(dep, "keychain"), strings.Contains(dep, "keyring"):
			t.Errorf("domain/usecase must not depend on keychain code: %s", dep)
		case strings.HasPrefix(dep, "github.com/stainedhead/snow-cli/internal/") &&
			(strings.HasSuffix(dep, "/sn") || strings.HasSuffix(dep, "/app") || strings.HasSuffix(dep, "/cli") ||
				strings.HasSuffix(dep, "/config") || strings.HasSuffix(dep, "/auditx")):
			t.Errorf("domain/usecase must not depend on adapter package %s", dep)
		}
	}
}
