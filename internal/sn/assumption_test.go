package sn_test

import (
	"errors"
	"testing"

	"github.com/stainedhead/agent-cli-core/auth/authtest"
	"github.com/stainedhead/agent-cli-core/httpx"
	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/snow-cli/internal/testsupport/snfake"
)

// TestAssumptionA14RetryAfterOnInboundRateLimit documents A-14 (ASSUMPTION
// unverified against a real instance): ServiceNow inbound REST rate limiting
// answers 429 with a Retry-After header that the core transport honours for
// reads; a limit that never clears ends in exit 8 with the attempt count.
func TestAssumptionA14RetryAfterOnInboundRateLimit(t *testing.T) {
	f := snfake.New(t)
	f.On("GET", "/api/now/v1/table/incident", snfake.Response{JSON: map[string]any{"result": []any{}}})
	f.Fail("GET", "/api/now/v1/table/incident", 2, snfake.Response{
		Status: 429, Header: map[string]string{"Retry-After": "0"}, JSON: map[string]any{"error": map[string]any{"message": "rate limited"}},
	})
	c := newClient(t, f, authtest.Valid)
	if _, err := get(c, "/api/now/v1/table/incident"); err != nil {
		t.Fatalf("429 with Retry-After must be retried: %v", err)
	}
	if n := f.Count("GET", "/api/now/v1/table/incident"); n != 3 {
		t.Errorf("attempts = %d, want 3", n)
	}

	g := snfake.New(t)
	g.Fail("GET", "/api/now/v1/table/incident", 20, snfake.Response{Status: 429, Header: map[string]string{"Retry-After": "0"}, JSON: map[string]any{}})
	c2 := newClient(t, g, authtest.Valid)
	_, err := get(c2, "/api/now/v1/table/incident")
	var rl *httpx.RateLimitedError
	if !errors.As(err, &rl) || output.ExitOf(err) != output.ExitRateLimited || rl.Attempts < 2 {
		t.Errorf("persistent 429: %v", err)
	}
}
