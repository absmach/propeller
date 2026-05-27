package manager

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/absmach/propeller/pkg/dag"
	"github.com/absmach/propeller/pkg/plugin"
	"github.com/absmach/propeller/pkg/storage"
	"github.com/absmach/propeller/pkg/task"
)

type WorkflowCoordinator struct {
	taskRepo storage.TaskRepository
	service  Service
	logger   *slog.Logger
	mu       sync.RWMutex
}

func NewWorkflowCoordinator(taskRepo storage.TaskRepository, service Service, logger *slog.Logger) *WorkflowCoordinator {
	return &WorkflowCoordinator{
		taskRepo: taskRepo,
		service:  service,
		logger:   logger,
	}
}

// SetService replaces the Service reference used to start workflow tasks.
// Call this after all middleware wrapping is complete so workflow-triggered
// StartTask calls flow through the full plugin middleware chain.
func (wc *WorkflowCoordinator) SetService(svc Service) {
	wc.mu.Lock()
	wc.service = svc
	wc.mu.Unlock()
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
		// Intentionally asymmetric with RunIfSuccess: triggers if ANY dependency
		// failed (error-handler pattern), rather than requiring all to fail.
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
		if t.State.IsTerminal() {
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

		wc.mu.RLock()
		svc := wc.service
		wc.mu.RUnlock()
		sysCtx := plugin.ContextWithSystem(ctx)
		if err := svc.StartTask(sysCtx, t.ID); err != nil {
			wc.logger.ErrorContext(ctx, "failed to start ready task", "task_id", t.ID, "error", err)

			continue
		}
		wc.logger.InfoContext(ctx, "started ready task", "task_id", t.ID)
	}

	return nil
}

func (wc *WorkflowCoordinator) OnTaskCompletion(ctx context.Context, taskID string) error {
	wc.mu.RLock()
	svc := wc.service
	wc.mu.RUnlock()
	t, err := svc.GetTask(ctx, taskID)
	if err != nil {
		return fmt.Errorf("failed to get completed task: %w", err)
	}

	if t.WorkflowID == "" {
		return nil
	}

	return wc.CheckAndStartReadyTasks(ctx, t.WorkflowID)
}

func (wc *WorkflowCoordinator) getWorkflowTasks(ctx context.Context, workflowID string) ([]task.Task, error) {
	return wc.taskRepo.ListByWorkflowID(ctx, workflowID)
}
