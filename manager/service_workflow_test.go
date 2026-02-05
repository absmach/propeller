package manager_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/absmach/propeller/manager"
	"github.com/absmach/propeller/pkg/mqtt"
	"github.com/absmach/propeller/pkg/scheduler"
	"github.com/absmach/propeller/pkg/storage"
	"github.com/absmach/propeller/task"
)

const testWorkflowID = "test-workflow"

type mockPubSub struct{}

func (m *mockPubSub) Publish(ctx context.Context, topic string, payload any) error {
	return nil
}

func (m *mockPubSub) Subscribe(ctx context.Context, topic string, handler mqtt.Handler) error {
	return nil
}

func (m *mockPubSub) Disconnect(ctx context.Context) error {
	return nil
}

func (m *mockPubSub) Unsubscribe(ctx context.Context, topic string) error {
	return nil
}

func setupTestService(t *testing.T) manager.Service {
	t.Helper()
	repos, err := storage.NewRepositories(storage.Config{Type: "memory"})
	if err != nil {
		t.Fatalf("Failed to create repositories: %v", err)
	}
	sched := scheduler.NewRoundRobin()
	pubsub := &mockPubSub{}
	logger := slog.Default()

	return manager.NewService(repos, sched, pubsub, "test-domain", "test-channel", logger)
}

func TestCreateWorkflow_ValidDAG(t *testing.T) {
	t.Parallel()
	svc := setupTestService(t)
	ctx := context.Background()

	tasks := []task.Task{
		{ID: "task1", Name: "Task 1", DependsOn: []string{}},
		{ID: "task2", Name: "Task 2", DependsOn: []string{"task1"}},
		{ID: "task3", Name: "Task 3", DependsOn: []string{"task2"}},
	}

	created, err := svc.CreateWorkflow(ctx, tasks)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if len(created) != 3 {
		t.Fatalf("Expected 3 tasks, got %d", len(created))
	}

	if created[0].WorkflowID == "" {
		t.Error("Expected workflow ID to be set")
	}

	if created[0].WorkflowID != created[1].WorkflowID || created[1].WorkflowID != created[2].WorkflowID {
		t.Error("Expected all tasks to have same workflow ID")
	}
}

func TestCreateWorkflow_CircularDependency(t *testing.T) {
	t.Parallel()
	svc := setupTestService(t)
	ctx := context.Background()

	tasks := []task.Task{
		{ID: "task1", Name: "Task 1", DependsOn: []string{"task2"}},
		{ID: "task2", Name: "Task 2", DependsOn: []string{"task1"}},
	}

	_, err := svc.CreateWorkflow(ctx, tasks)
	if err == nil {
		t.Fatal("Expected error for circular dependency, got nil")
	}
}

func TestCreateWorkflow_MissingDependency(t *testing.T) {
	t.Parallel()
	svc := setupTestService(t)
	ctx := context.Background()

	tasks := []task.Task{
		{ID: "task1", Name: "Task 1", DependsOn: []string{"nonexistent"}},
	}

	_, err := svc.CreateWorkflow(ctx, tasks)
	if err == nil {
		t.Fatal("Expected error for missing dependency, got nil")
	}
}

func TestCreateWorkflow_InvalidRunIf(t *testing.T) {
	t.Parallel()
	svc := setupTestService(t)
	ctx := context.Background()

	tasks := []task.Task{
		{ID: "task1", Name: "Task 1", DependsOn: []string{}, RunIf: "invalid"},
	}

	_, err := svc.CreateWorkflow(ctx, tasks)
	if err == nil {
		t.Fatal("Expected error for invalid run_if, got nil")
	}
}

func TestCreateWorkflow_EmptyTasks(t *testing.T) {
	t.Parallel()
	svc := setupTestService(t)
	ctx := context.Background()

	_, err := svc.CreateWorkflow(ctx, []task.Task{})
	if err == nil {
		t.Fatal("Expected error for empty tasks, got nil")
	}
}

func TestCreateTask_WithWorkflowID(t *testing.T) {
	t.Parallel()
	svc := setupTestService(t)
	ctx := context.Background()

	workflowID := testWorkflowID
	task1 := task.Task{Name: "Task 1", WorkflowID: workflowID}
	created1, err := svc.CreateTask(ctx, task1)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	task2 := task.Task{Name: "Task 2", WorkflowID: workflowID, DependsOn: []string{created1.ID}}
	_, err = svc.CreateTask(ctx, task2)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
}

func TestCreateTask_DependsOnWithoutWorkflowID(t *testing.T) {
	t.Parallel()
	svc := setupTestService(t)
	ctx := context.Background()

	task1 := task.Task{Name: "Task 1", DependsOn: []string{"some-task"}}
	_, err := svc.CreateTask(ctx, task1)
	if err == nil {
		t.Fatal("Expected error when depends_on specified without workflow_id, got nil")
	}
}

func TestGetTaskResults(t *testing.T) {
	t.Parallel()
	svc := setupTestService(t)
	ctx := context.Background()

	task1 := task.Task{Name: "Task 1"}
	created, err := svc.CreateTask(ctx, task1)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	created.Results = map[string]any{"output": "test"}
	_, err = svc.UpdateTask(ctx, created)
	if err != nil {
		t.Fatalf("Expected no error updating task, got: %v", err)
	}

	results, err := svc.GetTaskResults(ctx, created.ID)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if results == nil {
		t.Fatal("Expected results, got nil")
	}
}

func TestGetParentResults(t *testing.T) {
	t.Parallel()
	svc := setupTestService(t)
	ctx := context.Background()

	workflowID := testWorkflowID
	parent1 := task.Task{Name: "Parent 1", WorkflowID: workflowID}
	created1, err := svc.CreateTask(ctx, parent1)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	parent2 := task.Task{Name: "Parent 2", WorkflowID: workflowID}
	created2, err := svc.CreateTask(ctx, parent2)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	created1.Results = "result1"
	created1.State = task.Completed
	if _, err := svc.UpdateTask(ctx, created1); err != nil {
		t.Fatalf("failed to update task: %v", err)
	}

	created2.Results = "result2"
	created2.State = task.Completed
	if _, err := svc.UpdateTask(ctx, created2); err != nil {
		t.Fatalf("failed to update task: %v", err)
	}

	child := task.Task{Name: "Child", WorkflowID: workflowID, DependsOn: []string{created1.ID, created2.ID}}
	createdChild, err := svc.CreateTask(ctx, child)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	parentResults, err := svc.GetParentResults(ctx, createdChild.ID)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if len(parentResults) != 2 {
		t.Fatalf("Expected 2 parent results, got %d", len(parentResults))
	}

	if parentResults[created1.ID] != "result1" {
		t.Errorf("Expected result1 for parent1, got %v", parentResults[created1.ID])
	}

	if parentResults[created2.ID] != "result2" {
		t.Errorf("Expected result2 for parent2, got %v", parentResults[created2.ID])
	}
}

func TestStartTask_WithDependencies(t *testing.T) {
	t.Parallel()
	svc := setupTestService(t)
	ctx := context.Background()

	workflowID := testWorkflowID
	parent := task.Task{Name: "Parent", WorkflowID: workflowID}
	createdParent, err := svc.CreateTask(ctx, parent)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	child := task.Task{Name: "Child", WorkflowID: workflowID, DependsOn: []string{createdParent.ID}}
	createdChild, err := svc.CreateTask(ctx, child)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	err = svc.StartTask(ctx, createdChild.ID)
	if err != nil {
		t.Logf("StartTask returned error (coordinator may have tried to start): %v", err)
	}

	childAfter, err := svc.GetTask(ctx, createdChild.ID)
	if err != nil {
		t.Fatalf("Failed to get child task: %v", err)
	}

	if childAfter.State == task.Running {
		t.Error("Task should not be running when dependencies are not met")
	}

	createdParent.State = task.Completed
	if _, err := svc.UpdateTask(ctx, createdParent); err != nil {
		t.Fatalf("failed to update parent task: %v", err)
	}

	err = svc.StartTask(ctx, createdChild.ID)
	if err != nil {
		t.Logf("StartTask error (may be expected due to missing proplets): %v", err)
	}
}
