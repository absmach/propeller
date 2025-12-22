package manager

import (
	"context"

	"github.com/absmach/propeller/pkg/proplet"
	"github.com/absmach/propeller/task"
)

type Service interface {
	GetProplet(ctx context.Context, propletID string) (proplet.Proplet, error)
	ListProplets(ctx context.Context, offset, limit uint64) (proplet.PropletPage, error)
	SelectProplet(ctx context.Context, task task.Task) (proplet.Proplet, error)

	CreateTask(ctx context.Context, task task.Task) (task.Task, error)
	GetTask(ctx context.Context, taskID string) (task.Task, error)
	ListTasks(ctx context.Context, offset, limit uint64) (task.TaskPage, error)
	UpdateTask(ctx context.Context, task task.Task) (task.Task, error)
	DeleteTask(ctx context.Context, taskID string) error
	StartTask(ctx context.Context, taskID string) error
	StopTask(ctx context.Context, taskID string) error

	Subscribe(ctx context.Context) error
}
