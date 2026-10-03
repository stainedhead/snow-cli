package provenance

import "testing"

func TestPrefix(t *testing.T) {
	if got := Prefix("agent-1", "run-9"); got != "[snow-cli agent=agent-1 run=run-9]" {
		t.Fatalf("got %q", got)
	}
}

func TestPrefixSanitisesForgedIDs(t *testing.T) {
	got := Prefix("a] [other agent=x", "r\nun")
	if got != "[snow-cli agent=a___other_agent_x run=r_un]" {
		t.Fatalf("got %q", got)
	}
}

func TestPrefixEmptyIDs(t *testing.T) {
	if got := Prefix("", ""); got != "[snow-cli agent=unknown run=unknown]" {
		t.Fatalf("got %q", got)
	}
}

func TestWorkNote(t *testing.T) {
	if got := WorkNote("a", "r", "disk is full"); got != "[snow-cli agent=a run=r] disk is full" {
		t.Fatalf("got %q", got)
	}
	if got := WorkNote("a", "r", ""); got != "[snow-cli agent=a run=r]" {
		t.Fatalf("empty note: %q", got)
	}
}

// Assumption: correlation_display exists on task in the target release (A-04).
func TestAssumptionA04CorrelationDisplay(t *testing.T) {
	if got := CorrelationDisplay("agent-1"); got != "agent:agent-1" {
		t.Fatalf("got %q", got)
	}
	if got := CorrelationDisplay(""); got != "agent:unknown" {
		t.Fatalf("got %q", got)
	}
}
