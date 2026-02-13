package manager

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/absmach/propeller/pkg/dag"
	"github.com/absmach/propeller/pkg/storage"
	"github.com/absmach/propeller/pkg/task"
)

type WorkflowCoordinator struct {
	taskRepo storage.TaskRepository
	service  Service
	logger   *slog.Logger
}

func NewWorkflowCoordinator(taskRepo storage.TaskRepository, service Service, logger *slog.Logger) *WorkflowCoordinator {
	return &WorkflowCoordinator{
		taskRepo: taskRepo,
		service:  service,
		logger:   logger,
	}
}

func (wc *WorkflowCoordinator) EvaluateConditionalExecution(ctx context.Context, t task.Task, parentStates map[string]task.State) bool {
	if len(t.DependsOn) == 0 {
		return true
	}

	runIf := t.RunIf
	if runIf == "" {
		runIf = task.RunIfSuccess
	}

	switch runIf {
	case task.RunIfSuccess:
		for _, depID := range t.DependsOn {
			state, exists := parentStates[depID]
			if !exists || state != task.Completed {
				return false
			}
		}

		return true

	case task.RunIfFailure:
		for _, depID := range t.DependsOn {
			state, exists := parentStates[depID]
			if exists && state == task.Failed {
				return true
			}
		}

		return false

	default:
		wc.logger.WarnContext(ctx, "invalid run_if value", "task_id", t.ID, "run_if", runIf)

		return false
	}
}

func (wc *WorkflowCoordinator) CheckAndStartReadyTasks(ctx context.Context, workflowID string) error {
	tasks, err := wc.getWorkflowTasks(ctx, workflowID)
	if err != nil {
		return fmt.Errorf("failed to get workflow tasks: %w", err)
	}

	if len(tasks) == 0 {
		return nil
	}

	completed := make(map[string]task.State)
	for i := range tasks {
		t := &tasks[i]
		if t.State == task.Completed || t.State == task.Failed || t.State == task.Skipped {
			completed[t.ID] = t.State
		}
	}

	readyTasks := dag.GetReadyTasks(tasks, completed)

	for i := range readyTasks {
		t := &readyTasks[i]
		if t.State == task.Running || t.State == task.Scheduled {
			continue
		}

		parentStates := make(map[string]task.State)
		for _, depID := range t.DependsOn {
			if state, exists := completed[depID]; exists {
				parentStates[depID] = state
			}
		}

		shouldRun := wc.EvaluateConditionalExecution(ctx, *t, parentStates)

		if !shouldRun {
			t.State = task.Skipped
			t.UpdatedAt = time.Now()
			if err := wc.taskRepo.Update(ctx, *t); err != nil {
				wc.logger.ErrorContext(ctx, "failed to mark task as skipped", "task_id", t.ID, "error", err)

				continue
			}
			wc.logger.InfoContext(ctx, "task skipped due to conditional execution", "task_id", t.ID, "run_if", t.RunIf)

			continue
		}

		if err := wc.service.StartTask(ctx, t.ID); err != nil {
			wc.logger.ErrorContext(ctx, "failed to start ready task", "task_id", t.ID, "error", err)

			continue
		}
		wc.logger.InfoContext(ctx, "started ready task", "task_id", t.ID)
	}

	return nil
}

func (wc *WorkflowCoordinator) OnTaskCompletion(ctx context.Context, taskID string) error {
	t, err := wc.service.GetTask(ctx, taskID)
	if err != nil {
		return fmt.Errorf("failed to get completed task: %w", err)
	}

	if t.WorkflowID == "" {
		return nil
	}

	return wc.CheckAndStartReadyTasks(ctx, t.WorkflowID)
}

func (wc *WorkflowCoordinator) getWorkflowTasks(ctx context.Context, workflowID string) ([]task.Task, error) {
	allTasks, _, err := wc.taskRepo.List(ctx, 0, 10000)
	if err != nil {
		return nil, err
	}

	tasks := make([]task.Task, 0)
	for i := range allTasks {
		if allTasks[i].WorkflowID == workflowID {
			tasks = append(tasks, allTasks[i])
		}
	}

	return tasks, nil
}
