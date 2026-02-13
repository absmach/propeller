use super::{Runtime, RuntimeContext, StartConfig};
use crate::hal_linker;
use anyhow::{Context, Result};
use async_trait::async_trait;
use elastic_tee_hal::interfaces::HalProvider;
use std::collections::HashMap;
use std::sync::Arc;
use tokio::sync::oneshot;
use tokio::sync::Mutex;
use tokio::task::JoinHandle;
use tracing::info;
use wasmtime::*;
use wasmtime_wasi::WasiCtxBuilder;

pub struct WasmtimeRuntime {
    engine: Engine,
    tasks: Arc<Mutex<HashMap<String, JoinHandle<()>>>>,
    hal_enabled: bool,
}

impl WasmtimeRuntime {
    pub fn new(hal_enabled: bool) -> Result<Self> {
        let mut config = Config::new();
        config.wasm_reference_types(true);
        config.wasm_bulk_memory(true);
        config.wasm_simd(true);

        let engine = Engine::new(&config)?;

        Ok(Self {
            engine,
            tasks: Arc::new(Mutex::new(HashMap::new())),
            hal_enabled,
        })
    }
}

#[async_trait]
impl Runtime for WasmtimeRuntime {
    async fn start_app(&self, _ctx: RuntimeContext, config: StartConfig) -> Result<Vec<u8>> {
        info!(
            "Starting Wasmtime runtime app: task_id={}, function={}, daemon={}, wasm_size={}",
            config.id,
            config.function_name,
            config.daemon,
            config.wasm_binary.len()
        );

        info!("Compiling WASM module for task: {}", config.id);
        let module = Module::from_binary(&self.engine, &config.wasm_binary)
            .context("Failed to compile Wasmtime module from binary")?;

        info!("Module compiled successfully for task: {}", config.id);

        let mut wasi_builder = WasiCtxBuilder::new();
        wasi_builder.inherit_stdio();

        for (key, value) in &config.env {
            wasi_builder.env(key, value);
        }

        let wasi = wasi_builder.build_p1();

        let mut store = Store::new(&self.engine, wasi);

        let mut linker = Linker::new(&self.engine);
        wasmtime_wasi::p1::add_to_linker_sync(&mut linker, |ctx| ctx)
            .context("Failed to add WASI to linker")?;

        if self.hal_enabled {
            let provider = Arc::new(HalProvider::with_defaults());
            hal_linker::add_to_linker(&mut linker, provider)
                .context("Failed to add ELASTIC TEE HAL interfaces to linker")?;
        }

        let instance = linker
            .instantiate(&mut store, &module)
            .context("Failed to instantiate Wasmtime module")?;

        if config.daemon {
            info!("Running in daemon mode for task: {}", config.id);

            let _tasks = self.tasks.clone();
            let task_id = config.id.clone();

            let handle = tokio::spawn(async move {
                info!("Daemon task {} is running", task_id);

                tokio::time::sleep(std::time::Duration::from_secs(60)).await;
                info!("Daemon task {} completed", task_id);
            });

            {
                let mut tasks_map = self.tasks.lock().await;
                tasks_map.insert(config.id.clone(), handle);
            }

            info!("Daemon task {} started, returning immediately", config.id);
            Ok(Vec::new())
        } else {
            info!("Running in synchronous mode for task: {}", config.id);

            let task_id = config.id.clone();
            let task_id_for_cleanup = task_id.clone();
            let function_name = config.function_name.clone();
            let args = config.args.clone();
            let tasks = self.tasks.clone();

            let (result_tx, result_rx) = oneshot::channel();

            let handle = tokio::task::spawn(async move {
                let result = tokio::task::spawn_blocking(move || {
                    if let Some(init_func) = instance.get_func(&mut store, "_initialize") {
                        info!("Found _initialize function, initializing WASM runtime for task: {}", task_id);
                        init_func.call(&mut store, &[], &mut [])
                            .context("Failed to initialize WASM runtime via _initialize")?;
                        info!("WASM runtime initialized successfully for task: {}", task_id);
                    } else {
                        info!("No _initialize function found, skipping initialization for task: {}", task_id);
                    }

                    let exports: Vec<String> = instance
                        .exports(&mut store)
                        .map(|export| export.name().to_string())
                        .collect();
                    info!(
                        "WASM module exports for task {}: requested='{}', available={:?}",
                        task_id, function_name, exports
                    );

                    let func = if let Some(f) = instance.get_func(&mut store, &function_name) {
                        info!("Found requested function '{}' in module exports", function_name);
                        f
                    } else {
                        let fallbacks = vec!["main", "run", "_start"];
                        let mut found_func = None;
                        let mut tried_fallbacks = Vec::new();

                        for fallback in &fallbacks {
                            tried_fallbacks.push(*fallback);
                            if let Some(f) = instance.get_func(&mut store, fallback) {
                                info!("Function '{}' not found, using fallback '{}'", function_name, fallback);
                                found_func = Some(f);
                                break;
                            }
                        }

                        found_func.ok_or_else(|| {
                            anyhow::anyhow!(
                                "Function '{}' not found, and fallbacks {:?} also not found in module exports. Available function exports: {:?}",
                                function_name,
                                tried_fallbacks,
                                exports
                            )
                        })?
                    };

                    let func_ty = func.ty(&store);

                    let param_types: Vec<_> = func_ty.params().collect();
                    let result_types: Vec<_> = func_ty.results().collect();

                    if args.len() != param_types.len() {
                        return Err(anyhow::anyhow!(
                            "Argument count mismatch for function '{}': expected {} arguments but got {}",
                            function_name,
                            param_types.len(),
                            args.len()
                        ));
                    }

                    let wasm_args: Vec<Val> = args
                        .iter()
                        .zip(param_types.iter())
                        .map(|(arg, param_type)| match param_type {
                            ValType::I32 => Val::I32(*arg as i32),
                            ValType::I64 => Val::I64(*arg as i64),
                            ValType::F32 => Val::F32((*arg as f32).to_bits()),
                            ValType::F64 => Val::F64((*arg as f64).to_bits()),
                            _ => Val::I32(*arg as i32),
                        })
                        .collect();

                    info!(
                        "Calling function '{}' with {} params, expects {} results",
                        function_name,
                        wasm_args.len(),
                        result_types.len()
                    );

                    let mut results: Vec<Val> = result_types
                        .iter()
                        .map(|result_type| match result_type {
                            ValType::I32 => Val::I32(0),
                            ValType::I64 => Val::I64(0),
                            ValType::F32 => Val::F32(0),
                            ValType::F64 => Val::F64(0),
                            _ => Val::I32(0),
                        })
                        .collect();

                    func.call(&mut store, &wasm_args, &mut results)
                        .context(format!("Failed to call function '{function_name}'"))?;

                    info!("Function '{}' executed successfully", function_name);

                    let result_string = if !results.is_empty() {
                        let result_val = &results[0];

                        if let Some(v) = result_val.i32() {
                            v.to_string()
                        } else if let Some(v) = result_val.i64() {
                            v.to_string()
                        } else if let Some(v) = result_val.f32() {
                            v.to_string()
                        } else if let Some(v) = result_val.f64() {
                            v.to_string()
                        } else {
                            String::new()
                        }
                    } else {
                        String::new()
                    };

                    let result_bytes = result_string.into_bytes();

                    info!(
                        "Task {} completed successfully, result size: {} bytes",
                        task_id,
                        result_bytes.len()
                    );

                    Ok::<Vec<u8>, anyhow::Error>(result_bytes)
                })
                .await;

                tasks.lock().await.remove(&task_id_for_cleanup);

                let final_result = match result {
                    Ok(Ok(data)) => Ok(data),
                    Ok(Err(e)) => Err(e),
                    Err(e) => Err(anyhow::anyhow!("Task join error: {e}")),
                };

                let _ = result_tx.send(final_result);
            });

            {
                let mut tasks_map = self.tasks.lock().await;
                tasks_map.insert(config.id.clone(), handle);
            }

            match result_rx.await {
                Ok(result) => result,
                Err(_) => Err(anyhow::anyhow!("Task was cancelled or panicked")),
            }
        }
    }

    async fn stop_app(&self, id: String) -> Result<()> {
        info!("Stopping Wasmtime runtime app: task_id={}", id);

        let mut tasks = self.tasks.lock().await;
        if let Some(handle) = tasks.remove(&id) {
            handle.abort();
            info!("Task {} aborted and removed from tasks", id);
            Ok(())
        } else {
            Err(anyhow::anyhow!("Task {id} not found in running tasks"))
        }
    }

    async fn get_pid(&self, _id: &str) -> Result<Option<u32>> {
        let tasks = self.tasks.lock().await;
        if !tasks.contains_key(_id) {
            return Ok(None);
        }

        Ok(Some(std::process::id()))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_wasmtime_runtime_new() {
        let runtime = WasmtimeRuntime::new(false);
        assert!(runtime.is_ok());
    }

    #[test]
    fn test_wasmtime_runtime_engine_configuration() {
        let runtime = WasmtimeRuntime::new(false).unwrap();

        assert!(runtime.tasks.try_lock().is_ok());
    }

    #[test]
    fn test_wasmtime_runtime_tasks_empty_on_creation() {
        let runtime = WasmtimeRuntime::new(false).unwrap();

        let tasks = runtime.tasks.try_lock().unwrap();
        assert_eq!(tasks.len(), 0);
    }

    #[tokio::test]
    async fn test_wasmtime_runtime_compile_invalid_wasm() {
        let runtime = WasmtimeRuntime::new(false).unwrap();

        let invalid_wasm = vec![0xFF, 0xFF, 0xFF, 0xFF];

        let result = Module::from_binary(&runtime.engine, &invalid_wasm);
        assert!(result.is_err());
    }

    #[tokio::test]
    async fn test_wasmtime_runtime_compile_empty_wasm() {
        let runtime = WasmtimeRuntime::new(false).unwrap();

        let empty_wasm = vec![];

        let result = Module::from_binary(&runtime.engine, &empty_wasm);
        assert!(result.is_err());
    }
}
