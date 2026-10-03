package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stainedhead/agent-cli-core/auth"
	"github.com/stainedhead/agent-cli-core/httpx"
	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/agent-cli-core/policy"
	"github.com/stainedhead/snow-cli/internal/cli"
	"github.com/stainedhead/snow-cli/internal/domain"
	"github.com/stainedhead/snow-cli/internal/sn"
)

var update = flag.Bool("update", false, "rewrite golden files")

type harness struct {
	r        *cli.Router
	out, err bytes.Buffer
	envCalls int
	env      *cli.Env
	factErr  error
	lastG    cli.GlobalFlags
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{env: &cli.Env{Mode: domain.ModeAgent}}
	h.r = cli.NewRouter(cli.Options{
		Build:  cli.BuildInfo{Version: "1.2.3", Commit: "abc", Date: "2026-10-03"},
		Stdout: &h.out,
		Stderr: &h.err,
		EnvFactory: func(_ context.Context, g cli.GlobalFlags) (*cli.Env, error) {
			h.envCalls++
			h.lastG = g
			return h.env, h.factErr
		},
	})
	return h
}

func (h *harness) run(args ...string) int {
	h.out.Reset()
	h.err.Reset()
	return h.r.Execute(context.Background(), args)
}

func (h *harness) reg(path string, run func(context.Context, *cli.Call) (cli.Result, error)) {
	h.r.Register(cli.Command{Path: strings.Fields(path), Summary: "test " + path, Run: run})
}

func decode(t *testing.T, h *harness) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(h.out.Bytes(), &m); err != nil {
		t.Fatalf("stdout is not one JSON envelope: %v\n%s", err, h.out.String())
	}
	return m
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	p := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(p, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("missing golden %s (run with -update): %v", p, err)
	}
	if string(want) != got {
		t.Errorf("golden %s mismatch\n got: %s\nwant: %s", name, got, want)
	}
}

func ok(data any) func(context.Context, *cli.Call) (cli.Result, error) {
	return func(context.Context, *cli.Call) (cli.Result, error) { return cli.Result{Data: data}, nil }
}

func TestEnvelopeGoldenSuccess(t *testing.T) {
	h := newHarness(t)
	h.reg("demo get", ok(map[string]any{"number": "INC1", "n": 2}))
	if code := h.run("demo", "get"); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	golden(t, "success.golden.json", h.out.String())
}

func TestEnvelopeGoldenFailure(t *testing.T) {
	h := newHarness(t)
	h.reg("demo fail", func(context.Context, *cli.Call) (cli.Result, error) {
		return cli.Result{}, &sn.NotFoundError{Status: 404, Message: "No Record found"}
	})
	if code := h.run("demo", "fail"); code != 5 {
		t.Fatalf("exit = %d", code)
	}
	golden(t, "notfound.golden.json", h.out.String())
}

func TestEveryErrorCategoryMapsToItsExitCode(t *testing.T) {
	p, _ := policy.Parse([]byte("version: 1\nrules:\n  - id: r\n    effect: allow\n    verbs: [get]\n    resources: [x]\n"))
	denied := p.Evaluate(policy.Request{Verb: "update", Resource: "x"}).Err()
	tests := []struct {
		name string
		err  error
		exit int
	}{
		{"general", errors.New("boom"), 1},
		{"validation", &sn.ValidationError{Status: 400, Message: "m"}, 9},
		{"auth", &httpx.AuthError{}, 3},
		{"daemon", &auth.UnreachableError{Socket: "/s"}, 3},
		{"forbidden", &httpx.ForbiddenError{}, 4},
		{"notfound", &sn.NotFoundError{Status: 404}, 5},
		{"policy", sn.AdaptPolicyError(denied), 6},
		{"conflict", &sn.ConflictError{Status: 409}, 7},
		{"ratelimited", &httpx.RateLimitedError{Status: 503, Attempts: 1}, 8},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.reg("x y", func(context.Context, *cli.Call) (cli.Result, error) { return cli.Result{}, tc.err })
			if code := h.run("x", "y"); code != tc.exit {
				t.Errorf("exit = %d, want %d", code, tc.exit)
			}
			m := decode(t, h)
			if m["ok"] != false {
				t.Errorf("envelope = %v", m)
			}
		})
	}
}

func TestUsageErrors(t *testing.T) {
	h := newHarness(t)
	h.reg("demo get", ok("x"))
	tests := []struct {
		name string
		args []string
	}{
		{"no args", nil},
		{"unknown command", []string{"nope"}},
		{"incomplete group", []string{"demo"}},
		{"unknown flag", []string{"demo", "get", "--bogus"}},
		{"bad format", []string{"demo", "get", "--format", "yaml"}},
		{"negative limit", []string{"demo", "get", "--limit", "-1"}},
		{"negative offset", []string{"demo", "get", "--offset", "-5"}},
		{"negative max-bytes", []string{"demo", "get", "--max-bytes", "-5"}},
		{"non numeric limit", []string{"demo", "get", "--limit", "abc"}},
		{"flag missing value", []string{"demo", "get", "--limit"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if code := h.run(tc.args...); code != 2 {
				t.Errorf("exit = %d, want 2; out=%s", code, h.out.String())
			}
			m := decode(t, h)
			e, _ := m["error"].(map[string]any)
			if e["code"] != "usage" {
				t.Errorf("error = %v", m["error"])
			}
		})
	}
}

func TestUnknownFlagDoesNotCallHandlerOrFactory(t *testing.T) {
	h := newHarness(t)
	called := false
	h.reg("demo get", func(context.Context, *cli.Call) (cli.Result, error) { called = true; return cli.Result{}, nil })
	h.run("demo", "get", "--bogus")
	if called || h.envCalls != 0 {
		t.Errorf("called=%v envCalls=%d", called, h.envCalls)
	}
}

func TestGlobalFlagsParsedAnywhereAndPositionals(t *testing.T) {
	h := newHarness(t)
	var got *cli.Call
	h.reg("incident get", func(_ context.Context, c *cli.Call) (cli.Result, error) { got = c; return cli.Result{Data: "ok"}, nil })
	code := h.run("incident", "get", "INC0010001", "--format", "text", "--limit", "5", "--offset=10", "extra",
		"--max-bytes", "1000", "--dry-run", "--idempotency-key", "k", "--profile", "p", "--policy", "agent",
		"--fields", "number,state", "--yes", "--config", "/c.yaml")
	if code != 0 {
		t.Fatalf("exit = %d: %s", code, h.out.String())
	}
	g := got.Global
	if g.Format != "text" || g.Limit != 5 || g.Offset != 10 || g.MaxBytes != 1000 || !g.DryRun || g.IdempotencyKey != "k" ||
		g.Profile != "p" || g.Policy != "agent" || !g.Yes || g.Config != "/c.yaml" {
		t.Errorf("globals = %+v", g)
	}
	if strings.Join(g.FieldList(), "|") != "number|state" {
		t.Errorf("FieldList = %v", g.FieldList())
	}
	if strings.Join(got.Args, "|") != "INC0010001|extra" {
		t.Errorf("positionals = %v", got.Args)
	}
	if h.lastG.Profile != "p" {
		t.Error("factory must receive the parsed globals")
	}
	if len(cli.GlobalFlags{}.FieldList()) != 0 {
		t.Error("empty --fields must give an empty list")
	}
}

func TestDoubleDashEndsFlags(t *testing.T) {
	h := newHarness(t)
	var got *cli.Call
	h.reg("a b", func(_ context.Context, c *cli.Call) (cli.Result, error) { got = c; return cli.Result{}, nil })
	if code := h.run("a", "b", "--", "--format", "x"); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if strings.Join(got.Args, "|") != "--format|x" {
		t.Errorf("args = %v", got.Args)
	}
}

func TestCommandFlags(t *testing.T) {
	h := newHarness(t)
	var kind string
	h.r.Register(cli.Command{
		Path: []string{"my", "work"}, Summary: "s",
		Flags: func(fs *flag.FlagSet) { fs.StringVar(&kind, "kind", "all", "kind") },
		Run:   ok("fine"),
	})
	if code := h.run("my", "work", "--kind", "incident"); code != 0 || kind != "incident" {
		t.Errorf("exit=%d kind=%q", code, kind)
	}
}

func TestFormats(t *testing.T) {
	h := newHarness(t)
	h.reg("l l", ok([]map[string]any{{"a": "1"}, {"a": "2"}}))
	for _, f := range []string{"json", "table", "text"} {
		if code := h.run("l", "l", "--format", f); code != 0 {
			t.Errorf("%s exit = %d", f, code)
		}
		if f == "table" && !strings.Contains(h.out.String(), "count=2") {
			t.Errorf("table output = %q", h.out.String())
		}
	}
}

func TestMaxBytesTruncatesAndLimitsClamp(t *testing.T) {
	h := newHarness(t)
	items := make([]map[string]any, 50)
	for i := range items {
		items[i] = map[string]any{"v": strings.Repeat("x", 40)}
	}
	h.reg("big list", ok(items))
	if code := h.run("big", "list", "--max-bytes", "600"); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	meta := decode(t, h)["meta"].(map[string]any)
	if meta["truncated"] != true || meta["next_offset"] == nil {
		t.Errorf("meta = %v", meta)
	}
	// policy limits clamp --max-bytes down
	h.env.Limits = policy.Limits{MaxBytes: 300}
	h.run("big", "list", "--max-bytes", "5000")
	if len(h.out.Bytes()) > 300 {
		t.Errorf("output %d bytes exceeds policy clamp of 300", len(h.out.Bytes()))
	}
	// too small a bound is a usage error envelope, exit 2
	if code := h.run("big", "list", "--max-bytes", "1"); code != 2 {
		t.Errorf("tiny bound exit = %d", code)
	}
}

func TestTraceOnlyInHumanMode(t *testing.T) {
	h := newHarness(t)
	h.reg("a b", ok("x"))
	if code := h.run("a", "b", "--trace"); code != 6 {
		t.Errorf("agent --trace exit = %d, want 6", code)
	}
	h.env = &cli.Env{Mode: domain.ModeHuman}
	if code := h.run("a", "b", "--trace"); code != 0 {
		t.Errorf("human --trace exit = %d", code)
	}
}

func TestFactoryErrorAndNoEnvCommands(t *testing.T) {
	h := newHarness(t)
	h.reg("a b", ok("x"))
	h.r.Register(cli.Command{Path: []string{"noenv"}, Summary: "s", NoEnv: true, Run: ok("fine")})
	h.factErr = &auth.UnreachableError{Socket: "/sock"}
	if code := h.run("a", "b"); code != 3 {
		t.Errorf("factory error exit = %d", code)
	}
	if code := h.run("noenv"); code != 0 || h.envCalls != 1 {
		t.Errorf("noenv exit=%d envCalls=%d (factory must not run)", code, h.envCalls)
	}
}

func TestHelp(t *testing.T) {
	h := newHarness(t)
	h.reg("demo get", ok("x"))
	h.r.Register(cli.Command{Path: []string{"demo", "put"}, Summary: "puts", Usage: "snow demo put <x>", Examples: []string{"snow demo put 1"}, Run: ok("x")})
	for _, args := range [][]string{{"help"}, {"--help"}, {"-h"}} {
		if code := h.run(args...); code != 0 || !strings.Contains(h.out.String(), "demo get") {
			t.Errorf("%v: exit=%d out=%q", args, code, h.out.String())
		}
	}
	if code := h.run("demo", "put", "-h"); code != 0 || !strings.Contains(h.out.String(), "snow demo put <x>") {
		t.Errorf("command help exit=%d out=%q", code, h.out.String())
	}
	if code := h.run("help", "demo", "put"); code != 0 || !strings.Contains(h.out.String(), "puts") {
		t.Errorf("help <cmd> exit=%d out=%q", code, h.out.String())
	}
	if h.envCalls != 0 {
		t.Error("help must not build the environment")
	}
}

func TestRegisterRejectsDuplicatesAndBadCommands(t *testing.T) {
	h := newHarness(t)
	h.reg("a b", ok("x"))
	for name, c := range map[string]cli.Command{
		"duplicate": {Path: []string{"a", "b"}, Run: ok("x")},
		"empty":     {Path: nil, Run: ok("x")},
		"no run":    {Path: []string{"z"}},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: Register should panic", name)
				}
			}()
			h.r.Register(c)
		}()
	}
}

func TestCommandTreeForDocgen(t *testing.T) {
	h := newHarness(t)
	h.r.Register(cli.Command{Path: []string{"incident", "get"}, Summary: "gets", Usage: "u", Examples: []string{"e"}, Forbidden: []string{"never"}, Run: ok("x")})
	tree := h.r.CommandTree("snow", "desc")
	if tree.Name != "snow" || len(tree.Commands) != 1 || tree.Commands[0].Name != "incident get" ||
		tree.Commands[0].Forbidden[0] != "never" || tree.Commands[0].Description != "gets" {
		t.Errorf("tree = %+v", tree)
	}
}

func TestAreaStubsRegisterWithoutPanic(t *testing.T) {
	cli.RegisterAll(cli.NewRouter(cli.Options{Stdout: &bytes.Buffer{}}))
	// Area stubs exist and may be empty today; calling them twice on fresh routers must not panic.
	r2 := cli.NewRouter(cli.Options{Stdout: &bytes.Buffer{}})
	cli.RegisterRead(r2)
	cli.RegisterWrite(r2)
	cli.RegisterAuth(r2)
	cli.RegisterSelftest(r2)
	cli.RegisterSkill(r2)
}

func TestHandlerPanicIsNotSwallowedButEnvelopeForNilResult(t *testing.T) {
	h := newHarness(t)
	h.reg("n n", func(context.Context, *cli.Call) (cli.Result, error) { return cli.Result{}, nil })
	if code := h.run("n", "n"); code != 0 {
		t.Errorf("exit = %d", code)
	}
	if decode(t, h)["ok"] != true {
		t.Error("nil data is still a success envelope")
	}
}

func TestMetaPassedThrough(t *testing.T) {
	h := newHarness(t)
	h.reg("m m", func(context.Context, *cli.Call) (cli.Result, error) {
		return cli.Result{Data: []int{1}, Meta: &output.Meta{RequestID: "req-1"}}, nil
	})
	h.run("m", "m")
	meta := decode(t, h)["meta"].(map[string]any)
	if meta["request_id"] != "req-1" {
		t.Errorf("meta = %v", meta)
	}
}
