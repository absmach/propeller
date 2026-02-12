pub mod host;
pub mod tee_runtime;
pub mod wasmtime_runtime;

use anyhow::Result;
use async_trait::async_trait;
use std::collections::HashMap;

#[derive(Clone, Debug)]
pub struct StartConfig {
    pub id: String,
    pub function_name: String,
    pub daemon: bool,
    pub wasm_binary: Vec<u8>,
    pub cli_args: Vec<String>,
    pub env: HashMap<String, String>,
    pub args: Vec<u64>,
    #[allow(dead_code)]
    pub mode: Option<String>,
}

#[async_trait]
pub trait Runtime: Send + Sync {
    async fn start_app(&self, ctx: RuntimeContext, config: StartConfig) -> Result<Vec<u8>>;

    async fn stop_app(&self, id: String) -> Result<()>;

    /// Returns the process ID for the task with the given id.
    ///
    /// For runtimes that spawn separate OS processes (e.g., HostRuntime),
    /// this returns the PID of the task's dedicated process.
    /// For in-process runtimes (e.g., WasmtimeRuntime), this returns
    /// the PID of the parent runtime process.
    ///
    /// Returns `None` if:
    /// - The task does not exist or is not running
    /// - The platform does not support PID retrieval
    async fn get_pid(&self, id: &str) -> Result<Option<u32>>;
}

#[derive(Clone)]
pub struct RuntimeContext {
    #[allow(dead_code)]
    pub proplet_id: String,
}

#[cfg(test)]
mod tests {
    use uuid::Uuid;

    use super::*;

    #[test]
    fn test_runtime_context_creation() {
        let id = Uuid::new_v4();
        let ctx = RuntimeContext {
            proplet_id: id.to_string(),
        };

        assert_eq!(ctx.proplet_id, id.to_string());
    }

    #[test]
    fn test_runtime_context_clone() {
        let id = Uuid::new_v4();
        let ctx1 = RuntimeContext {
            proplet_id: id.to_string(),
        };
        let ctx2 = ctx1.clone();

        assert_eq!(ctx1.proplet_id, ctx2.proplet_id);
    }
}
