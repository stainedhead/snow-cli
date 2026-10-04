package app

// I2: end-to-end tests per milestone acceptance (spec section 10). They drive
// the real command tree (cli.RegisterAll) through the composition root
// (NewEnvFactory) with the SHIPPED policies (policies.Named), httptest
// ServiceNow and Okta fakes, and authtest/in-memory credential stores.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stainedhead/agent-cli-core/auth/authtest"
	"github.com/stainedhead/agent-cli-core/httpx"
	"github.com/stainedhead/snow-cli/internal/cli"
	"github.com/stainedhead/snow-cli/internal/humanauth"
	"github.com/stainedhead/snow-cli/internal/testsupport/oktafake"
	"github.com/stainedhead/snow-cli/internal/testsupport/snfake"
	"github.com/stainedhead/snow-cli/policies"
)

const (
	itSID   = "0123456789abcdef0123456789abcdef"
	itSID2  = "fedcba9876543210fedcba9876543210"
	itToken = "ACCESS-TOKEN-should-never-leak-9f3a"
)

type itg struct {
	t        *testing.T
	f        *snfake.Fake
	okta     *oktafake.Fake
	dir      string
	cfg      string
	audit    string
	mode     string
	policy   string
	override bool
	out      bytes.Buffer
	errb     bytes.Buffer
	opts     Options
}

type itgOpt func(*itg)

func withPolicyOverride() itgOpt { return func(i *itg) { i.override = true } }

func withPolicyText(text string) itgOpt { return func(i *itg) { i.policy = text } }

// newItg writes a config using the shipped policy for mode (or a variant).
func newItg(t *testing.T, mode string, opts ...itgOpt) *itg {
	t.Helper()
	i := &itg{t: t, f: snfake.New(t), dir: t.TempDir(), mode: mode}
	for _, o := range opts {
		o(i)
	}
	i.audit = filepath.Join(i.dir, "audit.jsonl")
	polRef := mode
	if i.policy != "" {
		polRef = filepath.Join(i.dir, "policy.yaml")
		if err := os.WriteFile(polRef, []byte(i.policy), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cfg := "default_profile: p\nprofiles:\n  p:\n    mode: " + mode + "\n    agent_id: agent-1\n" +
		"    instance:\n      host: " + i.f.Host() + "\n" +
		"    incident:\n      create_via: table\n" +
		"    audit:\n      path: " + i.audit + "\n    policy:\n      path: " + polRef + "\n" + overrideLine(i.override) +
		"    selftest:\n      fixture_incident: INC0000001\n      foreign_incident: INC0000002\n"
	if mode == "human" {
		i.okta = oktafake.New(t)
		cfg += "    okta:\n      issuer: " + i.okta.Issuer() + "\n      client_id: cid\n"
	}
	i.cfg = filepath.Join(i.dir, "config.yaml")
	if err := os.WriteFile(i.cfg, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	i.opts = Options{
		Getenv: func(k string) string {
			if k == "SNOW_RUN_ID" {
				return "run-1"
			}
			return ""
		},
		HomeDir: i.dir, Stderr: &i.errb, Insecure: true, NamedPolicy: policies.Named,
		Transport: httpx.Config{BaseDelay: 1, MaxDelay: 2, Jitter: -1},
	}
	if mode == "agent" {
		i.opts.DaemonClient = authtest.New(authtest.Valid)
	}
	return i
}

// seedHuman stores a session in an in-memory credential store.
func (i *itg) seedHuman(access string, expiry time.Time) *humanauth.MemoryStore {
	i.t.Helper()
	st := humanauth.NewMemoryStore()
	if err := st.Save("p", humanauth.Credentials{
		Issuer: i.okta.Issuer(), ClientID: "cid", Subject: "00u1", AccessToken: access,
		RefreshToken: "REFRESH-SECRET-1", Expiry: expiry,
	}); err != nil {
		i.t.Fatal(err)
	}
	old := storeFactory
	storeFactory = func() humanauth.Store { return st }
	i.t.Cleanup(func() { storeFactory = old })
	return st
}

func (i *itg) run(args ...string) (int, map[string]any) {
	i.t.Helper()
	i.out.Reset()
	i.errb.Reset()
	r := cli.NewRouter(cli.Options{EnvFactory: NewEnvFactory(i.opts), Stdout: &i.out, Stderr: &i.errb})
	cli.RegisterAll(r)
	code := r.Execute(context.Background(), append(args, "--config", i.cfg))
	var m map[string]any
	if s := strings.TrimSpace(i.out.String()); strings.HasPrefix(s, "{") {
		if err := json.Unmarshal(i.out.Bytes(), &m); err != nil {
			i.t.Fatalf("not an envelope: %v\n%s", err, s)
		}
	}
	return code, m
}

func (i *itg) auditLines() []map[string]any {
	i.t.Helper()
	b, err := os.ReadFile(i.audit)
	if err != nil {
		return nil
	}
	var out []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if l == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			i.t.Fatalf("audit line: %v", err)
		}
		out = append(out, m)
	}
	return out
}

func errMsg(m map[string]any) string {
	e, _ := m["error"].(map[string]any)
	s, _ := e["message"].(string)
	return s
}

// ---- M1 read path ----

func TestIntegrationM1ReadAllowlistedDeniedAndServerStatuses(t *testing.T) {
	i := newItg(t, "agent")
	i.f.On("GET", "/api/now/v1/table/incident", snfake.Records(1, map[string]any{"sys_id": itSID, "number": "INC1", "short_description": "disk"}))
	code, m := i.run("table", "list", "incident")
	if code != 0 || m["ok"] != true || len(data(m)["items"].([]any)) != 1 {
		t.Fatalf("exit %d: %s", code, i.out.String())
	}
	// Omitted --fields is replaced by the policy allowlist (WS-B request 1).
	q := query(t, i.f, 0).Get("sysparm_fields")
	if !strings.Contains(q, "number") || strings.Contains(q, "business_hours") {
		t.Errorf("sysparm_fields = %q", q)
	}
	// Denied table: client policy, no request.
	n := len(i.f.Requests())
	if code, _ := i.run("table", "list", "sys_user"); code != 6 || len(i.f.Requests()) != n {
		t.Errorf("denied table: exit %d, requests %d->%d", code, n, len(i.f.Requests()))
	}
	// Field outside the allowlist: exit 6.
	if code, _ := i.run("table", "list", "incident", "--fields", "number,sys_created_by"); code != 6 {
		t.Errorf("field outside allowlist: exit %d", code)
	}
	// Server 403 -> 4, 404 -> 5.
	i.f.On("GET", "/api/now/v1/table/problem", snfake.Error(403, "forbidden"))
	if code, _ := i.run("table", "list", "problem"); code != 4 {
		t.Errorf("403 exit %d", code)
	}
	if code, _ := i.run("table", "get", "incident", itSID2); code != 5 {
		t.Errorf("404 exit %d", code)
	}
}

func TestIntegrationM1TruncationAndACLFilteredPage(t *testing.T) {
	i := newItg(t, "agent")
	i.f.On("GET", "/api/now/v1/table/incident", snfake.Records(30))
	_, m := i.run("table", "list", "incident", "--limit", "10", "--offset", "20")
	if data(m)["acl_filtered_possible"] != true {
		t.Errorf("empty page with X-Total-Count beyond the offset: %v", data(m))
	}
	var recs []map[string]any
	for n := 0; n < 40; n++ {
		recs = append(recs, map[string]any{"number": "INC", "short_description": strings.Repeat("y", 150)})
	}
	i.f.On("GET", "/api/now/v1/table/incident", snfake.Records(400, recs...))
	code, m := i.run("table", "list", "incident", "--limit", "40", "--max-bytes", "2000", "--fields", "number,short_description")
	d := data(m)
	if code != 0 || d["truncated"] != true || d["page"].(map[string]any)["next_offset"] == nil || i.out.Len() > 2000 {
		t.Errorf("truncation: exit %d data=%v", code, d)
	}
}

func TestIntegrationM1HumanProfileReadsThroughTheSameUseCases(t *testing.T) {
	i := newItg(t, "human")
	i.seedHuman(itToken, time.Now().Add(time.Hour))
	i.f.On("GET", "/api/now/v1/table/incident", snfake.Records(1, map[string]any{"sys_id": itSID, "number": "INC1"}))
	code, m := i.run("incident", "list", "--fields", "number")
	if code != 0 || len(items(m)) != 1 {
		t.Fatalf("exit %d: %s", code, i.out.String())
	}
	if got := i.f.Requests()[0].Header.Get("Authorization"); got != "Bearer "+itToken {
		t.Errorf("human bearer not sent: %q", got)
	}
	if i.okta != nil && len(i.okta.Requests()) != 0 {
		t.Error("a valid session must not call Okta")
	}
}

func TestIntegrationM1HumanExpiredTokenRefreshesAgainstOkta(t *testing.T) {
	i := newItg(t, "human")
	st := i.seedHuman("OLD-ACCESS", time.Now().Add(-time.Hour))
	i.okta.QueueToken(200, `{"access_token":"NEW-ACCESS-7","refresh_token":"REFRESH-ROTATED-2","token_type":"Bearer","expires_in":3600}`)
	i.f.On("GET", "/api/now/v1/table/incident", snfake.Records(0))
	if code, _ := i.run("incident", "list", "--fields", "number"); code != 0 {
		t.Fatalf("exit %d: %s", code, i.out.String())
	}
	if got := i.f.Requests()[0].Header.Get("Authorization"); got != "Bearer NEW-ACCESS-7" {
		t.Errorf("refreshed bearer not used: %q", got)
	}
	c, err := st.Load("p")
	if err != nil || c.RefreshToken != "REFRESH-ROTATED-2" {
		t.Errorf("rotation not persisted: %v", err)
	}
}

func TestIntegrationM3RefreshFailureExit3ShowsOktaError(t *testing.T) {
	i := newItg(t, "human")
	i.seedHuman("OLD", time.Now().Add(-time.Hour))
	i.okta.QueueToken(400, `{"error":"invalid_grant","error_description":"The refresh token is invalid or expired."}`)
	code, m := i.run("incident", "list")
	if code != 3 || !strings.Contains(errMsg(m), "invalid_grant") {
		t.Fatalf("exit %d: %s", code, i.out.String())
	}
	if len(i.f.Requests()) != 0 {
		t.Error("no ServiceNow request without a token")
	}
}

func TestIntegrationM3HumanWriteViaSameUseCasesNeedsYes(t *testing.T) {
	i := newItg(t, "human")
	i.seedHuman(itToken, time.Now().Add(time.Hour))
	i.f.On("GET", "/api/now/v1/table/incident", snfake.Records(0))
	i.f.On("POST", "/api/now/v1/table/incident", snfake.Response{Status: 201, JSON: map[string]any{"result": map[string]any{"sys_id": itSID, "number": "INC9"}}})
	create := []string{"incident", "create", "--short-description", "Disk full", "--description", "98%", "--ci", "db01", "--impact", "1", "--urgency", "2"}
	// Non-interactive human mode without --yes is a usage error.
	if code, _ := i.run(create...); code != 2 || i.f.Posts() != 0 {
		t.Fatalf("without --yes: exit %d posts %d", code, i.f.Posts())
	}
	code, m := i.run(append(create, "--yes")...)
	if code != 0 || i.f.Posts() != 1 {
		t.Fatalf("exit %d posts %d: %s", code, i.f.Posts(), i.out.String())
	}
	// Humans may set impact 1 (agents may not) and are never given the
	// "agent" provenance as the actor (the note still names mode/ids).
	if data(m)["record"].(map[string]any)["number"] != "INC9" {
		t.Errorf("%v", m)
	}
}

// selftestFixtures serves every read row of the agent matrix and denies the
// server-side deny rows.
func selftestFixtures(f *snfake.Fake) {
	rec := map[string]any{"sys_id": itSID, "number": "INC0000001", "sys_mod_count": "1"}
	for _, tbl := range []string{"incident", "cmdb_ci", "cmdb_rel_ci", "cmdb_ci_service", "sc_request", "sc_req_item", "task", "change_request", "problem"} {
		f.On("GET", "/api/now/v1/table/"+tbl, snfake.Records(1, rec))
	}
	f.On("GET", "/api/now/v1/table/incident/"+itSID, snfake.Response{JSON: map[string]any{"result": rec}})
	f.On("GET", "/api/now/v1/stats/incident", snfake.Response{JSON: map[string]any{"result": map[string]any{"stats": map[string]any{"count": "1"}}}})
	f.On("GET", "/api/sn_sc/servicecatalog/items", snfake.Response{JSON: map[string]any{"result": []any{}}})
	f.On("GET", "/api/x_corp_agent/v1/whoami", snfake.Response{JSON: map[string]any{"result": map[string]any{"user_name": "svc.agent", "roles": []string{"x"}}}})
	f.On("GET", "/api/now/v1/table/sys_user", snfake.Error(403, "no"))
	f.On("GET", "/api/now/v1/table/sys_properties", snfake.Error(403, "no"))
}

func TestIntegrationM1SelftestPassesAndFailsOnOverGrant(t *testing.T) {
	i := newItg(t, "agent")
	selftestFixtures(i.f)
	code, m := i.run("selftest")
	d := data(m)
	if code != 0 || d["failed"] != float64(0) || d["skipped"] != float64(2) || d["passed"].(float64) < 10 {
		t.Fatalf("selftest: exit %d: %s", code, i.out.String())
	}
	if n := i.f.Posts() + i.f.Count("PATCH", "/api/now/v1/table/incident/"+itSID); n != 0 {
		t.Errorf("read-only selftest wrote %d times", n)
	}
	// Over-grant: the server answers 200 for sys_user.
	i.f.On("GET", "/api/now/v1/table/sys_user", snfake.Records(1, map[string]any{"sys_id": itSID}))
	code, m = i.run("selftest")
	if code != 1 || !strings.Contains(errMsg(m), "expected deny, got allow") || !strings.Contains(errMsg(m), "sys_user") {
		t.Fatalf("over-grant must exit 1 naming the row: exit %d: %s", code, i.out.String())
	}
}

func TestIntegrationSelftestIncludeWritesRunsServerDenyProbes(t *testing.T) {
	i := newItg(t, "agent")
	selftestFixtures(i.f)
	i.f.On("PATCH", "/api/now/v1/table/incident/"+itSID, snfake.Error(403, "acl"))
	code, m := i.run("selftest", "--include-writes")
	if code != 0 || data(m)["skipped"] != float64(0) {
		t.Fatalf("exit %d: %s", code, i.out.String())
	}
	if n := i.f.Count("PATCH", "/api/now/v1/table/incident/"+itSID); n != 2 {
		t.Errorf("both write rows must be probed against the server, got %d PATCH", n)
	}
	// A server that accepts the foreign update over-grants: exit 1.
	i.f.On("PATCH", "/api/now/v1/table/incident/"+itSID, snfake.Response{JSON: map[string]any{"result": map[string]any{"sys_id": itSID}}})
	if code, _ := i.run("selftest", "--include-writes"); code != 1 {
		t.Errorf("over-granted writes: exit %d", code)
	}
}

// ---- M2 write path ----

var itCreate = []string{"incident", "create", "--short-description", "Disk full", "--description", "98%", "--ci", "db01", "--impact", "2", "--urgency", "3"}

func okCreate(f *snfake.Fake) {
	f.On("GET", "/api/now/v1/table/incident", snfake.Records(0))
	f.On("POST", "/api/now/v1/table/incident", snfake.Response{Status: 201, JSON: map[string]any{"result": map[string]any{"sys_id": itSID, "number": "INC1"}}})
}

func TestIntegrationM2DuplicateCreateDeduplicatedOnePost(t *testing.T) {
	i := newItg(t, "agent")
	okCreate(i.f)
	if code, _ := i.run(itCreate...); code != 0 || i.f.Posts() != 1 {
		t.Fatalf("first create: exit %d posts %d: %s", code, i.f.Posts(), i.out.String())
	}
	// Same inputs again: the dedupe lookup now finds the record.
	i.f.On("GET", "/api/now/v1/table/incident", snfake.Records(1, map[string]any{"sys_id": itSID, "number": "INC1"}))
	code, m := i.run(itCreate...)
	if code != 0 || data(m)["deduplicated"] != true || i.f.Posts() != 1 {
		t.Fatalf("exit %d posts %d: %s", code, i.f.Posts(), i.out.String())
	}
}

func TestIntegrationM2DeniedFieldsPriorityAndImpactSendNothing(t *testing.T) {
	i := newItg(t, "agent")
	okCreate(i.f)
	checks := [][]string{
		{"incident", "update", itSID, "--set", "caller_id=abc"},                                                                  // field outside allowlist
		{"incident", "update", itSID, "--set", "priority=1"},                                                                     // priority never sent
		{"incident", "create", "--short-description", "x", "--description", "y", "--ci", "c", "--impact", "1", "--urgency", "3"}, // D-d
		{"incident", "resolve", itSID, "--close-code", "c", "--close-notes", "n"},
	}
	for _, args := range checks {
		code, _ := i.run(args...)
		if code != 6 && code != 9 {
			t.Errorf("%v: exit %d", args, code)
		}
	}
	for _, r := range i.f.Requests() {
		if r.Method == "POST" || r.Method == "PATCH" || strings.Contains(string(r.Body), "priority") {
			t.Errorf("write or priority sent: %s %s %s", r.Method, r.Path, r.Body)
		}
	}
}

func TestIntegrationM2PostRetriedAfterDedupeMissAndProvenance(t *testing.T) {
	i := newItg(t, "agent")
	okCreate(i.f)
	i.f.Fail("POST", "/api/now/v1/table/incident", 2, snfake.Error(503, "busy"))
	if code, _ := i.run(append(itCreate, "--note", "from test")...); code != 0 || i.f.Posts() != 3 {
		t.Fatalf("exit %d posts %d", code, i.f.Posts())
	}
	var posted string
	for _, r := range i.f.Requests() {
		if r.Method == "POST" {
			posted = string(r.Body)
		}
	}
	if !strings.Contains(posted, "[snow-cli agent=agent-1 run=run-1]") || !strings.Contains(posted, `"correlation_display":"agent:agent-1"`) {
		t.Errorf("provenance missing from the create body: %s", posted)
	}
	if strings.Contains(posted, "priority") {
		t.Error("priority must never be sent")
	}
}

func TestIntegrationM2UpdateBodyCarriesProvenanceAndConflictIsExit7(t *testing.T) {
	i := newItg(t, "agent")
	mods := 0
	i.f.OnFunc("GET", "/api/now/v1/table/incident/"+itSID, func(w http.ResponseWriter, _ *http.Request) {
		mods++
		_ = json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"sys_id": itSID, "sys_mod_count": "5"}})
	})
	i.f.On("PATCH", "/api/now/v1/table/incident/"+itSID, snfake.Response{JSON: map[string]any{"result": map[string]any{"sys_id": itSID}}})
	if code, _ := i.run("incident", "update", itSID, "--work-note", "restarted"); code != 0 {
		t.Fatalf("exit %d: %s", code, i.out.String())
	}
	var body string
	for _, r := range i.f.Requests() {
		if r.Method == "PATCH" {
			body = string(r.Body)
		}
	}
	if !strings.Contains(body, "[snow-cli agent=agent-1 run=run-1] restarted") {
		t.Errorf("PATCH body %s", body)
	}
	// sys_mod_count that jumps by more than one during the update -> exit 7.
	n := 0
	i.f.OnFunc("GET", "/api/now/v1/table/incident/"+itSID, func(w http.ResponseWriter, _ *http.Request) {
		n += 3
		_ = json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"sys_id": itSID, "sys_mod_count": json.Number(string(rune('0' + n)))}})
	})
	if code, _ := i.run("incident", "update", itSID, "--work-note", "again"); code != 7 {
		t.Fatalf("conflict exit %d: %s", code, i.out.String())
	}
}

type failAfter struct{ n, max int }

func (w *failAfter) Write(p []byte) (int, error) {
	w.n++
	if w.n > w.max {
		return 0, errors.New("disk full")
	}
	return len(p), nil
}

func TestIntegrationM2AuditWriteFailureBlocksTheWrite(t *testing.T) {
	i := newItg(t, "agent")
	okCreate(i.f)
	i.opts.AuditWriter = &failAfter{max: 0}
	code, m := i.run(itCreate...)
	if code != 1 || i.f.Posts() != 0 || !strings.Contains(errMsg(m), "request was not sent") {
		t.Fatalf("exit %d posts %d: %s", code, i.f.Posts(), i.out.String())
	}
	for _, r := range i.f.Requests() {
		if r.Method != "GET" {
			t.Errorf("unexpected %s", r.Method)
		}
	}
}

func TestIntegrationM2AuditRecordsHaveRequiredFieldsAndNoBodies(t *testing.T) {
	i := newItg(t, "agent")
	okCreate(i.f)
	if code, _ := i.run(append(itCreate, "--note", "SECRET-BODY-TEXT")...); code != 0 {
		t.Fatalf("exit %d", code)
	}
	lines := i.auditLines()
	if len(lines) < 2 {
		t.Fatalf("audit lines: %v", lines)
	}
	for _, l := range lines {
		for _, k := range []string{"ts", "tool", "agent_id", "run_id", "verb", "resource", "outcome", "policy_decision"} {
			if l[k] == nil || l[k] == "" {
				t.Errorf("audit record lacks %s: %v", k, l)
			}
		}
	}
	raw, _ := os.ReadFile(i.audit)
	for _, s := range []string{"SECRET-BODY-TEXT", "Disk full", "98%", itToken, "Bearer"} {
		if strings.Contains(string(raw), s) {
			t.Errorf("audit log leaks %q", s)
		}
	}
	if lines[0]["outcome"] != "pending" || lines[len(lines)-1]["outcome"] != "ok" {
		t.Errorf("pending/outcome order: %v", lines)
	}
}

// ---- M4 catalog ----

const itItem = "0123456789abcdef0123456789abcdef"

func catalogFixtures(f *snfake.Fake) {
	f.On("GET", "/api/sn_sc/servicecatalog/items/"+itItem, snfake.Response{JSON: map[string]any{"result": map[string]any{"sys_id": itItem, "name": "Laptop", "short_description": "A laptop"}}})
	f.On("GET", "/api/sn_sc/servicecatalog/items/"+itItem+"/variables", snfake.Response{JSON: map[string]any{"result": []any{
		map[string]any{"name": "model", "label": "Model", "type": "6", "mandatory": true},
	}}})
	f.On("GET", "/api/sn_sc/servicecatalog/items", snfake.Response{JSON: map[string]any{"result": []any{
		map[string]any{"sys_id": itItem, "name": "Laptop", "short_description": "A laptop"},
	}}})
	f.On("POST", "/api/sn_sc/servicecatalog/items/"+itItem+"/order_now", snfake.Response{JSON: map[string]any{"result": map[string]any{"sys_id": itSID, "number": "REQ1", "request_number": "REQ1"}}})
}

func TestIntegrationM4CatalogReadsAndOrderIsDryRunByDefault(t *testing.T) {
	i := newItg(t, "agent")
	catalogFixtures(i.f)
	if code, m := i.run("catalog", "search", "laptop"); code != 0 || len(items(m)) != 1 {
		t.Fatalf("search exit %d: %s", code, i.out.String())
	}
	if code, _ := i.run("catalog", "get", itItem); code != 0 {
		t.Fatalf("get exit %d", code)
	}
	if code, _ := i.run("catalog", "vars", itItem); code != 0 {
		t.Fatalf("vars exit %d", code)
	}
	code, m := i.run("catalog", "order", itItem, "--var", "model=x1")
	if code != 0 || i.f.Posts() != 0 || data(m)["dry_run"] != true {
		t.Fatalf("default order must be dry-run only: exit %d posts %d: %s", code, i.f.Posts(), i.out.String())
	}
}

func optedInPolicy(t *testing.T) string {
	t.Helper()
	b, err := policies.Named("agent")
	if err != nil {
		t.Fatal(err)
	}
	rule := "  - { id: order-laptop, effect: allow, mode: allow, verbs: [order], resources: [\"catalog:item:" + itItem + "\"], rate_limit: { per_run: 10 } }\n"
	s := string(b)
	idx := strings.Index(s, "  - id: catalog-order-dry-run")
	if idx < 0 {
		t.Fatal("shipped policy no longer has the dry-run order rule")
	}
	return s[:idx] + rule + s[idx:]
}

func TestIntegrationM4OptedInOrderValidatesVarsAndPostIsNeverRetried(t *testing.T) {
	i := newItg(t, "agent", withPolicyText(optedInPolicy(t)))
	catalogFixtures(i.f)
	if code, _ := i.run("catalog", "order", itItem); code != 9 {
		t.Fatalf("missing mandatory variable: exit %d", code)
	}
	if i.f.Posts() != 0 {
		t.Fatal("validation failure must not POST")
	}
	if code, _ := i.run("catalog", "order", itItem, "--var", "model=x1"); code != 0 || i.f.Posts() != 1 {
		t.Fatalf("opted-in order: exit %d posts %d: %s", code, i.f.Posts(), i.out.String())
	}
	// A 503 on the order POST makes exactly one attempt (A-07).
	i.f.Fail("POST", "/api/sn_sc/servicecatalog/items/"+itItem+"/order_now", 5, snfake.Error(503, "busy"))
	before := i.f.Posts()
	code, _ := i.run("catalog", "order", itItem, "--var", "model=x1")
	if code != 8 || i.f.Posts() != before+1 {
		t.Fatalf("503 on order: exit %d posts %d->%d", code, before, i.f.Posts())
	}
}

func TestIntegrationM4ChangeReadsExistAndChangeCreateDoesNot(t *testing.T) {
	i := newItg(t, "agent")
	i.f.On("GET", "/api/now/v1/table/change_request", snfake.Records(1, map[string]any{"sys_id": itSID, "number": "CHG1"}))
	if code, _ := i.run("change", "list", "--fields", "number"); code != 0 {
		t.Fatalf("change list exit %d: %s", code, i.out.String())
	}
	if code, _ := i.run("change", "get", "CHG1", "--fields", "number"); code != 0 && code != 5 {
		t.Fatalf("change get exit %d", code)
	}
	if code, _ := i.run("change", "create", "--short-description", "x"); code != 2 {
		t.Fatalf("change create must be a usage error, exit %d", code)
	}
}

// ---- Mode wiring (verification step 5) ----

func TestIntegrationHumanModeUsesCredentialStore(t *testing.T) {
	h := newItg(t, "human")
	// Default store is a fail-closed stub on this OS: exit 3 naming the escape hatch or login.
	if code, _ := h.run("whoami"); code != 3 {
		t.Fatalf("human without a session: exit %d: %s", code, h.out.String())
	}
}

// TestAssumptionA10JournalFieldsReadableAndWritable documents A-10 (ASSUMPTION
// unverified against a real instance): work_notes and comments are readable
// and writable under their own field ACLs. When the write ACL is missing the
// PATCH is refused by the server (exit 4); when the read ACL is missing the
// fields are simply absent from the record.
func TestAssumptionA10JournalFieldsReadableAndWritable(t *testing.T) {
	i := newItg(t, "agent")
	rec := map[string]any{"sys_id": itSID, "number": "INC1", "sys_mod_count": "3", "work_notes": "n1", "comments": "c1"}
	i.f.On("GET", "/api/now/v1/table/incident/"+itSID, snfake.Response{JSON: map[string]any{"result": rec}})
	code, m := i.run("incident", "get", itSID, "--fields", "number,work_notes,comments")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, i.out.String())
	}
	_ = m
	if !strings.Contains(i.out.String(), "work_notes") || !strings.Contains(i.out.String(), "comments") {
		t.Errorf("journal fields not returned: %s", i.out.String())
	}
	i.f.On("PATCH", "/api/now/v1/table/incident/"+itSID, snfake.Response{JSON: map[string]any{"result": map[string]any{"sys_id": itSID}}})
	if code, _ := i.run("incident", "update", itSID, "--work-note", "hello"); code != 0 {
		t.Fatalf("write exit %d: %s", code, i.out.String())
	}
	i.f.On("PATCH", "/api/now/v1/table/incident/"+itSID, snfake.Error(403, "Operation against file 'incident' is not permitted: field work_notes"))
	if code, _ := i.run("incident", "update", itSID, "--work-note", "hello"); code != 4 {
		t.Errorf("missing journal write ACL must exit 4, got %d", code)
	}
}

// Every read command passes the shipped agent and human policies (WS-B
// request 3): no command hits default deny.
func TestIntegrationShippedPoliciesAllowEveryReadCommand(t *testing.T) {
	for _, mode := range []string{"agent", "human"} {
		t.Run(mode, func(t *testing.T) {
			i := newItg(t, mode)
			if mode == "human" {
				i.seedHuman(itToken, time.Now().Add(time.Hour))
			}
			rec := map[string]any{"sys_id": itSID, "number": "X1", "name": "db01", "sys_class_name": "cmdb_ci_server", "sys_mod_count": "1", "parent": itSID, "child": itSID2, "type": "Runs on"}
			for _, tbl := range []string{"incident", "sc_request", "sc_req_item", "task", "change_request", "problem", "cmdb_ci", "cmdb_ci_service", "cmdb_rel_ci"} {
				i.f.On("GET", "/api/now/v1/table/"+tbl, snfake.Records(1, rec))
			}
			i.f.On("GET", "/api/now/v1/table/cmdb_ci/"+itSID, snfake.Response{JSON: map[string]any{"result": rec}})
			i.f.On("GET", "/api/now/v1/table/incident/"+itSID, snfake.Response{JSON: map[string]any{"result": rec}})
			i.f.On("GET", "/api/now/v1/stats/incident", snfake.Response{JSON: map[string]any{"result": map[string]any{"stats": map[string]any{"count": "4"}}}})
			i.f.On("GET", "/api/x_corp_agent/v1/whoami", snfake.Response{JSON: map[string]any{"result": map[string]any{"user_name": "svc", "roles": []string{"x"}}}})
			catalogFixtures(i.f)
			for _, args := range [][]string{
				{"whoami"},
				{"table", "get", "incident", itSID}, {"table", "list", "incident"}, {"table", "count", "incident"},
				{"incident", "get", itSID}, {"incident", "list", "--mine"},
				{"request", "list"}, {"ritm", "list"}, {"task", "list"}, {"problem", "list"}, {"change", "list"},
				{"cmdb", "ci", "get", itSID}, {"cmdb", "ci", "search", "--query", "nameLIKEdb"},
				{"cmdb", "ci", "related", itSID}, {"cmdb", "app", "db01"},
				{"my", "work"},
				{"catalog", "search", "laptop"}, {"catalog", "get", itItem}, {"catalog", "vars", itItem}, {"catalog", "get", "Laptop"},
			} {
				code, _ := i.run(args...)
				if code == 6 {
					t.Errorf("%v: denied by the shipped %s policy: %s", args, mode, i.out.String())
				}
			}
		})
	}
}

func overrideLine(on bool) string {
	if on {
		return "      allow_override: true\n"
	}
	return ""
}

func okWhoami() snfake.Response {
	return snfake.Response{JSON: map[string]any{"result": map[string]any{"user_name": "svc.agent", "roles": []string{"x"}}}}
}

// FR-R04: every selftest server probe is audited (pending first for the
// writes), with distinct verbs; a failing audit sink sends no probe.
func TestIntegrationSelftestProbesAreAudited(t *testing.T) {
	i := newItg(t, "agent")
	selftestFixtures(i.f)
	i.f.On("PATCH", "/api/now/v1/table/incident/"+itSID, snfake.Error(403, "acl"))
	if code, _ := i.run("selftest", "--include-writes"); code != 0 {
		t.Fatalf("exit %d: %s", code, i.out.String())
	}
	verbs := map[string][]string{}
	refs := map[string]bool{}
	for _, l := range i.auditLines() {
		v, _ := l["verb"].(string)
		if r, _ := l["resource"].(string); strings.HasPrefix(v, "selftest:probe-") {
			refs[v+" "+r] = true
		}
		o, _ := l["outcome"].(string)
		verbs[v] = append(verbs[v], o)
		if strings.HasPrefix(v, "selftest:probe-") && l["policy_decision"] != "probe_bypass" {
			t.Errorf("probe record %v lacks the probe_bypass decision", l)
		}
	}
	for _, v := range []string{"selftest:probe-resolve", "selftest:probe-update"} {
		if got := verbs[v]; len(got) != 2 || got[0] != "pending" || got[1] != "error" {
			t.Errorf("%s audit outcomes = %v, want pending then error", v, got)
		}
	}
	if !refs["selftest:probe-resolve incident:INC0000001"] || !refs["selftest:probe-update incident:INC0000002"] {
		t.Errorf("probe resources lack the target ref: %v", refs)
	}
	if got := verbs["selftest:probe-list"]; len(got) != 4 { // sys_user and sys_properties: pending + error
		t.Errorf("server read probes audited %v", got)
	}
}

func TestIntegrationSelftestFailedAuditSendsNoProbe(t *testing.T) {
	i := newItg(t, "agent")
	selftestFixtures(i.f)
	i.f.On("PATCH", "/api/now/v1/table/incident/"+itSID, snfake.Error(403, "acl"))
	// Reads warn only; the first probe's pending record is the failure point.
	i.opts.AuditWriter = probeFailWriter{}
	code, _ := i.run("selftest", "--include-writes")
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	for _, r := range i.f.Requests() {
		if strings.Contains(r.Path, "sys_user") || strings.Contains(r.Path, "sys_properties") || r.Method == "PATCH" {
			t.Errorf("probe request sent after its audit record failed: %s %s", r.Method, r.Path)
		}
	}
}

// probeFailWriter fails any write whose line mentions a selftest probe.
type probeFailWriter struct{}

func (probeFailWriter) Write(p []byte) (int, error) {
	if strings.Contains(string(p), "selftest:probe-") {
		return 0, errors.New("disk full")
	}
	return len(p), nil
}
