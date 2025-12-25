use crate::types::MonitoringProfile;
use std::time::Duration;

impl MonitoringProfile {
    pub fn standard() -> Self {
        Self {
            enabled: true,
            interval: Duration::from_secs(10),
            collect_cpu: true,
            collect_memory: true,
            collect_disk_io: true,
            collect_threads: true,
            collect_file_descriptors: true,
            export_to_mqtt: true,
            retain_history: true,
            history_size: 100,
        }
    }

    pub fn long_running_daemon() -> Self {
        Self {
            enabled: true,
            interval: Duration::from_secs(120),
            collect_cpu: true,
            collect_memory: true,
            collect_disk_io: true,
            collect_threads: true,
            collect_file_descriptors: true,
            export_to_mqtt: true,
            retain_history: true,
            history_size: 500,
        }
    }
}
