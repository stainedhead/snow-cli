// Package domain holds ServiceNow value objects and records. It has no
// dependency on HTTP, the filesystem or any keychain.
package domain

import (
	"errors"
	"fmt"
	"strings"
)

// SysID is a 32-character lowercase hex ServiceNow record identifier.
type SysID string

// LooksLikeSysID reports whether s is a 32-character hex string.
func LooksLikeSysID(s string) bool {
	if len(s) != 32 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}

// ParseSysID validates s and returns it lowercased.
func ParseSysID(s string) (SysID, error) {
	if !LooksLikeSysID(s) {
		return "", fmt.Errorf("invalid sys_id %q: want 32 hex characters", s)
	}
	return SysID(strings.ToLower(s)), nil
}

// NumberKind identifies the record family of a Number.
type NumberKind int

// Number kinds.
const (
	KindUnknown NumberKind = iota
	KindIncident
	KindRequest
	KindRequestItem
	KindCatalogTask
	KindChange
	KindProblem
)

var numberKinds = []struct {
	prefix string
	kind   NumberKind
	table  string
}{
	// SCTASK and RITM must precede shorter prefixes only if ambiguous; none are.
	{"INC", KindIncident, "incident"},
	{"REQ", KindRequest, "sc_request"},
	{"RITM", KindRequestItem, "sc_req_item"},
	{"SCTASK", KindCatalogTask, "sc_task"},
	{"CHG", KindChange, "change_request"},
	{"PRB", KindProblem, "problem"},
}

// Number is a human-readable record number such as INC0010001.
type Number struct {
	s    string
	kind NumberKind
}

// ParseNumber validates and normalises (upper-cases) a record number.
func ParseNumber(s string) (Number, error) {
	up := strings.ToUpper(s)
	for _, k := range numberKinds {
		digits, ok := strings.CutPrefix(up, k.prefix)
		if !ok || digits == "" {
			continue
		}
		if strings.Trim(digits, "0123456789") != "" {
			break
		}
		return Number{s: up, kind: k.kind}, nil
	}
	return Number{}, fmt.Errorf("invalid record number %q: want INC/REQ/RITM/SCTASK/CHG/PRB followed by digits", s)
}

// String returns the normalised number.
func (n Number) String() string { return n.s }

// Kind returns the record family.
func (n Number) Kind() NumberKind { return n.kind }

// Table returns the ServiceNow table holding records of this number's kind.
func (n Number) Table() string {
	for _, k := range numberKinds {
		if k.kind == n.kind {
			return k.table
		}
	}
	return ""
}

// Mode is the authentication mode of a profile.
type Mode string

// Modes.
const (
	ModeAgent Mode = "agent"
	ModeHuman Mode = "human"
)

// ParseMode validates a mode string.
func ParseMode(s string) (Mode, error) {
	switch Mode(s) {
	case ModeAgent, ModeHuman:
		return Mode(s), nil
	}
	return "", fmt.Errorf("invalid mode %q: want agent or human", s)
}

// String returns the mode text.
func (m Mode) String() string { return string(m) }

// Scale is the instance's impact/urgency scale. ServiceNow's out-of-box scale
// is 1=High, 2=Medium, 3=Low (ASSUMPTION A-01).
type Scale struct {
	High   int `json:"high"`
	Medium int `json:"medium"`
	Low    int `json:"low"`
}

// DefaultScale returns the out-of-box scale.
// ASSUMPTION(unverified against a real instance): 1=High, 2=Medium, 3=Low (A-01).
func DefaultScale() Scale { return Scale{High: 1, Medium: 2, Low: 3} }

// Validate checks the scale values are positive and distinct.
func (s Scale) Validate() error {
	vals := []int{s.High, s.Medium, s.Low}
	seen := map[int]bool{}
	for _, v := range vals {
		if v <= 0 {
			return errors.New("scale values must be positive")
		}
		if seen[v] {
			return errors.New("scale values must be distinct")
		}
		seen[v] = true
	}
	return nil
}

// Contains reports whether v is one of the scale's values.
func (s Scale) Contains(v int) bool {
	_, ok := s.Level(v)
	return ok
}

// Level names v as "high", "medium" or "low".
func (s Scale) Level(v int) (string, bool) {
	switch v {
	case s.High:
		return "high", true
	case s.Medium:
		return "medium", true
	case s.Low:
		return "low", true
	}
	return "", false
}

// Page describes one page of a list result (spec D-e, D-h).
type Page struct {
	Offset     int  `json:"offset"`
	Returned   int  `json:"returned"`
	Total      *int `json:"total"`
	NextOffset *int `json:"next_offset"`
}

// NewPage builds a Page. total is nil when X-Total-Count was absent. The next
// offset is absolute (a sysparm_offset) and nil when the result set is
// exhausted: with a known total, when offset+returned >= total; without one,
// when the page came back short.
func NewPage(offset, returned, limit int, total *int) Page {
	p := Page{Offset: offset, Returned: returned, Total: total}
	next := offset + returned
	switch {
	case returned == 0:
	case total != nil && next >= *total:
	case total == nil && returned < limit:
	default:
		p.NextOffset = &next
	}
	return p
}

// ACLFilteredPossible reports whether a short or empty page may have been cut
// by ACL filtering: the Table API applies the limit before ACLs, so a page
// smaller than limit while the total says more rows exist is suspicious
// (PRD 7.1).
func (p Page) ACLFilteredPossible(limit int) bool {
	if p.Total == nil {
		return false
	}
	return p.Returned < limit && p.Offset+p.Returned < *p.Total
}

// Record is a generic table record: display-agnostic string fields.
type Record struct {
	Table  string
	Fields map[string]string
}

// Get returns a field value, "" when absent.
func (r Record) Get(field string) string { return r.Fields[field] }

// SysID returns the record's sys_id when present and valid.
func (r Record) SysID() (SysID, bool) {
	id, err := ParseSysID(r.Fields["sys_id"])
	return id, err == nil
}

// Identity is the resolved caller (spec FR-010).
type Identity struct {
	User        string   `json:"user"`
	DisplayName string   `json:"display_name,omitempty"`
	Roles       []string `json:"roles,omitempty"`
	Instance    string   `json:"instance,omitempty"`
	Profile     string   `json:"profile,omitempty"`
	Mode        Mode     `json:"mode"`
	AgentID     string   `json:"agent_id,omitempty"`
	RunID       string   `json:"run_id,omitempty"`
}
