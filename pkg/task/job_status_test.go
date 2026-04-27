package task_test

import (
	"testing"

	"github.com/absmach/propeller/pkg/task"
	"github.com/stretchr/testify/assert"
)

func TestJobStatusString(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc   string
		status task.JobStatus
		value  string
	}{
		{
			desc:   "pending status",
			status: task.PendingStatus,
			value:  task.JobStatusPending,
		},
		{
			desc:   "running status",
			status: task.RunningStatus,
			value:  task.JobStatusRunning,
		},
		{
			desc:   "completed status",
			status: task.CompletedStatus,
			value:  task.JobStatusCompleted,
		},
		{
			desc:   "failed status",
			status: task.FailedStatus,
			value:  task.JobStatusFailed,
		},
		{
			desc:   "unknown status",
			status: task.JobStatus(100),
			value:  task.JobStatusUnknown,
		},
	}

	for _, tc := range cases {
		got := tc.status.String()
		assert.Equal(t, tc.value, got, "JobStatus.String() = %v, expected %v", got, tc.value)
	}
}

func TestToJobStatus(t *testing.T) {
	t.Parallel()

	cases := []struct {
		desc   string
		value  string
		status task.JobStatus
		err    error
	}{
		{
			desc:   "pending status",
			value:  task.JobStatusPending,
			status: task.PendingStatus,
			err:    nil,
		},
		{
			desc:   "running status",
			value:  task.JobStatusRunning,
			status: task.RunningStatus,
			err:    nil,
		},
		{
			desc:   "completed status",
			value:  task.JobStatusCompleted,
			status: task.CompletedStatus,
			err:    nil,
		},
		{
			desc:   "failed status",
			value:  task.JobStatusFailed,
			status: task.FailedStatus,
			err:    nil,
		},
		{
			desc:   "invalid status",
			value:  "unknown",
			status: task.JobStatus(0),
			err:    task.ErrInvalidJobStatus,
		},
	}

	for _, tc := range cases {
		got, err := task.ToJobStatus(tc.value)
		assert.Equal(t, tc.err, err, "ToJobStatus() error = %v, expected %v", err, tc.err)
		assert.Equal(t, tc.status, got, "ToJobStatus() = %v, expected %v", got, tc.status)
	}
}
