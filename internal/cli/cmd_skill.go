package cli

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/stainedhead/agent-cli-core/docgen"
)

// SkillDescription is the tool description in the generated skill.
const SkillDescription = "Task-shaped ServiceNow access for autonomous SDLC agents and human teammates: " +
	"read tables, look up CMDB configuration items, and read, create and update work items. " +
	"No raw REST passthrough; ServiceNow roles and ACLs are the security boundary."

// DefaultSkillPath is where `snow skill generate` writes without --out
// (git-ignored).
const DefaultSkillPath = "dist/snow-cli.md"

// skillResult is the data of a successful skill generate.
type skillResult struct {
	Path  string `json:"path"`
	Bytes int    `json:"bytes"`
	Check bool   `json:"check,omitempty"`
}

// driftError reports a stale or missing skill file (exit 1).
type driftError struct{ msg string }

func (e *driftError) Error() string { return e.msg }
func (*driftError) Hint() string {
	return "Run `make skill` to regenerate, then compare with the committed copy or the root skill."
}

// RegisterSkill registers the skill commands (FR-051). The skill is generated
// by agent-cli-core docgen from this router's command tree, so commands,
// flags and forbidden actions cannot drift from the code.
func RegisterSkill(r *Router) {
	var out, check string
	r.Register(Command{
		Path:    []string{"skill", "generate"},
		Summary: "Generate the agent skill document from the command tree.",
		Usage:   "snow skill generate [--out <file>] [--check <file>]",
		Examples: []string{
			"snow skill generate",
			"snow skill generate --out dist/snow-cli.md",
			"snow skill generate --check dist/snow-cli.md",
		},
		NoEnv: true,
		Flags: func(fs *flag.FlagSet) {
			fs.StringVar(&out, "out", DefaultSkillPath, "output file for the skill document")
			fs.StringVar(&check, "check", "", "do not write; fail (exit 1) if this file differs from the generated skill")
		},
		Run: func(_ context.Context, _ *Call) (Result, error) {
			doc, err := docgen.Generate(r.CommandTree("snow", SkillDescription))
			if err != nil {
				return Result{}, err
			}
			if check != "" {
				have, err := os.ReadFile(check)
				if err != nil {
					return Result{}, &driftError{msg: fmt.Sprintf("cannot read %s: %v", check, err)}
				}
				if !bytes.Equal(have, doc) {
					return Result{}, &driftError{msg: fmt.Sprintf("%s differs from the generated skill", check)}
				}
				return Result{Data: skillResult{Path: check, Bytes: len(doc), Check: true}}, nil
			}
			if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
				return Result{}, err
			}
			if err := os.WriteFile(out, doc, 0o644); err != nil {
				return Result{}, err
			}
			return Result{Data: skillResult{Path: out, Bytes: len(doc)}}, nil
		},
	})
}
