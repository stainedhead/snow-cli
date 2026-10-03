package sn

import (
	"errors"
	"strings"
	"testing"

	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/agent-cli-core/policy"
)

func TestDeniedAdapter(t *testing.T) {
	p, err := policy.Parse([]byte("version: 1\nrules:\n  - id: r1\n    effect: allow\n    verbs: [get]\n    resources: [incident]\n"))
	if err != nil {
		t.Fatal(err)
	}
	d := p.Evaluate(policy.Request{Verb: "resolve", Resource: "incident"})
	err = AdaptPolicyError(d.Err())
	if output.ExitOf(err) != output.ExitPolicyDenied {
		t.Fatalf("exit = %d", output.ExitOf(err))
	}
	var de *policy.DeniedError
	if !errors.As(err, &de) || de.Decision.Allowed {
		t.Error("original policy.DeniedError must stay reachable")
	}
	env := output.FromError(err)
	if env.Error == nil || env.Error.Code != output.CategoryPolicyDenied || env.Error.Hint == "" {
		t.Errorf("envelope = %+v", env.Error)
	}
	if !strings.Contains(err.Error(), "denied") {
		t.Errorf("message = %q", err)
	}
}

func TestAdaptPolicyErrorPassthrough(t *testing.T) {
	if AdaptPolicyError(nil) != nil {
		t.Error("nil stays nil")
	}
	plain := errors.New("x")
	if AdaptPolicyError(plain) != plain {
		t.Error("non-policy errors pass through unchanged")
	}
}
