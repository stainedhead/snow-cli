package read

import (
	"context"
	"fmt"
	"strings"

	"github.com/stainedhead/snow-cli/internal/domain"
	"github.com/stainedhead/snow-cli/internal/policymap"
	"github.com/stainedhead/snow-cli/internal/usecase"
)

// Kind is a work item family (FR-031..037).
type Kind string

// Work item kinds.
const (
	KindIncident Kind = "incident"
	KindRequest  Kind = "request"
	KindRITM     Kind = "ritm"
	KindTask     Kind = "task"
	KindProblem  Kind = "problem"
	KindChange   Kind = "change"
)

type kindInfo struct {
	table    string
	resource string
	number   domain.NumberKind
	// mine is the reference field matched against the caller.
	mine     string
	defaults []string
}

var commonFields = []string{"sys_id", "number", "short_description", "state", "priority", "assigned_to", "assignment_group", "opened_at", "sys_updated_on"}

var kinds = map[Kind]kindInfo{
	KindIncident: {"incident", policymap.ResIncident, domain.KindIncident, "assigned_to",
		append(append([]string(nil), commonFields...), "impact", "urgency", "cmdb_ci", "business_service", "caller_id")},
	KindRequest: {"sc_request", policymap.ResRequest, domain.KindRequest, "requested_for",
		[]string{"sys_id", "number", "short_description", "state", "requested_for", "opened_at", "sys_updated_on"}},
	KindRITM: {"sc_req_item", policymap.ResRITM, domain.KindRequestItem, "assigned_to",
		append(append([]string(nil), commonFields...), "cat_item", "request")},
	KindTask: {"sc_task", policymap.ResTask, domain.KindCatalogTask, "assigned_to",
		append(append([]string(nil), commonFields...), "request_item")},
	KindProblem: {"problem", policymap.ResProblem, domain.KindProblem, "assigned_to", commonFields},
	KindChange: {"change_request", policymap.ResChange, domain.KindChange, "assigned_to",
		append(append([]string(nil), commonFields...), "type", "risk")},
}

// ParseKind validates a kind name.
func ParseKind(s string) (Kind, error) {
	k := Kind(s)
	if _, ok := kinds[k]; !ok {
		return "", &ValidationError{Msg: fmt.Sprintf("unknown work item kind %q", s), Hnt: "Use incident, request, ritm, task, problem or change."}
	}
	return k, nil
}

// WorkGet reads one work item by number or sys_id.
func (s Service) WorkGet(ctx context.Context, kind Kind, ref string, o Options) (map[string]any, error) {
	info, ok := kinds[kind]
	if !ok {
		return nil, &ValidationError{Msg: fmt.Sprintf("unknown work item kind %q", kind)}
	}
	var number string
	if !domain.LooksLikeSysID(ref) {
		n, err := domain.ParseNumber(ref)
		if err != nil {
			return nil, &ValidationError{Msg: err.Error()}
		}
		if n.Kind() != info.number {
			return nil, &ValidationError{Msg: fmt.Sprintf("%s is not a %s number", n, kind), Hnt: "Use the command that matches the record number prefix."}
		}
		number = n.String()
	}
	fields := s.effectiveFields(policymap.VerbGet, info.resource, o.Fields, append(append([]string(nil), info.defaults...), "description"))
	var out map[string]any
	err := s.guardedRef(ctx, firstNonEmpty(number, strings.ToLower(ref)), policymap.VerbGet, info.resource, fields, func(ctx context.Context) error {
		var r domain.Record
		if number == "" {
			var err error
			if r, err = s.Tables.Get(ctx, info.table, strings.ToLower(ref), usecase.GetOptions{Fields: fields, Display: o.Display}); err != nil {
				return err
			}
		} else {
			res, err := s.Tables.List(ctx, usecase.ListQuery{
				Table: info.table, Query: "number=" + number, Fields: fields, Limit: 1, Display: o.Display,
			})
			if err != nil {
				return err
			}
			if len(res.Items) == 0 {
				return &NotFoundError{Msg: fmt.Sprintf("%s %s not found", kind, number)}
			}
			r = res.Items[0]
		}
		out = Present(r)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// WorkFilter narrows a work item list (FR-031).
type WorkFilter struct {
	Mine  bool
	CI    string // incident only
	App   string // incident only (business_service)
	Group string
	State string
}

// WorkList lists work items of one kind.
func (s Service) WorkList(ctx context.Context, kind Kind, f WorkFilter, o ListOptions) (ListData, error) {
	info, ok := kinds[kind]
	if !ok {
		return ListData{}, &ValidationError{Msg: fmt.Sprintf("unknown work item kind %q", kind)}
	}
	if kind != KindIncident && (f.CI != "" || f.App != "") {
		return ListData{}, &ValidationError{Msg: "--ci and --app are only supported by `incident list`"}
	}
	conds, err := f.conditions()
	if err != nil {
		return ListData{}, err
	}
	fields := s.effectiveFields(policymap.VerbList, info.resource, o.Fields, info.defaults)
	build := func(ctx context.Context) (string, error) {
		own := conds
		if f.Mine {
			user, err := s.caller(ctx)
			if err != nil {
				return "", err
			}
			c, err := cond(info.mine+".user_name", user)
			if err != nil {
				return "", err
			}
			own = append([]string{c}, own...)
		}
		return strings.Join(own, "^"), nil
	}
	return s.list(ctx, policymap.VerbList, info.resource, info.table, build, fields, o)
}

// conditions renders the static filter conditions in a fixed order.
func (f WorkFilter) conditions() ([]string, error) {
	var out []string
	add := func(c string, err error) error {
		if err != nil {
			return err
		}
		out = append(out, c)
		return nil
	}
	if f.CI != "" {
		if err := add(refCond("cmdb_ci", f.CI)); err != nil {
			return nil, err
		}
	}
	if f.App != "" {
		if err := add(refCond("business_service", f.App)); err != nil {
			return nil, err
		}
	}
	if f.Group != "" {
		if err := add(refCond("assignment_group", f.Group)); err != nil {
			return nil, err
		}
	}
	if f.State != "" {
		if err := add(cond("state", f.State)); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// caller resolves the identity's ServiceNow user name through the whoami port
// (the identity call is part of the surrounding audited read).
func (s Service) caller(ctx context.Context) (string, error) {
	id, err := s.Identity.Whoami(ctx)
	if err != nil {
		return "", err
	}
	if id.User == "" {
		return "", &ValidationError{Msg: "cannot resolve the caller identity for --mine / my work"}
	}
	return id.User, nil
}

var myWorkClasses = map[string]string{
	"incident": "sys_class_name=incident",
	"request":  "sys_class_nameINsc_request,sc_req_item",
	"task":     "sys_class_name=sc_task",
	"change":   "sys_class_name=change_request",
}

// MyWork lists open task records assigned to the caller (FR-030). An empty
// kind means every kind. The resource is the Task table (`table:task`).
func (s Service) MyWork(ctx context.Context, kind string, o ListOptions) (ListData, error) {
	var class string
	if kind != "" {
		c, ok := myWorkClasses[kind]
		if !ok {
			return ListData{}, &ValidationError{Msg: fmt.Sprintf("unknown --kind %q", kind), Hnt: "Use incident, request, task or change."}
		}
		class = c
	}
	res := policymap.Table("task")
	fields := s.effectiveFields(policymap.VerbList, res, o.Fields,
		[]string{"sys_id", "number", "sys_class_name", "short_description", "state", "priority", "assigned_to", "sys_updated_on"})
	build := func(ctx context.Context) (string, error) {
		user, err := s.caller(ctx)
		if err != nil {
			return "", err
		}
		c, err := cond("assigned_to.user_name", user)
		if err != nil {
			return "", err
		}
		q := "active=true^" + c
		if class != "" {
			q += "^" + class
		}
		return q, nil
	}
	return s.list(ctx, policymap.VerbList, res, "task", build, fields, o)
}
