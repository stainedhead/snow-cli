package read

import (
	"fmt"
	"strings"

	"github.com/stainedhead/snow-cli/internal/domain"
)

// ValidateQuery rejects encoded queries that carry script (FR-021): the
// "javascript:" dynamic-value prefix and script tags. The server stays the
// authority; this is the guardrail layer.
func ValidateQuery(q string) error {
	l := strings.ToLower(q)
	if strings.Contains(l, "javascript:") || strings.Contains(l, "<script") {
		return &ValidationError{
			Msg: "encoded query contains script (javascript: or <script) and was refused",
			Hnt: "Use plain field=value conditions in --query.",
		}
	}
	return nil
}

func validTable(name string) error {
	if !tableName.MatchString(name) {
		return &ValidationError{Msg: fmt.Sprintf("invalid table name %q", name), Hnt: "Table names are lower-case letters, digits and underscores."}
	}
	return nil
}

func validSysID(id string) error {
	if !domain.LooksLikeSysID(id) {
		return &ValidationError{Msg: fmt.Sprintf("invalid sys_id %q: want 32 hex characters", id)}
	}
	return nil
}

// orderClause turns "field" / "-field" into an ORDERBY clause with sys_id as
// tiebreak so offsets stay stable (spec D-h). Empty means the default order.
func orderClause(spec string) (string, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", nil
	}
	desc := strings.HasPrefix(spec, "-")
	field := strings.TrimPrefix(spec, "-")
	if !tableName.MatchString(field) && !validDotted(field) {
		return "", &ValidationError{Msg: fmt.Sprintf("invalid order field %q", spec)}
	}
	clause := "ORDERBY" + field
	if desc {
		clause = "ORDERBYDESC" + field
	}
	if field != "sys_id" {
		clause += "^ORDERBYsys_id"
	}
	return clause, nil
}

func validDotted(f string) bool {
	parts := strings.Split(f, ".")
	for _, p := range parts {
		if !tableName.MatchString(p) {
			return false
		}
	}
	return len(parts) > 1
}

// joinQuery ANDs encoded queries with "^", skipping empty parts.
func joinQuery(parts ...string) string {
	var out []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, "^")
}

// cond builds "field=value", refusing values that could inject further
// encoded-query clauses ("^") or break the line.
func cond(field, value string) (string, error) {
	if value == "" || strings.ContainsAny(value, "^\r\n") {
		return "", &ValidationError{
			Msg: fmt.Sprintf("invalid value for %s: must be non-empty and must not contain '^' or line breaks", field),
		}
	}
	return field + "=" + value, nil
}

// refCond matches a reference field by sys_id or, otherwise, by the
// referenced record's name (dot-walk).
func refCond(field, ref string) (string, error) {
	if domain.LooksLikeSysID(ref) {
		return cond(field, strings.ToLower(ref))
	}
	return cond(field+".name", ref)
}
