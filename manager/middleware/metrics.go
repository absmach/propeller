package middleware

import (
	"context"
	"time"

	"github.com/absmach/propeller/manager"
	"github.com/absmach/propeller/proplet"
	"github.com/absmach/propeller/task"
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

func (mm *metricsMiddleware) CreateProplet(ctx context.Context, w proplet.Proplet) (proplet.Proplet, error) {
	defer func(begin time.Time) {
		mm.counter.With("method", "create-proplet").Add(1)
		mm.latency.With("method", "create-proplet").Observe(time.Since(begin).Seconds())
	}(time.Now())

	return mm.svc.CreateProplet(ctx, w)
}

func (mm *metricsMiddleware) GetProplet(ctx context.Context, id string) (proplet.Proplet, error) {
	defer func(begin time.Time) {
		mm.counter.With("method", "get-proplet").Add(1)
		mm.latency.With("method", "get-proplet").Observe(time.Since(begin).Seconds())
	}(time.Now())

	return mm.svc.GetProplet(ctx, id)
}

func (mm *metricsMiddleware) ListProplets(ctx context.Context, offset, limit uint64) (proplet.PropletPage, error) {
	defer func(begin time.Time) {
		mm.counter.With("method", "list-proplets").Add(1)
		mm.latency.With("method", "list-proplets").Observe(time.Since(begin).Seconds())
	}(time.Now())

	return mm.svc.ListProplets(ctx, offset, limit)
}

func (mm *metricsMiddleware) UpdateProplet(ctx context.Context, w proplet.Proplet) (proplet.Proplet, error) {
	defer func(begin time.Time) {
		mm.counter.With("method", "update-proplet").Add(1)
		mm.latency.With("method", "update-proplet").Observe(time.Since(begin).Seconds())
	}(time.Now())

	return mm.svc.UpdateProplet(ctx, w)
}

func (mm *metricsMiddleware) DeleteProplet(ctx context.Context, id string) error {
	defer func(begin time.Time) {
		mm.counter.With("method", "delete-proplet").Add(1)
		mm.latency.With("method", "delete-proplet").Observe(time.Since(begin).Seconds())
	}(time.Now())

	return mm.svc.DeleteProplet(ctx, id)
}

func (mm *metricsMiddleware) SelectProplet(ctx context.Context, t task.Task) (proplet.Proplet, error) {
	defer func(begin time.Time) {
		mm.counter.With("method", "select-proplet").Add(1)
		mm.latency.With("method", "select-proplet").Observe(time.Since(begin).Seconds())
	}(time.Now())

	return mm.svc.SelectProplet(ctx, t)
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
