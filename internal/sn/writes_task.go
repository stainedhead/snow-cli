package sn

import (
	"context"

	"github.com/stainedhead/snow-cli/internal/domain"
	"github.com/stainedhead/snow-cli/internal/usecase"
	"github.com/stainedhead/snow-cli/internal/usecase/write"
)

const taskTable = "sc_task"

// TaskAdapter implements usecase.TaskWriter and write.TaskFetcher.
type TaskAdapter struct{ c *Client }

var (
	_ usecase.TaskWriter = (*TaskAdapter)(nil)
	_ write.TaskFetcher  = (*TaskAdapter)(nil)
)

// NewTaskAdapter binds the sc_task endpoints to a client.
func NewTaskAdapter(c *Client) *TaskAdapter { return &TaskAdapter{c: c} }

// FetchTask reads a task including its assignee's user name.
func (a *TaskAdapter) FetchTask(ctx context.Context, ref string) (domain.Record, error) {
	return a.c.wFetch(ctx, taskTable, ref, write.AssignedToUserName)
}

// UpdateTask patches a task with the sys_mod_count guard (D-j).
func (a *TaskAdapter) UpdateTask(ctx context.Context, in usecase.TaskUpdate) (domain.WriteResult, error) {
	return a.c.wGuardedPatch(ctx, taskTable, in.Ref, in.Fields, in.ExpectedModCount)
}
