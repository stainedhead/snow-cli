package read_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/snow-cli/internal/usecase/read"
)

func TestParseQueryAccepts(t *testing.T) {
	cases := []struct {
		q      string
		fields []string
	}{
		{"", nil},
		{"active=true", []string{"active"}},
		{"active=true^priority<=2", []string{"active", "priority"}},
		{"state=1^ORstate=2", []string{"state"}},
		{"assigned_to.user_name=abel.tuter^stateIN1,2,3", []string{"assigned_to.user_name", "state"}},
		{"short_descriptionLIKEvpn^ORDERBYDESCsys_updated_on", []string{"short_description", "sys_updated_on"}},
		{"nameSTARTSWITHweb^descriptionISNOTEMPTY", []string{"name", "description"}},
		{"opened_at>=2026-01-01 00:00:00^number!=INC1", []string{"number", "opened_at"}},
		{"nameLIKEgsx", []string{"name"}},
		{"short_descriptionLIKEbugs.in the app", []string{"short_description"}},
	}
	for _, c := range cases {
		info, err := read.ParseQuery(c.q)
		if err != nil {
			t.Errorf("%q: %v", c.q, err)
			continue
		}
		got := sorted(info.Fields)
		want := sorted(c.fields)
		if len(got) == 0 && len(want) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%q: fields %v, want %v", c.q, got, want)
		}
	}
}

func TestParseQueryRejects(t *testing.T) {
	cases := map[string]string{
		"new query":             "active=true^NQactive=false",
		"new query mid":         "a=1^NQ",
		"end query":             "a=1^EQ",
		"dynamic":               "assigned_toDYNAMICde4c0000",
		"dynamic lower":         "assigned_todynamicx",
		"javascript prefix":     "assigned_to=javascript:gs.getUserID()",
		"javascript mixed case": "a=JaVaScRiPt:1",
		"javascript percent":    "a=%6aavascript:1",
		"javascript double enc": "a=%256aavascript:1",
		"gs dot":                "a=GS.getUserID()",
		"gs dot encoded":        "a=%67s.log(1)",
		"script tag":            "name=<SCRIPT>alert(1)</script>",
		"newline":               "a=1\n^b=2",
		"cr":                    "a=1\r",
		"nul":                   "a=1\x00",
		"tab":                   "a=1\tb",
		"del":                   "a=\x7f",
		"no operator":           "justtext",
		"empty clause":          "a=1^^b=2",
		"trailing caret":        "a=1^",
		"leading caret":         "^a=1",
		"unknown operator":      "aFOOBARb",
		"bad field":             "a b=1",
		"or alone":              "a=1^OR",
		"orderby empty":         "a=1^ORDERBY",
		"orderby bad field":     "ORDERBYa;b",
		"field upper":           "A=1",
		"semicolon field":       "a;b=1",
		"parens":                "(a=1)",
	}
	for name, q := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := read.ParseQuery(q)
			if err == nil {
				t.Fatalf("%q accepted", q)
			}
			if output.ExitOf(err) != output.ExitValidation {
				t.Fatalf("%q: exit %d", q, output.ExitOf(err))
			}
		})
	}
}

func TestParseQueryOrderBy(t *testing.T) {
	info, _ := read.ParseQuery("a=1^ORDERBYnumber")
	if !info.HasOrderBy {
		t.Error("ORDERBY clause not detected")
	}
	info, _ = read.ParseQuery("short_descriptionLIKEORDERBYx")
	if info.HasOrderBy {
		t.Error("ORDERBY inside a value must not count")
	}
}

func TestListQueryFieldsGoToPolicy(t *testing.T) {
	const pol = `
version: 1
rules:
  - {id: r, effect: allow, verbs: [list, count], resources: ["table:incident"], fields: [number, state, short_description]}
`
	ctx := context.Background()
	// A clause on a field outside the allowlist is denied (exit 6) with no HTTP.
	ft := incidentTables()
	_, err := svc(t, ft, newGuard(t, pol)).TableList(ctx, "incident", read.ListOptions{Fields: []string{"number"}, Query: "secret_field=1"})
	if exitOf(t, err) != output.ExitPolicyDenied || len(ft.lists) != 0 {
		t.Errorf("clause field: exit %d, lists %d", output.ExitOf(err), len(ft.lists))
	}
	// So is a dot-walked field and --order-by.
	_, err = svc(t, ft, newGuard(t, pol)).TableList(ctx, "incident", read.ListOptions{Fields: []string{"number"}, Query: "caller_id.password=x"})
	if exitOf(t, err) != output.ExitPolicyDenied {
		t.Error("dot-walked clause must be denied")
	}
	_, err = svc(t, ft, newGuard(t, pol)).TableList(ctx, "incident", read.ListOptions{Fields: []string{"number"}, OrderBy: "-secret_field"})
	if exitOf(t, err) != output.ExitPolicyDenied || len(ft.lists) != 0 {
		t.Errorf("order-by field: exit %d", output.ExitOf(err))
	}
	// ORDERBY clause inside --query too.
	_, err = svc(t, ft, newGuard(t, pol)).TableList(ctx, "incident", read.ListOptions{Fields: []string{"number"}, Query: "state=1^ORDERBYsecret_field"})
	if exitOf(t, err) != output.ExitPolicyDenied {
		t.Error("ORDERBY field in query must be denied")
	}
	// Allowed fields pass, and the guard saw the clause fields.
	g := newGuard(t, pol)
	if _, err = svc(t, ft, g).TableList(ctx, "incident", read.ListOptions{Fields: []string{"number"}, Query: "state=1", OrderBy: "number"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := g.Requests[0].Fields["state"]; !ok {
		t.Errorf("request fields %v lack the query field", g.Requests[0].Fields)
	}
	// count --query is field-checked.
	ft = incidentTables()
	_, err = svc(t, ft, newGuard(t, pol)).TableCount(ctx, "incident", "secret_field=1")
	if exitOf(t, err) != output.ExitPolicyDenied || len(ft.counts) != 0 {
		t.Errorf("count: exit %d counts %d", output.ExitOf(err), len(ft.counts))
	}
	if _, err = svc(t, ft, newGuard(t, pol)).TableCount(ctx, "incident", "state=1"); err != nil {
		t.Fatal(err)
	}
}

func TestListRejectsNQBeforeAnyRequest(t *testing.T) {
	ft := incidentTables()
	g := newGuard(t, allowAll)
	_, err := svc(t, ft, g).WorkList(context.Background(), read.KindIncident, read.WorkFilter{Mine: true}, read.ListOptions{Query: "active=true^NQactive=false"})
	if exitOf(t, err) != output.ExitValidation || len(ft.lists) != 0 || len(g.Requests) != 0 {
		t.Errorf("exit %d lists %d reqs %d", output.ExitOf(err), len(ft.lists), len(g.Requests))
	}
}
