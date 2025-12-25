pub mod metrics;
pub mod profiles;
pub mod system;

use crate::types::MonitoringProfile;
use anyhow::Result;
use async_trait::async_trait;

#[async_trait]
pub trait ProcessMonitor: Send + Sync {
    async fn start_monitoring(&self, task_id: &str, profile: MonitoringProfile) -> Result<()>;
    async fn stop_monitoring(&self, task_id: &str) -> Result<()>;
}
