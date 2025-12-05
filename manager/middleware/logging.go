package middleware

import (
	"context"
	"log/slog"
	"time"

	"github.com/absmach/propeller/manager"
	"github.com/absmach/propeller/pkg/proplet"
	"github.com/absmach/propeller/task"
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

func (lm *loggingMiddleware) GetProplet(ctx context.Context, id string) (resp proplet.Proplet, err error) {
	defer func(begin time.Time) {
		args := []any{
			slog.String("duration", time.Since(begin).String()),
			slog.Group("proplet",
				slog.String("id", id),
			),
		}
		if err != nil {
			args = append(args, slog.Any("error", err))
			lm.logger.Warn("Get proplet failed", args...)

			return
		}
		lm.logger.Info("Get proplet completed successfully", args...)
	}(time.Now())

	return lm.svc.GetProplet(ctx, id)
}

func (lm *loggingMiddleware) ListProplets(ctx context.Context, offset, limit uint64) (resp proplet.PropletPage, err error) {
	defer func(begin time.Time) {
		args := []any{
			slog.String("duration", time.Since(begin).String()),
			slog.Uint64("offset", offset),
			slog.Uint64("limit", limit),
		}
		if err != nil {
			args = append(args, slog.Any("error", err))
			lm.logger.Warn("List proplets failed", args...)

			return
		}
		lm.logger.Info("List proplets completed successfully", args...)
	}(time.Now())

	return lm.svc.ListProplets(ctx, offset, limit)
}

func (lm *loggingMiddleware) SelectProplet(ctx context.Context, t task.Task) (w proplet.Proplet, err error) {
	defer func(begin time.Time) {
		args := []any{
			slog.String("duration", time.Since(begin).String()),
			slog.Group("task",
				slog.String("name", t.Name),
				slog.String("id", t.ID),
			),
			slog.Group("proplet",
				slog.String("name", w.Name),
				slog.String("id", w.ID),
			),
		}
		if err != nil {
			args = append(args, slog.Any("error", err))
			lm.logger.Warn("Select proplet failed", args...)

			return
		}
		lm.logger.Info("Select proplet completed successfully", args...)
	}(time.Now())

	return lm.svc.SelectProplet(ctx, t)
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

func (lm *loggingMiddleware) StartTask(ctx context.Context, id string) (err error) {
	defer func(begin time.Time) {
		args := []any{
			slog.String("duration", time.Since(begin).String()),
			slog.Group("task",
				slog.String("id", id),
			),
		}
		if err != nil {
			args = append(args, slog.Any("error", err))
			lm.logger.Warn("Starting task failed", args...)

			return
		}
		lm.logger.Info("Starting task completed successfully", args...)
	}(time.Now())

	return lm.svc.StartTask(ctx, id)
}

func (lm *loggingMiddleware) StopTask(ctx context.Context, id string) (err error) {
	defer func(begin time.Time) {
		args := []any{
			slog.String("duration", time.Since(begin).String()),
			slog.Group("task",
				slog.String("id", id),
			),
		}
		if err != nil {
			args = append(args, slog.Any("error", err))
			lm.logger.Warn("Stopping task failed", args...)

			return
		}
		lm.logger.Info("Stopping task completed successfully", args...)
	}(time.Now())

	return lm.svc.StopTask(ctx, id)
}

func (lm *loggingMiddleware) GetTaskMetrics(ctx context.Context, taskID string, offset, limit uint64) (resp manager.TaskMetricsPage, err error) {
	defer func(begin time.Time) {
		args := []any{
			slog.String("duration", time.Since(begin).String()),
			slog.Group("task",
				slog.String("id", taskID),
			),
			slog.Uint64("offset", offset),
			slog.Uint64("limit", limit),
		}
		if err != nil {
			args = append(args, slog.Any("error", err))
			lm.logger.Warn("Get task metrics failed", args...)

			return
		}
		lm.logger.Info("Get task metrics completed successfully", args...)
	}(time.Now())

	return lm.svc.GetTaskMetrics(ctx, taskID, offset, limit)
}

func (lm *loggingMiddleware) GetPropletMetrics(ctx context.Context, propletID string, offset, limit uint64) (resp manager.PropletMetricsPage, err error) {
	defer func(begin time.Time) {
		args := []any{
			slog.String("duration", time.Since(begin).String()),
			slog.Group("proplet",
				slog.String("id", propletID),
			),
			slog.Uint64("offset", offset),
			slog.Uint64("limit", limit),
		}
		if err != nil {
			args = append(args, slog.Any("error", err))
			lm.logger.Warn("Get proplet metrics failed", args...)

			return
		}
		lm.logger.Info("Get proplet metrics completed successfully", args...)
	}(time.Now())

	return lm.svc.GetPropletMetrics(ctx, propletID, offset, limit)
}

func (lm *loggingMiddleware) Subscribe(ctx context.Context) (err error) {
	defer func(begin time.Time) {
		args := []any{
			slog.String("duration", time.Since(begin).String()),
		}
		if err != nil {
			args = append(args, slog.Any("error", err))
			lm.logger.Warn("Subscribe to MQTT topic failed", args...)

			return
		}
		lm.logger.Info("Subscribe to MQTT topic completed successfully", args...)
	}(time.Now())

	return lm.svc.Subscribe(ctx)
}

// FL Orchestration methods
func (lm *loggingMiddleware) ConfigureExperiment(ctx context.Context, config manager.ExperimentConfig) (err error) {
	defer func(begin time.Time) {
		args := []any{
			slog.String("duration", time.Since(begin).String()),
			slog.String("experiment_id", config.ExperimentID),
			slog.String("round_id", config.RoundID),
		}
		if err != nil {
			args = append(args, slog.Any("error", err))
			lm.logger.Warn("Configure experiment failed", args...)
			return
		}
		lm.logger.Info("Configure experiment completed successfully", args...)
	}(time.Now())

	return lm.svc.ConfigureExperiment(ctx, config)
}

// FL Coordination methods
func (lm *loggingMiddleware) GetFLTask(ctx context.Context, roundID, propletID string) (resp manager.FLTask, err error) {
	defer func(begin time.Time) {
		args := []any{
			slog.String("duration", time.Since(begin).String()),
			slog.String("round_id", roundID),
			slog.String("proplet_id", propletID),
		}
		if err != nil {
			args = append(args, slog.Any("error", err))
			lm.logger.Warn("Get FL task failed", args...)
			return
		}
		lm.logger.Info("Get FL task completed successfully", args...)
	}(time.Now())

	return lm.svc.GetFLTask(ctx, roundID, propletID)
}

func (lm *loggingMiddleware) PostFLUpdate(ctx context.Context, update manager.FLUpdate) (err error) {
	defer func(begin time.Time) {
		args := []any{
			slog.String("duration", time.Since(begin).String()),
			slog.String("round_id", update.RoundID),
			slog.String("proplet_id", update.PropletID),
		}
		if err != nil {
			args = append(args, slog.Any("error", err))
			lm.logger.Warn("Post FL update failed", args...)
			return
		}
		lm.logger.Info("Post FL update completed successfully", args...)
	}(time.Now())

	return lm.svc.PostFLUpdate(ctx, update)
}

func (lm *loggingMiddleware) PostFLUpdateCBOR(ctx context.Context, updateData []byte) (err error) {
	defer func(begin time.Time) {
		args := []any{
			slog.String("duration", time.Since(begin).String()),
			slog.Int("data_size", len(updateData)),
		}
		if err != nil {
			args = append(args, slog.Any("error", err))
			lm.logger.Warn("Post FL update CBOR failed", args...)
			return
		}
		lm.logger.Info("Post FL update CBOR completed successfully", args...)
	}(time.Now())

	return lm.svc.PostFLUpdateCBOR(ctx, updateData)
}

func (lm *loggingMiddleware) GetRoundStatus(ctx context.Context, roundID string) (resp manager.RoundStatus, err error) {
	defer func(begin time.Time) {
		args := []any{
			slog.String("duration", time.Since(begin).String()),
			slog.String("round_id", roundID),
		}
		if err != nil {
			args = append(args, slog.Any("error", err))
			lm.logger.Warn("Get round status failed", args...)
			return
		}
		lm.logger.Info("Get round status completed successfully", args...)
	}(time.Now())

	return lm.svc.GetRoundStatus(ctx, roundID)
}

