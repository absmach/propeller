package proplet

import "time"

type Metrics struct {
	Version   string        `json:"version"`
	Timestamp time.Time     `json:"timestamp"`
	CPU       CPUMetrics    `json:"cpu"`
	Memory    MemoryMetrics `json:"memory"`
}

type CPUMetrics struct {
	UserSeconds   float64 `json:"user_seconds"`
	SystemSeconds float64 `json:"system_seconds"`
	Percent       float64 `json:"percent"` // 100% = one full CPU core
}

type MemoryMetrics struct {
	RSSBytes uint64 `json:"rss_bytes"`

	HeapAllocBytes uint64 `json:"heap_alloc_bytes"`
	HeapSysBytes   uint64 `json:"heap_sys_bytes"`
	HeapInuseBytes uint64 `json:"heap_inuse_bytes"`

	ContainerUsageBytes *uint64 `json:"container_usage_bytes,omitempty"`
	ContainerLimitBytes *uint64 `json:"container_limit_bytes,omitempty"`
}
