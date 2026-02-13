package storage

import (
	"context"
	"time"

	"github.com/absmach/propeller/pkg/proplet"
	"github.com/absmach/propeller/pkg/task"
)

// Job represents a logical grouping of tasks that work together as a cohesive unit.
// A Job aggregates one or more Tasks (e.g., an API server + reverse proxy).
type Job struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	ExecutionMode string    `json:"execution_mode"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type TaskRepository interface {
	Create(ctx context.Context, t task.Task) (task.Task, error)
	Get(ctx context.Context, id string) (task.Task, error)
	Update(ctx context.Context, t task.Task) error
	List(ctx context.Context, offset, limit uint64) ([]task.Task, uint64, error)
	ListByWorkflowID(ctx context.Context, workflowID string) ([]task.Task, error)
	ListByJobID(ctx context.Context, jobID string) ([]task.Task, error)
	Delete(ctx context.Context, id string) error
}

type PropletRepository interface {
	Create(ctx context.Context, p proplet.Proplet) error
	Get(ctx context.Context, id string) (proplet.Proplet, error)
	Update(ctx context.Context, p proplet.Proplet) error
	List(ctx context.Context, offset, limit uint64) ([]proplet.Proplet, uint64, error)
	Delete(ctx context.Context, id string) error
}

type TaskPropletRepository interface {
	Create(ctx context.Context, taskID, propletID string) error
	Get(ctx context.Context, taskID string) (string, error)
	Delete(ctx context.Context, taskID string) error
}

type JobRepository interface {
	Create(ctx context.Context, j Job) (Job, error)
	Get(ctx context.Context, id string) (Job, error)
	List(ctx context.Context, offset, limit uint64) ([]Job, uint64, error)
	Delete(ctx context.Context, id string) error
}

type MetricsRepository interface {
	CreateTaskMetrics(ctx context.Context, m TaskMetrics) error
	CreatePropletMetrics(ctx context.Context, m PropletMetrics) error
	ListTaskMetrics(ctx context.Context, taskID string, offset, limit uint64) ([]TaskMetrics, uint64, error)
	ListPropletMetrics(ctx context.Context, propletID string, offset, limit uint64) ([]PropletMetrics, uint64, error)
}
