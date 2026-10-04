package auditx

import (
	"reflect"
	"testing"

	"github.com/stainedhead/agent-cli-core/policy"
)

const allowlistPolicy = `
version: 1
rules:
  - {id: d, effect: deny, verbs: ["*"], resources: ["table:sys_*"]}
  - {id: a, effect: allow, verbs: [get, list], resources: [incident, "table:incident"], fields: [number, sys_id]}
  - {id: b, effect: allow, verbs: [get], resources: ["catalog:item:*"]}
  - {id: c, effect: allow, verbs: [get], resources: ["table:*"], fields: [name]}
`

func TestAllowedFields(t *testing.T) {
	p, err := policy.Parse([]byte(allowlistPolicy))
	if err != nil {
		t.Fatal(err)
	}
	g := &Guard{Engine: policy.NewEngine(p, nil)}
	cases := []struct {
		verb, res string
		want      []string
	}{
		{"get", "incident", []string{"number", "sys_id"}},
		{"list", "table:incident", []string{"number", "sys_id"}},
		{"get", "catalog:item:abc", nil},         // rule without allowlist
		{"get", "table:sys_user", nil},           // denied first
		{"get", "table:other", []string{"name"}}, // wildcard rule
		{"count", "incident", nil},               // no rule
	}
	for _, c := range cases {
		if got := g.AllowedFields(c.verb, c.res); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s %s: got %v want %v", c.verb, c.res, got, c.want)
		}
	}
	if (&Guard{}).AllowedFields("get", "incident") != nil {
		t.Error("nil engine must return nil")
	}
}
