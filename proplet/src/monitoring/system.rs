use super::metrics::{AggregatedMetrics, ProcessMetrics};
use super::ProcessMonitor;
use crate::types::MonitoringProfile;
use anyhow::Result;
use async_trait::async_trait;
use std::collections::HashMap;
use std::sync::Arc;
use std::time::SystemTime;
use sysinfo::{Pid, ProcessRefreshKind, ProcessesToUpdate, System};
use tokio::sync::Mutex;
use tokio::time::interval;
use tracing::{debug, warn};

struct MonitoredTask {
    pid: Option<u32>,
    profile: MonitoringProfile,
    metrics_history: Vec<ProcessMetrics>,
    start_time: SystemTime,
}

pub struct SystemMonitor {
    tasks: Arc<Mutex<HashMap<String, MonitoredTask>>>,
    system: Arc<Mutex<System>>,
}

impl SystemMonitor {
    pub fn new(_default_profile: MonitoringProfile) -> Self {
        let mut system = System::new_all();
        system.refresh_all();

        Self {
            tasks: Arc::new(Mutex::new(HashMap::new())),
            system: Arc::new(Mutex::new(system)),
        }
    }

    async fn collect_process_metrics(
        sys: &mut System,
        pid: u32,
        profile: &MonitoringProfile,
        start_time: SystemTime,
    ) -> Result<ProcessMetrics> {
        let pid = Pid::from_u32(pid);

        sys.refresh_processes_specifics(
            ProcessesToUpdate::Some(&[pid]),
            true,
            ProcessRefreshKind::new()
                .with_cpu()
                .with_memory()
                .with_disk_usage(),
        );

        let process = sys
            .process(pid)
            .ok_or_else(|| anyhow::anyhow!("Process with PID {} not found", pid.as_u32()))?;

        let mut metrics = ProcessMetrics {
            cpu_percent: 0.0,
            memory_bytes: 0,
            memory_percent: 0.0,
            disk_read_bytes: 0,
            disk_write_bytes: 0,
            uptime_seconds: 0,
            thread_count: 0,
            file_descriptor_count: 0,
        };

        if profile.collect_cpu {
            metrics.cpu_percent = process.cpu_usage() as f64;
        }

        if profile.collect_memory {
            metrics.memory_bytes = process.memory() * 1024;
        }

        if profile.collect_disk_io {
            let disk_usage = process.disk_usage();
            metrics.disk_read_bytes = disk_usage.total_read_bytes;
            metrics.disk_write_bytes = disk_usage.total_written_bytes;
        }

        if profile.collect_threads {
            #[cfg(not(target_os = "windows"))]
            {
                metrics.thread_count = sys
                    .process(pid)
                    .and_then(|p| p.tasks())
                    .map(|t| t.len() as u32)
                    .unwrap_or(0);
            }
            #[cfg(target_os = "windows")]
            {
                metrics.thread_count = 1;
            }
        }

        if profile.collect_file_descriptors {
            #[cfg(target_os = "linux")]
            {
                if let Ok(entries) = std::fs::read_dir(format!("/proc/{}/fd", pid.as_u32())) {
                    metrics.file_descriptor_count = entries.count() as u32;
                }
            }
            #[cfg(target_os = "macos")]
            {
                use tokio::process::Command;
                if let Ok(output) = Command::new("lsof")
                    .args(["-p", &pid.as_u32().to_string()])
                    .output()
                    .await
                {
                    let count = output.stdout.split(|&b| b == b'\n').count();
                    metrics.file_descriptor_count = count.saturating_sub(1) as u32;
                }
            }
            #[cfg(target_os = "windows")]
            {
                metrics.file_descriptor_count = 0;
            }
        }

        if let Ok(duration) = SystemTime::now().duration_since(start_time) {
            metrics.uptime_seconds = duration.as_secs();
        }

        Ok(metrics)
    }

    pub async fn attach_pid(
        &self,
        task_id: &str,
        pid: u32,
        export_fn: impl Fn(ProcessMetrics, Option<AggregatedMetrics>) + Send + 'static,
    ) {
        let profile = {
            let mut tasks_guard = self.tasks.lock().await;
            if let Some(task) = tasks_guard.get_mut(task_id) {
                task.pid = Some(pid);
                task.profile.clone()
            } else {
                return;
            }
        };

        let task_id = task_id.to_string();
        let tasks = self.tasks.clone();
        let system = self.system.clone();

        tokio::spawn(async move {
            Self::monitor_task(task_id, pid, profile, tasks, system, export_fn).await;
        });
    }

    async fn monitor_task(
        task_id: String,
        pid: u32,
        profile: MonitoringProfile,
        tasks: Arc<Mutex<HashMap<String, MonitoredTask>>>,
        system: Arc<Mutex<System>>,
        export_fn: impl Fn(ProcessMetrics, Option<AggregatedMetrics>) + Send + 'static,
    ) {
        {
            let mut sys = system.lock().await;
            let pid_val = Pid::from_u32(pid);
            sys.refresh_processes_specifics(
                ProcessesToUpdate::Some(&[pid_val]),
                true,
                ProcessRefreshKind::new()
                    .with_cpu()
                    .with_memory()
                    .with_disk_usage(),
            );
        }

        let mut interval_timer = interval(profile.interval);

        let start_time = {
            let tasks_guard = tasks.lock().await;
            tasks_guard
                .get(&task_id)
                .map(|t| t.start_time)
                .unwrap_or_else(SystemTime::now)
        };

        loop {
            interval_timer.tick().await;

            let should_continue = {
                let tasks_guard = tasks.lock().await;
                tasks_guard.contains_key(&task_id)
            };

            if !should_continue {
                debug!("Stopping monitoring for task {}", task_id);
                break;
            }

            let mut sys = system.lock().await;
            match Self::collect_process_metrics(&mut sys, pid, &profile, start_time).await {
                Ok(metrics) => {
                    let mut tasks_guard = tasks.lock().await;
                    if let Some(task) = tasks_guard.get_mut(&task_id) {
                        if profile.retain_history {
                            task.metrics_history.push(metrics.clone());
                            if task.metrics_history.len() > profile.history_size {
                                task.metrics_history.remove(0);
                            }
                        }

                        let aggregated = if !task.metrics_history.is_empty() {
                            Some(Self::calculate_aggregated(&task.metrics_history))
                        } else {
                            None
                        };

                        if profile.export_to_mqtt {
                            export_fn(metrics, aggregated);
                        }
                    }
                }
                Err(e) => {
                    warn!("Failed to collect metrics for task {}: {}", task_id, e);
                    let mut tasks_guard = tasks.lock().await;
                    tasks_guard.remove(&task_id);
                    break;
                }
            }
        }
    }

    fn calculate_aggregated(history: &[ProcessMetrics]) -> AggregatedMetrics {
        if history.is_empty() {
            return AggregatedMetrics {
                avg_cpu_usage: 0.0,
                max_cpu_usage: 0.0,
                avg_memory_usage: 0,
                max_memory_usage: 0,
                total_disk_read: 0,
                total_disk_write: 0,
                sample_count: 0,
            };
        }

        let count = history.len() as f64;
        let avg_cpu = history.iter().map(|s| s.cpu_percent).sum::<f64>() / count;
        let max_cpu = history.iter().map(|s| s.cpu_percent).fold(0.0, f64::max);
        let avg_mem = (history.iter().map(|s| s.memory_bytes).sum::<u64>() as f64 / count) as u64;
        let max_mem = history.iter().map(|s| s.memory_bytes).max().unwrap_or(0);

        let last = history.last().unwrap();
        let first = history.first().unwrap();

        AggregatedMetrics {
            avg_cpu_usage: avg_cpu,
            max_cpu_usage: max_cpu,
            avg_memory_usage: avg_mem,
            max_memory_usage: max_mem,
            total_disk_read: last.disk_read_bytes.saturating_sub(first.disk_read_bytes),
            total_disk_write: last.disk_write_bytes.saturating_sub(first.disk_write_bytes),
            sample_count: history.len(),
        }
    }
}

#[async_trait]
impl ProcessMonitor for SystemMonitor {
    async fn start_monitoring(&self, task_id: &str, profile: MonitoringProfile) -> Result<()> {
        if !profile.enabled {
            debug!("Monitoring disabled for task {}", task_id);
            return Ok(());
        }

        debug!("Starting monitoring for task {}", task_id);

        let mut tasks = self.tasks.lock().await;
        tasks.insert(
            task_id.to_string(),
            MonitoredTask {
                pid: None,
                profile,
                metrics_history: Vec::new(),
                start_time: SystemTime::now(),
            },
        );

        Ok(())
    }

    async fn stop_monitoring(&self, task_id: &str) -> Result<()> {
        debug!("Stopping monitoring for task {}", task_id);
        let mut tasks = self.tasks.lock().await;
        tasks.remove(task_id);
        Ok(())
    }
}
