// Copyright (c) Abstract Machines
// SPDX-License-Identifier: Apache-2.0

package manager_test

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/absmach/propeller/manager"
	"github.com/absmach/propeller/pkg/dag"
	pkgerrors "github.com/absmach/propeller/pkg/errors"
	"github.com/absmach/propeller/pkg/mqtt"
	"github.com/absmach/propeller/pkg/scheduler"
	"github.com/absmach/propeller/pkg/storage"
	"github.com/absmach/propeller/task"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testWorkflowID = "test-workflow"

var (
	errEmptyWorkflow    = errors.New("workflow must contain at least one task")
	errInvalidRunIf     = errors.New("invalid run_if value")
	errWorkflowRequired = errors.New("workflow_id is required when depends_on is specified")
)

type mockPubSub struct{}

func (m *mockPubSub) Publish(_ context.Context, _ string, _ any) error {
	return nil
}

func (m *mockPubSub) Subscribe(_ context.Context, _ string, _ mqtt.Handler) error {
	return nil
}

func (m *mockPubSub) Disconnect(_ context.Context) error {
	return nil
}

func (m *mockPubSub) Unsubscribe(_ context.Context, _ string) error {
	return nil
}

// errContains checks if err contains the target error.
func errContains(err, target error) bool {
	if err == nil {
		return target == nil
	}
	if target == nil {
		return false
	}

	return errors.Is(err, target) || strings.Contains(err.Error(), target.Error())
}

func newService(t *testing.T) manager.Service {
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

func TestCreateWorkflow(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc    string
		tasks   []task.Task
		taskLen int
		err     error
	}{
		{
			desc: "create workflow successfully",
			tasks: []task.Task{
				{ID: "task1", Name: "Task 1", DependsOn: []string{}},
				{ID: "task2", Name: "Task 2", DependsOn: []string{"task1"}},
				{ID: "task3", Name: "Task 3", DependsOn: []string{"task2"}},
			},
			taskLen: 3,
			err:     nil,
		},
		{
			desc: "create workflow with circular dependency",
			tasks: []task.Task{
				{ID: "task1", Name: "Task 1", DependsOn: []string{"task2"}},
				{ID: "task2", Name: "Task 2", DependsOn: []string{"task1"}},
			},
			err: dag.ErrCircularDependency,
		},
		{
			desc: "create workflow with missing dependency",
			tasks: []task.Task{
				{ID: "task1", Name: "Task 1", DependsOn: []string{"nonexistent"}},
			},
			err: dag.ErrMissingDependency,
		},
		{
			desc: "create workflow with invalid run_if",
			tasks: []task.Task{
				{ID: "task1", Name: "Task 1", DependsOn: []string{}, RunIf: "invalid"},
			},
			err: errInvalidRunIf,
		},
		{
			desc:  "create workflow with empty tasks",
			tasks: []task.Task{},
			err:   errEmptyWorkflow,
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			svc := newService(t)
			created, err := svc.CreateWorkflow(context.Background(), tc.tasks)
			assert.True(t, errContains(err, tc.err), "%s: expected %v got %v", tc.desc, tc.err, err)
			if tc.err == nil {
				assert.Len(t, created, tc.taskLen)
				assert.NotEmpty(t, created[0].WorkflowID)
				for i := 1; i < len(created); i++ {
					assert.Equal(t, created[0].WorkflowID, created[i].WorkflowID)
				}
			}
		})
	}
}

func TestCreateTask(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc  string
		setup func(t *testing.T, svc manager.Service) task.Task
		err   error
	}{
		{
			desc: "create task with workflow id successfully",
			setup: func(t *testing.T, svc manager.Service) task.Task {
				t.Helper()
				prereq, err := svc.CreateTask(context.Background(), task.Task{Name: "Prereq", WorkflowID: testWorkflowID})
				require.NoError(t, err)

				return task.Task{Name: "Task 2", WorkflowID: testWorkflowID, DependsOn: []string{prereq.ID}}
			},
			err: nil,
		},
		{
			desc: "create task with depends_on without workflow_id",
			setup: func(_ *testing.T, _ manager.Service) task.Task {
				return task.Task{Name: "Task 1", DependsOn: []string{"some-task"}}
			},
			err: errWorkflowRequired,
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			svc := newService(t)
			taskInput := tc.setup(t, svc)
			created, err := svc.CreateTask(context.Background(), taskInput)
			assert.True(t, errContains(err, tc.err), "%s: expected %v got %v", tc.desc, tc.err, err)
			if tc.err == nil {
				assert.NotEmpty(t, created.ID)
			}
		})
	}
}

func TestGetTaskResults(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc  string
		setup func(t *testing.T, svc manager.Service) string
		err   error
	}{
		{
			desc: "get task results successfully",
			setup: func(t *testing.T, svc manager.Service) string {
				t.Helper()
				created, err := svc.CreateTask(context.Background(), task.Task{Name: "Task 1"})
				require.NoError(t, err)

				return created.ID
			},
			err: nil,
		},
		{
			desc: "get task results with non-existing task",
			setup: func(_ *testing.T, _ manager.Service) string {
				return "nonexistent"
			},
			err: pkgerrors.ErrNotFound,
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			svc := newService(t)
			taskID := tc.setup(t, svc)
			_, err := svc.GetTaskResults(context.Background(), taskID)
			assert.True(t, errContains(err, tc.err), "%s: expected %v got %v", tc.desc, tc.err, err)
		})
	}
}

func TestGetParentResults(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc  string
		setup func(t *testing.T, svc manager.Service) string
		err   error
	}{
		{
			desc: "get parent results successfully",
			setup: func(t *testing.T, svc manager.Service) string {
				t.Helper()
				parent1, err := svc.CreateTask(context.Background(), task.Task{Name: "Parent 1", WorkflowID: testWorkflowID})
				require.NoError(t, err)

				parent2, err := svc.CreateTask(context.Background(), task.Task{Name: "Parent 2", WorkflowID: testWorkflowID})
				require.NoError(t, err)

				child, err := svc.CreateTask(context.Background(), task.Task{
					Name:       "Child",
					WorkflowID: testWorkflowID,
					DependsOn:  []string{parent1.ID, parent2.ID},
				})
				require.NoError(t, err)

				return child.ID
			},
			err: nil,
		},
		{
			desc: "get parent results with non-existing task",
			setup: func(_ *testing.T, _ manager.Service) string {
				return "nonexistent"
			},
			err: pkgerrors.ErrNotFound,
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			svc := newService(t)
			taskID := tc.setup(t, svc)
			_, err := svc.GetParentResults(context.Background(), taskID)
			assert.True(t, errContains(err, tc.err), "%s: expected %v got %v", tc.desc, tc.err, err)
		})
	}
}

func TestStartTask(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc  string
		setup func(t *testing.T, svc manager.Service) string
		check func(t *testing.T, svc manager.Service, taskID string)
	}{
		{
			desc: "start task with unmet dependencies",
			setup: func(t *testing.T, svc manager.Service) string {
				t.Helper()
				parent, err := svc.CreateTask(context.Background(), task.Task{Name: "Parent", WorkflowID: testWorkflowID})
				require.NoError(t, err)

				child, err := svc.CreateTask(context.Background(), task.Task{
					Name:       "Child",
					WorkflowID: testWorkflowID,
					DependsOn:  []string{parent.ID},
				})
				require.NoError(t, err)

				return child.ID
			},
			check: func(t *testing.T, svc manager.Service, taskID string) {
				t.Helper()
				childAfter, err := svc.GetTask(context.Background(), taskID)
				require.NoError(t, err)
				assert.NotEqual(t, task.Running, childAfter.State)
			},
		},
		{
			desc: "start task after dependencies are met",
			setup: func(t *testing.T, svc manager.Service) string {
				t.Helper()
				parent, err := svc.CreateTask(context.Background(), task.Task{Name: "Parent", WorkflowID: testWorkflowID})
				require.NoError(t, err)

				child, err := svc.CreateTask(context.Background(), task.Task{
					Name:       "Child",
					WorkflowID: testWorkflowID,
					DependsOn:  []string{parent.ID},
				})
				require.NoError(t, err)

				parent.State = task.Completed
				_, err = svc.UpdateTask(context.Background(), parent)
				require.NoError(t, err)

				return child.ID
			},
			check: func(_ *testing.T, _ manager.Service, _ string) {},
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			svc := newService(t)
			taskID := tc.setup(t, svc)
			_ = svc.StartTask(context.Background(), taskID)
			tc.check(t, svc, taskID)
		})
	}
}
