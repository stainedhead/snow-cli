package humanauth_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/snow-cli/internal/humanauth"
	"github.com/stainedhead/snow-cli/internal/testsupport/oktafake"
)

func TestTokenEndpointOddResponses(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		oauth  bool
	}{
		{"200 with error body", 200, `{"error":"invalid_grant","error_description":"x"}`, true},
		{"malformed json", 200, `<html>`, false},
		{"500 non-json", 500, `oops`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, "", -time.Hour)
			e.f.QueueToken(tc.status, tc.body)
			_, err := e.src.Token(context.Background())
			if err == nil {
				t.Fatal("want error")
			}
			notContains(t, "error", err.Error())
			if tc.oauth && output.ExitOf(err) != 3 {
				t.Errorf("exit = %d", output.ExitOf(err))
			}
		})
	}
}

func TestRefreshRotationKeepsOldSessionOnPersistFailure(t *testing.T) {
	e := newEnv(t, "", -time.Hour)
	e.f.QueueToken(200, tokenBody("new-access", "rotated-refresh", nil, 3600))
	e.st.SaveErr = errors.New("disk full")
	_, err := e.src.Refresh(context.Background())
	if err == nil || output.ExitOf(err) != 3 {
		t.Fatalf("err = %v", err)
	}
	notContains(t, "error", err.Error(), "new-access", "rotated-refresh")
}

func TestRefreshWithoutSessionRequiresLogin(t *testing.T) {
	f := oktafake.New(t)
	src := humanauth.NewSource(cfgFor(f), humanauth.NewMemoryStore(), "dev")
	_, err := src.Refresh(context.Background())
	var lr *humanauth.LoginRequiredError
	if !errors.As(err, &lr) {
		t.Fatalf("err = %T %v", err, err)
	}
	if f.Count("/v1/token") != 0 {
		t.Error("no network without a session")
	}
}

func TestFileStoreFailures(t *testing.T) {
	dir := t.TempDir()
	cr := humanauth.Credentials{Issuer: "i", AccessToken: secretAccess, RefreshToken: secretRefresh}

	// parent path is a regular file: MkdirAll fails
	blocker := filepath.Join(dir, "blocker")
	must(t, os.WriteFile(blocker, []byte("x"), 0o600))
	if err := humanauth.NewFileStore(filepath.Join(blocker, "sub", "c.json")).Save("p", cr); err == nil {
		t.Error("save under a file must fail")
	}

	// target is a directory: rename fails and the temp file is removed
	target := filepath.Join(dir, "target")
	must(t, os.Mkdir(target, 0o700))
	must(t, os.WriteFile(filepath.Join(target, "keep"), []byte("x"), 0o600))
	if err := humanauth.NewFileStore(target).Save("p", cr); err == nil {
		t.Error("save onto a directory must fail")
	}
	left, _ := filepath.Glob(filepath.Join(dir, ".credentials-*"))
	if len(left) != 0 {
		t.Errorf("temp files left behind: %v", left)
	}

	// corrupt file: every operation fails closed
	bad := filepath.Join(dir, "bad.json")
	must(t, os.WriteFile(bad, []byte("{not json"), 0o600))
	s := humanauth.NewFileStore(bad)
	if _, err := s.Load("p"); err == nil || errors.Is(err, humanauth.ErrNotFound) {
		t.Errorf("load corrupt: %v", err)
	}
	if s.Save("p", cr) == nil || s.Delete("p") == nil {
		t.Error("corrupt file must block save and delete")
	}

	// unreadable directory entry type
	if _, err := humanauth.NewFileStore(target).Load("p"); err == nil || errors.Is(err, humanauth.ErrNotFound) {
		t.Errorf("load directory: %v", err)
	}
}

func TestFileStoreModeIs0600(t *testing.T) {
	p := filepath.Join(t.TempDir(), "d", "c.json")
	s := humanauth.NewFileStore(p)
	must(t, s.Save("p", humanauth.Credentials{AccessToken: "a"}))
	fi, err := os.Stat(p)
	must(t, err)
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", fi.Mode().Perm())
	}
}
