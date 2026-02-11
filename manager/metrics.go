package manager

import (
	"time"

	"github.com/absmach/propeller/pkg/storage"
	"github.com/absmach/propeller/pkg/task"
)

type (
	TaskMetrics    = storage.TaskMetrics
	PropletMetrics = storage.PropletMetrics
)

type TaskMetricsPage struct {
	Offset  uint64        `json:"offset"`
	Limit   uint64        `json:"limit"`
	Total   uint64        `json:"total"`
	Metrics []TaskMetrics `json:"metrics"`
}

type PropletMetricsPage struct {
	Offset  uint64           `json:"offset"`
	Limit   uint64           `json:"limit"`
	Total   uint64           `json:"total"`
	Metrics []PropletMetrics `json:"metrics"`
}

type JobSummary struct {
	JobID      string      `json:"job_id"`
	Name       string      `json:"name,omitempty"`
	State      task.State  `json:"state"`
	Tasks      []task.Task `json:"tasks"`
	StartTime  time.Time   `json:"start_time"`
	FinishTime time.Time   `json:"finish_time"`
	CreatedAt  time.Time   `json:"created_at"`
}

type JobPage struct {
	Offset uint64       `json:"offset"`
	Limit  uint64       `json:"limit"`
	Total  uint64       `json:"total"`
	Jobs   []JobSummary `json:"jobs"`
}
