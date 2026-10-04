package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stainedhead/agent-cli-core/auth/authtest"
	"github.com/stainedhead/agent-cli-core/httpx"
	"github.com/stainedhead/snow-cli/internal/cli"
	"github.com/stainedhead/snow-cli/internal/testsupport/snfake"
)

const writePolicy = `
version: 1
rules:
  - id: create
    effect: allow
    verbs: [create]
    resources: [incident]
    constraints:
      impact: {min: 2, max: 3}
      urgency: {min: 2, max: 3}
  - {id: update, effect: allow, verbs: [update], resources: [incident]}
  - {id: resolve, effect: deny, verbs: [resolve], resources: [incident]}
`

type e2e struct {
	f      *snfake.Fake
	dir    string
	cfg    string
	out    bytes.Buffer
	opts   Options
	router *cli.Router
}

func newE2E(t *testing.T) *e2e {
	t.Helper()
	e := &e2e{f: snfake.New(t), dir: t.TempDir()}
	pol := filepath.Join(e.dir, "p.yaml")
	if err := os.WriteFile(pol, []byte(writePolicy), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := "default_profile: p\nprofiles:\n  p:\n    mode: agent\n    agent_id: agent-1\n" +
		"    instance:\n      host: " + e.f.Host() + "\n" +
		"    incident:\n      create_via: table\n" +
		"    audit:\n      path: " + filepath.Join(e.dir, "audit.jsonl") + "\n" +
		"    policy:\n      path: " + pol + "\n"
	e.cfg = filepath.Join(e.dir, "config.yaml")
	if err := os.WriteFile(e.cfg, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	e.opts = Options{
		Getenv: func(k string) string {
			if k == "SNOW_RUN_ID" {
				return "run-1"
			}
			return ""
		},
		HomeDir: e.dir, Insecure: true, DaemonClient: authtest.New(authtest.Valid),
		Transport: httpx.Config{BaseDelay: 1, MaxDelay: 2, Jitter: -1},
	}
	e.router = cli.NewRouter(cli.Options{EnvFactory: NewEnvFactory(e.opts), Stdout: &e.out})
	cli.RegisterWrite(e.router)
	return e
}

func (e *e2e) run(args ...string) (int, map[string]any) {
	e.out.Reset()
	code := e.router.Execute(context.Background(), append(args, "--config", e.cfg))
	var m map[string]any
	_ = json.Unmarshal(e.out.Bytes(), &m)
	return code, m
}

func (e *e2e) audit(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(e.dir, "audit.jsonl"))
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

var e2eCreate = []string{"incident", "create", "--short-description", "Disk full", "--description", "98%", "--ci", "db01", "--impact", "2", "--urgency", "3"}

func okIncident(f *snfake.Fake) {
	f.On("GET", "/api/now/v1/table/incident", snfake.Records(0))
	f.On("POST", "/api/now/v1/table/incident", snfake.Response{Status: 201, JSON: map[string]any{"result": map[string]any{"sys_id": strings.Repeat("a", 32), "number": "INC1"}}})
}

func TestE2ECreateWritesPendingAndOutcomeAudit(t *testing.T) {
	e := newE2E(t)
	okIncident(e.f)
	code, m := e.run(e2eCreate...)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, e.out.String())
	}
	if m["data"].(map[string]any)["record"].(map[string]any)["number"] != "INC1" {
		t.Fatalf("%v", m)
	}
	a := e.audit(t)
	if len(a) != 2 || !strings.Contains(a[0], `"pending"`) || !strings.Contains(a[1], `"ok"`) || !strings.Contains(a[1], `"create"`) {
		t.Fatalf("audit: %v", a)
	}
	if e.f.Posts() != 1 {
		t.Fatalf("posts %d", e.f.Posts())
	}
}

func TestE2EDuplicateCreateIsDeduplicated(t *testing.T) {
	e := newE2E(t)
	e.f.On("GET", "/api/now/v1/table/incident", snfake.Records(1, map[string]any{"sys_id": strings.Repeat("a", 32), "number": "INC7"}))
	code, m := e.run(e2eCreate...)
	d, _ := m["data"].(map[string]any)
	if code != 0 || d["deduplicated"] != true || e.f.Posts() != 0 {
		t.Fatalf("exit %d posts=%d %v", code, e.f.Posts(), m)
	}
}

func TestE2ECreate503ReDedupesBeforeEachResend(t *testing.T) {
	e := newE2E(t)
	okIncident(e.f)
	e.f.Fail("POST", "/api/now/v1/table/incident", 2, snfake.Error(503, "busy"))
	if code, _ := e.run(e2eCreate...); code != 0 {
		t.Fatalf("exit %d: %s", code, e.out.String())
	}
	if e.f.Posts() != 3 || e.f.Count("GET", "/api/now/v1/table/incident") != 4 { // 2 initial (this + previous hour) + 2 before re-sends
		t.Fatalf("posts=%d dedupe GETs=%d", e.f.Posts(), e.f.Count("GET", "/api/now/v1/table/incident"))
	}
}

func TestE2EDryRunSendsZeroRequestsAndNoAuditWritesBlock(t *testing.T) {
	e := newE2E(t)
	okIncident(e.f)
	code, m := e.run(append(e2eCreate, "--dry-run")...)
	if code != 0 || len(e.f.Requests()) != 0 || m["data"].(map[string]any)["dry_run"] != true {
		t.Fatalf("exit %d requests=%d", code, len(e.f.Requests()))
	}
}

func TestE2EPolicyDeniesP1AndResolve(t *testing.T) {
	e := newE2E(t)
	okIncident(e.f)
	args := append([]string(nil), e2eCreate...)
	args[len(args)-3] = "1" // impact 1
	if code, _ := e.run(args...); code != 6 {
		t.Fatalf("impact 1 must be denied, exit %d", code)
	}
	if code, _ := e.run("incident", "resolve", "INC1", "--close-code", "c", "--close-notes", "n"); code != 6 {
		t.Fatalf("resolve must be denied for agents, exit %d", code)
	}
	if len(e.f.Requests()) != 0 {
		t.Fatalf("denied writes must send nothing: %d", len(e.f.Requests()))
	}
}

func TestE2EUpdateConflictExit7(t *testing.T) {
	e := newE2E(t)
	id := strings.Repeat("a", 32)
	n := 0
	e.f.OnFunc("GET", "/api/now/v1/table/incident/"+id, func(w http.ResponseWriter, _ *http.Request) {
		n += 2
		_ = json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"sys_id": id, "sys_mod_count": string(rune('0' + n))}})
	})
	e.f.On("PATCH", "/api/now/v1/table/incident/"+id, snfake.Response{Status: 200, JSON: map[string]any{"result": map[string]any{"sys_id": id}}})
	if code, _ := e.run("incident", "update", id, "--set", "state=2"); code != 7 {
		t.Fatalf("exit %d: %s", code, e.out.String())
	}
}

func lastAudit(t *testing.T, e *e2e) map[string]any {
	t.Helper()
	a := e.audit(t)
	var m map[string]any
	if err := json.Unmarshal([]byte(a[len(a)-1]), &m); err != nil {
		t.Fatalf("audit line: %v", err)
	}
	return m
}

// An applied-then-conflict write (FR-R08) is audited with outcome
// applied_conflict and the target record as the resource suffix (FR-R10).
func TestE2EAppliedConflictAuditOutcomeAndRefSuffix(t *testing.T) {
	e := newE2E(t)
	id := strings.Repeat("a", 32)
	n := 0
	e.f.OnFunc("GET", "/api/now/v1/table/incident/"+id, func(w http.ResponseWriter, _ *http.Request) {
		n += 2
		_ = json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"sys_id": id, "sys_mod_count": string(rune('0' + n))}})
	})
	e.f.On("PATCH", "/api/now/v1/table/incident/"+id, snfake.Response{Status: 200, JSON: map[string]any{"result": map[string]any{"sys_id": id}}})
	if code, _ := e.run("incident", "update", id, "--set", "state=2"); code != 7 {
		t.Fatalf("exit %d: %s", code, e.out.String())
	}
	rec := lastAudit(t, e)
	if rec["outcome"] != "applied_conflict" || rec["resource"] != "incident:"+id {
		t.Fatalf("audit: %v", rec)
	}
}

// --dry-run is audited as outcome dry_run with the create key as the ref.
func TestE2EDryRunAuditOutcomeAndRefSuffix(t *testing.T) {
	e := newE2E(t)
	okIncident(e.f)
	code, _ := e.run(append(e2eCreate, "--dry-run", "--idempotency-key", "key-1")...)
	if code != 0 || len(e.f.Requests()) != 0 {
		t.Fatalf("exit %d requests=%d", code, len(e.f.Requests()))
	}
	rec := lastAudit(t, e)
	if rec["outcome"] != "dry_run" || rec["resource"] != "incident:key-1" {
		t.Fatalf("audit: %v", rec)
	}
}

// --expected-mod-count refuses the PATCH when the record moved (exit 7).
func TestE2EExpectedModCountFlagMismatchSendsNoPatch(t *testing.T) {
	e := newE2E(t)
	id := strings.Repeat("a", 32)
	e.f.On("GET", "/api/now/v1/table/incident/"+id, snfake.Response{Status: 200, JSON: map[string]any{"result": map[string]any{"sys_id": id, "sys_mod_count": "5"}}})
	e.f.On("PATCH", "/api/now/v1/table/incident/"+id, snfake.Response{Status: 200, JSON: map[string]any{"result": map[string]any{"sys_id": id}}})
	code, _ := e.run("incident", "update", id, "--set", "state=2", "--expected-mod-count", "3")
	if code != 7 || e.f.Count("PATCH", "/api/now/v1/table/incident/"+id) != 0 {
		t.Fatalf("exit %d patches=%d: %s", code, e.f.Count("PATCH", "/api/now/v1/table/incident/"+id), e.out.String())
	}
	if rec := lastAudit(t, e); rec["outcome"] != "error" {
		t.Fatalf("not-applied conflict must be an error outcome: %v", rec)
	}
	if code, _ := e.run("incident", "update", id, "--set", "state=2", "--expected-mod-count", "0"); code != 9 {
		t.Fatalf("explicit 0 must be a validation error, exit %d", code)
	}
}
