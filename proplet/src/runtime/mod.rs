pub mod host;
pub mod wasmtime_runtime;

use anyhow::Result;
use async_trait::async_trait;
use std::collections::HashMap;
use uuid::Uuid;

#[derive(Clone, Debug)]
pub struct StartConfig {
    pub id: String,
    pub function_name: String,
    pub daemon: bool,
    pub wasm_binary: Vec<u8>,
    pub cli_args: Vec<String>,
    pub env: HashMap<String, String>,
    pub args: Vec<u64>,
}

#[async_trait]
pub trait Runtime: Send + Sync {
    async fn start_app(&self, ctx: RuntimeContext, config: StartConfig) -> Result<Vec<u8>>;

    async fn stop_app(&self, id: String) -> Result<()>;
}

#[derive(Clone)]
pub struct RuntimeContext {
    #[allow(dead_code)]
    pub proplet_id: Uuid,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_runtime_context_creation() {
        let id = Uuid::new_v4();
        let ctx = RuntimeContext { proplet_id: id };

        assert_eq!(ctx.proplet_id, id);
    }

    #[test]
    fn test_runtime_context_clone() {
        let id = Uuid::new_v4();
        let ctx1 = RuntimeContext { proplet_id: id };
        let ctx2 = ctx1.clone();

        assert_eq!(ctx1.proplet_id, ctx2.proplet_id);
    }
}
