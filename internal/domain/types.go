package domain

import "encoding/json"

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
