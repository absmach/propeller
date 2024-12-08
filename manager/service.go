package manager

import (
	"context"
	"time"

	"github.com/absmach/propeller/pkg/errors"
	"github.com/absmach/propeller/pkg/scheduler"
	"github.com/absmach/propeller/pkg/storage"
	"github.com/absmach/propeller/task"
	"github.com/absmach/propeller/worker"
	"github.com/google/uuid"
)

type service struct {
	tasksDB      storage.Storage
	workersDB    storage.Storage
	workerTaskDB storage.Storage
	taskWorkerDB storage.Storage
	scheduler    scheduler.Scheduler
}

func NewService(
	tasksDB, workersDB, workerTaskDB, taskWorkerDB storage.Storage,
	s scheduler.Scheduler,
) Service {
	return &service{
		tasksDB:      tasksDB,
		workersDB:    workersDB,
		workerTaskDB: workerTaskDB,
		taskWorkerDB: taskWorkerDB,
		scheduler:    s,
	}
}

func (svc *service) CreateWorker(ctx context.Context, w worker.Worker) (worker.Worker, error) {
	w.ID = uuid.New().String()
	if err := svc.workersDB.Create(ctx, w.ID, w); err != nil {
		return worker.Worker{}, err
	}

	return w, nil
}

func (svc *service) GetWorker(ctx context.Context, workerID string) (worker.Worker, error) {
	data, err := svc.workersDB.Get(ctx, workerID)
	if err != nil {
		return worker.Worker{}, err
	}
	w, ok := data.(worker.Worker)
	if !ok {
		return worker.Worker{}, errors.ErrInvalidData
	}

	return w, nil
}

func (svc *service) ListWorkers(ctx context.Context, offset, limit uint64) (worker.WorkerPage, error) {
	data, total, err := svc.workersDB.List(ctx, offset, limit)
	if err != nil {
		return worker.WorkerPage{}, err
	}
	workers := make([]worker.Worker, total)
	for i := range data {
		w, ok := data[i].(worker.Worker)
		if !ok {
			return worker.WorkerPage{}, errors.ErrInvalidData
		}
		workers[i] = w
	}

	return worker.WorkerPage{
		Offset:  offset,
		Limit:   limit,
		Total:   total,
		Workers: workers,
	}, nil
}

func (svc *service) UpdateWorker(ctx context.Context, w worker.Worker) (worker.Worker, error) {
	if err := svc.workersDB.Update(ctx, w.ID, w); err != nil {
		return worker.Worker{}, err
	}

	return w, nil
}

func (svc *service) DeleteWorker(ctx context.Context, workerID string) error {
	return svc.workersDB.Delete(ctx, workerID)
}

func (svc *service) SelectWorker(_ context.Context, t task.Task) (worker.Worker, error) {
	return svc.scheduler.SelectWorker(t, nil)
}

func (svc *service) CreateTask(ctx context.Context, t task.Task) (task.Task, error) {
	t.ID = uuid.NewString()
	t.CreatedAt = time.Now()

	if err := svc.tasksDB.Create(ctx, t.ID, t); err != nil {
		return task.Task{}, err
	}

	return t, nil
}

func (svc *service) GetTask(ctx context.Context, taskID string) (task.Task, error) {
	data, err := svc.tasksDB.Get(ctx, taskID)
	if err != nil {
		return task.Task{}, err
	}
	t, ok := data.(task.Task)
	if !ok {
		return task.Task{}, errors.ErrInvalidData
	}

	return t, nil
}

func (svc *service) ListTasks(ctx context.Context, offset, limit uint64) (task.TaskPage, error) {
	data, total, err := svc.tasksDB.List(ctx, offset, limit)
	if err != nil {
		return task.TaskPage{}, err
	}

	tasks := make([]task.Task, total)
	for i := range data {
		t, ok := data[i].(task.Task)
		if !ok {
			return task.TaskPage{}, errors.ErrInvalidData
		}

		tasks[i] = t
	}

	return task.TaskPage{
		Offset: offset,
		Limit:  limit,
		Total:  total,
		Tasks:  tasks,
	}, nil
}

func (svc *service) UpdateTask(ctx context.Context, t task.Task) (task.Task, error) {
	if err := svc.tasksDB.Update(ctx, t.ID, t); err != nil {
		return task.Task{}, err
	}

	return t, nil
}

func (svc *service) DeleteTask(ctx context.Context, taskID string) error {
	return svc.tasksDB.Delete(ctx, taskID)
}
