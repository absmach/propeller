package middleware

import (
	"context"
	"log/slog"
	"time"

	"github.com/absmach/propeller/manager"
	"github.com/absmach/propeller/task"
	"github.com/absmach/propeller/worker"
)

type loggingMiddleware struct {
	logger *slog.Logger
	svc    manager.Service
}

func Logging(logger *slog.Logger, svc manager.Service) manager.Service {
	return &loggingMiddleware{
		logger: logger,
		svc:    svc,
	}
}

func (lm *loggingMiddleware) CreateWorker(ctx context.Context, w worker.Worker) (resp worker.Worker, err error) {
	defer func(begin time.Time) {
		args := []any{
			slog.String("duration", time.Since(begin).String()),
			slog.Group("worker",
				slog.String("name", w.Name),
			),
		}
		if err != nil {
			args = append(args, slog.Any("error", err))
			lm.logger.Warn("Save worker failed", args...)

			return
		}
		lm.logger.Info("Save worker completed successfully", args...)
	}(time.Now())

	return lm.svc.CreateWorker(ctx, w)
}

func (lm *loggingMiddleware) GetWorker(ctx context.Context, id string) (resp worker.Worker, err error) {
	defer func(begin time.Time) {
		args := []any{
			slog.String("duration", time.Since(begin).String()),
			slog.Group("worker",
				slog.String("id", id),
			),
		}
		if err != nil {
			args = append(args, slog.Any("error", err))
			lm.logger.Warn("Get worker failed", args...)

			return
		}
		lm.logger.Info("Get worker completed successfully", args...)
	}(time.Now())

	return lm.svc.GetWorker(ctx, id)
}

func (lm *loggingMiddleware) ListWorkers(ctx context.Context, offset, limit uint64) (resp worker.WorkerPage, err error) {
	defer func(begin time.Time) {
		args := []any{
			slog.String("duration", time.Since(begin).String()),
			slog.Uint64("offset", offset),
			slog.Uint64("limit", limit),
		}
		if err != nil {
			args = append(args, slog.Any("error", err))
			lm.logger.Warn("List workers failed", args...)

			return
		}
		lm.logger.Info("List workers completed successfully", args...)
	}(time.Now())

	return lm.svc.ListWorkers(ctx, offset, limit)
}

func (lm *loggingMiddleware) UpdateWorker(ctx context.Context, t worker.Worker) (resp worker.Worker, err error) {
	defer func(begin time.Time) {
		args := []any{
			slog.String("duration", time.Since(begin).String()),
			slog.Group("worker",
				slog.String("name", resp.Name),
				slog.String("id", t.ID),
			),
		}
		if err != nil {
			args = append(args, slog.Any("error", err))
			lm.logger.Warn("Update worker failed", args...)

			return
		}
		lm.logger.Info("Update worker completed successfully", args...)
	}(time.Now())

	return lm.svc.UpdateWorker(ctx, t)
}

func (lm *loggingMiddleware) DeleteWorker(ctx context.Context, id string) (err error) {
	defer func(begin time.Time) {
		args := []any{
			slog.String("duration", time.Since(begin).String()),
			slog.Group("worker",
				slog.String("id", id),
			),
		}
		if err != nil {
			args = append(args, slog.Any("error", err))
			lm.logger.Warn("Delete worker failed", args...)

			return
		}
		lm.logger.Info("Delete worker completed successfully", args...)
	}(time.Now())

	return lm.svc.DeleteWorker(ctx, id)
}

func (lm *loggingMiddleware) SelectWorker(ctx context.Context, t task.Task) (w worker.Worker, err error) {
	defer func(begin time.Time) {
		args := []any{
			slog.String("duration", time.Since(begin).String()),
			slog.Group("task",
				slog.String("name", t.Name),
				slog.String("id", t.ID),
			),
			slog.Group("worker",
				slog.String("name", w.Name),
				slog.String("id", w.ID),
			),
		}
		if err != nil {
			args = append(args, slog.Any("error", err))
			lm.logger.Warn("Select worker failed", args...)

			return
		}
		lm.logger.Info("Select worker completed successfully", args...)
	}(time.Now())

	return lm.svc.SelectWorker(ctx, t)
}

func (lm *loggingMiddleware) CreateTask(ctx context.Context, t task.Task) (resp task.Task, err error) {
	defer func(begin time.Time) {
		args := []any{
			slog.String("duration", time.Since(begin).String()),
			slog.Group("task",
				slog.String("name", t.Name),
			),
		}
		if err != nil {
			args = append(args, slog.Any("error", err))
			lm.logger.Warn("Save task failed", args...)

			return
		}
		lm.logger.Info("Save task completed successfully", args...)
	}(time.Now())

	return lm.svc.CreateTask(ctx, t)
}

func (lm *loggingMiddleware) GetTask(ctx context.Context, id string) (resp task.Task, err error) {
	defer func(begin time.Time) {
		args := []any{
			slog.String("duration", time.Since(begin).String()),
			slog.Group("task",
				slog.String("id", id),
			),
		}
		if err != nil {
			args = append(args, slog.Any("error", err))
			lm.logger.Warn("Get task failed", args...)

			return
		}
		lm.logger.Info("Get task completed successfully", args...)
	}(time.Now())

	return lm.svc.GetTask(ctx, id)
}

func (lm *loggingMiddleware) ListTasks(ctx context.Context, offset, limit uint64) (resp task.TaskPage, err error) {
	defer func(begin time.Time) {
		args := []any{
			slog.String("duration", time.Since(begin).String()),
			slog.Uint64("offset", offset),
			slog.Uint64("limit", limit),
		}
		if err != nil {
			args = append(args, slog.Any("error", err))
			lm.logger.Warn("List tasks failed", args...)

			return
		}
		lm.logger.Info("List tasks completed successfully", args...)
	}(time.Now())

	return lm.svc.ListTasks(ctx, offset, limit)
}

func (lm *loggingMiddleware) UpdateTask(ctx context.Context, t task.Task) (resp task.Task, err error) {
	defer func(begin time.Time) {
		args := []any{
			slog.String("duration", time.Since(begin).String()),
			slog.Group("task",
				slog.String("name", resp.Name),
				slog.String("id", t.ID),
			),
		}
		if err != nil {
			args = append(args, slog.Any("error", err))
			lm.logger.Warn("Update task failed", args...)

			return
		}
		lm.logger.Info("Update task completed successfully", args...)
	}(time.Now())

	return lm.svc.UpdateTask(ctx, t)
}

func (lm *loggingMiddleware) DeleteTask(ctx context.Context, id string) (err error) {
	defer func(begin time.Time) {
		args := []any{
			slog.String("duration", time.Since(begin).String()),
			slog.Group("task",
				slog.String("id", id),
			),
		}
		if err != nil {
			args = append(args, slog.Any("error", err))
			lm.logger.Warn("Delete task failed", args...)

			return
		}
		lm.logger.Info("Delete task completed successfully", args...)
	}(time.Now())

	return lm.svc.DeleteTask(ctx, id)
}
