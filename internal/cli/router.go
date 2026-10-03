package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"

	"github.com/stainedhead/agent-cli-core/docgen"
	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/agent-cli-core/policy"
	"github.com/stainedhead/snow-cli/internal/domain"
	"github.com/stainedhead/snow-cli/internal/sn"
)

// Command is one leaf command, addressed by its Path ("incident", "get").
type Command struct {
	Path      []string
	Summary   string
	Usage     string
	Examples  []string
	Forbidden []string
	// NoEnv commands (version, help-like) never build the Env.
	NoEnv bool
	// Flags registers command-specific flags next to the global ones.
	Flags func(fs *flag.FlagSet)
	// Run executes the command. Positional arguments are in Call.Args.
	Run func(ctx context.Context, c *Call) (Result, error)
}

// Name is the space-joined path.
func (c Command) Name() string { return strings.Join(c.Path, " ") }

// Call is what Run receives.
type Call struct {
	Env    *Env // nil for NoEnv commands
	Global GlobalFlags
	Args   []string
	Flags  *flag.FlagSet
	Build  BuildInfo
}

// Result is a command's success value; the router wraps it in the envelope.
type Result struct {
	Data any
	Meta *output.Meta
}

// Options builds a Router.
type Options struct {
	Build      BuildInfo
	EnvFactory EnvFactory
	Stdout     io.Writer
	Stderr     io.Writer
}

// Router dispatches commands.
type Router struct {
	opts Options
	cmds []Command
}

// NewRouter returns an empty router.
func NewRouter(o Options) *Router {
	if o.Stdout == nil {
		o.Stdout = io.Discard
	}
	if o.Stderr == nil {
		o.Stderr = io.Discard
	}
	return &Router{opts: o}
}

// Register adds a command. It panics on programmer errors (empty path, no
// Run, duplicate path) so they surface in tests.
func (r *Router) Register(c Command) {
	if len(c.Path) == 0 || c.Run == nil {
		panic("cli: command needs a path and a Run function")
	}
	for _, e := range r.cmds {
		if slices.Equal(e.Path, c.Path) {
			panic("cli: duplicate command " + c.Name())
		}
	}
	r.cmds = append(r.cmds, c)
}

// Commands returns the registered commands sorted by name.
func (r *Router) Commands() []Command {
	out := append([]Command(nil), r.cmds...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

// CommandTree converts the registry for docgen (FR-051).
func (r *Router) CommandTree(name, description string) docgen.CommandTree {
	t := docgen.CommandTree{Name: name, Description: description}
	for _, c := range r.Commands() {
		t.Commands = append(t.Commands, docgen.Command{
			Name: c.Name(), Description: c.Summary, Usage: c.Usage,
			Examples: c.Examples, Forbidden: c.Forbidden,
		})
	}
	return t
}

// find returns the command whose path is the longest prefix of args.
func (r *Router) find(args []string) (*Command, []string) {
	var best *Command
	for i := range r.cmds {
		c := &r.cmds[i]
		if len(c.Path) <= len(args) && slices.Equal(c.Path, args[:len(c.Path)]) {
			if best == nil || len(c.Path) > len(best.Path) {
				best = c
			}
		}
	}
	if best == nil {
		return nil, args
	}
	return best, args[len(best.Path):]
}

// Execute runs one invocation and returns the process exit code. The
// envelope always goes to stdout.
func (r *Router) Execute(ctx context.Context, args []string) int {
	if len(args) == 0 {
		return r.fail(usageErr("no command given"), GlobalFlags{})
	}
	switch args[0] {
	case "help", "--help", "-h":
		return r.help(args[1:])
	}
	cmd, rest := r.find(args)
	if cmd == nil {
		if r.isGroup(args) {
			return r.fail(&UsageError{Msg: fmt.Sprintf("incomplete command %q", strings.Join(args, " ")), Hnt: "Run `snow help` to list subcommands."}, GlobalFlags{})
		}
		return r.fail(&UsageError{Msg: fmt.Sprintf("unknown command %q", args[0]), Hnt: "Run `snow help` to list commands."}, GlobalFlags{})
	}

	var g GlobalFlags
	fs := flag.NewFlagSet(cmd.Name(), flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	g.register(fs)
	if cmd.Flags != nil {
		cmd.Flags(fs)
	}
	pos, err := parseInterleaved(fs, rest)
	if errors.Is(err, flag.ErrHelp) {
		return r.commandHelp(cmd)
	}
	if err != nil {
		return r.fail(&UsageError{Msg: err.Error(), Hnt: "Run `snow help " + cmd.Name() + "` for usage."}, g)
	}
	if err := g.validate(); err != nil {
		return r.fail(err, g)
	}

	call := &Call{Global: g, Args: pos, Flags: fs, Build: r.opts.Build}
	var limits policy.Limits
	if !cmd.NoEnv {
		if r.opts.EnvFactory == nil {
			return r.fail(errors.New("no environment factory configured"), g)
		}
		env, err := r.opts.EnvFactory(ctx, g)
		if err != nil {
			return r.fail(err, g)
		}
		call.Env = env
		limits = env.Limits
		if g.Trace && env.Mode != domain.ModeHuman {
			return r.fail(sn.AdaptPolicyError(&policy.DeniedError{Decision: policy.Decision{
				RuleID: "cli-trace-human-only", Reason: "--trace is only available in human mode",
			}}), g)
		}
	}
	res, err := cmd.Run(ctx, call)
	if err != nil {
		return Render(r.opts.Stdout, output.FromError(err), g, limits)
	}
	return Render(r.opts.Stdout, output.Success(res.Data, res.Meta), g, limits)
}

func (r *Router) fail(err error, g GlobalFlags) int {
	return Render(r.opts.Stdout, output.FromError(err), g, policy.Limits{})
}

func (r *Router) isGroup(args []string) bool {
	for _, c := range r.cmds {
		if len(c.Path) > len(args) && slices.Equal(c.Path[:len(args)], args) {
			return true
		}
	}
	return false
}

// parseInterleaved parses flags that may appear before, between and after
// positional arguments; "--" ends flag parsing.
func parseInterleaved(fs *flag.FlagSet, args []string) ([]string, error) {
	var tail []string
	if i := slices.Index(args, "--"); i >= 0 {
		tail = args[i+1:]
		args = args[:i]
	}
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			break
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
	return append(pos, tail...), nil
}

func (r *Router) help(args []string) int {
	w := r.opts.Stdout
	if len(args) > 0 {
		if cmd, rest := r.find(args); cmd != nil && len(rest) == 0 {
			return r.commandHelp(cmd)
		}
	}
	_, _ = fmt.Fprintln(w, "snow - ServiceNow CLI for agents and humans")
	_, _ = fmt.Fprintln(w, "\nCommands:")
	for _, c := range r.Commands() {
		_, _ = fmt.Fprintf(w, "  %-24s %s\n", c.Name(), c.Summary)
	}
	_, _ = fmt.Fprintln(w, "\nGlobal flags go after the command: --format --fields --limit --offset --max-bytes --dry-run --idempotency-key --profile --policy --trace --config --yes")
	return 0
}

func (r *Router) commandHelp(c *Command) int {
	w := r.opts.Stdout
	_, _ = fmt.Fprintf(w, "snow %s - %s\n", c.Name(), c.Summary)
	if c.Usage != "" {
		_, _ = fmt.Fprintf(w, "\nUsage: %s\n", c.Usage)
	}
	for _, e := range c.Examples {
		_, _ = fmt.Fprintf(w, "  %s\n", e)
	}
	return 0
}
