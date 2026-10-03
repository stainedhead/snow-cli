// Package policies embeds the shipped policy files (spec D-f): the agent and
// human guardrail policies in the agent-cli-core strict schema. The
// composition root exposes Named as app.Options.NamedPolicy so that
// `--policy agent|human` works without a file on disk.
package policies

import (
	_ "embed"
	"fmt"
)

//go:embed agent.policy.yaml
var agent []byte

//go:embed human.policy.yaml
var human []byte

// Named returns the bytes of a built-in policy: "agent" or "human".
func Named(name string) ([]byte, error) {
	switch name {
	case "agent":
		return append([]byte(nil), agent...), nil
	case "human":
		return append([]byte(nil), human...), nil
	}
	return nil, fmt.Errorf("unknown built-in policy %q (want agent or human)", name)
}
