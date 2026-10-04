package auditx

import (
	"strings"

	"github.com/stainedhead/agent-cli-core/policy"
)

// AllowedFields reports the field allowlist of the rule that would decide a
// verb and resource (WS-B request 1): the first matching allow rule, unless a
// deny rule matches first. It returns nil when no allowlist applies, in which
// case the caller falls back to its own defaults.
func (g *Guard) AllowedFields(verb, resource string) []string {
	if g.Engine == nil || g.Engine.Policy() == nil {
		return nil
	}
	for _, r := range g.Engine.Policy().Rules {
		if !globAny(r.Verbs, verb) || !globAny(r.Resources, resource) {
			continue
		}
		if r.Effect == policy.EffectDeny {
			return nil
		}
		return append([]string(nil), r.Fields...)
	}
	return nil
}

func globAny(patterns []string, s string) bool {
	for _, p := range patterns {
		if glob(p, s) {
			return true
		}
	}
	return false
}

// glob matches s against pattern where "*" matches any run of characters
// (the same semantics as the core policy engine).
func glob(pattern, s string) bool {
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return pattern == s
	}
	if !strings.HasPrefix(s, parts[0]) {
		return false
	}
	s = s[len(parts[0]):]
	last := parts[len(parts)-1]
	for _, mid := range parts[1 : len(parts)-1] {
		i := strings.Index(s, mid)
		if i < 0 {
			return false
		}
		s = s[i+len(mid):]
	}
	return strings.HasSuffix(s, last)
}
