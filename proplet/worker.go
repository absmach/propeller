package proplet

import (
	"context"

	"github.com/absmach/propeller/task"
)

type Service interface {
	StartTask(ctx context.Context, task task.Task) error
	RunTask(ctx context.Context, taskID string) ([]uint64, error)
	StopTask(ctx context.Context, taskID string) error
	RemoveTask(ctx context.Context, taskID string) error
}

type Proplet struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	TaskCount uint64 `json:"task_count"`
}

type PropletPage struct {
	Offset   uint64    `json:"offset"`
	Limit    uint64    `json:"limit"`
	Total    uint64    `json:"total"`
	Proplets []Proplet `json:"proplets"`
}
