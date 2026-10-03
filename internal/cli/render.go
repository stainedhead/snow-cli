package cli

import (
	"io"

	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/agent-cli-core/policy"
)

// Render writes env through output.Write and returns the exit code
// (FR-004). A write failure (for example a bound too small) is itself
// rendered as an error envelope.
func Render(w io.Writer, env output.Envelope, g GlobalFlags, limits policy.Limits) int {
	format, err := output.ParseFormat(g.Format)
	if err != nil {
		format = output.FormatJSON
	}
	maxBytes := g.MaxBytes
	if maxBytes == 0 {
		maxBytes = output.DefaultMaxBytes
	}
	maxBytes = limits.ClampBytes(maxBytes)
	opts := output.Options{Format: format, Bounds: output.Bounds{MaxBytes: maxBytes}}
	if err := output.Write(w, env, opts); err != nil {
		fail := output.FromError(err)
		_ = output.Write(w, fail, output.Options{Format: format})
		return int(fail.ExitCode())
	}
	return int(env.ExitCode())
}
