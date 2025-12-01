use super::{Runtime, RuntimeContext};
use anyhow::{Context, Result};
use async_trait::async_trait;
use std::collections::HashMap;
use std::sync::Arc;
use tokio::sync::Mutex;
use tracing::{debug, error, info};
use uuid::Uuid;
use wasmtime::*;

pub struct WasmtimeRuntime {
    instances: Arc<Mutex<HashMap<Uuid, Arc<Mutex<WasmInstance>>>>>,
}

struct WasmInstance {
    engine: Engine,
    store: Store<()>,
    instance: Instance,
}

impl WasmtimeRuntime {
    pub fn new() -> Self {
        Self {
            instances: Arc::new(Mutex::new(HashMap::new())),
        }
    }
}

#[async_trait]
impl Runtime for WasmtimeRuntime {
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
    ) -> Result<Vec<u8>> {
        info!("Starting Wasmtime app: task_id={}, function={}", id, function_name);

        // Create engine with default configuration
        let engine = Engine::default();

        // Create store with empty context
        // Note: CLI args and env vars would require WASI support
        let mut store = Store::new(&engine, ());

        // Create module from binary
        let module = Module::from_binary(&engine, &wasm_binary)
            .context("Failed to create Wasmtime module from binary")?;

        // Create linker
        let linker = Linker::new(&engine);

        // Instantiate module
        let instance = linker
            .instantiate(&mut store, &module)
            .context("Failed to instantiate Wasmtime module")?;

        debug!("Module instantiated successfully");

        // Store instance for potential later cleanup
        let wasm_inst = Arc::new(Mutex::new(WasmInstance {
            engine: engine.clone(),
            store,
            instance: instance.clone(),
        }));

        {
            let mut instances = self.instances.lock().await;
            instances.insert(id, wasm_inst.clone());
        }

        // Execute the function
        let result = if daemon {
            // For daemon mode, spawn in background
            let instances = self.instances.clone();
            let func_name = function_name.clone();

            tokio::spawn(async move {
                // Wait a bit before executing (mimics Go implementation)
                tokio::time::sleep(std::time::Duration::from_secs(5)).await;

                let mut inst = wasm_inst.lock().await;
                if let Err(e) = execute_function(&mut inst, &func_name, args).await {
                    error!("Daemon task {} failed: {}", id, e);
                }

                // Remove from instances map
                instances.lock().await.remove(&id);
            });

            Ok(Vec::new())
        } else {
            // Execute synchronously
            let mut inst = wasm_inst.lock().await;
            let result = execute_function(&mut inst, &function_name, args).await;

            // Remove from instances map
            self.instances.lock().await.remove(&id);

            result
        };

        result
    }

    async fn stop_app(&self, id: Uuid) -> Result<()> {
        info!("Stopping Wasmtime app: task_id={}", id);

        let mut instances = self.instances.lock().await;
        if instances.remove(&id).is_some() {
            debug!("Task {} stopped and removed", id);
            Ok(())
        } else {
            Err(anyhow::anyhow!("Task {} not found", id))
        }
    }
}

async fn execute_function(
    inst: &mut WasmInstance,
    function_name: &str,
    args: Vec<f64>,
) -> Result<Vec<u8>> {
    // Get the exported function
    let func = inst
        .instance
        .get_func(&mut inst.store, function_name)
        .context(format!("Function '{}' not found in module", function_name))?;

    // Convert f64 args to wasmtime values (as f64)
    let wasm_args: Vec<Val> = args.into_iter().map(|v| Val::F64(v.to_bits())).collect();

    // Call the function
    let mut results = vec![Val::I32(0)];
    func.call(&mut inst.store, &wasm_args, &mut results)
        .context(format!("Failed to call function '{}'", function_name))?;

    // Convert result to bytes
    let result_bytes = match &results[0] {
        Val::I32(v) => v.to_le_bytes().to_vec(),
        Val::I64(v) => v.to_le_bytes().to_vec(),
        Val::F32(v) => v.to_le_bytes().to_vec(),
        Val::F64(v) => v.to_le_bytes().to_vec(),
        _ => Vec::new(),
    };

    debug!("Function executed successfully, result size: {} bytes", result_bytes.len());

    Ok(result_bytes)
}
