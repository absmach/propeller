package worker

import (
	"context"

	"github.com/absmach/propeller/task"
)

type WorkerInterface interface {
	StartTask(ctx context.Context, task task.Task) error
	RunTask(ctx context.Context, taskID string, proplet *Proplet) ([]uint64, error)
	StopTask(ctx context.Context, taskID string) error
	RemoveTask(ctx context.Context, taskID string) error
}
