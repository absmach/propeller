use super::{Runtime, RuntimeContext, StartConfig};
use crate::hal::PropletHal;
use crate::hal_component;
use anyhow::{Context, Result};
use async_trait::async_trait;
use elastic_tee_hal::StorageInterface;
use http_body_util::BodyExt;
use hyper::server::conn::http1;
use rustls::client::danger::{HandshakeSignatureValid, ServerCertVerified, ServerCertVerifier};
use rustls::pki_types::{CertificateDer, ServerName, UnixTime};
use rustls::{DigitallySignedStruct, RootCertStore, SignatureScheme};
use socket2::{Domain, Protocol, Socket, Type};
use std::collections::HashMap;
use std::net::SocketAddr;
use std::path::PathBuf;
use std::sync::Arc;
use tokio::net::TcpListener;
use tokio::sync::oneshot;
use tokio::sync::watch;
use tokio::sync::Mutex;
use tokio::task::JoinHandle;
use tokio::time::timeout;
use tracing::{error, info, warn};
use wasm_wave;
use wasmtime::component::ResourceTable;
use wasmtime::*;
use wasmtime_wasi::p2::bindings::Command;
use wasmtime_wasi::{DirPerms, FilePerms, WasiCtx, WasiCtxBuilder, WasiCtxView, WasiView};
use wasmtime_wasi_http::io::TokioIo;
use wasmtime_wasi_http::p2::bindings::http::types::Scheme;
use wasmtime_wasi_http::p2::bindings::ProxyPre;
use wasmtime_wasi_http::p2::body::HyperOutgoingBody;
use wasmtime_wasi_http::p2::types::{HostFutureIncomingResponse, OutgoingRequestConfig};
use wasmtime_wasi_http::p2::{
    default_send_request, hyper_request_error, WasiHttpHooks, WasiHttpView,
};
use wasmtime_wasi_http::WasiHttpCtx;
use wasmtime_wasi_usb::{WasiUsbCtx, WasiUsbCtxView, WasiUsbView};

fn is_wasm_component(bytes: &[u8]) -> bool {
    bytes.len() >= 8 && bytes[0..4] == [0x00, 0x61, 0x73, 0x6d] && bytes[4] == 0x0d
}

fn is_proxy_component(bytes: &[u8]) -> bool {
    bytes
        .windows(b"wasi:http/incoming-handler".len())
        .any(|w| w == b"wasi:http/incoming-handler")
}

/// Resolve a component export by name. Supports three shapes so the same
/// `function_name` field on `StartConfig` can target both top-level commands
/// and per-interface instance exports (e.g. the BRT EUCNC demo's
/// `elastic:brt-eucnc-front-service/server-api@0.1.0#start-server`):
///
/// 1. Bare `"start-server"` — top-level lookup, then a fallback scan over
///    instance-style exports discovered in the binary that expose a function
///    with that name.
/// 2. Qualified `"instance#func"` — look up `instance` first, then `func`
///    inside it.
/// 3. The qualified form without `#` is also accepted (treated like a
///    top-level name).
fn resolve_component_export(
    instance: &component::Instance,
    store: &mut Store<StoreData>,
    name: &str,
    wasm_binary: &[u8],
) -> Option<component::Func> {
    if let Some((instance_name, func_name)) = name.split_once('#') {
        let inst_idx = instance.get_export_index(&mut *store, None, instance_name)?;
        let func_idx = instance.get_export_index(&mut *store, Some(&inst_idx), func_name)?;
        return instance.get_func(&mut *store, func_idx);
    }

    if let Some(func) = instance.get_func(&mut *store, name) {
        return Some(func);
    }

    for inst_name in scan_export_instance_names(wasm_binary) {
        let Some(inst_idx) = instance.get_export_index(&mut *store, None, &inst_name) else {
            continue;
        };
        if let Some(func_idx) = instance.get_export_index(&mut *store, Some(&inst_idx), name) {
            if let Some(func) = instance.get_func(&mut *store, func_idx) {
                return Some(func);
            }
        }
    }

    None
}

/// Best-effort scan for component instance export names embedded in the
/// component string table (`namespace:pkg/iface@x.y.z`). Used as a hint for
/// the fallback path in [`resolve_component_export`] when the guest only
/// gives us a bare function name; the linker remains the source of truth.
fn scan_export_instance_names(bytes: &[u8]) -> Vec<String> {
    fn is_id_byte(b: u8) -> bool {
        b.is_ascii_alphanumeric() || matches!(b, b':' | b'/' | b'-' | b'@' | b'.' | b'_')
    }

    let mut names = Vec::new();
    let mut seen = std::collections::HashSet::new();
    let candidate_prefixes: &[&[u8]] = &[b"elastic:", b"wasi:"];
    for prefix in candidate_prefixes {
        let mut i = 0;
        while i + prefix.len() <= bytes.len() {
            let Some(off) = bytes[i..].windows(prefix.len()).position(|w| w == *prefix) else {
                break;
            };
            let start = i + off;
            let mut end = start;
            while end < bytes.len() && end - start < 128 && is_id_byte(bytes[end]) {
                end += 1;
            }
            if end > start {
                if let Ok(s) = std::str::from_utf8(&bytes[start..end]) {
                    if s.contains('/') && seen.insert(s.to_string()) {
                        names.push(s.to_string());
                    }
                }
            }
            i = end.max(start + 1);
        }
    }
    names
}

#[derive(Debug)]
struct InsecureVerifier;

impl ServerCertVerifier for InsecureVerifier {
    fn verify_server_cert(
        &self,
        _end_entity: &CertificateDer<'_>,
        _intermediates: &[CertificateDer<'_>],
        _server_name: &ServerName<'_>,
        _ocsp_response: &[u8],
        _now: UnixTime,
    ) -> Result<ServerCertVerified, rustls::Error> {
        Ok(ServerCertVerified::assertion())
    }

    fn verify_tls12_signature(
        &self,
        _message: &[u8],
        _cert: &CertificateDer<'_>,
        _dss: &DigitallySignedStruct,
    ) -> Result<HandshakeSignatureValid, rustls::Error> {
        Ok(HandshakeSignatureValid::assertion())
    }

    fn verify_tls13_signature(
        &self,
        _message: &[u8],
        _cert: &CertificateDer<'_>,
        _dss: &DigitallySignedStruct,
    ) -> Result<HandshakeSignatureValid, rustls::Error> {
        Ok(HandshakeSignatureValid::assertion())
    }

    fn supported_verify_schemes(&self) -> Vec<SignatureScheme> {
        vec![
            SignatureScheme::ECDSA_NISTP256_SHA256,
            SignatureScheme::ECDSA_NISTP384_SHA384,
            SignatureScheme::ED25519,
            SignatureScheme::RSA_PSS_SHA256,
            SignatureScheme::RSA_PSS_SHA384,
            SignatureScheme::RSA_PKCS1_SHA256,
            SignatureScheme::RSA_PKCS1_SHA384,
            SignatureScheme::RSA_PKCS1_SHA512,
        ]
    }
}

fn build_tls_client_config(
    ca_cert_path: Option<&str>,
    insecure_skip_verify: bool,
) -> Result<rustls::ClientConfig> {
    let mut root_cert_store = RootCertStore {
        roots: webpki_roots::TLS_SERVER_ROOTS.into(),
    };

    if let Some(path) = ca_cert_path {
        let pem_data = std::fs::read(path)
            .with_context(|| format!("Failed to read CA certificate: {path}"))?;
        let mut cursor = std::io::Cursor::new(pem_data);
        let certs: Vec<CertificateDer<'_>> = rustls_pemfile::certs(&mut cursor)
            .collect::<Result<Vec<_>, _>>()
            .map_err(|e| anyhow::anyhow!("Failed to parse CA certificate: {e}"))?;
        for cert in certs {
            root_cert_store
                .add(cert)
                .map_err(|e| anyhow::anyhow!("Failed to add CA certificate to root store: {e}"))?;
        }
    }

    let mut config = rustls::ClientConfig::builder()
        .with_root_certificates(root_cert_store)
        .with_no_client_auth();

    if insecure_skip_verify {
        config
            .dangerous()
            .set_certificate_verifier(Arc::new(InsecureVerifier));
    }

    Ok(config)
}

struct CustomTlsHttpHooks {
    tls_config: Option<Arc<rustls::ClientConfig>>,
}

impl CustomTlsHttpHooks {
    fn new(tls_config: Option<Arc<rustls::ClientConfig>>) -> Self {
        Self { tls_config }
    }
}

impl WasiHttpHooks for CustomTlsHttpHooks {
    fn send_request(
        &mut self,
        request: hyper::Request<HyperOutgoingBody>,
        config: OutgoingRequestConfig,
    ) -> wasmtime_wasi_http::p2::HttpResult<HostFutureIncomingResponse> {
        match &self.tls_config {
            Some(tls_config) => {
                let tls_config = tls_config.clone();
                let handle = wasmtime_wasi::runtime::spawn(async move {
                    Ok(custom_tls_send_request_handler(request, config, tls_config).await)
                });
                Ok(HostFutureIncomingResponse::pending(handle))
            }
            None => Ok(default_send_request(request, config)),
        }
    }
}

async fn custom_tls_send_request_handler(
    mut request: hyper::Request<HyperOutgoingBody>,
    OutgoingRequestConfig {
        use_tls,
        connect_timeout,
        first_byte_timeout,
        between_bytes_timeout,
    }: OutgoingRequestConfig,
    tls_config: Arc<rustls::ClientConfig>,
) -> Result<
    wasmtime_wasi_http::p2::types::IncomingResponse,
    wasmtime_wasi_http::p2::bindings::http::types::ErrorCode,
> {
    use wasmtime_wasi_http::p2::bindings::http::types::ErrorCode;

    let authority = if let Some(authority) = request.uri().authority() {
        if authority.port().is_some() {
            authority.to_string()
        } else {
            let port = if use_tls { 443 } else { 80 };
            format!("{authority}:{port}")
        }
    } else {
        return Err(ErrorCode::HttpRequestUriInvalid);
    };

    let tcp_stream = timeout(connect_timeout, tokio::net::TcpStream::connect(&authority))
        .await
        .map_err(|_| ErrorCode::ConnectionTimeout)?
        .map_err(|e| match e.kind() {
            std::io::ErrorKind::AddrNotAvailable => {
                dns_error("address not available".to_string(), 0)
            }
            _ => {
                if e.to_string()
                    .starts_with("failed to lookup address information")
                {
                    dns_error("address not available".to_string(), 0)
                } else {
                    ErrorCode::ConnectionRefused
                }
            }
        })?;

    let (mut sender, worker) = if use_tls {
        let connector = tokio_rustls::TlsConnector::from(tls_config);
        let domain = ServerName::try_from(authority.split(':').next().unwrap_or(&authority))
            .map_err(|e| {
                tracing::warn!("dns lookup error: {e:?}");
                dns_error("invalid dns name".to_string(), 0)
            })?
            .to_owned();
        let stream = connector.connect(domain, tcp_stream).await.map_err(|e| {
            tracing::warn!("tls protocol error: {e:?}");
            ErrorCode::TlsProtocolError
        })?;
        let stream = TokioIo::new(stream);

        let (sender, conn) = timeout(
            connect_timeout,
            hyper::client::conn::http1::handshake(stream),
        )
        .await
        .map_err(|_| ErrorCode::ConnectionTimeout)?
        .map_err(hyper_request_error)?;

        let worker = wasmtime_wasi::runtime::spawn(async move {
            match conn.await {
                Ok(()) => {}
                Err(e) => tracing::warn!("dropping error {e}"),
            }
        });

        (sender, worker)
    } else {
        let tcp_stream = TokioIo::new(tcp_stream);
        let (sender, conn) = timeout(
            connect_timeout,
            hyper::client::conn::http1::handshake(tcp_stream),
        )
        .await
        .map_err(|_| ErrorCode::ConnectionTimeout)?
        .map_err(hyper_request_error)?;

        let worker = wasmtime_wasi::runtime::spawn(async move {
            match conn.await {
                Ok(()) => {}
                Err(e) => tracing::warn!("dropping error {e}"),
            }
        });

        (sender, worker)
    };

    *request.uri_mut() = hyper::Uri::builder()
        .path_and_query(
            request
                .uri()
                .path_and_query()
                .map(|p| p.as_str())
                .unwrap_or("/"),
        )
        .build()
        .expect("comes from valid request");

    let resp = timeout(first_byte_timeout, sender.send_request(request))
        .await
        .map_err(|_| ErrorCode::ConnectionReadTimeout)?
        .map_err(hyper_request_error)?
        .map(|body| body.map_err(hyper_request_error).boxed_unsync());

    Ok(wasmtime_wasi_http::p2::types::IncomingResponse {
        resp,
        worker: Some(worker),
        between_bytes_timeout,
    })
}

fn dns_error(
    rcode: String,
    info_code: u16,
) -> wasmtime_wasi_http::p2::bindings::http::types::ErrorCode {
    wasmtime_wasi_http::p2::bindings::http::types::ErrorCode::DnsError(
        wasmtime_wasi_http::p2::bindings::http::types::DnsErrorPayload {
            rcode: Some(rcode),
            info_code: Some(info_code),
        },
    )
}

pub struct StoreData {
    wasi: WasiCtx,
    http: WasiHttpCtx,
    usb: WasiUsbCtx,
    table: ResourceTable,
    pub(crate) hal: Option<Arc<PropletHal>>,
    pub(crate) storage: Option<Arc<StorageInterface>>,
    http_hooks: CustomTlsHttpHooks,
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
    fn http(&mut self) -> wasmtime_wasi_http::p2::WasiHttpCtxView<'_> {
        wasmtime_wasi_http::p2::WasiHttpCtxView {
            ctx: &mut self.http,
            table: &mut self.table,
            hooks: &mut self.http_hooks,
        }
    }
}

impl WasiUsbView for StoreData {
    fn usb(&mut self) -> WasiUsbCtxView<'_> {
        WasiUsbCtxView {
            ctx: &mut self.usb,
            table: &mut self.table,
        }
    }
}

pub struct WasmtimeRuntime {
    engine: Engine,
    tasks: Arc<Mutex<HashMap<String, JoinHandle<()>>>>,
    proxy_ports: Arc<Mutex<HashMap<u16, String>>>,
    proxy_cancellers: Arc<Mutex<HashMap<String, watch::Sender<bool>>>>,
    hal_enabled: bool,
    hal: Arc<PropletHal>,
    http_enabled: bool,
    usb_enabled: bool,
    preopened_dirs: Vec<String>,
    proxy_port: u16,
    http_tls_config: Option<Arc<rustls::ClientConfig>>,
}

impl WasmtimeRuntime {
    pub fn new_with_options(
        hal_enabled: bool,
        http_enabled: bool,
        usb_enabled: bool,
        preopened_dirs: Vec<String>,
        proxy_port: u16,
        http_tls_ca_cert: Option<&str>,
        http_tls_insecure_skip_verify: bool,
    ) -> Result<Self> {
        let mut config = Config::new();
        config.wasm_reference_types(true);
        config.wasm_bulk_memory(true);
        config.wasm_simd(true);
        config.wasm_component_model(true);

        let engine = Engine::new(&config)?;

        let http_tls_config = match (http_tls_ca_cert, http_tls_insecure_skip_verify) {
            (Some(_), _) | (None, true) => {
                match build_tls_client_config(http_tls_ca_cert, http_tls_insecure_skip_verify) {
                    Ok(cfg) => {
                        if http_tls_ca_cert.is_some() {
                            info!("Custom TLS CA certificate loaded for outgoing HTTP requests");
                        }
                        if http_tls_insecure_skip_verify {
                            warn!("TLS certificate verification is disabled for outgoing HTTP requests");
                        }
                        Some(Arc::new(cfg))
                    }
                    Err(e) => {
                        warn!("Failed to build custom TLS config, using default: {e}");
                        None
                    }
                }
            }
            _ => None,
        };

        Ok(Self {
            engine,
            tasks: Arc::new(Mutex::new(HashMap::new())),
            proxy_ports: Arc::new(Mutex::new(HashMap::new())),
            proxy_cancellers: Arc::new(Mutex::new(HashMap::new())),
            hal_enabled,
            hal: PropletHal::new(),
            http_enabled,
            usb_enabled,
            preopened_dirs,
            proxy_port,
            http_tls_config,
        })
    }
}

/// Derive the storage base for a task: explicit override on the StartConfig
/// wins, otherwise a per-task default under `/tmp/proplet/hal-storage/<id>`
/// keeps each workload isolated.
fn task_hal_storage_path(config: &StartConfig) -> PathBuf {
    if let Some(p) = &config.hal_storage_path {
        PathBuf::from(p)
    } else {
        PathBuf::from("/tmp/proplet/hal-storage").join(&config.id)
    }
}

impl WasmtimeRuntime {
    async fn init_task_storage(&self, config: &StartConfig) -> Option<Arc<StorageInterface>> {
        if !self.hal_enabled {
            return None;
        }
        let path = task_hal_storage_path(config);
        match StorageInterface::new(&path).await {
            Ok(s) => {
                info!(
                    "Initialised per-task storage at {} for task {}",
                    path.display(),
                    config.id
                );
                Some(Arc::new(s))
            }
            Err(e) => {
                warn!(
                    "Failed to initialise per-task storage at {}: {e}",
                    path.display()
                );
                None
            }
        }
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

        let has_custom_export = !config.function_name.is_empty()
            && config.function_name != "_start"
            && !config.function_name.starts_with("fl-round-");

        if is_proxy {
            self.start_app_proxy(config).await
        } else if is_component && has_custom_export {
            self.start_app_component_export(config).await
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
        let module = match Module::from_binary(&self.engine, &config.wasm_binary) {
            Ok(module) => module,
            Err(e) => {
                return Err(anyhow::anyhow!(
                    "Failed to compile Wasmtime module from binary: {e}"
                ))
            }
        };

        info!("Module compiled successfully for task: {}", config.id);

        let mut wasi_builder = wasmtime_wasi::WasiCtxBuilder::new();
        wasi_builder.inherit_stdio();

        for (key, value) in &config.env {
            wasi_builder.env(key, value);
        }

        for dir in &self.preopened_dirs {
            let _ = wasi_builder
                .preopened_dir(dir, dir, DirPerms::all(), FilePerms::all())
                .map_err(|e| format!("Failed to preopen directory '{dir}': {e}"));
        }

        let wasi = wasi_builder.build_p1();

        let mut store = Store::new(&self.engine, wasi);

        let mut linker = Linker::new(&self.engine);
        let _ = wasmtime_wasi::p1::add_to_linker_sync(&mut linker, |ctx| ctx)
            .map_err(|e| format!("Failed to add WASI to linker: {e}"));

        // HAL is exposed to P2 components only (see hal_component); P1 core
        // modules get WASI but no HAL.

        let instance = match linker.instantiate(&mut store, &module) {
            Ok(instance) => instance,
            Err(e) => {
                return Err(anyhow::anyhow!(
                    "Failed to instantiate Wasmtime module: {e}"
                ))
            }
        };

        self.run_core_instance(config, store, instance).await
    }

    async fn start_app_component(&self, config: StartConfig) -> Result<Vec<u8>> {
        info!(
            "Compiling WASM P2 command component for task: {}",
            config.id
        );

        let component = match component::Component::from_binary(&self.engine, &config.wasm_binary) {
            Ok(component) => component,
            Err(e) => {
                return Err(anyhow::anyhow!(
                    "Failed to compile WASM component from binary: {e}"
                ))
            }
        };

        info!("Component compiled successfully for task: {}", config.id);

        let mut wasi_builder = WasiCtxBuilder::new();
        wasi_builder.inherit_stdio();

        for (key, value) in &config.env {
            wasi_builder.env(key, value);
        }

        for dir in &self.preopened_dirs {
            let _ = wasi_builder
                .preopened_dir(dir, dir, DirPerms::all(), FilePerms::all())
                .map_err(|e| format!("Failed to preopen directory '{dir}': {e}"));
        }

        let wasi = wasi_builder.build();

        let storage = self.init_task_storage(&config).await;
        let store_data = StoreData {
            wasi,
            http: WasiHttpCtx::new(),
            usb: WasiUsbCtx::new(false),
            table: ResourceTable::new(),
            hal: self.hal_enabled.then(|| self.hal.clone()),
            storage,
            http_hooks: CustomTlsHttpHooks::new(self.http_tls_config.clone()),
        };

        let mut store = Store::new(&self.engine, store_data);

        let mut linker: component::Linker<StoreData> = component::Linker::new(&self.engine);
        let _ = wasmtime_wasi::p2::add_to_linker_async(&mut linker)
            .map_err(|e| format!("Failed to add WASI P2 to component linker: {e}"));
        let _ = wasmtime_wasi_http::p2::add_only_http_to_linker_async(&mut linker)
            .map_err(|e| format!("Failed to add wasi:http to component linker: {e}"));
        if self.hal_enabled {
            // Single call registers both consolidated `elastic:hal/*` and
            // modular `elastic:sockets`/`elastic:storage`/`elastic:crypto`/
            // `elastic:clock`/`elastic:random` packagings.
            hal_component::add_to_linker(&mut linker)
                .context("Failed to add ELASTIC TEE HAL to component linker")?;
        }

        if self.usb_enabled {
            let _ = wasmtime_wasi_usb::add_to_linker(&mut linker)
                .map_err(|e| format!("Failed to add ELASTIC wasi:usb to component linker: {e}"));
        }

        let task_id = config.id.clone();
        let task_id_for_cleanup = task_id.clone();
        let tasks = self.tasks.clone();

        let (result_tx, result_rx) = oneshot::channel();

        let handle = tokio::task::spawn(async move {
            let result = async {
                let command =
                    match Command::instantiate_async(&mut store, &component, &linker).await {
                        Ok(command) => command,
                        Err(e) => {
                            return Err(anyhow::anyhow!(
                                "Failed to instantiate WASM command component: {e}"
                            ))
                        }
                    };

                let program_result =
                    command
                        .wasi_cli_run()
                        .call_run(&mut store)
                        .await
                        .map_err(|e| {
                            anyhow::anyhow!("Failed to call wasi:cli/run on component: {e}")
                        })?;

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
            }
            .await;

            tasks.lock().await.remove(&task_id_for_cleanup);

            let _ = result_tx.send(result);
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

    async fn start_app_component_export(&self, config: StartConfig) -> Result<Vec<u8>> {
        info!(
            "Compiling WASM component for custom export '{}' for task: {}",
            config.function_name, config.id
        );

        let component = match component::Component::from_binary(&self.engine, &config.wasm_binary) {
            Ok(c) => c,
            Err(e) => {
                return Err(anyhow::anyhow!(
                    "Failed to compile WASM component from binary: {e}"
                ))
            }
        };

        let mut wasi_builder = WasiCtxBuilder::new();
        wasi_builder.inherit_stdio();
        for (key, value) in &config.env {
            wasi_builder.env(key, value);
        }
        for dir in &self.preopened_dirs {
            let _ = wasi_builder
                .preopened_dir(dir, dir, DirPerms::all(), FilePerms::all())
                .map_err(|e| format!("Failed to preopen directory '{dir}': {e}"));
        }
        let wasi = wasi_builder.build();

        let storage = self.init_task_storage(&config).await;
        let store_data = StoreData {
            wasi,
            http: WasiHttpCtx::new(),
            usb: WasiUsbCtx::new(false),
            table: ResourceTable::new(),
            hal: self.hal_enabled.then(|| self.hal.clone()),
            storage,
            http_hooks: CustomTlsHttpHooks::new(self.http_tls_config.clone()),
        };

        let mut store = Store::new(&self.engine, store_data);

        let mut linker: component::Linker<StoreData> = component::Linker::new(&self.engine);
        let _ = wasmtime_wasi::p2::add_to_linker_async(&mut linker)
            .map_err(|e| format!("Failed to add WASI P2 to component linker: {e}"));
        let _ = wasmtime_wasi_http::p2::add_only_http_to_linker_sync(&mut linker)
            .map_err(|e| format!("Failed to add wasi:http to component linker: {e}"));
        if self.hal_enabled {
            // Single call registers both consolidated `elastic:hal/*` and
            // modular `elastic:sockets`/`elastic:storage`/`elastic:crypto`/
            // `elastic:clock`/`elastic:random` packagings.
            hal_component::add_to_linker(&mut linker)
                .context("Failed to add ELASTIC TEE HAL to component linker")?;
        }

        if self.usb_enabled {
            let _ = wasmtime_wasi_usb::add_to_linker(&mut linker)
                .map_err(|e| format!("Failed to add ELASTIC wasi:usb to component linker: {e}"));
        }

        let task_id = config.id.clone();
        let task_id_for_cleanup = task_id.clone();
        let function_name = config.function_name.clone();
        let args = config.args.clone();
        let tasks = self.tasks.clone();
        let wasm_binary = config.wasm_binary.clone();

        let (result_tx, result_rx) = oneshot::channel();

        let handle = tokio::task::spawn(async move {
            let result = async {
                let instance = match linker.instantiate_async(&mut store, &component).await {
                    Ok(i) => i,
                    Err(e) => {
                        return Err(anyhow::anyhow!("Failed to instantiate WASM component: {e}"))
                    }
                };

                let func =
                    resolve_component_export(&instance, &mut store, &function_name, &wasm_binary)
                        .ok_or_else(|| {
                        anyhow::anyhow!(
                            "Export '{}' not found in component for task {}",
                            function_name,
                            task_id
                        )
                    })?;

                let func_ty = func.ty(&store);
                let param_types: Vec<_> = func_ty.params().collect();
                let result_count = func_ty.results().count();

                if args.len() != param_types.len() {
                    return Err(anyhow::anyhow!(
                        "Argument count mismatch for '{}': expected {} but got {}",
                        function_name,
                        param_types.len(),
                        args.len()
                    ));
                }

                let wasm_args: Vec<component::Val> = args
                    .iter()
                    .zip(param_types.iter())
                    .map(|(wave_str, (_, ty))| {
                        wasm_wave::from_str::<component::Val>(ty, wave_str).map_err(|e| {
                            anyhow::anyhow!(
                                "Failed to parse WAVE argument '{}' for export '{}': {e}",
                                wave_str,
                                function_name
                            )
                        })
                    })
                    .collect::<Result<Vec<_>>>()?;

                let mut results: Vec<component::Val> = (0..result_count)
                    .map(|_| component::Val::Bool(false))
                    .collect();

                func.call_async(&mut store, &wasm_args, &mut results)
                    .await
                    .map_err(|e| {
                        anyhow::anyhow!("Failed to call export '{}': {e}", function_name)
                    })?;

                let result_string = results
                    .first()
                    .and_then(|v| wasm_wave::to_string(v).ok())
                    .unwrap_or_default();

                info!(
                    "Export '{}' for task {} completed, result: {}",
                    function_name, task_id, result_string
                );

                Ok(result_string.into_bytes())
            }
            .await;

            tasks.lock().await.remove(&task_id_for_cleanup);

            let _ = result_tx.send(result);
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

        let component = component::Component::from_binary(&self.engine, &config.wasm_binary)
            .map_err(|e| anyhow::anyhow!("Failed to compile WASM proxy component: {e}"))?;

        let mut linker: component::Linker<StoreData> = component::Linker::new(&self.engine);
        wasmtime_wasi::p2::add_to_linker_async(&mut linker)
            .map_err(|e| anyhow::anyhow!("Failed to add WASI P2 async to proxy linker: {e}"))?;
        wasmtime_wasi_http::p2::add_only_http_to_linker_async(&mut linker)
            .map_err(|e| anyhow::anyhow!("Failed to add wasi:http async to proxy linker: {e}"))?;

        if self.usb_enabled {
            let _ = wasmtime_wasi_usb::add_to_linker(&mut linker)
                .map_err(|e| format!("Failed to add ELASTIC wasi:usb to component linker: {e}"));
        }

        let pre = Arc::new(
            ProxyPre::new(linker.instantiate_pre(&component)?)
                .map_err(|e| anyhow::anyhow!("Failed to create ProxyPre: {e}"))?,
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
        let http_tls_config = self.http_tls_config.clone();

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
                let tls_config = http_tls_config.clone();

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
                                let tls_cfg = tls_config.clone();
                                async move {
                                    handle_proxy_request(pre, env, dirs, req, task_id_req, tls_cfg)
                                        .await
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
                            .map_err(|e| anyhow::anyhow!("Failed to initialize WASM runtime via _initialize: {e}"))?;
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
                            ValType::I32 => arg
                                .parse::<i32>()
                                .map(Val::I32)
                                .map_err(|e| anyhow::anyhow!("Failed to parse '{}' as i32: {e}", arg)),
                            ValType::I64 => arg
                                .parse::<i64>()
                                .map(Val::I64)
                                .map_err(|e| anyhow::anyhow!("Failed to parse '{}' as i64: {e}", arg)),
                            ValType::F32 => arg
                                .parse::<f32>()
                                .map(|f| Val::F32(f.to_bits()))
                                .map_err(|e| anyhow::anyhow!("Failed to parse '{}' as f32: {e}", arg)),
                            ValType::F64 => arg
                                .parse::<f64>()
                                .map(|f| Val::F64(f.to_bits()))
                                .map_err(|e| anyhow::anyhow!("Failed to parse '{}' as f64: {e}", arg)),
                            _ => Err(anyhow::anyhow!("Unsupported Wasm value type for arg '{}'", arg)),
                        })
                        .collect::<Result<Vec<_>>>()?;

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
                        .map_err(|e| anyhow::anyhow!("Failed to call function '{function_name}': {e}"))?;

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
    http_tls_config: Option<Arc<rustls::ClientConfig>>,
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
        usb: WasiUsbCtx::new(false),
        table: ResourceTable::new(),
        hal: None,
        storage: None,
        http_hooks: CustomTlsHttpHooks::new(http_tls_config),
    };

    let mut store = Store::new(pre.engine(), store_data);

    let (sender, receiver) = oneshot::channel();
    let incoming = store
        .data_mut()
        .http()
        .new_incoming_request(Scheme::Http, req)
        .map_err(|e| anyhow::anyhow!("Failed to create incoming request: {e}"))?;
    let outparam = store
        .data_mut()
        .http()
        .new_response_outparam(sender)
        .map_err(|e| anyhow::anyhow!("Failed to create response outparam: {e}"))?;

    let task = tokio::task::spawn(async move {
        let proxy = pre
            .instantiate_async(&mut store)
            .await
            .map_err(|e| anyhow::anyhow!("Failed to instantiate proxy component: {e}"))?;
        proxy
            .wasi_http_incoming_handler()
            .call_handle(&mut store, incoming, outparam)
            .await
            .map_err(|e| anyhow::anyhow!("Failed to call incoming-handler: {e}"))?;
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
    use std::collections::HashMap;

    #[test]
    fn test_wasmtime_runtime_new() {
        let runtime =
            WasmtimeRuntime::new_with_options(false, false, false, Vec::new(), 8222, None, false);
        assert!(runtime.is_ok());
    }

    #[test]
    fn test_wasmtime_runtime_new_with_http() {
        let runtime =
            WasmtimeRuntime::new_with_options(false, true, false, Vec::new(), 8222, None, false);
        assert!(runtime.is_ok());
    }

    #[test]
    fn test_wasmtime_runtime_engine_configuration() {
        let runtime =
            WasmtimeRuntime::new_with_options(false, false, false, Vec::new(), 8222, None, false)
                .unwrap();
        assert!(runtime.tasks.try_lock().is_ok());
    }

    #[test]
    fn test_wasmtime_runtime_tasks_empty_on_creation() {
        let runtime =
            WasmtimeRuntime::new_with_options(false, false, false, Vec::new(), 8222, None, false)
                .unwrap();
        let tasks = runtime.tasks.try_lock().unwrap();
        assert_eq!(tasks.len(), 0);
    }

    #[tokio::test]
    async fn test_wasmtime_runtime_compile_invalid_wasm() {
        let runtime =
            WasmtimeRuntime::new_with_options(false, false, false, Vec::new(), 8222, None, false)
                .unwrap();
        let invalid_wasm = vec![0xFF, 0xFF, 0xFF, 0xFF];
        let result = Module::from_binary(&runtime.engine, &invalid_wasm);
        assert!(result.is_err());
    }

    #[tokio::test]
    async fn test_wasmtime_runtime_compile_empty_wasm() {
        let runtime =
            WasmtimeRuntime::new_with_options(false, false, false, Vec::new(), 8222, None, false)
                .unwrap();
        let empty_wasm = vec![];
        let result = Module::from_binary(&runtime.engine, &empty_wasm);
        assert!(result.is_err());
    }

    #[tokio::test]
    async fn test_custom_export_with_wasi_http() {
        let wasm_path = concat!(
            env!("CARGO_MANIFEST_DIR"),
            "/../examples/http-greet-component/target/wasm32-wasip2/release/http_greet_component.wasm"
        );
        let wasm_binary = match std::fs::read(wasm_path) {
            Ok(b) => b,
            Err(e) => {
                eprintln!(
                    "Skipping: WASM binary not found at {wasm_path}: {e}. \
                     Build it with: make http-greet-component"
                );
                return;
            }
        };

        let runtime = WasmtimeRuntime::new_with_options(false, false, Vec::new(), 8222).unwrap();
        let ctx = RuntimeContext {
            proplet_id: "test".to_string(),
        };
        let config = StartConfig {
            id: uuid::Uuid::new_v4().to_string(),
            function_name: "my-function".to_string(),
            daemon: false,
            wasm_binary,
            cli_args: Vec::new(),
            env: HashMap::new(),
            args: Vec::new(),
            mode: None,
            hal_storage_path: None,
        };

        let result = runtime.start_app(ctx, config).await;
        assert!(
            result.is_ok(),
            "Custom export with wasi:http should succeed: {:?}",
            result.err()
        );
        let output = result.unwrap();
        assert!(!output.is_empty());
    }
}
