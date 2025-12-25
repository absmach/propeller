package manager

import (
	"time"

	"github.com/absmach/propeller/pkg/proplet"
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
