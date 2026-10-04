package cli

import (
	"context"
	"flag"

	"github.com/stainedhead/agent-cli-core/output"
)

// failedError is a failing matrix: a general-category failure, so the process
// exits 1 (not 4 or 6) as the core selftest does. The message names each
// failing row.
type failedError struct{ msg string }

func (e *failedError) Error() string           { return e.msg }
func (*failedError) Category() output.Category { return output.CategoryGeneral }
func (*failedError) Hint() string {
	return "Fix the failing rows or the instance configuration (roles, ACLs, policy), then rerun `snow selftest`."
}

// RegisterSelftest registers `snow selftest` (FR-050).
func RegisterSelftest(r *Router) {
	r.Register(Command{
		Path:    []string{"selftest"},
		Summary: "Probe the allow/deny matrix for this identity (read-only unless --include-writes).",
		Usage:   "snow selftest [--profile <name>] [--include-writes]",
		Examples: []string{
			"snow selftest",
			"snow selftest --include-writes",
		},
		Forbidden: []string{"Do not use --include-writes against production data; it needs the fixture incidents named in the profile."},
		Flags: func(fs *flag.FlagSet) {
			fs.Bool("include-writes", false, "also run the two write rows against the configured fixture incidents")
		},
		Run: func(ctx context.Context, c *Call) (Result, error) {
			if c.Env.Selftest == nil {
				return Result{}, &UsageError{Msg: "selftest is not wired in this build"}
			}
			svc := *c.Env.Selftest
			svc.IncludeWrites = c.Flags.Lookup("include-writes").Value.String() == "true"
			res, err := svc.Run(ctx)
			if err != nil {
				return Result{}, err
			}
			if !res.OK() {
				return Result{}, &failedError{msg: res.Envelope().Error.Message}
			}
			return Result{Data: res, Meta: &output.Meta{Count: len(res.Rows)}}, nil
		},
	})
}
