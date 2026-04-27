package manager_test

import (
	"context"
	"testing"

	"github.com/absmach/propeller/manager"
	pkgerrors "github.com/absmach/propeller/pkg/errors"
	"github.com/absmach/propeller/pkg/task"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateJob(t *testing.T) {
	t.Parallel()
	svc := newService(t)

	tasks := []task.Task{
		{Name: "task1", State: task.Pending},
		{Name: "task2", State: task.Pending},
	}

	jobID, createdTasks, err := svc.CreateJob(context.Background(), "test-job", tasks, "parallel")
	require.NoError(t, err)
	assert.NotEmpty(t, jobID)
	assert.Len(t, createdTasks, 2)

	for _, ct := range createdTasks {
		assert.Equal(t, jobID, ct.JobID)
		assert.NotEmpty(t, ct.ID)
	}
}

func TestGetJob(t *testing.T) {
	t.Parallel()
	svc := newService(t)

	jobID := uuid.NewString()
	tasks := []task.Task{
		{ID: uuid.NewString(), Name: "task1", JobID: jobID, State: task.Pending},
		{ID: uuid.NewString(), Name: "task2", JobID: jobID, State: task.Pending},
	}

	for _, taskItem := range tasks {
		_, err := svc.CreateTask(context.Background(), taskItem)
		require.NoError(t, err)
	}

	jobTasks, err := svc.GetJob(context.Background(), jobID)
	require.NoError(t, err)
	assert.Len(t, jobTasks, 2)
}

func TestListJobs(t *testing.T) {
	t.Parallel()
	svc := newService(t)

	_, _, err := svc.CreateJob(context.Background(), "job-1", []task.Task{
		{Name: "t1", State: task.Pending},
	}, "parallel")
	require.NoError(t, err)

	_, _, err = svc.CreateJob(context.Background(), "job-2", []task.Task{
		{Name: "t2", State: task.Pending},
		{Name: "t3", State: task.Pending},
	}, "sequential")
	require.NoError(t, err)

	page, err := svc.ListJobs(context.Background(), 0, 100, "")
	require.NoError(t, err)
	assert.Equal(t, uint64(2), page.Total)
	assert.Len(t, page.Jobs, 2)
}

func TestListJobsIncludesLegacyTaskOnlyJob(t *testing.T) {
	t.Parallel()
	svc := newService(t)

	_, _, err := svc.CreateJob(context.Background(), "stored-job", []task.Task{
		{Name: "stored-task", State: task.Pending},
	}, "parallel")
	require.NoError(t, err)

	_, err = svc.CreateTask(context.Background(), task.Task{
		Name:  "legacy-task",
		JobID: "legacy-job-id",
		State: task.Pending,
	})
	require.NoError(t, err)

	page, err := svc.ListJobs(context.Background(), 0, 100, "")
	require.NoError(t, err)
	assert.Equal(t, uint64(2), page.Total)

	ids := make(map[string]struct{}, len(page.Jobs))
	for _, j := range page.Jobs {
		ids[j.JobID] = struct{}{}
	}

	_, ok := ids["legacy-job-id"]
	assert.True(t, ok)
}

func TestCreateJobPersistsJobEntity(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc          string
		name          string
		executionMode string
	}{
		{
			desc:          "persist job in sequential mode",
			name:          "persisted-job-sequential",
			executionMode: "sequential",
		},
		{
			desc:          "persist job in parallel mode",
			name:          "persisted-job-parallel",
			executionMode: "parallel",
		},
		{
			desc:          "persist job in configurable mode",
			name:          "persisted-job-configurable",
			executionMode: "configurable",
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			svc := newService(t)

			jobID, _, err := svc.CreateJob(context.Background(), tc.name, []task.Task{
				{Name: "task1", State: task.Pending},
			}, tc.executionMode)
			require.NoError(t, err)

			jobTasks, err := svc.GetJob(context.Background(), jobID)
			require.NoError(t, err)
			require.Len(t, jobTasks, 1)
			assert.Equal(t, jobID, jobTasks[0].JobID)
		})
	}
}

func TestCreateJobWithDependencies(t *testing.T) {
	t.Parallel()
	svc := newService(t)

	tasks := []task.Task{
		{ID: "dep-a", Name: "Task A", State: task.Pending, DependsOn: []string{}},
		{ID: "dep-b", Name: "Task B", State: task.Pending, DependsOn: []string{"dep-a"}},
		{ID: "dep-c", Name: "Task C", State: task.Pending, DependsOn: []string{"dep-b"}},
	}

	jobID, createdTasks, err := svc.CreateJob(context.Background(), "dag-job", tasks, "configurable")
	require.NoError(t, err)
	assert.NotEmpty(t, jobID)
	assert.Len(t, createdTasks, 3)

	for _, ct := range createdTasks {
		assert.Equal(t, jobID, ct.JobID)
	}
}

func TestCreateJobCyclicDependencyRejected(t *testing.T) {
	t.Parallel()
	svc := newService(t)

	tasks := []task.Task{
		{ID: "cyc-a", Name: "Task A", State: task.Pending, DependsOn: []string{"cyc-b"}},
		{ID: "cyc-b", Name: "Task B", State: task.Pending, DependsOn: []string{"cyc-a"}},
	}

	_, _, err := svc.CreateJob(context.Background(), "cyclic-job", tasks, "parallel")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DAG validation failed")
}

func TestCreateJobEmptyTasksRejected(t *testing.T) {
	t.Parallel()
	svc := newService(t)

	_, _, err := svc.CreateJob(context.Background(), "empty-job", []task.Task{}, "parallel")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one task")
}

func TestListJobsPagination(t *testing.T) {
	t.Parallel()
	svc := newService(t)

	for range 5 {
		_, _, err := svc.CreateJob(context.Background(), "job", []task.Task{
			{Name: "t", State: task.Pending},
		}, "parallel")
		require.NoError(t, err)
	}

	page, err := svc.ListJobs(context.Background(), 0, 3, "")
	require.NoError(t, err)
	assert.Equal(t, uint64(5), page.Total)
	assert.Len(t, page.Jobs, 3)

	page2, err := svc.ListJobs(context.Background(), 3, 3, "")
	require.NoError(t, err)
	assert.Len(t, page2.Jobs, 2)
}

func TestComputeJobState(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		tasks    []task.Task
		expected task.State
	}{
		{
			name: "all pending",
			tasks: []task.Task{
				{State: task.Pending},
				{State: task.Pending},
			},
			expected: task.Pending,
		},
		{
			name: "one running",
			tasks: []task.Task{
				{State: task.Running},
				{State: task.Pending},
			},
			expected: task.Running,
		},
		{
			name: "all completed",
			tasks: []task.Task{
				{State: task.Completed},
				{State: task.Completed},
			},
			expected: task.Completed,
		},
		{
			name: "one failed",
			tasks: []task.Task{
				{State: task.Completed},
				{State: task.Failed},
			},
			expected: task.Failed,
		},
		{
			name: "mixed completed and skipped",
			tasks: []task.Task{
				{State: task.Completed},
				{State: task.Skipped},
			},
			expected: task.Completed,
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

func TestListJobsFilterByStatus(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc          string
		status        string
		expectedTotal uint64
		expectedState task.State
		err           bool
	}{
		{
			desc:          "list all jobs without filter",
			status:        "",
			expectedTotal: 4,
		},
		{
			desc:          "filter by pending",
			status:        task.PendingStatus.String(),
			expectedTotal: 1,
			expectedState: task.Pending,
		},
		{
			desc:          "filter by running",
			status:        task.RunningStatus.String(),
			expectedTotal: 1,
			expectedState: task.Running,
		},
		{
			desc:          "filter by completed",
			status:        task.CompletedStatus.String(),
			expectedTotal: 1,
			expectedState: task.Completed,
		},
		{
			desc:          "filter by failed",
			status:        task.FailedStatus.String(),
			expectedTotal: 1,
			expectedState: task.Failed,
		},
		{
			desc:   "invalid status returns error",
			status: "invalid",
			err:    true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			svc := newService(t)
			ctx := context.Background()

			if !tc.err {
				for _, s := range []task.State{task.Pending, task.Running, task.Completed, task.Failed} {
					_, _, err := svc.CreateJob(ctx, "job", []task.Task{{Name: "t", State: s}}, "parallel")
					require.NoError(t, err)
				}
			}

			page, err := svc.ListJobs(ctx, 0, 100, tc.status)
			if tc.err {
				require.Error(t, err)
				require.ErrorIs(t, err, pkgerrors.ErrInvalidValue)
				require.ErrorIs(t, err, task.ErrInvalidJobStatus)

				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.expectedTotal, page.Total)
			if tc.status != "" && len(page.Jobs) > 0 {
				assert.Equal(t, tc.expectedState, page.Jobs[0].State)
			}
		})
	}
}

func TestListJobsStateMapping(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc          string
		taskState     task.State
		filterStatus  string
		expectedTotal uint64
	}{
		{
			desc:          "interrupted task maps to failed",
			taskState:     task.Interrupted,
			filterStatus:  task.FailedStatus.String(),
			expectedTotal: 1,
		},
		{
			desc:          "scheduled task maps to running",
			taskState:     task.Scheduled,
			filterStatus:  task.RunningStatus.String(),
			expectedTotal: 1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			svc := newService(t)
			ctx := context.Background()

			_, _, err := svc.CreateJob(ctx, "job", []task.Task{{Name: "t", State: tc.taskState}}, "parallel")
			require.NoError(t, err)

			page, err := svc.ListJobs(ctx, 0, 100, tc.filterStatus)
			require.NoError(t, err)
			assert.Equal(t, tc.expectedTotal, page.Total)
		})
	}
}
