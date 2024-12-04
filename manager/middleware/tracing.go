package middleware

import (
	"context"

	"github.com/absmach/propeller/manager"
	"github.com/absmach/propeller/task"
	"github.com/absmach/propeller/worker"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var _ manager.Service = (*tracing)(nil)

type tracing struct {
	tracer trace.Tracer
	svc    manager.Service
}

func Tracing(tracer trace.Tracer, svc manager.Service) manager.Service {
	return &tracing{tracer, svc}
}

func (tm *tracing) CreateWorker(ctx context.Context, w worker.Worker) (resp worker.Worker, err error) {
	ctx, span := tm.tracer.Start(ctx, "create-worker", trace.WithAttributes(
		attribute.String("name", resp.Name),
		attribute.String("id", resp.ID),
	))
	defer span.End()

	return tm.svc.CreateWorker(ctx, w)
}

func (tm *tracing) GetWorker(ctx context.Context, id string) (resp worker.Worker, err error) {
	ctx, span := tm.tracer.Start(ctx, "get-worker", trace.WithAttributes(
		attribute.String("id", id),
	))
	defer span.End()

	return tm.svc.GetWorker(ctx, id)
}

func (tm *tracing) ListWorkers(ctx context.Context, offset, limit uint64) (resp worker.WorkerPage, err error) {
	ctx, span := tm.tracer.Start(ctx, "list-workers", trace.WithAttributes(
		attribute.Int64("offset", int64(offset)),
		attribute.Int64("limit", int64(limit)),
	))
	defer span.End()

	return tm.svc.ListWorkers(ctx, offset, limit)
}

func (tm *tracing) UpdateWorker(ctx context.Context, w worker.Worker) (resp worker.Worker, err error) {
	ctx, span := tm.tracer.Start(ctx, "update-worker", trace.WithAttributes(
		attribute.String("id", resp.ID),
		attribute.String("name", resp.Name),
	))
	defer span.End()

	return tm.svc.UpdateWorker(ctx, w)
}

func (tm *tracing) DeleteWorker(ctx context.Context, id string) (err error) {
	ctx, span := tm.tracer.Start(ctx, "delete-worker", trace.WithAttributes(
		attribute.String("id", id),
	))
	defer span.End()

	return tm.svc.DeleteWorker(ctx, id)
}

func (tm *tracing) SelectWorker(ctx context.Context, t task.Task) (resp worker.Worker, err error) {
	ctx, span := tm.tracer.Start(ctx, "create-task", trace.WithAttributes(
		attribute.String("task.name", t.Name),
		attribute.String("task.id", t.ID),
		attribute.String("worker.name", resp.Name),
		attribute.String("worker.id", resp.ID),
	))
	defer span.End()

	return tm.svc.SelectWorker(ctx, t)
}

func (tm *tracing) CreateTask(ctx context.Context, t task.Task) (resp task.Task, err error) {
	ctx, span := tm.tracer.Start(ctx, "create-task", trace.WithAttributes(
		attribute.String("name", resp.Name),
		attribute.String("id", resp.ID),
	))
	defer span.End()

	return tm.svc.CreateTask(ctx, t)
}

func (tm *tracing) GetTask(ctx context.Context, id string) (resp task.Task, err error) {
	ctx, span := tm.tracer.Start(ctx, "get-task", trace.WithAttributes(
		attribute.String("id", id),
	))
	defer span.End()

	return tm.svc.GetTask(ctx, id)
}

func (tm *tracing) ListTasks(ctx context.Context, offset, limit uint64) (resp task.TaskPage, err error) {
	ctx, span := tm.tracer.Start(ctx, "list-tasks", trace.WithAttributes(
		attribute.Int64("offset", int64(offset)),
		attribute.Int64("limit", int64(limit)),
	))
	defer span.End()

	return tm.svc.ListTasks(ctx, offset, limit)
}

func (tm *tracing) UpdateTask(ctx context.Context, t task.Task) (resp task.Task, err error) {
	ctx, span := tm.tracer.Start(ctx, "update-task", trace.WithAttributes(
		attribute.String("id", resp.ID),
		attribute.String("name", resp.Name),
	))
	defer span.End()

	return tm.svc.UpdateTask(ctx, t)
}

func (tm *tracing) DeleteTask(ctx context.Context, id string) (err error) {
	ctx, span := tm.tracer.Start(ctx, "delete-task", trace.WithAttributes(
		attribute.String("id", id),
	))
	defer span.End()

	return tm.svc.DeleteTask(ctx, id)
}
