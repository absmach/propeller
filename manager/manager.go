package manager

import (
	"context"

	"github.com/absmach/propeller/task"
	"github.com/absmach/propeller/worker"
)

type Service interface {
	CreateWorker(ctx context.Context, worker worker.Worker) (worker.Worker, error)
	GetWorker(ctx context.Context, workerID string) (worker.Worker, error)
	ListWorkers(ctx context.Context, offset, limit uint64) (worker.WorkerPage, error)
	UpdateWorker(ctx context.Context, worker worker.Worker) (worker.Worker, error)
	DeleteWorker(ctx context.Context, workerID string) error
	SelectWorker(ctx context.Context, task task.Task) (worker.Worker, error)

	CreateTask(ctx context.Context, task task.Task) (task.Task, error)
	GetTask(ctx context.Context, taskID string) (task.Task, error)
	ListTasks(ctx context.Context, offset, limit uint64) (task.TaskPage, error)
	UpdateTask(ctx context.Context, task task.Task) (task.Task, error)
	DeleteTask(ctx context.Context, taskID string) error
}
