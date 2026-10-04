// Command snow is the ServiceNow CLI for agents and humans. This file is the
// composition root entry point; wiring lives in internal/app.
package main

import (
	"context"
	"io"
	"os"
	"os/signal"

	"github.com/stainedhead/snow-cli/internal/app"
	"github.com/stainedhead/snow-cli/internal/cli"
	"github.com/stainedhead/snow-cli/policies"
)

// Stamped by ldflags (see the Makefile build target).
var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

func run(ctx context.Context, args []string, stdout, stderr io.Writer, o app.Options) int {
	r := cli.NewRouter(cli.Options{
		Build:      cli.BuildInfo{Version: version, Commit: commit, Date: date},
		EnvFactory: app.NewEnvFactory(o),
		Stdout:     stdout,
		Stderr:     stderr,
	})
	cli.RegisterAll(r)
	return r.Execute(ctx, args)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr,
		app.Options{Stdin: os.Stdin, Stderr: os.Stderr, NamedPolicy: policies.Named})
	stop()
	os.Exit(code)
}
