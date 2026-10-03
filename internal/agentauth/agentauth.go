// Package agentauth holds the agent-mode credential daemon client. The real
// adapter over agent-okta-d is unreleased (docs/deferred.md, ADR), so this
// release ships a stub that fails closed: every call reports the daemon as
// unreachable via the core's *auth.UnreachableError (exit 3, message names
// the socket). agent-okta-d is intentionally not a dependency.
package agentauth

import (
	"context"
	"errors"

	"github.com/stainedhead/agent-cli-core/auth"
)

// ErrAdapterNotBuilt explains why the daemon is unreachable.
var ErrAdapterNotBuilt = errors.New("the agent-okta-d client adapter is not built into this release")

type unavailable struct{ socket string }

// NewUnavailableClient returns an auth.DaemonClient that always reports the
// daemon at socket as unreachable.
func NewUnavailableClient(socket string) auth.DaemonClient { return unavailable{socket: socket} }

func (u unavailable) err() error {
	return &auth.UnreachableError{Socket: u.socket, Err: ErrAdapterNotBuilt}
}

func (u unavailable) Fetch(context.Context, string) (auth.Token, error) {
	return auth.Token{}, u.err()
}

func (u unavailable) Refresh(context.Context, string) (auth.Token, error) {
	return auth.Token{}, u.err()
}
