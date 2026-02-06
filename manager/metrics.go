package manager

import (
	"github.com/absmach/propeller/pkg/storage"
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
