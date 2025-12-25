package middleware

import (
	"context"

	"github.com/absmach/propeller/manager"
	"github.com/absmach/propeller/pkg/proplet"
	"github.com/absmach/propeller/task"
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

func (tm *tracing) GetProplet(ctx context.Context, id string) (resp proplet.Proplet, err error) {
	ctx, span := tm.tracer.Start(ctx, "get-proplet", trace.WithAttributes(
		attribute.String("id", id),
	))
	defer span.End()

	return tm.svc.GetProplet(ctx, id)
}

func (tm *tracing) ListProplets(ctx context.Context, offset, limit uint64) (resp proplet.PropletPage, err error) {
	ctx, span := tm.tracer.Start(ctx, "list-proplets", trace.WithAttributes(
		attribute.Int64("offset", int64(offset)),
		attribute.Int64("limit", int64(limit)),
	))
	defer span.End()

	return tm.svc.ListProplets(ctx, offset, limit)
}

func (tm *tracing) SelectProplet(ctx context.Context, t task.Task) (resp proplet.Proplet, err error) {
	ctx, span := tm.tracer.Start(ctx, "create-task", trace.WithAttributes(
		attribute.String("task.name", t.Name),
		attribute.String("task.id", t.ID),
		attribute.String("proplet.name", resp.Name),
		attribute.String("proplet.id", resp.ID),
	))
	defer span.End()

	return tm.svc.SelectProplet(ctx, t)
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

func (tm *tracing) StartTask(ctx context.Context, id string) (err error) {
	ctx, span := tm.tracer.Start(ctx, "start-task", trace.WithAttributes(
		attribute.String("id", id),
	))
	defer span.End()

	return tm.svc.StartTask(ctx, id)
}

func (tm *tracing) StopTask(ctx context.Context, id string) (err error) {
	ctx, span := tm.tracer.Start(ctx, "stop-task", trace.WithAttributes(
		attribute.String("id", id),
	))
	defer span.End()

	return tm.svc.StopTask(ctx, id)
}

func (tm *tracing) GetTaskMetrics(ctx context.Context, taskID string, offset, limit uint64) (resp manager.TaskMetricsPage, err error) {
	ctx, span := tm.tracer.Start(ctx, "get-task-metrics", trace.WithAttributes(
		attribute.String("task_id", taskID),
		attribute.Int64("offset", int64(offset)),
		attribute.Int64("limit", int64(limit)),
	))
	defer span.End()

	return tm.svc.GetTaskMetrics(ctx, taskID, offset, limit)
}

func (tm *tracing) GetPropletMetrics(ctx context.Context, propletID string, offset, limit uint64) (resp manager.PropletMetricsPage, err error) {
	ctx, span := tm.tracer.Start(ctx, "get-proplet-metrics", trace.WithAttributes(
		attribute.String("proplet_id", propletID),
		attribute.Int64("offset", int64(offset)),
		attribute.Int64("limit", int64(limit)),
	))
	defer span.End()

	return tm.svc.GetPropletMetrics(ctx, propletID, offset, limit)
}

func (tm *tracing) Subscribe(ctx context.Context) (err error) {
	ctx, span := tm.tracer.Start(ctx, "subscribe")
	defer span.End()

	return tm.svc.Subscribe(ctx)
}
