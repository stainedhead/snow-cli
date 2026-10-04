package domain

import (
	"encoding/json"
	"regexp"

	"github.com/stainedhead/agent-cli-core/output"
)

// CatalogItem is a service catalog item summary.
type CatalogItem struct {
	SysID            string `json:"sys_id"`
	Name             string `json:"name"`
	ShortDescription string `json:"short_description,omitempty"`
	Category         string `json:"category,omitempty"`
}

// CatalogVariable describes one variable of a catalog item.
type CatalogVariable struct {
	Name      string   `json:"name"`
	Label     string   `json:"label,omitempty"`
	Type      string   `json:"type,omitempty"`
	Mandatory bool     `json:"mandatory"`
	Choices   []string `json:"choices,omitempty"`
}

// MarshalJSON emits the label and choices as untrusted text (FR-R12): they are
// written by catalog authors, not by the tool. The variable name and type are
// identifiers and stay plain; a name that is not identifier-shaped is marked
// too.
func (v CatalogVariable) MarshalJSON() ([]byte, error) {
	var choices []any
	for _, c := range v.Choices {
		choices = append(choices, untrusted(c))
	}
	var name any = v.Name
	if !identifier.MatchString(v.Name) {
		name = untrusted(v.Name)
	}
	var label any
	if v.Label != "" {
		label = untrusted(v.Label)
	}
	return json.Marshal(struct {
		Name      any    `json:"name"`
		Label     any    `json:"label,omitempty"`
		Type      string `json:"type,omitempty"`
		Mandatory bool   `json:"mandatory"`
		Choices   []any  `json:"choices,omitempty"`
	}{name, label, v.Type, v.Mandatory, choices})
}

var identifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.]{0,79}$`)

func untrusted(s string) any {
	if s == "" {
		return s
	}
	return output.Untrusted{Value: s}
}

// WriteResult is the outcome of a write (spec D-e: extras live in data).
type WriteResult struct {
	Record       Record `json:"-"`
	Deduplicated bool   `json:"deduplicated"`
	DryRun       bool   `json:"dry_run"`
}

// MarshalJSON flattens the record fields next to the extras.
func (w WriteResult) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Table        string            `json:"table,omitempty"`
		Record       map[string]string `json:"record,omitempty"`
		Deduplicated bool              `json:"deduplicated"`
		DryRun       bool              `json:"dry_run"`
	}{w.Record.Table, w.Record.Fields, w.Deduplicated, w.DryRun})
}
