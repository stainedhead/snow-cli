package cli

import (
	"bytes"
	"context"
	"testing"
)

func TestSelftestNeedsWiring(t *testing.T) {
	var out bytes.Buffer
	r := NewRouter(Options{Stdout: &out, EnvFactory: func(context.Context, GlobalFlags) (*Env, error) {
		return &Env{}, nil
	}})
	RegisterSelftest(r)
	if code := r.Execute(context.Background(), []string{"selftest"}); code != 2 {
		t.Fatalf("exit %d: %s", code, out.String())
	}
}
