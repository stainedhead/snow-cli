package humanauth_test

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"testing"

	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/snow-cli/internal/humanauth"
	"github.com/stainedhead/snow-cli/internal/testsupport/oktafake"
)

// TestAssumptionA06FixedLoopbackPortWhenConfigured documents A-06
// (ASSUMPTION unverified against a real instance): the Okta app may require a
// fixed loopback port, so the redirect honours an explicit port and a taken
// port is reported, never silently replaced.
func TestAssumptionA06FixedLoopbackPortWhenConfigured(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	f := oktafake.New(t)
	f.QueueToken(200, tokenBody("a", "r", nil, 60))
	b := &browser{t: t}
	if _, err := humanauth.LoginPKCE(context.Background(), cfgFor(f), humanauth.PKCEOptions{Launch: b.launch, Port: port}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.gotURL, "127.0.0.1%3A"+strconv.Itoa(port)) {
		t.Errorf("redirect must use port %d: %s", port, b.gotURL)
	}

	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = busy.Close() }()
	_, err = humanauth.LoginPKCE(context.Background(), cfgFor(f), humanauth.PKCEOptions{
		Launch: b.launch, Port: busy.Addr().(*net.TCPAddr).Port,
	})
	if err == nil {
		t.Error("a taken fixed port must be an error")
	}
}

// TestAssumptionA13NoKeychainBackendFailsClosedWithEscapeHatch documents A-13
// (ASSUMPTION unverified against a real instance): WSL2 and Linux may or may
// not provide a keychain; this release has no real backend, so every platform
// fails closed (exit 3) and names --insecure-store.
func TestAssumptionA13NoKeychainBackendFailsClosedWithEscapeHatch(t *testing.T) {
	for _, tc := range []struct {
		goos string
		wsl  bool
	}{{"linux", false}, {"linux", true}, {"darwin", false}} {
		s := humanauth.SelectStore(tc.goos, tc.wsl, false, "")
		_, err := s.Load("p")
		var su *humanauth.StoreUnavailableError
		if !errors.As(err, &su) || output.ExitOf(err) != output.ExitAuth || !strings.Contains(su.Hint(), "--insecure-store") {
			t.Errorf("%s wsl=%v: %v", tc.goos, tc.wsl, err)
		}
	}
}
