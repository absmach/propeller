package manager_test

import (
	"context"
	"testing"

	"github.com/absmach/propeller/manager"
	"github.com/absmach/propeller/task"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateJob(t *testing.T) {
	t.Parallel()
	svc := setupTestService(t)

	tasks := []task.Task{
		{Name: "task1", State: task.Pending},
		{Name: "task2", State: task.Pending},
	}

	jobID, createdTasks, err := svc.CreateJob(context.Background(), "test-job", tasks, "parallel")
	require.NoError(t, err)
	assert.NotEmpty(t, jobID)
	assert.Len(t, createdTasks, 2)

	// Verify all tasks have the same JobID
	for _, task := range createdTasks {
		assert.Equal(t, jobID, task.JobID)
		assert.NotEmpty(t, task.ID)
	}

	// Verify execution mode is stored in Env
	if createdTasks[0].Env != nil {
		assert.Equal(t, "parallel", createdTasks[0].Env["_job_execution_mode"])
		assert.Equal(t, "test-job", createdTasks[0].Env["_job_name"])
	}
}

func TestGetJob(t *testing.T) {
	t.Parallel()
	svc := setupTestService(t)

	jobID := uuid.NewString()
	tasks := []task.Task{
		{ID: uuid.NewString(), Name: "task1", JobID: jobID, State: task.Pending},
		{ID: uuid.NewString(), Name: "task2", JobID: jobID, State: task.Pending},
	}

	// Create tasks manually
	for _, taskItem := range tasks {
		_, err := svc.CreateTask(context.Background(), taskItem)
		require.NoError(t, err)
	}

	// Get job
	jobTasks, err := svc.GetJob(context.Background(), jobID)
	require.NoError(t, err)
	assert.Len(t, jobTasks, 2)
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
