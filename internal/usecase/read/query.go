package read

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/stainedhead/snow-cli/internal/domain"
)

// QueryInfo is what ParseQuery learned about an accepted encoded query.
type QueryInfo struct {
	// Fields are the field names (dot-walked names included) the query
	// references, in clause order without duplicates: conditions and ORDERBY.
	// They are submitted to the policy as requested fields (FR-R05).
	Fields []string
	// HasOrderBy is set when a clause (not a value) is an ORDERBY.
	HasOrderBy bool
}

// queryOps are the encoded-query operators a caller may use. DYNAMIC and the
// change-tracking operators are deliberately absent.
var queryOps = func() []string {
	ops := []string{
		"!=", ">=", "<=", "=", ">", "<",
		"STARTSWITH", "ENDSWITH", "LIKE", "NOTLIKE", "CONTAINS", "DOESNOTCONTAIN",
		"IN", "NOTIN", "ISEMPTY", "ISNOTEMPTY", "EMPTYSTRING", "BETWEEN",
		"SAMEAS", "NSAMEAS", "ON", "NOTON", "ANYTHING", "INSTANCEOF",
	}
	sort.Slice(ops, func(i, j int) bool { return len(ops[i]) > len(ops[j]) })
	return ops
}()

var gsCall = regexp.MustCompile(`(^|[^a-z0-9_])gs\.`)

func badQuery(msg string) error {
	return &ValidationError{
		Msg: "encoded query refused: " + msg,
		Hnt: "Use plain `field OP value` conditions joined by ^ (and ^OR); NQ, DYNAMIC and script are not allowed.",
	}
}

// ParseQuery accepts only `field OP value` clauses joined by "^" (and "^OR"),
// plus ORDERBY / ORDERBYDESC clauses (FR-R05). It rejects ^NQ and ^EQ, DYNAMIC
// and other unknown operators, javascript / gs. in any case or percent
// encoding, script tags and control characters. The server stays the
// authority; this is the guardrail layer. An empty query is valid.
func ParseQuery(q string) (QueryInfo, error) {
	var info QueryInfo
	if q == "" {
		return info, nil
	}
	for _, r := range q {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return info, badQuery("it contains a control character")
		}
	}
	if hasScript(q) {
		return info, badQuery("it contains script (javascript:, gs. or <script)")
	}
	seen := map[string]bool{}
	addField := func(f string) {
		if !seen[f] {
			seen[f] = true
			info.Fields = append(info.Fields, f)
		}
	}
	for _, clause := range strings.Split(q, "^") {
		field, isOrder, err := parseClause(clause)
		if err != nil {
			return QueryInfo{}, err
		}
		if isOrder {
			info.HasOrderBy = true
		}
		addField(field)
	}
	return info, nil
}

// hasScript looks for script markers in the query as written and after up to
// three rounds of percent-decoding, case-insensitively.
func hasScript(q string) bool {
	cur := q
	for i := 0; i < 4; i++ {
		l := strings.ToLower(cur)
		if strings.Contains(l, "javascript") || strings.Contains(l, "<script") || gsCall.MatchString(l) {
			return true
		}
		next, err := url.QueryUnescape(cur)
		if err != nil || next == cur {
			return false
		}
		cur = next
	}
	return false
}

var fieldSegment = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

func validFieldName(f string) bool {
	for _, seg := range strings.Split(f, ".") {
		if !fieldSegment.MatchString(seg) {
			return false
		}
	}
	return true
}

// parseClause validates one ^-separated clause and returns its field name.
func parseClause(c string) (field string, isOrder bool, err error) {
	switch {
	case c == "":
		return "", false, badQuery("it has an empty clause")
	case strings.HasPrefix(c, "NQ"), strings.HasPrefix(c, "EQ"):
		return "", false, badQuery("^NQ / ^EQ (new query) is not allowed")
	case strings.HasPrefix(c, "ORDERBYDESC"):
		field, isOrder = strings.TrimPrefix(c, "ORDERBYDESC"), true
	case strings.HasPrefix(c, "ORDERBY"):
		field, isOrder = strings.TrimPrefix(c, "ORDERBY"), true
	}
	if isOrder {
		if !validFieldName(field) {
			return "", false, badQuery(fmt.Sprintf("invalid ORDERBY field %q", field))
		}
		return field, true, nil
	}
	c = strings.TrimPrefix(c, "OR")
	end := 0
	for end < len(c) && (c[end] == '_' || c[end] == '.' || c[end] >= 'a' && c[end] <= 'z' || c[end] >= '0' && c[end] <= '9') {
		end++
	}
	field, rest := c[:end], c[end:]
	if !validFieldName(field) {
		return "", false, badQuery(fmt.Sprintf("clause %q does not start with a field name", c))
	}
	for _, op := range queryOps {
		if strings.HasPrefix(rest, op) {
			return field, false, nil
		}
	}
	return "", false, badQuery(fmt.Sprintf("clause %q has no supported operator", c))
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
// It also returns the field so the policy sees it (FR-R05).
func orderClause(spec string) (clause, field string, err error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", "", nil
	}
	desc := strings.HasPrefix(spec, "-")
	field = strings.TrimPrefix(spec, "-")
	if !tableName.MatchString(field) && !validDotted(field) {
		return "", "", &ValidationError{Msg: fmt.Sprintf("invalid order field %q", spec)}
	}
	clause = "ORDERBY" + field
	if desc {
		clause = "ORDERBYDESC" + field
	}
	if field != "sys_id" {
		clause += "^ORDERBYsys_id"
	}
	return clause, field, nil
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
