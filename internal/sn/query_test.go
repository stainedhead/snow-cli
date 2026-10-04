package sn

import (
	"net/url"
	"testing"
)

func TestTableParams(t *testing.T) {
	tests := []struct {
		name string
		p    TableParams
		want url.Values
	}{
		{"defaults", TableParams{}, url.Values{
			"sysparm_exclude_reference_link": {"true"},
			"sysparm_query":                  {DefaultOrder},
		}},
		{"fields limit offset", TableParams{Fields: []string{"number", "state"}, Limit: 10, Offset: 20}, url.Values{
			"sysparm_exclude_reference_link": {"true"},
			"sysparm_fields":                 {"number,state"},
			"sysparm_limit":                  {"10"},
			"sysparm_offset":                 {"20"},
			"sysparm_query":                  {DefaultOrder},
		}},
		{"caller query gets default order appended", TableParams{Query: "active=true"}, url.Values{
			"sysparm_exclude_reference_link": {"true"},
			"sysparm_query":                  {"active=true^" + DefaultOrder},
		}},
		{"caller order wins", TableParams{Query: "active=true^ORDERBYnumber"}, url.Values{
			"sysparm_exclude_reference_link": {"true"},
			"sysparm_query":                  {"active=true^ORDERBYnumber"},
		}},
		{"explicit order option", TableParams{OrderBy: "ORDERBYDESCnumber"}, url.Values{
			"sysparm_exclude_reference_link": {"true"},
			"sysparm_query":                  {"ORDERBYDESCnumber"},
		}},
		{"ORDERBY inside a value does not suppress the default order", TableParams{Query: "short_descriptionLIKEORDERBYx"}, url.Values{
			"sysparm_exclude_reference_link": {"true"},
			"sysparm_query":                  {"short_descriptionLIKEORDERBYx^" + DefaultOrder},
		}},
		{"ORDERBY clause after a value suppresses the default", TableParams{Query: "nameLIKEx^ORDERBYDESCnumber"}, url.Values{
			"sysparm_exclude_reference_link": {"true"},
			"sysparm_query":                  {"nameLIKEx^ORDERBYDESCnumber"},
		}},
		{"explicit order is kept next to a caller ORDERBY", TableParams{Query: "a=1^ORDERBYnumber", OrderBy: "ORDERBYname"}, url.Values{
			"sysparm_exclude_reference_link": {"true"},
			"sysparm_query":                  {"a=1^ORDERBYnumber^ORDERBYname"},
		}},
		{"display only when asked", TableParams{Display: true}, url.Values{
			"sysparm_exclude_reference_link": {"true"},
			"sysparm_display_value":          {"true"},
			"sysparm_query":                  {DefaultOrder},
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.p.Values()
			if got.Encode() != tc.want.Encode() {
				t.Errorf("got  %s\nwant %s", got.Encode(), tc.want.Encode())
			}
		})
	}
}

func TestTableParamsNoOrder(t *testing.T) {
	v := TableParams{Query: "a=b", NoOrder: true}.Values()
	if v.Get("sysparm_query") != "a=b" {
		t.Errorf("query = %q", v.Get("sysparm_query"))
	}
	if (TableParams{NoOrder: true}).Values().Has("sysparm_query") {
		t.Error("empty query without order must be omitted")
	}
}

func TestTablePath(t *testing.T) {
	if got := TablePath("incident"); got != "/api/now/v1/table/incident" {
		t.Errorf("TablePath = %q", got)
	}
	if got := TablePath("incident", "abc"); got != "/api/now/v1/table/incident/abc" {
		t.Errorf("TablePath = %q", got)
	}
	if got := StatsPath("incident"); got != "/api/now/v1/stats/incident" {
		t.Errorf("StatsPath = %q", got)
	}
}

// ASSUMPTION A-11: count uses the Aggregate API at /api/now/v1/stats/{table}.
func TestAssumptionA11AggregatePathForCount(t *testing.T) {
	if StatsPath("cmdb_ci") != "/api/now/v1/stats/cmdb_ci" {
		t.Error("stats path drifted")
	}
}
