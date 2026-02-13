package manager_test

import (
	"context"
	"testing"

	"github.com/absmach/propeller/manager"
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

	page, err := svc.ListJobs(context.Background(), 0, 100)
	require.NoError(t, err)
	assert.Equal(t, uint64(2), page.Total)
	assert.Len(t, page.Jobs, 2)
}

func TestCreateJobPersistsJobEntity(t *testing.T) {
	t.Parallel()
	svc := newService(t)

	jobID, _, err := svc.CreateJob(context.Background(), "persisted-job", []task.Task{
		{Name: "task1", State: task.Pending},
	}, "sequential")
	require.NoError(t, err)

	jobTasks, err := svc.GetJob(context.Background(), jobID)
	require.NoError(t, err)
	assert.Len(t, jobTasks, 1)
	assert.Equal(t, jobID, jobTasks[0].JobID)
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

	page, err := svc.ListJobs(context.Background(), 0, 3)
	require.NoError(t, err)
	assert.Equal(t, uint64(5), page.Total)
	assert.Len(t, page.Jobs, 3)

	page2, err := svc.ListJobs(context.Background(), 3, 3)
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
