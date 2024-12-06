package middleware

import (
	"context"
	"time"

	"github.com/absmach/propeller/manager"
	"github.com/absmach/propeller/task"
	"github.com/absmach/propeller/worker"
	"github.com/go-kit/kit/metrics"
)

var _ manager.Service = (*metricsMiddleware)(nil)

type metricsMiddleware struct {
	counter metrics.Counter
	latency metrics.Histogram
	svc     manager.Service
}

func Metrics(counter metrics.Counter, latency metrics.Histogram, svc manager.Service) manager.Service {
	return &metricsMiddleware{
		counter: counter,
		latency: latency,
		svc:     svc,
	}
}

func (mm *metricsMiddleware) CreateWorker(ctx context.Context, w worker.Worker) (worker.Worker, error) {
	defer func(begin time.Time) {
		mm.counter.With("method", "create-worker").Add(1)
		mm.latency.With("method", "create-worker").Observe(time.Since(begin).Seconds())
	}(time.Now())

	return mm.svc.CreateWorker(ctx, w)
}

func (mm *metricsMiddleware) GetWorker(ctx context.Context, id string) (worker.Worker, error) {
	defer func(begin time.Time) {
		mm.counter.With("method", "get-worker").Add(1)
		mm.latency.With("method", "get-worker").Observe(time.Since(begin).Seconds())
	}(time.Now())

	return mm.svc.GetWorker(ctx, id)
}

func (mm *metricsMiddleware) ListWorkers(ctx context.Context, offset, limit uint64) (worker.WorkerPage, error) {
	defer func(begin time.Time) {
		mm.counter.With("method", "list-workers").Add(1)
		mm.latency.With("method", "list-workers").Observe(time.Since(begin).Seconds())
	}(time.Now())

	return mm.svc.ListWorkers(ctx, offset, limit)
}

func (mm *metricsMiddleware) UpdateWorker(ctx context.Context, w worker.Worker) (worker.Worker, error) {
	defer func(begin time.Time) {
		mm.counter.With("method", "update-worker").Add(1)
		mm.latency.With("method", "update-worker").Observe(time.Since(begin).Seconds())
	}(time.Now())

	return mm.svc.UpdateWorker(ctx, w)
}

func (mm *metricsMiddleware) DeleteWorker(ctx context.Context, id string) error {
	defer func(begin time.Time) {
		mm.counter.With("method", "delete-worker").Add(1)
		mm.latency.With("method", "delete-worker").Observe(time.Since(begin).Seconds())
	}(time.Now())

	return mm.svc.DeleteWorker(ctx, id)
}

func (mm *metricsMiddleware) SelectWorker(ctx context.Context, t task.Task) (worker.Worker, error) {
	defer func(begin time.Time) {
		mm.counter.With("method", "select-worker").Add(1)
		mm.latency.With("method", "select-worker").Observe(time.Since(begin).Seconds())
	}(time.Now())

	return mm.svc.SelectWorker(ctx, t)
}

func (mm *metricsMiddleware) CreateTask(ctx context.Context, t task.Task) (task.Task, error) {
	defer func(begin time.Time) {
		mm.counter.With("method", "create-task").Add(1)
		mm.latency.With("method", "create-task").Observe(time.Since(begin).Seconds())
	}(time.Now())

	return mm.svc.CreateTask(ctx, t)
}

func (mm *metricsMiddleware) GetTask(ctx context.Context, id string) (task.Task, error) {
	defer func(begin time.Time) {
		mm.counter.With("method", "get-task").Add(1)
		mm.latency.With("method", "get-task").Observe(time.Since(begin).Seconds())
	}(time.Now())

	return mm.svc.GetTask(ctx, id)
}

func (mm *metricsMiddleware) ListTasks(ctx context.Context, offset, limit uint64) (task.TaskPage, error) {
	defer func(begin time.Time) {
		mm.counter.With("method", "list-tasks").Add(1)
		mm.latency.With("method", "list-tasks").Observe(time.Since(begin).Seconds())
	}(time.Now())

	return mm.svc.ListTasks(ctx, offset, limit)
}

func (mm *metricsMiddleware) UpdateTask(ctx context.Context, t task.Task) (task.Task, error) {
	defer func(begin time.Time) {
		mm.counter.With("method", "update-task").Add(1)
		mm.latency.With("method", "update-task").Observe(time.Since(begin).Seconds())
	}(time.Now())

	return mm.svc.UpdateTask(ctx, t)
}

func (mm *metricsMiddleware) DeleteTask(ctx context.Context, id string) error {
	defer func(begin time.Time) {
		mm.counter.With("method", "delete-task").Add(1)
		mm.latency.With("method", "delete-task").Observe(time.Since(begin).Seconds())
	}(time.Now())

	return mm.svc.DeleteTask(ctx, id)
}
