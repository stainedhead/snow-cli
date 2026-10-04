package cli

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/snow-cli/internal/domain"
	"github.com/stainedhead/snow-cli/internal/usecase/write"
)

// kvFlag collects repeatable name=value flags.
type kvFlag struct{ m map[string]string }

func (k *kvFlag) String() string { return "" }
func (k *kvFlag) Set(s string) error {
	name, val, ok := strings.Cut(s, "=")
	if !ok || strings.TrimSpace(name) == "" {
		return fmt.Errorf("%q is not name=value", s)
	}
	if k.m == nil {
		k.m = map[string]string{}
	}
	k.m[strings.TrimSpace(name)] = val
	return nil
}

func flagStr(c *Call, name string) string { return c.Flags.Lookup(name).Value.String() }

func flagInt(c *Call, name string) int {
	var n int
	_, _ = fmt.Sscanf(c.Flags.Lookup(name).Value.String(), "%d", &n)
	return n
}

// expectedModCount reads the optional --expected-mod-count flag (FR-R08): 0
// when absent; a negative or zero explicit value is a validation error.
func expectedModCount(c *Call) (int, error) {
	if !flagSet(c, "expected-mod-count") {
		return 0, nil
	}
	n := flagInt(c, "expected-mod-count")
	if n < 1 {
		return 0, &write.ValidationError{Msg: "--expected-mod-count must be a positive integer"}
	}
	return n, nil
}

func flagSet(c *Call, name string) bool {
	set := false
	c.Flags.Visit(func(f *flag.Flag) {
		if f.Name == name {
			set = true
		}
	})
	return set
}

func addModCountFlag(fs *flag.FlagSet) {
	fs.Int("expected-mod-count", 0, "refuse to write unless the record's sys_mod_count equals N (exit 7, nothing applied)")
}

func flagKV(c *Call, name string) map[string]string {
	kv, _ := c.Flags.Lookup(name).Value.(*kvFlag)
	if kv == nil {
		return nil
	}
	return kv.m
}

// DeclinedError is a confirmation the human answered no (exit 1).
type DeclinedError struct{}

func (*DeclinedError) Error() string             { return "cancelled: the write was not confirmed" }
func (*DeclinedError) Category() output.Category { return output.CategoryGeneral }
func (*DeclinedError) Hint() string              { return "Nothing was sent. Re-run and answer y, or pass --yes." }

// interactive reports whether in can answer a prompt: a character device (a
// terminal) or any non-file reader.
func interactive(in any) bool {
	if in == nil {
		return false
	}
	f, ok := in.(*os.File)
	if !ok {
		return true
	}
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

// confirmer implements FR-047: human mode asks before a write unless --yes;
// without a terminal and without --yes it is a usage error (exit 2). Agent
// mode never prompts. Dry runs never reach the confirmer.
func confirmer(c *Call) func(string) error {
	e := c.Env
	if e.Mode != domain.ModeHuman || c.Global.Yes {
		return nil
	}
	return func(prompt string) error {
		if !interactive(e.In) {
			return &UsageError{Msg: "confirmation required but no terminal is available", Hnt: "Pass --yes to confirm non-interactively."}
		}
		if e.Err != nil {
			_, _ = fmt.Fprintf(e.Err, "%s [y/N]: ", prompt)
		}
		line, _ := bufio.NewReader(e.In).ReadString('\n')
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "y", "yes":
			return nil
		}
		return &DeclinedError{}
	}
}

func notWired(port string) error {
	return fmt.Errorf("the %s port is not wired in this build", port)
}

func writeBase(c *Call) write.Base {
	e := c.Env
	return write.Base{
		Guard: e.Guard, Clock: e.Clock, AgentID: e.AgentID, RunID: e.RunID,
		DryRun: c.Global.DryRun, Confirm: confirmer(c),
	}
}

func needRef(c *Call, what string) (string, error) {
	if len(c.Args) != 1 {
		return "", usageErr("exactly one " + what + " argument is required")
	}
	return c.Args[0], nil
}

// RegisterWrite registers the write commands (FR-040..048).
func RegisterWrite(r *Router) {
	r.Register(Command{
		Path:    []string{"incident", "create"},
		Summary: "Create an incident (deduplicated by idempotency key; priority is never written).",
		Usage:   "snow incident create --short-description <t> --description <t> (--ci <ci>|--app <app>) --impact <n> --urgency <n> [--assignment-group <g>] [--note <t>] [--idempotency-key <k>] [--dry-run] [--yes]",
		Examples: []string{
			`snow incident create --short-description "Disk full on db01" --description "98% used" --ci db01 --impact 2 --urgency 3`,
			`snow incident create --short-description "..." --description "..." --ci db01 --impact 2 --urgency 3 --dry-run`,
		},
		Forbidden: []string{"Do not set priority; it is derived by ServiceNow.", "Do not retry a create by hand; the idempotency key deduplicates repeats within the hour."},
		Flags: func(fs *flag.FlagSet) {
			for _, n := range []string{"short-description", "description", "ci", "app", "assignment-group", "note"} {
				fs.String(n, "", n)
			}
			fs.Int("impact", 0, "impact on the instance scale (1 high, 2 medium, 3 low)")
			fs.Int("urgency", 0, "urgency on the instance scale (1 high, 2 medium, 3 low)")
		},
		Run: func(ctx context.Context, c *Call) (Result, error) {
			e := c.Env
			if e.Incidents == nil {
				return Result{}, notWired("incident writer")
			}
			ci, app := flagStr(c, "ci"), flagStr(c, "app")
			if ci != "" && app != "" {
				return Result{}, &write.ValidationError{Msg: "pass --ci or --app, not both"}
			}
			if ci == "" {
				ci = app
			}
			svc := write.IncidentService{Base: writeBase(c), Writer: e.Incidents, Scale: e.Profile.Incident.Scale}
			res, err := svc.Create(ctx, write.CreateInput{
				ShortDescription: flagStr(c, "short-description"), Description: flagStr(c, "description"), CI: ci,
				AssignmentGroup: flagStr(c, "assignment-group"), Impact: flagInt(c, "impact"), Urgency: flagInt(c, "urgency"),
				IdempotencyKey: c.Global.IdempotencyKey, WorkNote: flagStr(c, "note"),
			})
			return Result{Data: res}, err
		},
	})

	r.Register(Command{
		Path:     []string{"incident", "update"},
		Summary:  "Update an incident's allowed fields (sys_mod_count guarded; conflict exits 7).",
		Usage:    "snow incident update <INC number|sys_id> [--set field=value]... [--work-note <t>] [--expected-mod-count <n>] [--dry-run] [--yes]",
		Examples: []string{`snow incident update INC0010001 --work-note "restarted the service"`, "snow incident update INC0010001 --set state=2"},
		Flags: func(fs *flag.FlagSet) {
			fs.Var(&kvFlag{}, "set", "field=value to change (repeatable)")
			fs.String("work-note", "", "work note (a provenance prefix is added)")
			addModCountFlag(fs)
		},
		Run: func(ctx context.Context, c *Call) (Result, error) {
			ref, err := needRef(c, "incident")
			if err != nil {
				return Result{}, err
			}
			if c.Env.Incidents == nil {
				return Result{}, notWired("incident writer")
			}
			fields := map[string]string{}
			for k, v := range flagKV(c, "set") {
				fields[k] = v
			}
			if n := flagStr(c, "work-note"); n != "" {
				fields["work_notes"] = n
			}
			mod, err := expectedModCount(c)
			if err != nil {
				return Result{}, err
			}
			svc := write.IncidentService{Base: writeBase(c), Writer: c.Env.Incidents, Scale: c.Env.Profile.Incident.Scale, ExpectedModCount: mod}
			res, err := svc.Update(ctx, ref, fields)
			return Result{Data: res}, err
		},
	})

	r.Register(Command{
		Path:      []string{"incident", "resolve"},
		Summary:   "Resolve an incident with a close code and notes (default deny for agents).",
		Usage:     "snow incident resolve <INC number|sys_id> --close-code <code> --close-notes <text> [--expected-mod-count <n>] [--dry-run] [--yes]",
		Examples:  []string{`snow incident resolve INC0010001 --close-code "Solved (Permanently)" --close-notes "Restarted the service"`},
		Forbidden: []string{"Agents are denied by default; ask a human to resolve."},
		Flags: func(fs *flag.FlagSet) {
			fs.String("close-code", "", "close code")
			fs.String("close-notes", "", "close notes")
			addModCountFlag(fs)
		},
		Run: func(ctx context.Context, c *Call) (Result, error) {
			ref, err := needRef(c, "incident")
			if err != nil {
				return Result{}, err
			}
			if c.Env.Incidents == nil {
				return Result{}, notWired("incident writer")
			}
			mod, err := expectedModCount(c)
			if err != nil {
				return Result{}, err
			}
			svc := write.IncidentService{Base: writeBase(c), Writer: c.Env.Incidents, Scale: c.Env.Profile.Incident.Scale, ExpectedModCount: mod}
			res, err := svc.Resolve(ctx, ref, flagStr(c, "close-code"), flagStr(c, "close-notes"))
			return Result{Data: res}, err
		},
	})

	r.Register(Command{
		Path:     []string{"task", "update"},
		Summary:  "Update a catalog task assigned to you: work notes, comments, limited state, assigned_to self.",
		Usage:    "snow task update <SCTASK number|sys_id> [--work-note <t>] [--comment <t>] [--state <n>] [--assigned-to <self>] [--expected-mod-count <n>] [--dry-run] [--yes]",
		Examples: []string{`snow task update SCTASK0010001 --work-note "provisioned" --state 3`},
		Flags: func(fs *flag.FlagSet) {
			for _, n := range []string{"work-note", "comment", "state", "assigned-to"} {
				fs.String(n, "", n)
			}
			addModCountFlag(fs)
		},
		Run: func(ctx context.Context, c *Call) (Result, error) {
			ref, err := needRef(c, "task")
			if err != nil {
				return Result{}, err
			}
			e := c.Env
			fetcher := e.TaskFetcher
			if e.Tasks == nil || fetcher == nil || e.Identity == nil {
				return Result{}, notWired("task writer")
			}
			fields := map[string]string{}
			for flagName, field := range map[string]string{"work-note": "work_notes", "comment": "comments", "state": "state", "assigned-to": "assigned_to"} {
				if v := flagStr(c, flagName); v != "" {
					fields[field] = v
				}
			}
			mod, err := expectedModCount(c)
			if err != nil {
				return Result{}, err
			}
			svc := write.TaskService{Base: writeBase(c), Writer: e.Tasks, Fetcher: fetcher, Identity: e.Identity, ExpectedModCount: mod}
			res, err := svc.Update(ctx, ref, fields)
			return Result{Data: res}, err
		},
	})

	r.Register(Command{
		Path:      []string{"catalog", "order"},
		Summary:   "Order a catalog item with validated variables (policy default: dry-run only).",
		Usage:     "snow catalog order <item name|sys_id> [--var name=value]... [--dry-run] [--yes]",
		Examples:  []string{`snow catalog order "Standard Laptop" --var model=x1 --dry-run`},
		Forbidden: []string{"Orders are never retried automatically; check `snow request list` before ordering again."},
		Flags:     func(fs *flag.FlagSet) { fs.Var(&kvFlag{}, "var", "variable name=value (repeatable)") },
		Run: func(ctx context.Context, c *Call) (Result, error) {
			item, err := needRef(c, "catalog item")
			if err != nil {
				return Result{}, err
			}
			e := c.Env
			if e.Orders == nil || e.Catalog == nil {
				return Result{}, notWired("catalog order")
			}
			svc := write.OrderService{Base: writeBase(c), Catalog: readService(c), Writer: e.Orders, IdempotencyKey: c.Global.IdempotencyKey}
			res, err := svc.Order(ctx, item, flagKV(c, "var"))
			return Result{Data: res}, err
		},
	})
}
