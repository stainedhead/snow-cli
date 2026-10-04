// Package provenance builds the attribution that marks agent writes
// (FR-044): a work-note prefix and correlation_display.
//
// ASSUMPTION(unverified against a real instance): correlation_id and
// correlation_display exist on task in the target release (A-04).
package provenance

import "strings"

const unknown = "unknown"

func clean(s string) string {
	if s == "" {
		return unknown
	}
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-', r == ':':
			return r
		}
		return '_'
	}, s)
}

// Prefix is "[snow-cli agent=<id> run=<run_id>]". The ids are restricted to a
// safe character set so a value cannot forge a second prefix.
func Prefix(agentID, runID string) string {
	return "[snow-cli agent=" + clean(agentID) + " run=" + clean(runID) + "]"
}

// WorkNote prepends the provenance prefix to note.
func WorkNote(agentID, runID, note string) string {
	if note == "" {
		return Prefix(agentID, runID)
	}
	return Prefix(agentID, runID) + " " + note
}

// CorrelationDisplay is "agent:<id>".
func CorrelationDisplay(agentID string) string { return "agent:" + clean(agentID) }
