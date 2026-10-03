package app

import (
	"os"
	"runtime"

	"github.com/stainedhead/agent-cli-core/auth"
	"github.com/stainedhead/snow-cli/internal/config"
	"github.com/stainedhead/snow-cli/internal/humanauth"
)

// This file is the single hook WS-D owns: human-mode token source and
// keychain construction (FR-015..017). Real OS keychain backends are
// fail-closed stubs (D-l); SNOW_INSECURE_STORE=1 opts into the 0600 file.

// EnvInsecureStore opts into the plain-file credential store.
const EnvInsecureStore = "SNOW_INSECURE_STORE"

// storeFactory is the test seam for the credential store.
var storeFactory = defaultStore

func defaultStore() humanauth.Store {
	insecure := os.Getenv(EnvInsecureStore) == "1"
	path := ""
	if insecure {
		home, _ := os.UserHomeDir()
		path = humanauth.DefaultInsecurePath(home)
	}
	return humanauth.SelectStore(runtime.GOOS, humanauth.DetectWSL(), insecure, path)
}

// newHumanTokenSource builds the human-mode token source for a profile. The
// returned *humanauth.Source implements auth.Refresher, so a 401 triggers one
// forced refresh in the core's transport.
func newHumanTokenSource(p config.Resolved) (auth.TokenSource, error) {
	return humanauth.NewSource(humanauth.Config{
		Issuer: p.Okta.Issuer, ClientID: p.Okta.ClientID, TokenType: p.Okta.TokenType,
	}, storeFactory(), p.Name), nil
}

// newKeychain returns the human-mode credential store (a humanauth.Store).
func newKeychain() any { return storeFactory() }
