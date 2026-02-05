package manager

import (
	"time"

	"github.com/absmach/propeller/pkg/proplet"
	"github.com/absmach/propeller/task"
)

type TaskMetrics struct {
	TaskID     string                     `json:"task_id"`
	PropletID  string                     `json:"proplet_id"`
	Metrics    proplet.ProcessMetrics     `json:"metrics"`
	Aggregated *proplet.AggregatedMetrics `json:"aggregated,omitempty"`
	Timestamp  time.Time                  `json:"timestamp"`
}

type PropletMetrics struct {
	PropletID string                `json:"proplet_id"`
	Namespace string                `json:"namespace"`
	Timestamp time.Time             `json:"timestamp"`
	CPU       proplet.CPUMetrics    `json:"cpu_metrics"`
	Memory    proplet.MemoryMetrics `json:"memory_metrics"`
}

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
