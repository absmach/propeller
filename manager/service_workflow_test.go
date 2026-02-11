// Copyright (c) Abstract Machines
// SPDX-License-Identifier: Apache-2.0

package manager_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/absmach/propeller/manager"
	pkgerrors "github.com/absmach/propeller/pkg/errors"
	mqttmocks "github.com/absmach/propeller/pkg/mqtt/mocks"
	"github.com/absmach/propeller/pkg/scheduler"
	"github.com/absmach/propeller/pkg/storage"
	"github.com/absmach/propeller/task"
	smqerrors "github.com/absmach/supermq/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const testWorkflowID = "test-workflow"

var (
	errEmptyWorkflow    = errors.New("workflow must contain at least one task")
	errCircularDep      = errors.New("DAG validation failed: circular dependency detected: cycle detected involving tasks task2 and task1")
	errMissingDep       = errors.New("dependency validation failed: dependency task not found: task task1 depends on nonexistent which does not exist")
	errInvalidRunIf     = errors.New("invalid run_if value for task task1: must be 'success' or 'failure'")
	errWorkflowRequired = errors.New("workflow_id is required when depends_on is specified")
)

func newService(t *testing.T) manager.Service {
	t.Helper()
	repos, err := storage.NewRepositories(storage.Config{Type: "memory"})
	require.NoError(t, err)
	sched := scheduler.NewRoundRobin()
	pubsub := mqttmocks.NewPubSub(t)
	pubsub.On("Publish", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	pubsub.On("Subscribe", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	pubsub.On("Unsubscribe", mock.Anything, mock.Anything).Return(nil).Maybe()
	pubsub.On("Disconnect", mock.Anything).Return(nil).Maybe()
	logger := slog.Default()

	svc, _ := manager.NewService(repos, sched, pubsub, "test-domain", "test-channel", logger)

	return svc
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
			err: errCircularDep,
		},
		{
			desc: "create workflow with missing dependency",
			tasks: []task.Task{
				{ID: "task1", Name: "Task 1", DependsOn: []string{"nonexistent"}},
			},
			err: errMissingDep,
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
			assert.True(t, smqerrors.Contains(err, tc.err), "%s: expected %v got %v", tc.desc, tc.err, err)
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
			assert.True(t, smqerrors.Contains(err, tc.err), "%s: expected %v got %v", tc.desc, tc.err, err)
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
			assert.True(t, smqerrors.Contains(err, tc.err), "%s: expected %v got %v", tc.desc, tc.err, err)
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
			assert.True(t, smqerrors.Contains(err, tc.err), "%s: expected %v got %v", tc.desc, tc.err, err)
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
