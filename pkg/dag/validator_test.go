package dag_test

import (
	"errors"
	"testing"

	"github.com/absmach/propeller/pkg/dag"
	"github.com/absmach/propeller/task"
)

func TestValidateDAG_NoCycles(t *testing.T) {
	t.Parallel()
	tasks := []task.Task{
		{ID: "task1", DependsOn: []string{}},
		{ID: "task2", DependsOn: []string{"task1"}},
		{ID: "task3", DependsOn: []string{"task2"}},
	}

	err := dag.ValidateDAG(tasks)
	if err != nil {
		t.Fatalf("Expected no error for valid DAG, got: %v", err)
	}
}

func TestValidateDAG_CircularDependency(t *testing.T) {
	t.Parallel()
	tasks := []task.Task{
		{ID: "task1", DependsOn: []string{"task2"}},
		{ID: "task2", DependsOn: []string{"task1"}},
	}

	err := dag.ValidateDAG(tasks)
	if err == nil {
		t.Fatal("Expected error for circular dependency, got nil")
	}
	if !errors.Is(err, dag.ErrCircularDependency) {
		t.Fatalf("Expected ErrCircularDependency, got: %v", err)
	}
}

func TestValidateDAG_ComplexCycle(t *testing.T) {
	t.Parallel()
	tasks := []task.Task{
		{ID: "task1", DependsOn: []string{"task3"}},
		{ID: "task2", DependsOn: []string{"task1"}},
		{ID: "task3", DependsOn: []string{"task2"}},
	}

	err := dag.ValidateDAG(tasks)
	if err == nil {
		t.Fatal("Expected error for circular dependency, got nil")
	}
}

func TestValidateDependenciesExist_AllExist(t *testing.T) {
	t.Parallel()
	tasks := []task.Task{
		{ID: "task1", DependsOn: []string{}},
		{ID: "task2", DependsOn: []string{"task1"}},
		{ID: "task3", DependsOn: []string{"task1", "task2"}},
	}

	err := dag.ValidateDependenciesExist(tasks)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
}

func TestValidateDependenciesExist_MissingDependency(t *testing.T) {
	t.Parallel()
	tasks := []task.Task{
		{ID: "task1", DependsOn: []string{}},
		{ID: "task2", DependsOn: []string{"task1", "nonexistent"}},
	}

	err := dag.ValidateDependenciesExist(tasks)
	if err == nil {
		t.Fatal("Expected error for missing dependency, got nil")
	}
	if !errors.Is(err, dag.ErrMissingDependency) {
		t.Fatalf("Expected ErrMissingDependency, got: %v", err)
	}
}

func TestTopologicalSort_ValidOrder(t *testing.T) {
	t.Parallel()
	const (
		task1ID = "task1"
		task2ID = "task2"
		task3ID = "task3"
	)
	tasks := []task.Task{
		{ID: task1ID, DependsOn: []string{}},
		{ID: task2ID, DependsOn: []string{task1ID}},
		{ID: task3ID, DependsOn: []string{task2ID}},
	}

	sorted, err := dag.TopologicalSort(tasks)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if len(sorted) != 3 {
		t.Fatalf("Expected 3 tasks, got %d", len(sorted))
	}

	if sorted[0].ID != task1ID {
		t.Errorf("Expected task1 first, got %s", sorted[0].ID)
	}
	if sorted[1].ID != task2ID {
		t.Errorf("Expected task2 second, got %s", sorted[1].ID)
	}
	if sorted[2].ID != task3ID {
		t.Errorf("Expected task3 third, got %s", sorted[2].ID)
	}
}

func TestTopologicalSort_ParallelTasks(t *testing.T) {
	t.Parallel()
	tasks := []task.Task{
		{ID: "task1", DependsOn: []string{}},
		{ID: "task2", DependsOn: []string{}},
		{ID: "task3", DependsOn: []string{"task1", "task2"}},
	}

	sorted, err := dag.TopologicalSort(tasks)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if len(sorted) != 3 {
		t.Fatalf("Expected 3 tasks, got %d", len(sorted))
	}

	if sorted[2].ID != "task3" {
		t.Errorf("Expected task3 last, got %s at position 2", sorted[2].ID)
	}
}

func TestGetReadyTasks_AllDependenciesCompleted(t *testing.T) {
	t.Parallel()
	tasks := []task.Task{
		{ID: "task1", State: task.Completed, DependsOn: []string{}},
		{ID: "task2", State: task.Completed, DependsOn: []string{"task1"}},
		{ID: "task3", State: task.Pending, DependsOn: []string{"task2"}},
	}

	completed := map[string]task.State{
		"task1": task.Completed,
		"task2": task.Completed,
	}

	ready := dag.GetReadyTasks(tasks, completed)

	if len(ready) != 1 {
		t.Fatalf("Expected 1 ready task, got %d", len(ready))
	}

	if ready[0].ID != "task3" {
		t.Errorf("Expected task3 to be ready, got %s", ready[0].ID)
	}
}

func TestGetReadyTasks_NoDependencies(t *testing.T) {
	t.Parallel()
	tasks := []task.Task{
		{ID: "task1", State: task.Pending, DependsOn: []string{}},
		{ID: "task2", State: task.Pending, DependsOn: []string{}},
	}

	completed := map[string]task.State{}

	ready := dag.GetReadyTasks(tasks, completed)

	if len(ready) != 2 {
		t.Fatalf("Expected 2 ready tasks, got %d", len(ready))
	}
}

func TestGetReadyTasks_ExcludesTerminalStates(t *testing.T) {
	t.Parallel()
	tasks := []task.Task{
		{ID: "task1", State: task.Completed, DependsOn: []string{}},
		{ID: "task2", State: task.Failed, DependsOn: []string{}},
		{ID: "task3", State: task.Skipped, DependsOn: []string{}},
		{ID: "task4", State: task.Pending, DependsOn: []string{}},
	}

	completed := map[string]task.State{}

	ready := dag.GetReadyTasks(tasks, completed)

	if len(ready) != 1 {
		t.Fatalf("Expected 1 ready task, got %d", len(ready))
	}

	if ready[0].ID != "task4" {
		t.Errorf("Expected task4 to be ready, got %s", ready[0].ID)
	}
}
