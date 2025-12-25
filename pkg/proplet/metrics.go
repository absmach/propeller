package proplet

import "time"

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

type ProcessMetrics struct {
	CPUPercent          float64 `json:"cpu_percent"`
	MemoryBytes         uint64  `json:"memory_bytes"`
	MemoryPercent       float32 `json:"memory_percent"`
	DiskReadBytes       uint64  `json:"disk_read_bytes"`
	DiskWriteBytes      uint64  `json:"disk_write_bytes"`
	UptimeSeconds       int64   `json:"uptime_seconds"`
	ThreadCount         int32   `json:"thread_count"`
	FileDescriptorCount int32   `json:"file_descriptor_count,omitempty"`
}

type AggregatedMetrics struct {
	AvgCPUUsage    float64 `json:"avg_cpu_usage"`
	MaxCPUUsage    float64 `json:"max_cpu_usage"`
	AvgMemoryUsage uint64  `json:"avg_memory_usage"`
	MaxMemoryUsage uint64  `json:"max_memory_usage"`
	TotalDiskRead  uint64  `json:"total_disk_read"`
	TotalDiskWrite uint64  `json:"total_disk_write"`
	SampleCount    int     `json:"sample_count"`
}

type MonitoringProfile struct {
	Enabled                bool          `json:"enabled"`
	Interval               time.Duration `json:"interval"`
	CollectCPU             bool          `json:"collect_cpu"`
	CollectMemory          bool          `json:"collect_memory"`
	CollectDiskIO          bool          `json:"collect_disk_io"`
	CollectThreads         bool          `json:"collect_threads"`
	CollectFileDescriptors bool          `json:"collect_file_descriptors"`
	ExportToMQTT           bool          `json:"export_to_mqtt"`
	RetainHistory          bool          `json:"retain_history"`
	HistorySize            int           `json:"history_size"`
}
