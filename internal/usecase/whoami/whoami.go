// Package whoami is the identity use case (FR-010).
package whoami

import (
	"context"
	"net/http"

	"github.com/stainedhead/agent-cli-core/policy"
	"github.com/stainedhead/snow-cli/internal/domain"
	"github.com/stainedhead/snow-cli/internal/policymap"
	"github.com/stainedhead/snow-cli/internal/usecase"
)

// Service resolves and describes the caller.
type Service struct {
	Identity usecase.Identity
	Guard    usecase.Guard
	Mode     domain.Mode
	Profile  string
	Instance string
	AgentID  string
	RunID    string
}

// Execute checks policy (verb whoami), audits, and returns the identity
// enriched with instance, profile, mode and agent attribution.
func (s Service) Execute(ctx context.Context) (domain.Identity, error) {
	var out domain.Identity
	act := usecase.Action{Kind: usecase.Read, Request: policymap.NewRequest(policymap.VerbWhoami, policymap.ResWhoami)}
	err := s.Guard.Run(ctx, act, func(ctx context.Context, _ policy.Decision) (int, error) {
		id, err := s.Identity.Whoami(ctx)
		if err != nil {
			return 0, err
		}
		id.Instance, id.Profile, id.Mode = s.Instance, s.Profile, s.Mode
		id.AgentID, id.RunID = s.AgentID, s.RunID
		out = id
		return http.StatusOK, nil
	})
	if err != nil {
		return domain.Identity{}, err
	}
	return out, nil
}
