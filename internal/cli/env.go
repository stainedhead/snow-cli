// Package cli is the command router, global flags, render helper and exit
// mapping (FR-003..005). Streams add commands in their own cmd_<area>.go
// files through the Register<Area> functions; the router and this file are
// frozen at A-GATE.
package cli

import (
	"context"
	"io"

	"github.com/stainedhead/agent-cli-core/policy"
	"github.com/stainedhead/snow-cli/internal/config"
	"github.com/stainedhead/snow-cli/internal/domain"
	"github.com/stainedhead/snow-cli/internal/usecase"
)

// BuildInfo is stamped by ldflags in cmd/snow.
type BuildInfo struct {
	Version string
	Commit  string
	Date    string
}

// Env is everything a command needs at run time: ports (implemented by
// adapters wired in the composition root), the guard, the resolved profile.
// Streams build their use cases from these ports inside their Run functions.
type Env struct {
	Mode    domain.Mode
	Profile config.Resolved

	Tables    usecase.TableReader
	Catalog   usecase.CatalogReader
	Incidents usecase.IncidentWriter
	Tasks     usecase.TaskWriter
	Orders    usecase.OrderWriter
	Identity  usecase.Identity
	Clock     usecase.Clock
	IDs       usecase.IDGen
	// Guard does policy check + audit + action (FR-005).
	Guard usecase.Guard
	// Limits are the policy's result/byte caps.
	Limits policy.Limits

	// In and Err serve prompts (FR-047); nil means no terminal.
	In  io.Reader
	Err io.Writer

	// Keychain is the placeholder for the human-mode credential store
	// (WS-D replaces its type through app/human.go; nil until then).
	Keychain any
	// Extra lets a stream carry adapter objects built in app/<stream>.go
	// without changing this struct.
	Extra map[string]any
}

// EnvFactory builds the Env for a command from the parsed global flags.
type EnvFactory func(ctx context.Context, g GlobalFlags) (*Env, error)
