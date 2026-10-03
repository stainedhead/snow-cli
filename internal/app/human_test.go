package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stainedhead/agent-cli-core/auth"
	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/snow-cli/internal/config"
	"github.com/stainedhead/snow-cli/internal/humanauth"
)

func TestHumanTokenSourceIsRefresherAndFailsClosed(t *testing.T) {
	old := storeFactory
	defer func() { storeFactory = old }()
	st := humanauth.NewMemoryStore()
	storeFactory = func() humanauth.Store { return st }

	var p config.Resolved
	p.Name = "dev"
	p.Okta.Issuer = "https://acme.okta.com"
	p.Okta.ClientID = "cid"
	src, err := newHumanTokenSource(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := src.(auth.Refresher); !ok {
		t.Fatal("human source must implement auth.Refresher")
	}
	_, err = src.Token(context.Background())
	var lr *humanauth.LoginRequiredError
	if !errors.As(err, &lr) || output.ExitOf(err) != 3 || !strings.Contains(err.Error(), "not logged in") {
		t.Errorf("err = %v", err)
	}
	if k, ok := newKeychain().(humanauth.Store); !ok || k != humanauth.Store(st) {
		t.Error("keychain must be the same Store")
	}
}

func TestDefaultStoreSelection(t *testing.T) {
	t.Setenv(EnvInsecureStore, "")
	if k := defaultStore().Kind(); k == "insecure-file" || k == "memory" {
		t.Errorf("default must be a fail-closed stub, got %s", k)
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv(EnvInsecureStore, "1")
	if k := defaultStore().Kind(); k != "insecure-file" {
		t.Errorf("opt-in store = %s", k)
	}
}
