package read

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/snow-cli/internal/domain"
	"github.com/stainedhead/snow-cli/internal/policymap"
	"github.com/stainedhead/snow-cli/internal/usecase"
)

// Relationship traversal bounds (FR-025).
const (
	DefaultRelatedDepth   = 2
	MaxRelatedDepth       = 5
	DefaultRelatedNodeCap = 200
	relFrontierChunk      = 25
	relPageLimit          = 200
	relMaxPages           = 10
)

const (
	tableCI  = "cmdb_ci"
	tableRel = "cmdb_rel_ci"
)

var ciDefaultFields = []string{"sys_id", "name", "sys_class_name", "short_description", "operational_status", "install_status", "environment", "support_group", "owned_by"}

var ciIdentityFields = []string{"sys_id", "name", "sys_class_name"}

var appDefaultFields = []string{"sys_id", "name", "sys_class_name", "short_description", "owned_by", "managed_by", "support_group", "operational_status", "business_criticality"}

// ciClass matches tables of the cmdb_ci hierarchy by name.
// ASSUMPTION(unverified against a real instance): CI classes are recognised by the "cmdb_ci" name prefix
// (policy pattern table:cmdb_ci*); custom classes outside it need the class hierarchy cache (deferred, A-12).
var ciClass = regexp.MustCompile(`^cmdb_ci[a-z0-9_]*$`)

// resolveCI finds one record of table by sys_id or exact name. Several name
// matches are an exit 9 error listing the candidates (FR-023).
func (s Service) resolveCI(ctx context.Context, table, ref string, fields []string, display bool) (domain.Record, error) {
	if domain.LooksLikeSysID(ref) {
		return s.Tables.Get(ctx, table, strings.ToLower(ref), usecase.GetOptions{Fields: fields, Display: display})
	}
	c, err := cond("name", ref)
	if err != nil {
		return domain.Record{}, err
	}
	res, err := s.Tables.List(ctx, usecase.ListQuery{
		Table: table, Query: c, Fields: union(fields, ciIdentityFields...), Limit: 6, Display: display,
	})
	if err != nil {
		return domain.Record{}, err
	}
	switch len(res.Items) {
	case 0:
		return domain.Record{}, &NotFoundError{Msg: fmt.Sprintf("no %s named %q", table, ref)}
	case 1:
		return res.Items[0], nil
	}
	return domain.Record{}, ambiguous(ref, res.Items)
}

func ambiguous(ref string, items []domain.Record) error {
	var b strings.Builder
	fmt.Fprintf(&b, "%q matches %d records; pass the sys_id of one: ", ref, len(items))
	for i, r := range items {
		if i > 0 {
			b.WriteString("; ")
		}
		fmt.Fprintf(&b, "%s (%s) %s", r.Get("name"), firstNonEmpty(r.Get("sys_class_name"), r.Table), r.Get("sys_id"))
	}
	return &ValidationError{Msg: b.String(), Hnt: "Re-run with the sys_id of the intended record."}
}

// CIGet reads one CI by sys_id or exact name (FR-023).
func (s Service) CIGet(ctx context.Context, ref string, o Options) (map[string]any, error) {
	fields := s.effectiveFields(policymap.VerbGet, policymap.ResCMDBCI, o.Fields, ciDefaultFields)
	policyFields := fields
	if !domain.LooksLikeSysID(ref) {
		policyFields = union(fields, ciIdentityFields...)
	}
	var out map[string]any
	err := s.guarded(ctx, policymap.VerbGet, policymap.ResCMDBCI, policyFields, func(ctx context.Context) error {
		r, err := s.resolveCI(ctx, tableCI, ref, fields, o.Display)
		if err != nil {
			return err
		}
		out = Present(r)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// CISearch lists CIs of a class (FR-024). An empty class means cmdb_ci.
func (s Service) CISearch(ctx context.Context, class string, o ListOptions) (ListData, error) {
	if class == "" {
		class = tableCI
	}
	if !ciClass.MatchString(class) {
		return ListData{}, &ValidationError{
			Msg: fmt.Sprintf("class %q is not in the cmdb_ci hierarchy", class),
			Hnt: "Use a CMDB class table such as cmdb_ci_server.",
		}
	}
	fields := s.effectiveFields(policymap.VerbSearch, policymap.ResCMDBCI, o.Fields, ciDefaultFields)
	return s.list(ctx, policymap.VerbSearch, policymap.ResCMDBCI, class, noQuery, fields, o)
}

// NodeData is a CI reached by a relationship traversal.
type NodeData struct {
	SysID string `json:"sys_id"`
	Name  string `json:"name"`
	Class string `json:"class,omitempty"`
	Depth int    `json:"depth"`
}

// EdgeData is one cmdb_rel_ci relationship.
type EdgeData struct {
	Parent     string `json:"parent"`
	ParentName string `json:"parent_name,omitempty"`
	Child      string `json:"child"`
	ChildName  string `json:"child_name,omitempty"`
	Type       string `json:"type,omitempty"`
	Depth      int    `json:"depth"`
}

// RelatedData is the data of `cmdb ci related`. Direction "down" follows
// relationships where the CI is the parent (what it depends on or contains);
// "up" follows relationships where it is the child (what depends on it).
type RelatedData struct {
	Root      NodeData   `json:"root"`
	Direction string     `json:"direction"`
	Depth     int        `json:"depth"`
	Nodes     []NodeData `json:"nodes"`
	Edges     []EdgeData `json:"edges"`
	Truncated bool       `json:"truncated,omitempty"`
}

// CIRelated traverses cmdb_rel_ci breadth first (FR-025). Depth 0 means the
// default; the traversal is cycle-safe and capped at RelatedNodeCap nodes.
// ASSUMPTION(unverified against a real instance): the Table API resolves dot-walked fields in sysparm_fields
// (parent.name, child.sys_class_name, type.name); without them names are empty and sys_ids remain.
func (s Service) CIRelated(ctx context.Context, ref, direction string, depth int) (RelatedData, error) {
	if direction == "" {
		direction = "down"
	}
	if direction != "down" && direction != "up" {
		return RelatedData{}, &ValidationError{Msg: fmt.Sprintf("invalid --direction %q", direction), Hnt: "Use up or down."}
	}
	if depth == 0 {
		depth = DefaultRelatedDepth
	}
	if depth < 0 || depth > MaxRelatedDepth {
		return RelatedData{}, &ValidationError{Msg: fmt.Sprintf("--depth must be between 1 and %d", MaxRelatedDepth)}
	}
	var out RelatedData
	policyFields := ciIdentityFields
	err := s.guarded(ctx, policymap.VerbRelated, policymap.ResCMDBCI, policyFields, func(ctx context.Context) error {
		root, err := s.resolveCI(ctx, tableCI, ref, ciIdentityFields, false)
		if err != nil {
			return err
		}
		out, err = s.traverse(ctx, nodeOf(root, 0), direction, depth)
		return err
	})
	if err != nil {
		return RelatedData{}, err
	}
	return out, nil
}

func nodeOf(r domain.Record, depth int) NodeData {
	return NodeData{SysID: r.Get("sys_id"), Name: r.Get("name"), Class: r.Get("sys_class_name"), Depth: depth}
}

var relFields = []string{
	"sys_id", "parent", "child", "type", "parent.name", "child.name",
	"parent.sys_class_name", "child.sys_class_name", "type.name",
}

func (s Service) traverse(ctx context.Context, root NodeData, direction string, depth int) (RelatedData, error) {
	nodeCap := s.RelatedNodeCap
	if nodeCap <= 0 {
		nodeCap = DefaultRelatedNodeCap
	}
	out := RelatedData{Root: root, Direction: direction, Depth: depth, Nodes: []NodeData{}, Edges: []EdgeData{}}
	near, far := "parent", "child" // down: match the parent, reach the child
	if direction == "up" {
		near, far = "child", "parent"
	}
	visited := map[string]bool{root.SysID: true}
	frontier := []string{root.SysID}
	for d := 1; d <= depth && len(frontier) > 0; d++ {
		var next []string
		for i := 0; i < len(frontier); i += relFrontierChunk {
			chunk := frontier[i:min(i+relFrontierChunk, len(frontier))]
			clauses := make([]string, len(chunk))
			for j, id := range chunk {
				clauses[j] = near + "=" + id
			}
			query := strings.Join(clauses, "^OR")
			for page := 0; page < relMaxPages; page++ {
				res, err := s.Tables.List(ctx, usecase.ListQuery{
					Table: tableRel, Query: query, Fields: relFields, Limit: s.limitOr(relPageLimit), Offset: page * s.limitOr(relPageLimit),
				})
				if err != nil {
					return RelatedData{}, err
				}
				for _, r := range res.Items {
					out.Edges = append(out.Edges, EdgeData{
						Parent: r.Get("parent"), ParentName: r.Get("parent.name"),
						Child: r.Get("child"), ChildName: r.Get("child.name"),
						Type: firstNonEmpty(r.Get("type.name"), r.Get("type")), Depth: d,
					})
					id := r.Get(far)
					if id == "" || visited[id] {
						continue
					}
					if len(out.Nodes) >= nodeCap {
						out.Truncated = true
						continue
					}
					visited[id] = true
					out.Nodes = append(out.Nodes, NodeData{SysID: id, Name: r.Get(far + ".name"), Class: r.Get(far + ".sys_class_name"), Depth: d})
					next = append(next, id)
				}
				if len(res.Items) < s.limitOr(relPageLimit) {
					break
				}
				if page == relMaxPages-1 {
					out.Truncated = true
				}
			}
		}
		frontier = next
	}
	return out, nil
}

// limitOr clamps a use-case page size to the policy cap.
func (s Service) limitOr(n int) int { return s.Limits.ClampResults(n) }

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// AppData is the data of `cmdb app`.
type AppData struct {
	Application map[string]any `json:"application"`
	Related     RelatedData    `json:"related"`
}

// App resolves a business service or application by name or sys_id and
// lists its direct relationships (FR-026). Display values are read so owner
// and support group show as names.
func (s Service) App(ctx context.Context, name string, o Options) (AppData, error) {
	fields := s.effectiveFields(policymap.VerbGet, policymap.ResCMDBApp, o.Fields, appDefaultFields)
	policyFields := union(fields, ciIdentityFields...)
	var out AppData
	err := s.guarded(ctx, policymap.VerbGet, policymap.ResCMDBApp, policyFields, func(ctx context.Context) error {
		var found []domain.Record
		for _, table := range []string{"cmdb_ci_service", "cmdb_ci_appl"} {
			r, err := s.resolveCI(ctx, table, name, fields, true)
			switch {
			case err == nil:
				found = append(found, r)
			case output.CategoryOf(err) == output.CategoryNotFound:
			default:
				return err
			}
		}
		switch len(found) {
		case 0:
			return &NotFoundError{Msg: fmt.Sprintf("no application or business service %q", name)}
		case 1:
		default:
			return ambiguous(name, found)
		}
		rel, err := s.traverse(ctx, nodeOf(found[0], 0), "down", 1)
		if err != nil {
			return err
		}
		out = AppData{Application: Present(found[0]), Related: rel}
		return nil
	})
	if err != nil {
		return AppData{}, err
	}
	return out, nil
}
