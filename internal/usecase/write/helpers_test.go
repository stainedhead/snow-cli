package write

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/agent-cli-core/policy"
	"github.com/stainedhead/snow-cli/internal/domain"
	"github.com/stainedhead/snow-cli/internal/usecase"
)

// engineGuard runs the real core policy engine like auditx.Guard, minus audit.
type engineGuard struct {
	eng     *policy.Engine
	actions []usecase.Action
}

func (g *engineGuard) AllowedFields(string, string) []string { return nil }

func (g *engineGuard) Run(ctx context.Context, a usecase.Action, fn usecase.ActionFunc) error {
	g.actions = append(g.actions, a)
	d := g.eng.Check(a.Request)
	if !d.Allowed && !d.DryRunOnly() {
		return &DeniedError{Msg: d.Err().Error()}
	}
	_, err := fn(ctx, d)
	return err
}

func mustPolicy(t *testing.T, yaml string) *engineGuard {
	t.Helper()
	p, err := policy.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	return &engineGuard{eng: policy.NewEngine(p, nil)}
}

const allowAll = `
version: 1
rules:
  - {id: all, effect: allow, verbs: ["*"], resources: ["*"]}
`

type fixedClock struct{}

func (fixedClock) Now() time.Time { return time.Date(2026, 10, 3, 10, 15, 0, 0, time.UTC) }

func base(g usecase.Guard) Base {
	return Base{Guard: g, Clock: fixedClock{}, AgentID: "agent-1", RunID: "run-1"}
}

func wantCategory(t *testing.T, err error, want output.Category) {
	t.Helper()
	if err == nil {
		t.Fatalf("want %s error, got nil", want)
	}
	if got := output.CategoryOf(err); got != want {
		t.Fatalf("category %s, want %s (%v)", got, want, err)
	}
}

var errBoom = errors.New("boom")

func rec(fields map[string]string) domain.Record {
	return domain.Record{Table: "incident", Fields: fields}
}

const denyAll = `
version: 1
rules:
  - {id: none, effect: deny, verbs: ["*"], resources: ["*"]}
`
