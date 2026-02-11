package manager_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/absmach/propeller/manager"
	"github.com/absmach/propeller/pkg/task"
)

func TestEvaluateConditionalExecution_Success(t *testing.T) {
	t.Parallel()
	coordinator := manager.NewWorkflowCoordinator(nil, nil, slog.Default())

	tests := []struct {
		name         string
		task         task.Task
		parentStates map[string]task.State
		expected     bool
	}{
		{
			name: "run_if success with all parents completed",
			task: task.Task{
				ID:        "task1",
				DependsOn: []string{"parent1", "parent2"},
				RunIf:     "success",
			},
			parentStates: map[string]task.State{
				"parent1": task.Completed,
				"parent2": task.Completed,
			},
			expected: true,
		},
		{
			name: "run_if success with one parent failed",
			task: task.Task{
				ID:        "task1",
				DependsOn: []string{"parent1", "parent2"},
				RunIf:     "success",
			},
			parentStates: map[string]task.State{
				"parent1": task.Completed,
				"parent2": task.Failed,
			},
			expected: false,
		},
		{
			name: "run_if success with skipped parent",
			task: task.Task{
				ID:        "task1",
				DependsOn: []string{"parent1"},
				RunIf:     "success",
			},
			parentStates: map[string]task.State{
				"parent1": task.Skipped,
			},
			expected: false,
		},
		{
			name: "run_if failure with one parent failed",
			task: task.Task{
				ID:        "task1",
				DependsOn: []string{"parent1", "parent2"},
				RunIf:     "failure",
			},
			parentStates: map[string]task.State{
				"parent1": task.Completed,
				"parent2": task.Failed,
			},
			expected: true,
		},
		{
			name: "run_if failure with all parents succeeded",
			task: task.Task{
				ID:        "task1",
				DependsOn: []string{"parent1", "parent2"},
				RunIf:     "failure",
			},
			parentStates: map[string]task.State{
				"parent1": task.Completed,
				"parent2": task.Completed,
			},
			expected: false,
		},
		{
			name: "default run_if success with all completed",
			task: task.Task{
				ID:        "task1",
				DependsOn: []string{"parent1"},
				RunIf:     "",
			},
			parentStates: map[string]task.State{
				"parent1": task.Completed,
			},
			expected: true,
		},
		{
			name: "no dependencies",
			task: task.Task{
				ID:        "task1",
				DependsOn: []string{},
			},
			parentStates: map[string]task.State{},
			expected:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := coordinator.EvaluateConditionalExecution(context.Background(), tt.task, tt.parentStates)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}
