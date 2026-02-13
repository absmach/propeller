package manager

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/absmach/propeller/pkg/cron"
	"github.com/absmach/propeller/pkg/scheduler"
	"github.com/absmach/propeller/pkg/storage"
	"github.com/absmach/propeller/pkg/task"
)

const defaultCronCheckInterval = time.Minute

// CronScheduler defines the behavior for managing cron-based task scheduling.
type CronScheduler interface {
	Start(ctx context.Context) error
	Stop()
	ScheduleTask(ctx context.Context, taskID string) error
	UnscheduleTask(ctx context.Context, taskID string) error
}

type cronScheduler struct {
	tasksDB       storage.TaskRepository
	service       Service
	logger        *slog.Logger
	checkInterval time.Duration
	stopChan      chan struct{}
}

func NewCronScheduler(tasksDB storage.TaskRepository, service Service, logger *slog.Logger) CronScheduler {
	return &cronScheduler{
		tasksDB:       tasksDB,
		service:       service,
		logger:        logger,
		checkInterval: defaultCronCheckInterval,
		stopChan:      make(chan struct{}),
	}
}

func (cs *cronScheduler) Start(ctx context.Context) error {
	if err := cs.loadScheduledTasks(ctx); err != nil {
		cs.logger.Warn("failed to load scheduled tasks from storage", slog.String("error", err.Error()))
	}

	ticker := time.NewTicker(cs.checkInterval)
	defer ticker.Stop()

	cs.logger.Info("cron scheduler started", slog.Duration("check_interval", cs.checkInterval))

	for {
		select {
		case <-ctx.Done():
			cs.logger.Info("cron scheduler stopping")

			return ctx.Err()
		case <-cs.stopChan:
			cs.logger.Info("cron scheduler stopped")

			return nil
		case <-ticker.C:
			if err := cs.processScheduledTasks(ctx); err != nil {
				cs.logger.Error("error processing scheduled tasks", slog.String("error", err.Error()))
			}
		}
	}
}

func (cs *cronScheduler) Stop() {
	close(cs.stopChan)
}

func (cs *cronScheduler) ScheduleTask(ctx context.Context, taskID string) error {
	t, err := cs.tasksDB.Get(ctx, taskID)
	if err != nil {
		return fmt.Errorf("failed to get task: %w", err)
	}

	if t.Schedule == "" {
		return nil
	}

	return cs.updateNextRun(ctx, t)
}

func (cs *cronScheduler) UnscheduleTask(ctx context.Context, taskID string) error {
	t, err := cs.tasksDB.Get(ctx, taskID)
	if err != nil {
		return fmt.Errorf("failed to get task: %w", err)
	}

	t.Schedule = ""
	t.NextRun = time.Time{}

	if err := cs.tasksDB.Update(ctx, t); err != nil {
		return fmt.Errorf("failed to unschedule task: %w", err)
	}

	return nil
}

func (cs *cronScheduler) processScheduledTasks(ctx context.Context) error {
	tasks, _, err := cs.tasksDB.List(ctx, 0, 10000)
	if err != nil {
		return fmt.Errorf("failed to list tasks: %w", err)
	}

	now := time.Now()
	var dueTasks []task.Task
	for i := range tasks {
		t := tasks[i]
		if t.Schedule != "" && !t.NextRun.IsZero() && !t.NextRun.After(now) {
			dueTasks = append(dueTasks, t)
		}
	}

	dueTasks = scheduler.GetReadyTasksByPriority(dueTasks)

	for i := range dueTasks {
		t := dueTasks[i]

		if err := cs.triggerScheduledTask(ctx, t); err != nil {
			cs.logger.Error("failed to trigger scheduled task",
				slog.String("task_id", t.ID),
				slog.String("error", err.Error()))

			continue
		}

		if t.IsRecurring {
			if err := cs.updateNextRun(ctx, t); err != nil {
				cs.logger.Error("failed to update next run",
					slog.String("task_id", t.ID),
					slog.String("error", err.Error()))
			}
		} else {
			t.Schedule = ""
			t.NextRun = time.Time{}
			if err := cs.tasksDB.Update(ctx, t); err != nil {
				cs.logger.Error("failed to clear schedule for one-time task",
					slog.String("task_id", t.ID),
					slog.String("error", err.Error()))
			}
		}
	}

	return nil
}

func (cs *cronScheduler) triggerScheduledTask(ctx context.Context, t task.Task) error {
	cs.logger.Info("triggering scheduled task",
		slog.String("task_id", t.ID),
		slog.String("name", t.Name))

	if err := cs.service.StartTask(ctx, t.ID); err != nil {
		return fmt.Errorf("failed to start scheduled task: %w", err)
	}

	return nil
}

func (cs *cronScheduler) updateNextRun(ctx context.Context, t task.Task) error {
	schedule, err := cron.ParseCronExpression(t.Schedule)
	if err != nil {
		return fmt.Errorf("failed to parse cron expression: %w", err)
	}

	timezone := t.Timezone
	if timezone == "" {
		timezone = "UTC"
	}

	now := time.Now()
	nextRun := cron.CalculateNextRun(schedule, now, timezone)

	t.NextRun = nextRun
	t.UpdatedAt = time.Now()

	if err := cs.tasksDB.Update(ctx, t); err != nil {
		return fmt.Errorf("failed to update task next run: %w", err)
	}

	cs.logger.Debug("updated next run for scheduled task",
		slog.String("task_id", t.ID),
		slog.Time("next_run", nextRun))

	return nil
}

func (cs *cronScheduler) loadScheduledTasks(ctx context.Context) error {
	tasks, _, err := cs.tasksDB.List(ctx, 0, 10000)
	if err != nil {
		return fmt.Errorf("failed to list tasks: %w", err)
	}

	loadedCount := 0
	for i := range tasks {
		t := tasks[i]

		if t.Schedule == "" {
			continue
		}

		now := time.Now()
		if t.NextRun.IsZero() || t.NextRun.Before(now) {
			if err := cs.updateNextRun(ctx, t); err != nil {
				cs.logger.Warn("failed to update next run for scheduled task on load",
					slog.String("task_id", t.ID),
					slog.String("error", err.Error()))

				continue
			}
			loadedCount++
		} else {
			loadedCount++
		}
	}

	if loadedCount > 0 {
		cs.logger.Info("loaded scheduled tasks from storage",
			slog.Int("count", loadedCount))
	}

	return nil
}
