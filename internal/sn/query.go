package sn

import (
	"net/url"
	"strconv"
	"strings"
)

// DefaultOrder makes offsets stable across pages (spec D-h).
const DefaultOrder = "ORDERBYDESCsys_updated_on^ORDERBYsys_id"

// TableParams builds Table API query parameters (FR-006).
type TableParams struct {
	Fields  []string
	Limit   int
	Offset  int
	Query   string // encoded query
	OrderBy string // explicit ORDERBY clause; overrides the default
	Display bool   // sysparm_display_value only when true
	NoOrder bool   // suppress the default ordering (single-record reads)
}

// Values renders the parameters. Reference links are always excluded;
// sysparm_fields is sent whenever fields are given.
func (p TableParams) Values() url.Values {
	v := url.Values{"sysparm_exclude_reference_link": {"true"}}
	if len(p.Fields) > 0 {
		v.Set("sysparm_fields", strings.Join(p.Fields, ","))
	}
	if p.Limit > 0 {
		v.Set("sysparm_limit", strconv.Itoa(p.Limit))
	}
	if p.Offset > 0 {
		v.Set("sysparm_offset", strconv.Itoa(p.Offset))
	}
	if p.Display {
		v.Set("sysparm_display_value", "true")
	}
	q := p.Query
	if !p.NoOrder && !strings.Contains(q, "ORDERBY") {
		order := p.OrderBy
		if order == "" {
			order = DefaultOrder
		}
		if q == "" {
			q = order
		} else {
			q += "^" + order
		}
	}
	if q != "" {
		v.Set("sysparm_query", q)
	}
	return v
}

const tableBase = "/api/now/v1/table/"

// TablePath returns /api/now/v1/table/<table>[/<sys_id>].
func TablePath(table string, sysID ...string) string {
	p := tableBase + url.PathEscape(table)
	if len(sysID) > 0 {
		p += "/" + url.PathEscape(sysID[0])
	}
	return p
}

// StatsPath returns the Aggregate API path for count (A-11).
// ASSUMPTION(unverified against a real instance): /api/now/v1/stats/{table} with sysparm_count=true.
func StatsPath(table string) string { return "/api/now/v1/stats/" + url.PathEscape(table) }
