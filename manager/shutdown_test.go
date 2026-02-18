package manager_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/absmach/propeller/manager"
	"github.com/absmach/propeller/pkg/dag"
	mqttmocks "github.com/absmach/propeller/pkg/mqtt/mocks"
	"github.com/absmach/propeller/pkg/scheduler"
	"github.com/absmach/propeller/pkg/storage"
	"github.com/absmach/propeller/pkg/task"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func newShutdownService(t *testing.T) manager.Service {
	t.Helper()

	return newService(t)
}

func TestShutdownInterruptsRunningTasks(t *testing.T) {
	t.Parallel()

	type taskSetup struct {
		name  string
		state task.State
	}
	type taskExpect struct {
		state    task.State
		errMsg   string
		checkErr bool
	}

	tests := []struct {
		name    string
		setup   []taskSetup
		expects []taskExpect
	}{
		{
			name: "running and scheduled are interrupted, completed and pending unchanged",
			setup: []taskSetup{
				{name: "running-task-1", state: task.Running},
				{name: "completed-task", state: task.Completed},
				{name: "pending-task", state: task.Pending},
				{name: "scheduled-task", state: task.Scheduled},
			},
			expects: []taskExpect{
				{state: task.Interrupted, errMsg: "interrupted by shutdown", checkErr: true},
				{state: task.Completed},
				{state: task.Pending},
				{state: task.Interrupted, errMsg: "interrupted by shutdown", checkErr: true},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc := newShutdownService(t)
			ctx := context.Background()

			ids := make([]string, len(tt.setup))
			for i, s := range tt.setup {
				created, err := svc.CreateTask(ctx, task.Task{Name: s.name, State: s.state})
				require.NoError(t, err)
				ids[i] = created.ID
			}

			err := svc.Shutdown(ctx)
			require.NoError(t, err)

			for i, exp := range tt.expects {
				got, err := svc.GetTask(ctx, ids[i])
				require.NoError(t, err)
				assert.Equal(t, exp.state, got.State, "task %d (%s)", i, tt.setup[i].name)
				if exp.checkErr {
					assert.Equal(t, exp.errMsg, got.Error, "task %d error", i)
				}
			}
		})
	}
}

func TestShutdownSignalsStopBeforeInterrupt(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repos, err := storage.NewRepositories(storage.Config{Type: "memory"})
	require.NoError(t, err)

	pubsub := mqttmocks.NewPubSub(t)
	pubsub.On("Subscribe", mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	pubsub.On("Unsubscribe", mock.Anything, mock.Anything).Return(nil).Maybe()
	pubsub.On("Disconnect", mock.Anything).Return(nil).Maybe()

	svc, _ := manager.NewService(repos, scheduler.NewRoundRobin(), pubsub, "test-domain", "test-channel", slog.Default())

	created, err := svc.CreateTask(ctx, task.Task{
		Name:      "running-task",
		State:     task.Running,
		PropletID: "proplet-1",
	})
	require.NoError(t, err)

	stopTopic := "m/test-domain/c/test-channel/control/manager/stop"
	pubsub.On("Publish", mock.Anything, stopTopic, mock.MatchedBy(func(msg any) bool {
		payload, ok := msg.(map[string]any)
		if !ok {
			return false
		}

		return payload["id"] == created.ID && payload["proplet_id"] == "proplet-1"
	})).Run(func(args mock.Arguments) {
		got, getErr := svc.GetTask(ctx, created.ID)
		require.NoError(t, getErr)
		assert.Equal(t, task.Running, got.State)
	}).Return(nil).Once()

	err = svc.Shutdown(ctx)
	require.NoError(t, err)

	got, err := svc.GetTask(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, task.Interrupted, got.State)
	assert.Equal(t, "interrupted by shutdown", got.Error)
}

func TestShutdownPreventsNewTasks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		taskName   string
		taskState  task.State
		errContain string
	}{
		{
			name:       "StartTask rejected after shutdown",
			taskName:   "pre-shutdown-task",
			taskState:  task.Pending,
			errContain: "shutting down",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc := newShutdownService(t)
			ctx := context.Background()

			created, err := svc.CreateTask(ctx, task.Task{Name: tt.taskName, State: tt.taskState})
			require.NoError(t, err)

			err = svc.Shutdown(ctx)
			require.NoError(t, err)

			err = svc.StartTask(ctx, created.ID)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errContain)
		})
	}
}

func TestShutdownPreventsNewJobs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		jobName    string
		taskName   string
		errContain string
	}{
		{
			name:       "StartJob rejected after shutdown",
			jobName:    "test-job",
			taskName:   "job-task-1",
			errContain: "shutting down",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc := newShutdownService(t)
			ctx := context.Background()

			jobID := uuid.NewString()
			tasks := []task.Task{
				{Name: tt.taskName, JobID: jobID, State: task.Pending},
			}
			_, createdTasks, err := svc.CreateJob(ctx, tt.jobName, tasks, "parallel")
			require.NoError(t, err)
			require.Len(t, createdTasks, 1)

			err = svc.Shutdown(ctx)
			require.NoError(t, err)

			err = svc.StartJob(ctx, createdTasks[0].JobID)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errContain)
		})
	}
}

func TestRecoverInterruptedTasks(t *testing.T) {
	t.Parallel()

	type taskSetup struct {
		name  string
		state task.State
		err   string
	}
	type taskExpect struct {
		state    task.State
		errMsg   string
		checkErr bool
	}

	tests := []struct {
		name    string
		setup   []taskSetup
		expects []taskExpect
	}{
		{
			name: "interrupted become failed, pending unchanged",
			setup: []taskSetup{
				{name: "interrupted-task-1", state: task.Interrupted, err: "interrupted by shutdown"},
				{name: "interrupted-task-2", state: task.Interrupted, err: "interrupted by shutdown"},
				{name: "pending-task", state: task.Pending},
			},
			expects: []taskExpect{
				{state: task.Failed, errMsg: "interrupted by shutdown", checkErr: true},
				{state: task.Failed},
				{state: task.Pending},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc := newShutdownService(t)
			ctx := context.Background()

			ids := make([]string, len(tt.setup))
			for i, s := range tt.setup {
				created, err := svc.CreateTask(ctx, task.Task{
					Name:  s.name,
					State: s.state,
					Error: s.err,
				})
				require.NoError(t, err)
				ids[i] = created.ID
			}

			err := svc.RecoverInterruptedTasks(ctx)
			require.NoError(t, err)

			for i, exp := range tt.expects {
				got, err := svc.GetTask(ctx, ids[i])
				require.NoError(t, err)
				assert.Equal(t, exp.state, got.State, "task %d (%s)", i, tt.setup[i].name)
				if exp.checkErr {
					assert.Equal(t, exp.errMsg, got.Error, "task %d error", i)
				}
			}
		})
	}
}

func TestComputeJobStateWithInterrupted(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		tasks    []task.Task
		expected task.State
	}{
		{
			name: "one interrupted",
			tasks: []task.Task{
				{State: task.Completed},
				{State: task.Interrupted},
			},
			expected: task.Failed,
		},
		{
			name: "all interrupted",
			tasks: []task.Task{
				{State: task.Interrupted},
				{State: task.Interrupted},
			},
			expected: task.Failed,
		},
		{
			name: "interrupted and running",
			tasks: []task.Task{
				{State: task.Running},
				{State: task.Interrupted},
			},
			expected: task.Failed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := manager.ComputeJobState(tt.tasks)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetReadyTasksWithInterrupted(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		tasks     []task.Task
		completed map[string]task.State
		readyLen  int
		readyIDs  []string
	}{
		{
			name: "interrupted tasks are skipped as candidates",
			tasks: []task.Task{
				{ID: "t1", State: task.Interrupted},
				{ID: "t2", State: task.Pending, DependsOn: []string{"t1"}},
			},
			completed: map[string]task.State{
				"t1": task.Interrupted,
			},
			readyLen: 1,
			readyIDs: []string{"t2"},
		},
		{
			name: "interrupted dependency satisfies requirement",
			tasks: []task.Task{
				{ID: "t1", State: task.Interrupted},
				{ID: "t2", State: task.Pending, DependsOn: []string{"t1"}},
				{ID: "t3", State: task.Pending},
			},
			completed: map[string]task.State{
				"t1": task.Interrupted,
			},
			readyLen: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ready := dag.GetReadyTasks(tt.tasks, tt.completed)
			assert.Len(t, ready, tt.readyLen)
			for i, id := range tt.readyIDs {
				assert.Equal(t, id, ready[i].ID)
			}
		})
	}
}

func TestInterruptedStateString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		state  task.State
		strVal string
		intVal task.State
	}{
		{
			name:   "Interrupted string and value",
			state:  task.Interrupted,
			strVal: "Interrupted",
			intVal: task.State(6),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.strVal, tt.state.String())
			assert.Equal(t, tt.state, tt.intVal)
		})
	}
}
