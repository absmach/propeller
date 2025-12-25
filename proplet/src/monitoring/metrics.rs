use serde::{Deserialize, Serialize};

#[derive(Default, Debug, Clone, Serialize, Deserialize)]
pub struct ProcessMetrics {
    pub cpu_percent: f64,
    pub memory_bytes: u64,
    pub memory_percent: f64,
    pub disk_read_bytes: u64,
    pub disk_write_bytes: u64,
    pub uptime_seconds: u64,
    pub thread_count: u32,
    pub file_descriptor_count: u32,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct AggregatedMetrics {
    pub avg_cpu_usage: f64,
    pub max_cpu_usage: f64,
    pub avg_memory_usage: u64,
    pub max_memory_usage: u64,
    pub total_disk_read: u64,
    pub total_disk_write: u64,
    pub sample_count: usize,
}
