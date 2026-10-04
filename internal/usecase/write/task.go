package write

import (
	"context"
	"strconv"
	"strings"

	"github.com/stainedhead/agent-cli-core/policy"
	"github.com/stainedhead/snow-cli/internal/domain"
	"github.com/stainedhead/snow-cli/internal/policymap"
	"github.com/stainedhead/snow-cli/internal/usecase"
)

// AssignedToUserName is the dot-walked field the fetcher must return so the
// assignee can be compared with the caller's user name.
//
// ASSUMPTION(unverified against a real instance): the Table API returns the
// dot-walked key "assigned_to.user_name" for sysparm_fields=assigned_to.user_name.
const AssignedToUserName = "assigned_to.user_name"

// allowedTaskFields are the only fields an agent may write on a catalog task
// (FR-045).
var allowedTaskFields = map[string]bool{"work_notes": true, "comments": true, "state": true, "assigned_to": true}

// allowedTaskStates is the limited state set.
//
// ASSUMPTION(unverified against a real instance): OOB sc_task states, 2 =
// Work in Progress and 3 = Closed Complete; other states are refused.
var allowedTaskStates = map[string]bool{"2": true, "3": true}

// TaskFetcher reads a task with AssignedToUserName populated.
type TaskFetcher interface {
	FetchTask(ctx context.Context, ref string) (domain.Record, error)
}

// TaskService updates catalog tasks.
type TaskService struct {
	Base
	Writer   usecase.TaskWriter
	Fetcher  TaskFetcher
	Identity usecase.Identity
	// ExpectedModCount is a sys_mod_count the caller read earlier; 0 uses the
	// count fetched for the assignment check (FR-R08).
	ExpectedModCount int
}

func parseTaskRef(ref string) (string, error) {
	if domain.LooksLikeSysID(ref) {
		return strings.ToLower(ref), nil
	}
	n, err := domain.ParseNumber(ref)
	if err != nil || n.Kind() != domain.KindCatalogTask {
		return "", invalid("%q is not a catalog task number (SCTASK...) or sys_id", ref)
	}
	return n.String(), nil
}

func validateTaskFields(fields map[string]string) error {
	if len(fields) == 0 {
		return invalid("at least one of work_notes, comments, state, assigned_to is required")
	}
	for k, v := range fields {
		if !allowedTaskFields[k] {
			return invalid("field %q cannot be updated on a task (allowed: work_notes, comments, state, assigned_to)", k)
		}
		if k == "state" && !allowedTaskStates[v] {
			return invalid("task state %q is not permitted (allowed: 2 work in progress, 3 closed complete)", v)
		}
	}
	return nil
}

// Update implements FR-045: only the caller's own tasks can be changed
// (an unassigned task may be claimed by assigning it to the caller).
func (s TaskService) Update(ctx context.Context, ref string, fields map[string]string) (domain.WriteResult, error) {
	id, err := parseTaskRef(ref)
	if err != nil {
		return domain.WriteResult{}, err
	}
	if err := validateTaskFields(fields); err != nil {
		return domain.WriteResult{}, err
	}
	req := policymap.WithValues(policymap.NewRequest(policymap.VerbUpdate, policymap.ResTask), policyValues(fields))
	send := s.withProvenance(fields)
	var out domain.WriteResult
	err = s.Guard.Run(ctx, usecase.Action{Kind: usecase.Write, Request: req, Ref: domain.AuditRef(id)}, func(ctx context.Context, d policy.Decision) (int, error) {
		me, err := s.Identity.Whoami(ctx)
		if err != nil {
			return 0, err
		}
		if v, ok := fields["assigned_to"]; ok && !strings.EqualFold(v, me.User) {
			return 0, &DeniedError{Msg: "assigned_to may only be set to the calling identity"}
		}
		task, err := s.Fetcher.FetchTask(ctx, id)
		if err != nil {
			return 0, err
		}
		cur := task.Get(AssignedToUserName)
		_, claiming := fields["assigned_to"]
		switch {
		case strings.EqualFold(cur, me.User):
		case cur == "" && claiming:
		default:
			return 0, &DeniedError{Msg: "task " + id + " is not assigned to the calling identity"}
		}
		if s.preview(ctx, d) {
			out = previewResult("sc_task", withRef(send, id))
			return 0, nil
		}
		if err := s.confirm("Update task " + id + " (" + strings.Join(sortedKeys(fields), ", ") + ")?"); err != nil {
			return 0, err
		}
		expected := s.ExpectedModCount
		if n, err := strconv.Atoi(task.Get("sys_mod_count")); expected == 0 && err == nil && n > 0 {
			expected = n
		}
		res, err := s.Writer.UpdateTask(ctx, usecase.TaskUpdate{Ref: id, Fields: send, ExpectedModCount: expected})
		if err != nil {
			noteOutcome(ctx, err)
			return 0, err
		}
		out = res
		return statusOK, nil
	})
	if err != nil {
		return domain.WriteResult{}, err
	}
	return out, nil
}
