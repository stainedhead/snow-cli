package cli

import (
	"flag"
	"strings"

	"github.com/stainedhead/agent-cli-core/output"
)

// GlobalFlags are the flags every command honours (spec section 5).
type GlobalFlags struct {
	Format         string
	Fields         string
	Limit          int
	Offset         int
	MaxBytes       int
	DryRun         bool
	IdempotencyKey string
	Profile        string
	Policy         string
	Trace          bool
	Config         string
	Yes            bool
}

// FieldList splits --fields into names.
func (g GlobalFlags) FieldList() []string {
	var out []string
	for _, f := range strings.Split(g.Fields, ",") {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}

func (g *GlobalFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&g.Format, "format", "json", "output format: json, table or text")
	fs.StringVar(&g.Fields, "fields", "", "comma-separated field names")
	fs.IntVar(&g.Limit, "limit", 0, "maximum records per page")
	fs.IntVar(&g.Offset, "offset", 0, "record offset for pagination")
	fs.IntVar(&g.MaxBytes, "max-bytes", 0, "maximum output bytes (default 32768)")
	fs.BoolVar(&g.DryRun, "dry-run", false, "preview a write without sending it")
	fs.StringVar(&g.IdempotencyKey, "idempotency-key", "", "idempotency key for writes")
	fs.StringVar(&g.Profile, "profile", "", "config profile")
	fs.StringVar(&g.Policy, "policy", "", "policy: agent, human or a file path")
	fs.BoolVar(&g.Trace, "trace", false, "trace HTTP requests (human mode only)")
	fs.StringVar(&g.Config, "config", "", "config file path")
	fs.BoolVar(&g.Yes, "yes", false, "skip the write confirmation prompt")
}

func (g GlobalFlags) validate() error {
	if _, err := output.ParseFormat(g.Format); err != nil {
		return usageErr(err.Error())
	}
	switch {
	case g.Limit < 0:
		return usageErr("--limit must not be negative")
	case g.Offset < 0:
		return usageErr("--offset must not be negative")
	case g.MaxBytes < 0:
		return usageErr("--max-bytes must not be negative")
	}
	return nil
}

// UsageError is a command-line usage problem (exit 2).
type UsageError struct {
	Msg string
	Hnt string
}

func (e *UsageError) Error() string { return e.Msg }

// Category is usage.
func (*UsageError) Category() output.Category { return output.CategoryUsage }

// Hint returns remediation text.
func (e *UsageError) Hint() string { return e.Hnt }

func usageErr(msg string) error { return &UsageError{Msg: msg, Hnt: "Run `snow help` for usage."} }
