package cli

import (
	"context"

	"github.com/stainedhead/snow-cli/internal/usecase/whoami"
)

// RegisterCore registers the foundation commands (FR-001, FR-010).
func RegisterCore(r *Router) {
	r.Register(Command{
		Path:     []string{"version"},
		Summary:  "Print the snow version, commit and build date.",
		Usage:    "snow version",
		Examples: []string{"snow version"},
		NoEnv:    true,
		Run: func(_ context.Context, c *Call) (Result, error) {
			return Result{Data: map[string]string{
				"version": c.Build.Version, "commit": c.Build.Commit, "date": c.Build.Date,
			}}, nil
		},
	})
	r.Register(Command{
		Path:      []string{"whoami"},
		Summary:   "Show the ServiceNow identity, roles, instance, profile and mode in use.",
		Usage:     "snow whoami [--profile <name>] [--format json|table|text]",
		Examples:  []string{"snow whoami", "snow whoami --profile prod"},
		Forbidden: []string{"Do not try to obtain or print the access token; there is no token command."},
		Run: func(ctx context.Context, c *Call) (Result, error) {
			e := c.Env
			id, err := whoami.Service{
				Identity: e.Identity, Guard: e.Guard, Mode: e.Mode, Profile: e.Profile.Name,
				Instance: e.Profile.Host, AgentID: e.AgentID, RunID: e.RunID,
			}.Execute(ctx)
			if err != nil {
				return Result{}, err
			}
			return Result{Data: id}, nil
		},
	})
}
