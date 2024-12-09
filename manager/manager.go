package manager

import (
	"context"

	"github.com/absmach/propeller/proplet"
	"github.com/absmach/propeller/task"
)

type Service interface {
	CreateProplet(ctx context.Context, proplet proplet.Proplet) (proplet.Proplet, error)
	GetProplet(ctx context.Context, propletID string) (proplet.Proplet, error)
	ListProplets(ctx context.Context, offset, limit uint64) (proplet.PropletPage, error)
	UpdateProplet(ctx context.Context, proplet proplet.Proplet) (proplet.Proplet, error)
	DeleteProplet(ctx context.Context, propletID string) error
	SelectProplet(ctx context.Context, task task.Task) (proplet.Proplet, error)

	CreateTask(ctx context.Context, task task.Task) (task.Task, error)
	GetTask(ctx context.Context, taskID string) (task.Task, error)
	ListTasks(ctx context.Context, offset, limit uint64) (task.TaskPage, error)
	UpdateTask(ctx context.Context, task task.Task) (task.Task, error)
	DeleteTask(ctx context.Context, taskID string) error
}
