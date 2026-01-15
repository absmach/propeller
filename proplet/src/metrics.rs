use serde::{Deserialize, Serialize};
use sysinfo::System;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CpuMetrics {
    pub user_seconds: f64,
    pub system_seconds: f64,
    pub percent: f64,
}

impl Default for CpuMetrics {
    fn default() -> Self {
        Self {
            user_seconds: 0.0,
            system_seconds: 0.0,
            percent: 0.0,
        }
    }
}

#[derive(Default, Debug, Clone, Serialize, Deserialize)]
pub struct MemoryMetrics {
    pub rss_bytes: u64,
    pub heap_alloc_bytes: u64,
    pub heap_sys_bytes: u64,
    pub heap_inuse_bytes: u64,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub container_usage_bytes: Option<u64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub container_limit_bytes: Option<u64>,
}

pub struct MetricsCollector {
    system: System,
}

impl MetricsCollector {
    pub fn new() -> Self {
        Self {
            system: System::new_all(),
        }
    }

    pub fn collect(&mut self) -> (CpuMetrics, MemoryMetrics) {
        self.system.refresh_all();

        let cpu = self.collect_cpu_metrics();
        let memory = self.collect_memory_metrics();

        (cpu, memory)
    }

    fn collect_cpu_metrics(&mut self) -> CpuMetrics {
        let mut cpu_metrics = CpuMetrics::default();

        // Get global CPU usage - use average of all CPUs
        let cpus = self.system.cpus();
        if !cpus.is_empty() {
            let total_usage: f32 = cpus.iter().map(|cpu| cpu.cpu_usage()).sum();
            cpu_metrics.percent = (total_usage / cpus.len() as f32) as f64;
        }

        // Try to get process-specific CPU times (Linux)
        #[cfg(target_os = "linux")]
        {
            if let Ok((user, system)) = Self::read_proc_stat() {
                cpu_metrics.user_seconds = user;
                cpu_metrics.system_seconds = system;
            }
        }

        cpu_metrics
    }

    #[cfg(target_os = "linux")]
    fn get_clock_ticks_per_second() -> Result<f64, std::io::Error> {
        // Query the system's clock ticks per second using sysconf
        let ticks = unsafe { libc::sysconf(libc::_SC_CLK_TCK) };

        if ticks <= 0 {
            return Err(std::io::Error::other(format!(
                "Invalid sysconf(_SC_CLK_TCK) return value: {}",
                ticks
            )));
        }

        Ok(ticks as f64)
    }

    #[cfg(target_os = "linux")]
    fn read_proc_stat() -> Result<(f64, f64), std::io::Error> {
        use std::fs;

        let contents = fs::read_to_string("/proc/self/stat")?;

        let close_paren = contents.rfind(')').ok_or_else(|| {
            std::io::Error::new(
                std::io::ErrorKind::InvalidData,
                "Invalid /proc/self/stat format",
            )
        })?;
        if close_paren + 2 >= contents.len() {
            return Ok((0.0, 0.0));
        }
        let fields: Vec<&str> = contents[close_paren + 2..].split_whitespace().collect();

        if fields.len() >= 14 {
            let utime: u64 = fields[11].parse().unwrap_or(0);
            let stime: u64 = fields[12].parse().unwrap_or(0);

            let hz = Self::get_clock_ticks_per_second().unwrap_or(100.0);

            let user_seconds = utime as f64 / hz;
            let system_seconds = stime as f64 / hz;

            return Ok((user_seconds, system_seconds));
        }

        Ok((0.0, 0.0))
    }

    fn collect_memory_metrics(&mut self) -> MemoryMetrics {
        let mut mem_metrics = MemoryMetrics::default();

        if let Ok(pid) = sysinfo::get_current_pid() {
            if let Some(process) = self.system.process(pid) {
                mem_metrics.rss_bytes = process.memory();
            }
        }

        #[cfg(target_os = "linux")]
        {
            if let Ok((usage, limit)) = Self::read_cgroup_memory() {
                mem_metrics.container_usage_bytes = Some(usage);
                if limit > 0 {
                    mem_metrics.container_limit_bytes = Some(limit);
                }
            }
        }

        mem_metrics
    }

    #[cfg(target_os = "linux")]
    fn read_cgroup_memory() -> Result<(u64, u64), std::io::Error> {
        use std::fs;

        if let Ok(usage_str) = fs::read_to_string("/sys/fs/cgroup/memory.current") {
            let usage = usage_str.trim().parse().unwrap_or(0);
            let limit = if let Ok(limit_str) = fs::read_to_string("/sys/fs/cgroup/memory.max") {
                let limit_str = limit_str.trim();
                if limit_str == "max" {
                    0
                } else {
                    limit_str.parse().unwrap_or(0)
                }
            } else {
                0
            };
            return Ok((usage, limit));
        }

        if let Ok(usage_str) = fs::read_to_string("/sys/fs/cgroup/memory/memory.usage_in_bytes") {
            let usage = usage_str.trim().parse().unwrap_or(0);
            let limit = if let Ok(limit_str) =
                fs::read_to_string("/sys/fs/cgroup/memory/memory.limit_in_bytes")
            {
                limit_str.trim().parse().unwrap_or(0)
            } else {
                0
            };
            return Ok((usage, limit));
        }

        Ok((0, 0))
    }
}

impl Default for MetricsCollector {
    fn default() -> Self {
        Self::new()
    }
}
