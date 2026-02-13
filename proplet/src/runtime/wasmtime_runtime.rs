use super::{Runtime, RuntimeContext, StartConfig};
use crate::hal_linker;
use anyhow::{Context, Result};
use async_trait::async_trait;
use elastic_tee_hal::interfaces::HalProvider;
use hyper::server::conn::http1;
use socket2::{Domain, Protocol, Socket, Type};
use std::collections::HashMap;
use std::net::SocketAddr;
use std::sync::Arc;
use tokio::net::TcpListener;
use tokio::sync::oneshot;
use tokio::sync::watch;
use tokio::sync::Mutex;
use tokio::task::JoinHandle;
use tracing::{error, info, warn};
use wasmtime::component::ResourceTable;
use wasmtime::*;
use wasmtime_wasi::p2::bindings::sync::Command;
use wasmtime_wasi::{DirPerms, FilePerms, WasiCtx, WasiCtxBuilder, WasiCtxView, WasiView};
use wasmtime_wasi_http::bindings::http::types::Scheme;
use wasmtime_wasi_http::bindings::ProxyPre;
use wasmtime_wasi_http::body::HyperOutgoingBody;
use wasmtime_wasi_http::io::TokioIo;
use wasmtime_wasi_http::{WasiHttpCtx, WasiHttpView};

fn is_wasm_component(bytes: &[u8]) -> bool {
    bytes.len() >= 8 && bytes[0..4] == [0x00, 0x61, 0x73, 0x6d] && bytes[4] == 0x0d
}

fn is_proxy_component(bytes: &[u8]) -> bool {
    bytes
        .windows(b"wasi:http/incoming-handler".len())
        .any(|w| w == b"wasi:http/incoming-handler")
}

pub struct StoreData {
    wasi: WasiCtx,
    http: WasiHttpCtx,
    table: ResourceTable,
}

impl WasiView for StoreData {
    fn ctx(&mut self) -> WasiCtxView<'_> {
        WasiCtxView {
            ctx: &mut self.wasi,
            table: &mut self.table,
        }
    }
}

impl WasiHttpView for StoreData {
    fn ctx(&mut self) -> &mut WasiHttpCtx {
        &mut self.http
    }
    fn table(&mut self) -> &mut ResourceTable {
        &mut self.table
    }
}

pub struct WasmtimeRuntime {
    engine: Engine,
    async_engine: Engine,
    tasks: Arc<Mutex<HashMap<String, JoinHandle<()>>>>,
    proxy_ports: Arc<Mutex<HashMap<u16, String>>>,
    proxy_cancellers: Arc<Mutex<HashMap<String, watch::Sender<bool>>>>,
    hal_enabled: bool,
    http_enabled: bool,
    preopened_dirs: Vec<String>,
    proxy_port: u16,
}

impl WasmtimeRuntime {
    pub fn new_with_options(
        hal_enabled: bool,
        http_enabled: bool,
        preopened_dirs: Vec<String>,
        proxy_port: u16,
    ) -> Result<Self> {
        let mut sync_config = Config::new();
        sync_config.wasm_reference_types(true);
        sync_config.wasm_bulk_memory(true);
        sync_config.wasm_simd(true);
        sync_config.wasm_component_model(true);

        let mut async_config = Config::new();
        async_config.wasm_component_model(true);
        async_config.async_support(true);

        let engine = Engine::new(&sync_config)?;
        let async_engine = Engine::new(&async_config)?;

        Ok(Self {
            engine,
            async_engine,
            tasks: Arc::new(Mutex::new(HashMap::new())),
            proxy_ports: Arc::new(Mutex::new(HashMap::new())),
            proxy_cancellers: Arc::new(Mutex::new(HashMap::new())),
            hal_enabled,
            http_enabled,
            preopened_dirs,
            proxy_port,
        })
    }
}

#[async_trait]
impl Runtime for WasmtimeRuntime {
    async fn start_app(&self, _ctx: RuntimeContext, config: StartConfig) -> Result<Vec<u8>> {
        let is_component = is_wasm_component(&config.wasm_binary);
        let is_proxy = is_component && is_proxy_component(&config.wasm_binary);
        info!(
            "Starting Wasmtime runtime app: task_id={}, function={}, daemon={}, wasm_size={}, is_component={}, is_proxy={}",
            config.id,
            config.function_name,
            config.daemon,
            config.wasm_binary.len(),
            is_component,
            is_proxy,
        );

        if is_proxy {
            self.start_app_proxy(config).await
        } else if is_component {
            self.start_app_component(config).await
        } else {
            self.start_app_core(config).await
        }
    }

    async fn stop_app(&self, id: String) -> Result<()> {
        info!("Stopping Wasmtime runtime app: task_id={}", id);

        self.proxy_ports
            .lock()
            .await
            .retain(|_port, tid| tid != &id);

        // Signal cancellation for proxy tasks
        let mut cancellers = self.proxy_cancellers.lock().await;
        if let Some(canceller) = cancellers.remove(&id) {
            let _ = canceller.send(true);
        }
        drop(cancellers);

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

impl WasmtimeRuntime {
    async fn start_app_core(&self, config: StartConfig) -> Result<Vec<u8>> {
        info!("Compiling WASM core module for task: {}", config.id);
        let module = Module::from_binary(&self.engine, &config.wasm_binary)
            .context("Failed to compile Wasmtime module from binary")?;

        info!("Module compiled successfully for task: {}", config.id);

        let mut wasi_builder = wasmtime_wasi::WasiCtxBuilder::new();
        wasi_builder.inherit_stdio();

        for (key, value) in &config.env {
            wasi_builder.env(key, value);
        }

        for dir in &self.preopened_dirs {
            wasi_builder
                .preopened_dir(dir, dir, DirPerms::all(), FilePerms::all())
                .with_context(|| format!("Failed to preopen directory '{dir}'"))?;
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

        self.run_core_instance(config, store, instance).await
    }

    async fn start_app_component(&self, config: StartConfig) -> Result<Vec<u8>> {
        info!(
            "Compiling WASM P2 command component for task: {}",
            config.id
        );
        let component = component::Component::from_binary(&self.engine, &config.wasm_binary)
            .context("Failed to compile WASM component from binary")?;

        info!("Component compiled successfully for task: {}", config.id);

        let mut wasi_builder = WasiCtxBuilder::new();
        wasi_builder.inherit_stdio();

        for (key, value) in &config.env {
            wasi_builder.env(key, value);
        }

        for dir in &self.preopened_dirs {
            wasi_builder
                .preopened_dir(dir, dir, DirPerms::all(), FilePerms::all())
                .with_context(|| format!("Failed to preopen directory '{dir}'"))?;
        }

        let wasi = wasi_builder.build();

        let store_data = StoreData {
            wasi,
            http: WasiHttpCtx::new(),
            table: ResourceTable::new(),
        };

        let mut store = Store::new(&self.engine, store_data);

        let mut linker: component::Linker<StoreData> = component::Linker::new(&self.engine);
        wasmtime_wasi::p2::add_to_linker_sync(&mut linker)
            .context("Failed to add WASI P2 to component linker")?;
        wasmtime_wasi_http::add_only_http_to_linker_sync(&mut linker)
            .context("Failed to add wasi:http to component linker")?;

        let task_id = config.id.clone();
        let task_id_for_cleanup = task_id.clone();
        let tasks = self.tasks.clone();

        let (result_tx, result_rx) = oneshot::channel();

        let handle = tokio::task::spawn(async move {
            let result = tokio::task::spawn_blocking(move || {
                let command = Command::instantiate(&mut store, &component, &linker)
                    .context("Failed to instantiate WASM command component")?;

                let program_result = command
                    .wasi_cli_run()
                    .call_run(&mut store)
                    .context("Failed to call wasi:cli/run on component")?;

                match program_result {
                    Ok(()) => {
                        info!("Task {} completed successfully", task_id);
                        Ok::<Vec<u8>, anyhow::Error>(Vec::new())
                    }
                    Err(()) => Err(anyhow::anyhow!(
                        "Component for task {} exited with error",
                        task_id
                    )),
                }
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

    async fn start_app_proxy(&self, config: StartConfig) -> Result<Vec<u8>> {
        if !self.http_enabled {
            return Err(anyhow::anyhow!(
                "HTTP proxy is disabled. Enable it by setting http_enabled=true in configuration"
            ));
        }

        let port = config
            .cli_args
            .windows(2)
            .find(|w| w[0] == "--port")
            .and_then(|w| w[1].parse().ok())
            .unwrap_or(self.proxy_port);
        let addr: SocketAddr = ([0, 0, 0, 0], port).into();
        info!(
            "Starting HTTP proxy server for task {} on {addr}",
            config.id
        );

        let component = component::Component::from_binary(&self.async_engine, &config.wasm_binary)
            .context("Failed to compile WASM proxy component")?;

        let mut linker: component::Linker<StoreData> = component::Linker::new(&self.async_engine);
        wasmtime_wasi::p2::add_to_linker_async(&mut linker)
            .context("Failed to add WASI P2 async to proxy linker")?;
        wasmtime_wasi_http::add_only_http_to_linker_async(&mut linker)
            .context("Failed to add wasi:http async to proxy linker")?;

        let pre = Arc::new(
            ProxyPre::new(linker.instantiate_pre(&component)?)
                .context("Failed to create ProxyPre")?,
        );

        let env: Arc<Vec<(String, String)>> = Arc::new(config.env.into_iter().collect());
        let preopened_dirs = self.preopened_dirs.clone();

        let listener = {
            let mut proxy_ports = self.proxy_ports.lock().await;

            if let Some(old_task_id) = proxy_ports.remove(&port) {
                warn!(
                    "Port {port} already bound by task {old_task_id} — aborting it to start task {}",
                    config.id
                );
                let mut tasks_map = self.tasks.lock().await;
                if let Some(handle) = tasks_map.remove(&old_task_id) {
                    handle.abort();
                    drop(tasks_map);

                    let mut attempts = 0;
                    loop {
                        tokio::time::sleep(std::time::Duration::from_millis(50)).await;
                        if let Ok(test_socket) =
                            Socket::new(Domain::IPV4, Type::STREAM, Some(Protocol::TCP))
                        {
                            if test_socket.bind(&addr.into()).is_ok() {
                                break;
                            }
                        }
                        attempts += 1;
                        if attempts > 20 {
                            return Err(anyhow::anyhow!(
                                "Port {port} still in use after aborting task {old_task_id}"
                            ));
                        }
                    }
                }
            }

            let socket = Socket::new(Domain::IPV4, Type::STREAM, Some(Protocol::TCP))?;
            socket.set_reuse_address(true)?;
            socket.set_nonblocking(true)?;
            socket
                .bind(&addr.into())
                .with_context(|| format!("Failed to bind HTTP proxy port {addr}"))?;
            socket.listen(128)?;
            let listener = TcpListener::from_std(std::net::TcpListener::from(socket))?;

            proxy_ports.insert(port, config.id.clone());

            listener
        };
        info!("HTTP proxy listening on {addr} for task {}", config.id);

        let task_id = config.id.clone();
        let tasks = self.tasks.clone();
        let proxy_cancellers = self.proxy_cancellers.clone();

        let (cancel_tx, mut cancel_rx) = watch::channel(false);
        proxy_cancellers
            .lock()
            .await
            .insert(config.id.clone(), cancel_tx);

        let handle = tokio::spawn(async move {
            loop {
                if *cancel_rx.borrow() {
                    break;
                }

                let accept_result = tokio::select! {
                    result = listener.accept() => result,
                    _ = cancel_rx.changed() => {
                        if *cancel_rx.borrow() {
                            break;
                        }
                        continue;
                    }
                };

                let (stream, peer) = match accept_result {
                    Ok(v) => v,
                    Err(e) => {
                        error!("HTTP proxy accept error for task {task_id}: {e}");
                        continue;
                    }
                };

                let pre = pre.clone();
                let env = env.clone();
                let dirs = preopened_dirs.clone();
                let task_id_conn = task_id.clone();
                let cancel_rx_conn = cancel_rx.clone();

                tokio::spawn(async move {
                    if *cancel_rx_conn.borrow() {
                        return;
                    }

                    info!("HTTP proxy handling connection from {peer} for task {task_id_conn}");

                    let task_id_warn = task_id_conn.clone();
                    if let Err(e) = http1::Builder::new()
                        .keep_alive(true)
                        .serve_connection(
                            TokioIo::new(stream),
                            hyper::service::service_fn(move |req| {
                                let pre = pre.clone();
                                let env = env.clone();
                                let dirs = dirs.clone();
                                let task_id_req = task_id_conn.clone();
                                async move {
                                    handle_proxy_request(pre, env, dirs, req, task_id_req).await
                                }
                            }),
                        )
                        .await
                    {
                        warn!(
                            "HTTP proxy connection error for task {task_id_warn} from {peer}: {e}"
                        );
                    }
                });
            }
        });

        tasks.lock().await.insert(config.id.clone(), handle);

        Ok(Vec::new())
    }

    async fn run_core_instance(
        &self,
        config: StartConfig,
        mut store: Store<wasmtime_wasi::p1::WasiP1Ctx>,
        instance: Instance,
    ) -> Result<Vec<u8>> {
        if config.daemon {
            info!("Running in daemon mode for task: {}", config.id);

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
                        info!(
                            "Found _initialize function, initializing WASM runtime for task: {}",
                            task_id
                        );
                        init_func
                            .call(&mut store, &[], &mut [])
                            .context("Failed to initialize WASM runtime via _initialize")?;
                        info!(
                            "WASM runtime initialized successfully for task: {}",
                            task_id
                        );
                    } else {
                        info!(
                            "No _initialize function found, skipping initialization for task: {}",
                            task_id
                        );
                    }

                    let exports: Vec<String> = instance
                        .exports(&mut store)
                        .map(|export: Export<'_>| export.name().to_string())
                        .collect();
                    info!(
                        "WASM module exports for task {}: requested='{}', available={:?}",
                        task_id, function_name, exports
                    );

                    let func = if let Some(f) = instance.get_func(&mut store, &function_name) {
                        info!(
                            "Found requested function '{}' in module exports",
                            function_name
                        );
                        f
                    } else {
                        let fallbacks = vec!["main", "run", "_start"];
                        let mut found_func = None;
                        let mut tried_fallbacks = Vec::new();

                        for fallback in &fallbacks {
                            tried_fallbacks.push(*fallback);
                            if let Some(f) = instance.get_func(&mut store, fallback) {
                                info!(
                                    "Function '{}' not found, using fallback '{}'",
                                    function_name, fallback
                                );
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
}

/// Handle one HTTP request by instantiating the proxy component and calling
/// `wasi:http/incoming-handler.handle`.
async fn handle_proxy_request(
    pre: Arc<ProxyPre<StoreData>>,
    env: Arc<Vec<(String, String)>>,
    preopened_dirs: Vec<String>,
    req: hyper::Request<hyper::body::Incoming>,
    task_id: String,
) -> Result<hyper::Response<HyperOutgoingBody>> {
    let mut wasi_builder = WasiCtxBuilder::new();
    wasi_builder.inherit_stdio();
    for (k, v) in env.iter() {
        wasi_builder.env(k, v);
    }
    for dir in &preopened_dirs {
        if let Err(e) = wasi_builder.preopened_dir(dir, dir, DirPerms::all(), FilePerms::all()) {
            warn!("Proxy request {task_id}: failed to preopen '{dir}': {e}");
        }
    }

    let store_data = StoreData {
        wasi: wasi_builder.build(),
        http: WasiHttpCtx::new(),
        table: ResourceTable::new(),
    };

    let mut store = Store::new(pre.engine(), store_data);

    let (sender, receiver) = oneshot::channel();
    let incoming = store
        .data_mut()
        .new_incoming_request(Scheme::Http, req)
        .context("Failed to create incoming request")?;
    let outparam = store
        .data_mut()
        .new_response_outparam(sender)
        .context("Failed to create response outparam")?;

    let task = tokio::task::spawn(async move {
        let proxy = pre
            .instantiate_async(&mut store)
            .await
            .context("Failed to instantiate proxy component")?;
        proxy
            .wasi_http_incoming_handler()
            .call_handle(&mut store, incoming, outparam)
            .await
            .context("Failed to call incoming-handler")?;
        Ok::<_, anyhow::Error>(())
    });

    match receiver.await {
        Ok(Ok(resp)) => Ok(resp),
        Ok(Err(e)) => Err(e.into()),
        Err(_) => match task.await {
            Ok(Ok(())) => anyhow::bail!(
                "proxy component for task {task_id} never called response-outparam::set"
            ),
            Ok(Err(e)) => Err(e),
            Err(e) => Err(anyhow::anyhow!("proxy task join error: {e}")),
        },
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_wasmtime_runtime_new() {
        let runtime = WasmtimeRuntime::new_with_options(false, false, Vec::new(), 8222);
        assert!(runtime.is_ok());
    }

    #[test]
    fn test_wasmtime_runtime_new_with_http() {
        let runtime = WasmtimeRuntime::new_with_options(false, true, Vec::new(), 8222);
        assert!(runtime.is_ok());
    }

    #[test]
    fn test_wasmtime_runtime_engine_configuration() {
        let runtime = WasmtimeRuntime::new_with_options(false, false, Vec::new(), 8222).unwrap();
        assert!(runtime.tasks.try_lock().is_ok());
    }

    #[test]
    fn test_wasmtime_runtime_tasks_empty_on_creation() {
        let runtime = WasmtimeRuntime::new_with_options(false, false, Vec::new(), 8222).unwrap();
        let tasks = runtime.tasks.try_lock().unwrap();
        assert_eq!(tasks.len(), 0);
    }

    #[tokio::test]
    async fn test_wasmtime_runtime_compile_invalid_wasm() {
        let runtime = WasmtimeRuntime::new_with_options(false, false, Vec::new(), 8222).unwrap();
        let invalid_wasm = vec![0xFF, 0xFF, 0xFF, 0xFF];
        let result = Module::from_binary(&runtime.engine, &invalid_wasm);
        assert!(result.is_err());
    }

    #[tokio::test]
    async fn test_wasmtime_runtime_compile_empty_wasm() {
        let runtime = WasmtimeRuntime::new_with_options(false, false, Vec::new(), 8222).unwrap();
        let empty_wasm = vec![];
        let result = Module::from_binary(&runtime.engine, &empty_wasm);
        assert!(result.is_err());
    }
}
