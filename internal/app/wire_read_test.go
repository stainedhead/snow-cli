package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stainedhead/agent-cli-core/auth/authtest"
	"github.com/stainedhead/snow-cli/internal/cli"
	"github.com/stainedhead/snow-cli/internal/testsupport/snfake"
)

const readPolicy = `
version: 1
limits: {max_results: 100, max_bytes: 32768}
rules:
  - id: deny-sys
    effect: deny
    verbs: ["*"]
    resources: ["table:sys_*"]
  - id: tables
    effect: allow
    verbs: [get, list, count]
    resources: ["table:incident", "table:task"]
    fields: [sys_id, number, short_description, description, work_notes, state, sys_class_name, assigned_to, sys_updated_on, sys_updated_by, priority, name, active]
  - id: typed
    effect: allow
    verbs: [get, list, search, related, vars]
    resources: [incident, request, ritm, task, problem, change, "cmdb:*", "catalog:*"]
  - id: ident
    effect: allow
    verbs: [whoami]
    resources: [whoami]
`

type env struct {
	*fixture
	out bytes.Buffer
}

func newReadEnv(t *testing.T) *env {
	t.Helper()
	e := &env{fixture: newFixture(t, "agent")}
	if err := os.WriteFile(filepath.Join(e.dir, "agent.policy.yaml"), []byte(readPolicy), 0o600); err != nil {
		t.Fatal(err)
	}
	return e
}

// run executes one snow invocation against the fake and decodes the envelope.
func (e *env) run(t *testing.T, args ...string) (int, map[string]any) {
	t.Helper()
	e.out.Reset()
	o := e.opts()
	o.DaemonClient = authtest.New(authtest.Valid)
	r := cli.NewRouter(cli.Options{EnvFactory: NewEnvFactory(o), Stdout: &e.out, Stderr: &e.stderr})
	cli.RegisterAll(r)
	code := r.Execute(context.Background(), append(args, "--config", e.cfgPath))
	var m map[string]any
	if strings.HasPrefix(strings.TrimSpace(e.out.String()), "{") {
		if err := json.Unmarshal(e.out.Bytes(), &m); err != nil {
			t.Fatalf("envelope: %v\n%s", err, e.out.String())
		}
	}
	return code, m
}

func data(m map[string]any) map[string]any { d, _ := m["data"].(map[string]any); return d }

func items(m map[string]any) []any { it, _ := data(m)["items"].([]any); return it }

func query(t *testing.T, f *snfake.Fake, i int) url.Values {
	t.Helper()
	q, err := url.ParseQuery(f.Requests()[i].Query)
	if err != nil {
		t.Fatal(err)
	}
	return q
}

func TestReadTableListAllowlisted(t *testing.T) {
	e := newReadEnv(t)
	e.fake.On("GET", "/api/now/v1/table/incident", snfake.Records(2,
		map[string]any{"sys_id": sid, "number": "INC1", "short_description": "disk"},
		map[string]any{"sys_id": sid2, "number": "INC2", "short_description": "cpu"}))
	code, m := e.run(t, "table", "list", "incident", "--query", "active=true", "--fields", "number,short_description", "--limit", "2")
	if code != 0 || m["ok"] != true {
		t.Fatalf("exit %d: %s", code, e.out.String())
	}
	if len(items(m)) != 2 {
		t.Errorf("items = %v", items(m))
	}
	page := data(m)["page"].(map[string]any)
	if page["returned"] != float64(2) || page["total"] != float64(2) || page["next_offset"] != nil {
		t.Errorf("page = %v", page)
	}
	if meta := m["meta"].(map[string]any); meta["count"] != float64(2) {
		t.Errorf("meta = %v", meta)
	}
	q := query(t, e.fake, 0)
	if q.Get("sysparm_fields") != "number,short_description" || q.Get("sysparm_limit") != "2" ||
		!strings.HasSuffix(q.Get("sysparm_query"), "ORDERBYsys_id") || !strings.HasPrefix(q.Get("sysparm_query"), "active=true^") {
		t.Errorf("query = %v", q)
	}
}

const (
	sid  = "0123456789abcdef0123456789abcdef"
	sid2 = "fedcba9876543210fedcba9876543210"
)

func TestReadClientPolicyDenialsSendNothing(t *testing.T) {
	e := newReadEnv(t)
	e.fake.On("GET", "/api/now/v1/table/sys_user", snfake.Records(1, map[string]any{"user_name": "x"}))
	if code, _ := e.run(t, "table", "list", "sys_user"); code != 6 {
		t.Errorf("denied table exit = %d", code)
	}
	if code, _ := e.run(t, "table", "list", "incident", "--fields", "caller_id"); code != 6 {
		t.Errorf("field outside allowlist exit = %d", code)
	}
	if code, _ := e.run(t, "table", "get", "cmdb_ci", sid); code != 6 {
		t.Errorf("unlisted table exit = %d", code)
	}
	if n := len(e.fake.Requests()); n != 0 {
		t.Errorf("%d requests were sent despite the denials", n)
	}
}

func TestReadServerStatusesMapToExitCodes(t *testing.T) {
	e := newReadEnv(t)
	e.fake.On("GET", "/api/now/v1/table/incident", snfake.Error(403, "ACL"))
	if code, _ := e.run(t, "table", "list", "incident"); code != 4 {
		t.Errorf("403 exit = %d", code)
	}
	if code, _ := e.run(t, "table", "get", "incident", sid); code != 5 {
		t.Errorf("404 exit = %d", code)
	}
}

func TestReadEmptyPageWithMoreRowsFlagsACLFiltering(t *testing.T) {
	e := newReadEnv(t)
	e.fake.On("GET", "/api/now/v1/table/incident", snfake.Records(30))
	code, m := e.run(t, "table", "list", "incident", "--limit", "10")
	if code != 0 || data(m)["acl_filtered_possible"] != true {
		t.Fatalf("exit %d data %v", code, data(m))
	}
	if data(m)["items"] == nil {
		t.Error("items must be [] not null")
	}
}

func TestReadOffsetAndNextOffset(t *testing.T) {
	e := newReadEnv(t)
	e.fake.On("GET", "/api/now/v1/table/incident", snfake.Records(50, map[string]any{"number": "INC3"}, map[string]any{"number": "INC4"}))
	_, m := e.run(t, "table", "list", "incident", "--limit", "2", "--offset", "2")
	page := data(m)["page"].(map[string]any)
	if page["offset"] != float64(2) || page["next_offset"] != float64(4) {
		t.Errorf("page = %v", page)
	}
	if query(t, e.fake, 0).Get("sysparm_offset") != "2" {
		t.Error("offset not sent")
	}
}

func TestReadTruncationTrimsItemsAndPointsAtNextOffset(t *testing.T) {
	e := newReadEnv(t)
	var recs []map[string]any
	for i := 0; i < 40; i++ {
		recs = append(recs, map[string]any{"number": "INC", "short_description": strings.Repeat("y", 150)})
	}
	e.fake.On("GET", "/api/now/v1/table/incident", snfake.Records(400, recs...))
	code, m := e.run(t, "table", "list", "incident", "--limit", "40", "--max-bytes", "2000", "--fields", "number,short_description")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, e.out.String())
	}
	if e.out.Len() > 2000 {
		t.Errorf("output %d bytes exceeds --max-bytes", e.out.Len())
	}
	d := data(m)
	kept := len(items(m))
	page := d["page"].(map[string]any)
	if d["truncated"] != true || kept == 0 || kept >= 40 || page["next_offset"] != float64(kept) {
		t.Errorf("truncated=%v kept=%d page=%v", d["truncated"], kept, page)
	}
	if m["meta"].(map[string]any)["count"] != float64(kept) {
		t.Errorf("meta = %v", m["meta"])
	}
}

func TestReadInjectionTextIsMarkedUntrusted(t *testing.T) {
	e := newReadEnv(t)
	e.fake.On("GET", "/api/now/v1/table/incident", snfake.FixtureFile(t, "../../testdata/fixtures/read/incident_list_injection.json"))
	code, m := e.run(t, "incident", "list", "--fields", "number,description,work_notes,sys_updated_by,sys_updated_on")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, e.out.String())
	}
	d := items(m)[0].(map[string]any)["description"].(map[string]any)
	if d["untrusted"] != true || d["author"] != "alice" || !strings.Contains(d["value"].(string), "ignore all previous instructions") {
		t.Errorf("description = %v", d)
	}
	if n := items(m)[0].(map[string]any)["number"]; n != "INC0010001" {
		t.Errorf("number must stay a plain string, got %v", n)
	}
	e.run(t, "incident", "list", "--fields", "number,description", "--format", "text")
	if !strings.Contains(e.out.String(), "<<<UNTRUSTED") {
		t.Errorf("text output lacks delimiters:\n%s", e.out.String())
	}
}

func TestReadIncidentGetByNumberAndCMDBAndCatalog(t *testing.T) {
	e := newReadEnv(t)
	e.fake.On("GET", "/api/now/v1/table/incident", snfake.Records(1, map[string]any{"sys_id": sid, "number": "INC0010001", "short_description": "disk"}))
	code, m := e.run(t, "incident", "get", "inc0010001")
	if code != 0 || data(m)["number"] != "INC0010001" {
		t.Fatalf("incident get: %d %s", code, e.out.String())
	}
	if query(t, e.fake, 0).Get("sysparm_query") != "number=INC0010001^"+"ORDERBYDESCsys_updated_on^ORDERBYsys_id" {
		t.Errorf("query = %v", query(t, e.fake, 0))
	}

	e.fake.On("GET", "/api/now/v1/table/cmdb_ci", snfake.Records(1, map[string]any{"sys_id": sid, "name": "web01", "sys_class_name": "cmdb_ci_server"}))
	code, m = e.run(t, "cmdb", "ci", "get", "web01")
	if code != 0 || data(m)["name"] != "web01" {
		t.Fatalf("cmdb ci get: %d %s", code, e.out.String())
	}

	e.fake.On("GET", "/api/sn_sc/servicecatalog/items", snfake.Response{JSON: map[string]any{"result": []any{
		map[string]any{"sys_id": sid, "name": "Laptop", "short_description": "A laptop", "category": map[string]any{"title": "Hardware"}},
	}}})
	code, m = e.run(t, "catalog", "search", "laptop")
	if code != 0 || len(items(m)) != 1 {
		t.Fatalf("catalog search: %d %s", code, e.out.String())
	}
}

func TestReadMyWorkResolvesIdentityThroughWhoami(t *testing.T) {
	e := newReadEnv(t)
	e.fake.On("GET", "/api/x_corp_agent/v1/whoami", snfake.Response{JSON: map[string]any{"result": map[string]any{"user_name": "svc.agent", "name": "Agent"}}})
	e.fake.On("GET", "/api/now/v1/table/task", snfake.Records(1, map[string]any{"number": "INC1", "sys_class_name": "incident"}))
	code, m := e.run(t, "my", "work", "--kind", "incident")
	if code != 0 || len(items(m)) != 1 {
		t.Fatalf("exit %d: %s", code, e.out.String())
	}
	q := query(t, e.fake, 1)
	if !strings.HasPrefix(q.Get("sysparm_query"), "active=true^assigned_to.user_name=svc.agent^sys_class_name=incident") {
		t.Errorf("query = %q", q.Get("sysparm_query"))
	}
}

func TestReadTableCount(t *testing.T) {
	e := newReadEnv(t)
	e.fake.On("GET", "/api/now/v1/stats/incident", snfake.Response{JSON: map[string]any{"result": map[string]any{"stats": map[string]any{"count": "12"}}}})
	code, m := e.run(t, "table", "count", "incident", "--query", "active=true")
	if code != 0 || data(m)["count"] != float64(12) {
		t.Fatalf("exit %d: %s", code, e.out.String())
	}
}

func TestReadUsageErrors(t *testing.T) {
	e := newReadEnv(t)
	for _, args := range [][]string{
		{"table", "get", "incident"}, {"table", "list"}, {"table", "count"}, {"cmdb", "ci", "get"}, {"cmdb", "app"},
		{"incident", "get"}, {"catalog", "get"}, {"catalog", "search"}, {"cmdb", "ci", "related"},
		{"table", "get", "incident", sid, "extra"},
	} {
		if code, _ := e.run(t, args...); code != 2 {
			t.Errorf("%v exit = %d, want 2", args, code)
		}
	}
	if code, _ := e.run(t, "cmdb", "ci", "related", "web01", "--depth", "x"); code != 2 {
		t.Errorf("bad --depth exit = %d", code)
	}
	if code, _ := e.run(t, "change", "create"); code != 2 {
		t.Errorf("change create must be absent (exit 2), got %d", code)
	}
}
