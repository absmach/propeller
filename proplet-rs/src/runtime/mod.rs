pub mod host;
pub mod wasmtime_runtime;

use anyhow::Result;
use async_trait::async_trait;
use std::collections::HashMap;
use uuid::Uuid;

#[async_trait]
pub trait Runtime: Send + Sync {
    async fn start_app(
        &self,
        ctx: RuntimeContext,
        wasm_binary: Vec<u8>,
        cli_args: Vec<String>,
        id: Uuid,
        function_name: String,
        daemon: bool,
        env: HashMap<String, String>,
        args: Vec<f64>,
    ) -> Result<Vec<u8>>;

    async fn stop_app(&self, id: Uuid) -> Result<()>;
}

#[derive(Clone)]
pub struct RuntimeContext {
    pub proplet_id: Uuid,
}
