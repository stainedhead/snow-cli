package app

import (
	"context"
	"errors"

	"github.com/stainedhead/agent-cli-core/auth"
	"github.com/stainedhead/snow-cli/internal/config"
)

// This file is the single hook WS-D replaces: human-mode token source and
// keychain construction. Until then both fail closed.

var errHumanUnavailable = errors.New("human-mode login is not available in this build; use an agent profile")

type unavailableSource struct{}

func (unavailableSource) Token(_ context.Context) (auth.Token, error) {
	return auth.Token{}, &auth.TokenError{Provider: "okta", Op: "fetch", Err: errHumanUnavailable}
}

// newHumanTokenSource builds the human-mode token source for a profile.
func newHumanTokenSource(_ config.Resolved) (auth.TokenSource, error) {
	return unavailableSource{}, nil
}

// keychainPlaceholder stands in for the OS keychain store (FR-016, D-l).
type keychainPlaceholder struct{}

// Unavailable reports why no credential store exists yet.
func (keychainPlaceholder) Unavailable() error { return errHumanUnavailable }

// newKeychain returns the human-mode credential store placeholder.
func newKeychain() any { return keychainPlaceholder{} }
