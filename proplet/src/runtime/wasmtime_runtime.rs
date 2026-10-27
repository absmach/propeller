use super::{Runtime, RuntimeContext, StartConfig};
use crate::hal::PropletHal;
use crate::hal_component;
use crate::wasi_security::WasiSecurity;
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
use std::future::Future;
use std::net::{IpAddr, SocketAddr};
use std::path::PathBuf;
use std::sync::Arc;
use std::time::Duration;
use tokio::net::TcpListener;
use tokio::sync::oneshot;
use tokio::sync::watch;
use tokio::sync::Mutex;
use tokio::sync::Notify;
use tokio::task::JoinHandle;
use tokio::time::timeout;
use tracing::{error, info, warn};
use wasm_wave;
use wasmtime::component::ResourceTable;
use wasmtime::*;
use wasmtime_wasi::p2::bindings::Command;
use wasmtime_wasi::p2::pipe::MemoryOutputPipe;
use wasmtime_wasi::sockets::SocketAddrUse;
use wasmtime_wasi::{FsPerms, WasiCtx, WasiCtxBuilder, WasiCtxView, WasiView};
use wasmtime_wasi_http::io::TokioIo;
use wasmtime_wasi_http::p2::bindings::http::types::Scheme;
use wasmtime_wasi_http::p2::bindings::ProxyPre;
use wasmtime_wasi_http::p2::body::HyperOutgoingBody;
use wasmtime_wasi_http::{
    Error as HttpError, RequestOptions, WasiBody, WasiHttpCtx, WasiHttpCtxView, WasiHttpHooks,
    WasiHttpView,
};
use wasmtime_wasi_usb::{WasiUsbCtx, WasiUsbCtxView, WasiUsbView};

/// Merge the task environment with the policy environment, keeping first-seen order.
fn merge_env<'a>(
    env: impl IntoIterator<Item = (&'a str, &'a str)>,
    policy_env: Option<&'a HashMap<String, String>>,
) -> Vec<(&'a str, &'a str)> {
    let mut merged: Vec<(&str, &str)> = Vec::new();
    let mut upsert = |key: &'a str, value: &'a str| match merged.iter_mut().find(|(k, _)| *k == key)
    {
        Some(entry) => entry.1 = value,
        None => merged.push((key, value)),
    };

    for (key, value) in env {
        upsert(key, value);
    }

    // The policy env wins over the task env, so it is applied last.
    for (key, value) in policy_env.into_iter().flatten() {
        upsert(key, value);
    }

    merged
}

/// Apply the task's environment, filesystem preopens and network policy to a fresh [`WasiCtxBuilder`].
fn configure_wasi<'a>(
    builder: &mut WasiCtxBuilder,
    task_id: &str,
    env: impl IntoIterator<Item = (&'a str, &'a str)>,
    global_preopens: &[String],
    policy: Option<&'a WasiSecurity>,
    inherit_stdio: bool,
) -> Result<()> {
    if inherit_stdio {
        builder.inherit_stdio();
    }

    for (key, value) in merge_env(env, policy.and_then(|p| p.env.as_ref())) {
        builder.env(key, value);
    }

    let Some(policy) = policy else {
        for dir in global_preopens {
            if let Err(e) = builder.preopened_dir(dir, dir, FsPerms::ReadWrite) {
                warn!("Task {task_id}: failed to preopen '{dir}': {e}");
            }
        }

        return Ok(());
    };

    if let Some(arguments) = &policy.arguments {
        builder.args(arguments);
    }

    // A preopen the policy asked for but that could not be granted is a silent
    // downgrade of the workload's capabilities, so fail the task instead.
    for (host, guest) in &policy.storage_readonly {
        builder
            .preopened_dir(host, guest, FsPerms::ReadOnly)
            .map_err(|e| {
                anyhow::anyhow!(
                    "Task {task_id}: failed to preopen read-only '{host}' as '{guest}': {e}"
                )
            })?;
    }

    for (host, guest) in &policy.storage_mount {
        builder
            .preopened_dir(host, guest, FsPerms::ReadWrite)
            .map_err(|e| {
                anyhow::anyhow!("Task {task_id}: failed to mount '{host}' as '{guest}': {e}")
            })?;
    }

    // The socket check runs on every socket operation and cannot move its capture into
    // the future it returns, so share the policy instead of deep-cloning it.
    let check_policy = Arc::new(policy.clone());
    builder.socket_addr_check(move |addr, use_| {
        let policy = check_policy.clone();

        Box::pin(async move { policy.allows_socket(addr, use_) })
    });
    builder.allow_tcp(policy.uses_tcp());
    builder.allow_udp(policy.uses_udp());
    builder.allow_ip_name_lookup(policy.allow_ip_name_lookup);

    Ok(())
}

fn is_wasm_component(bytes: &[u8]) -> bool {
    bytes.len() >= 8 && bytes[0..4] == [0x00, 0x61, 0x73, 0x6d] && bytes[4] == 0x0d
}

fn is_proxy_component(bytes: &[u8]) -> bool {
    bytes
        .windows(b"wasi:http/incoming-handler".len())
        .any(|w| w == b"wasi:http/incoming-handler")
}

const INVOCATION_OUTPUT_CAPACITY: usize = 1024 * 1024;

/// `socket_addr_check` for the listener the host binds itself, which never reaches WASI.
fn proxy_bind_allowed(policy: Option<&WasiSecurity>, addr: SocketAddr) -> bool {
    policy.is_none_or(|p| {
        p.allows_socket(addr, SocketAddrUse::TcpBind)
            && p.allows_socket(addr, SocketAddrUse::TcpListen)
    })
}

fn find_available_port(start_port: u16, policy: Option<&WasiSecurity>) -> Result<(Socket, u16)> {
    let max_attempts = 100u16;
    for port in start_port..start_port.saturating_add(max_attempts) {
        let addr: SocketAddr = ([0, 0, 0, 0], port).into();
        if !proxy_bind_allowed(policy, addr) {
            continue;
        }
        match Socket::new(Domain::IPV4, Type::STREAM, Some(Protocol::TCP)) {
            Ok(socket) => {
                let _ = socket.set_reuse_address(true);
                if socket.bind(&addr.into()).is_ok() {
                    return Ok((socket, port));
                }
            }
            Err(_) => continue,
        }
    }
    let end_port = start_port.saturating_add(max_attempts - 1);
    let granted = policy.map_or("", |_| " granted by the policy's [network] bind rules");
    Err(anyhow::anyhow!(
        "No available port found in range {start_port}-{end_port}{granted}"
    ))
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
    tls_config: Arc<rustls::ClientConfig>,
    /// The task's policy; only its `[network]` rules are consulted here.
    policy: Option<Arc<WasiSecurity>>,
}

/// Whether a URI host is an address rather than a name. IPv6 arrives bracketed (`[::1]`).
fn is_ip_literal(host: &str) -> bool {
    host.strip_prefix('[')
        .and_then(|h| h.strip_suffix(']'))
        .unwrap_or(host)
        .parse::<IpAddr>()
        .is_ok()
}

/// `authority[:port]`, filling in the scheme's default port so a rule always has one to match.
fn request_authority(uri: &hyper::Uri, use_tls: bool) -> Result<String, HttpError> {
    let authority = uri.authority().ok_or(HttpError::HttpRequestUriInvalid)?;
    if authority.port().is_some() {
        Ok(authority.to_string())
    } else {
        let port = if use_tls { 443 } else { 80 };
        Ok(format!("{authority}:{port}"))
    }
}

impl WasiHttpHooks for CustomTlsHttpHooks {
    fn send_request(
        &mut self,
        request: hyper::Request<WasiBody>,
        options: Option<RequestOptions>,
        fut: Box<dyn Future<Output = Result<(), HttpError>> + Send>,
    ) -> Box<
        dyn Future<
                Output = Result<
                    (
                        hyper::Response<WasiBody>,
                        Box<dyn Future<Output = Result<(), HttpError>> + Send>,
                    ),
                    HttpError,
                >,
            > + Send,
    > {
        _ = fut;
        let policy = self.policy.clone();
        let tls_config = self.tls_config.clone();
        Box::new(async move {
            let checked = enforce_http_egress(request.uri(), policy.as_deref()).await?;
            custom_tls_send_request_handler(request, options, tls_config, checked).await
        })
    }
}

/// Resolve the request target and keep the addresses the policy grants `TcpConnect` to.
/// `None` means the task carries no policy.
async fn enforce_http_egress(
    uri: &hyper::Uri,
    policy: Option<&WasiSecurity>,
) -> Result<Option<Vec<SocketAddr>>, HttpError> {
    let Some(policy) = policy else {
        return Ok(None);
    };

    // The host resolves this name on the task's behalf, the capability the flag governs.
    let host = uri.host().ok_or(HttpError::HttpRequestUriInvalid)?;
    if !policy.allow_ip_name_lookup && !is_ip_literal(host) {
        warn!(
            "wasi_security: denied outbound wasi:http to {host}; \
             resolving a name needs [network] allow_ip_name_lookup"
        );
        return Err(HttpError::HttpRequestDenied);
    }

    let use_tls = uri.scheme() == Some(&hyper::http::uri::Scheme::HTTPS);
    let authority = request_authority(uri, use_tls)?;
    let resolved: Vec<SocketAddr> = tokio::net::lookup_host(&authority)
        .await
        .map_err(HttpError::Connect)?
        .collect();
    let allowed: Vec<SocketAddr> = resolved
        .iter()
        .copied()
        .filter(|addr| policy.allows_socket(*addr, SocketAddrUse::TcpConnect))
        .collect();

    if allowed.is_empty() {
        warn!(
            "wasi_security: denied outbound wasi:http to {authority} (resolved {resolved:?}); \
             no matching [network] connect grant"
        );
        return Err(HttpError::HttpRequestDenied);
    }
    Ok(Some(allowed))
}

/// Response body that enforces the request's `between_bytes_timeout`.
struct IncomingResponseBody {
    incoming: hyper::body::Incoming,
    timeout: tokio::time::Interval,
}

impl http_body::Body for IncomingResponseBody {
    type Data = <hyper::body::Incoming as http_body::Body>::Data;
    type Error = HttpError;

    fn poll_frame(
        mut self: std::pin::Pin<&mut Self>,
        cx: &mut std::task::Context<'_>,
    ) -> std::task::Poll<Option<Result<http_body::Frame<Self::Data>, Self::Error>>> {
        use std::task::{ready, Poll};
        match std::pin::Pin::new(&mut self.as_mut().incoming).poll_frame(cx) {
            Poll::Ready(None) => Poll::Ready(None),
            Poll::Ready(Some(Err(err))) => {
                let err = if err.is_timeout() {
                    HttpError::HttpResponseTimeout
                } else {
                    HttpError::from(err)
                };
                Poll::Ready(Some(Err(err)))
            }
            Poll::Ready(Some(Ok(frame))) => {
                self.timeout.reset();
                Poll::Ready(Some(Ok(frame)))
            }
            Poll::Pending => {
                ready!(self.timeout.poll_tick(cx));
                Poll::Ready(Some(Err(HttpError::ConnectionReadTimeout)))
            }
        }
    }

    fn is_end_stream(&self) -> bool {
        self.incoming.is_end_stream()
    }

    fn size_hint(&self) -> http_body::SizeHint {
        self.incoming.size_hint()
    }
}

async fn custom_tls_send_request_handler(
    mut request: hyper::Request<WasiBody>,
    options: Option<RequestOptions>,
    tls_config: Arc<rustls::ClientConfig>,
    checked_addrs: Option<Vec<SocketAddr>>,
) -> Result<
    (
        hyper::Response<WasiBody>,
        Box<dyn Future<Output = Result<(), HttpError>> + Send>,
    ),
    HttpError,
> {
    let use_tls = request.uri().scheme() == Some(&hyper::http::uri::Scheme::HTTPS);
    let authority = request_authority(request.uri(), use_tls)?;

    let tcp_stream = timeout(
        options
            .and_then(|r| r.connect_timeout)
            .unwrap_or(Duration::from_secs(600)),
        // Connect to the checked addresses, not the name: a second lookup could differ.
        async {
            match &checked_addrs {
                Some(addrs) => tokio::net::TcpStream::connect(&addrs[..]).await,
                None => tokio::net::TcpStream::connect(&authority).await,
            }
        },
    )
    .await
    .map_err(|_| HttpError::ConnectionTimeout)?
    .map_err(HttpError::Connect)?;

    let (mut sender, worker) = {
        let connect_timeout = options
            .and_then(|r| r.connect_timeout)
            .unwrap_or(Duration::from_secs(600));
        if use_tls {
            let connector = tokio_rustls::TlsConnector::from(tls_config);
            let tenant = ServerName::try_from(authority.split(':').next().unwrap_or(&authority))
                .map_err(HttpError::InvalidDnsNameError)?
                .to_owned();
            let stream = connector
                .connect(tenant, tcp_stream)
                .await
                .map_err(HttpError::Tls)?;
            let stream = TokioIo::new(stream);

            let (sender, conn) = timeout(
                connect_timeout,
                hyper::client::conn::http1::handshake(stream),
            )
            .await
            .map_err(|_| HttpError::ConnectionTimeout)?
            .map_err(HttpError::from)?;

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
            .map_err(|_| HttpError::ConnectionTimeout)?
            .map_err(HttpError::from)?;

            let worker = wasmtime_wasi::runtime::spawn(async move {
                match conn.await {
                    Ok(()) => {}
                    Err(e) => tracing::warn!("dropping error {e}"),
                }
            });

            (sender, worker)
        }
    };

    preserve_host_header(&mut request);

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

    let resp = timeout(
        options
            .and_then(|r| r.first_byte_timeout)
            .unwrap_or(Duration::from_secs(600)),
        sender.send_request(request),
    )
    .await
    .map_err(|_| HttpError::ConnectionReadTimeout)?
    .map_err(HttpError::from)?
    .map(|incoming| {
        let mut between_bytes = tokio::time::interval(
            options
                .and_then(|r| r.between_bytes_timeout)
                .unwrap_or(Duration::from_secs(600)),
        );
        between_bytes.reset();
        IncomingResponseBody {
            incoming,
            timeout: between_bytes,
        }
        .boxed_unsync()
    });

    Ok((
        resp,
        Box::new(async move {
            let _ = worker.await;
            Ok(())
        }),
    ))
}

fn preserve_host_header(request: &mut hyper::Request<HyperOutgoingBody>) {
    if !request.headers().contains_key(hyper::http::header::HOST) {
        if let Some(authority) = request.uri().authority().cloned() {
            if let Ok(host_value) = hyper::http::HeaderValue::from_str(authority.as_str()) {
                request
                    .headers_mut()
                    .insert(hyper::http::header::HOST, host_value);
            }
        }
    }
}

pub struct StoreData {
    wasi: WasiCtx,
    http: WasiHttpCtx,
    usb: WasiUsbCtx,
    table: ResourceTable,
    pub(crate) hal: Option<Arc<PropletHal>>,
    pub(crate) storage: Option<Arc<StorageInterface>>,
    /// Per-task cancellation signal. Host bindings select on this so a
    /// guest parked in a blocking HAL call (e.g. tcp_accept) unblocks when
    /// the task is stopped, instead of surviving abort.
    pub(crate) cancel: Arc<Notify>,
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
    fn http(&mut self) -> WasiHttpCtxView<'_> {
        WasiHttpCtxView {
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

struct PrecompiledComponent {
    component: component::Component,
    config: StartConfig,
    is_proxy: bool,
    wasm_binary: Arc<Vec<u8>>,
}

pub struct WasmtimeRuntime {
    engine: Engine,
    tasks: Arc<Mutex<HashMap<String, JoinHandle<()>>>>,
    task_cancels: Arc<Mutex<HashMap<String, Arc<Notify>>>>,
    proxy_ports: Arc<Mutex<HashMap<u16, String>>>,
    proxy_cancellers: Arc<Mutex<HashMap<String, watch::Sender<bool>>>>,
    precompiled: Arc<Mutex<HashMap<String, PrecompiledComponent>>>,
    hal_enabled: bool,
    http_enabled: bool,
    usb_enabled: bool,
    preopened_dirs: Vec<String>,
    proxy_port: u16,
    http_tls_config: Arc<rustls::ClientConfig>,
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

        let custom_tls = build_tls_client_config(http_tls_ca_cert, http_tls_insecure_skip_verify)
            .inspect_err(|e| warn!("Failed to build custom TLS config, using default roots: {e}"));
        if custom_tls.is_ok() && http_tls_ca_cert.is_some() {
            info!("Custom TLS CA certificate loaded for outgoing HTTP requests");
        }
        if http_tls_insecure_skip_verify {
            warn!("TLS certificate verification is disabled for outgoing HTTP requests");
        }
        // Always built, so every outgoing request takes the same send path.
        let http_tls_config = Arc::new(match custom_tls {
            Ok(cfg) => cfg,
            Err(_) => build_tls_client_config(None, http_tls_insecure_skip_verify)?,
        });

        Ok(Self {
            engine,
            tasks: Arc::new(Mutex::new(HashMap::new())),
            task_cancels: Arc::new(Mutex::new(HashMap::new())),
            proxy_ports: Arc::new(Mutex::new(HashMap::new())),
            proxy_cancellers: Arc::new(Mutex::new(HashMap::new())),
            precompiled: Arc::new(Mutex::new(HashMap::new())),
            hal_enabled,
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

        // Unblock any guest parked in a HAL call (e.g. tcp_accept) for this
        // task before aborting its JoinHandle; otherwise an in-process daemon
        // keeps running and holding its listener.
        if let Some(cancel) = self.task_cancels.lock().await.remove(&id) {
            cancel.notify_waiters();
        }
        // Also cancel any in-flight invocations of a latent task.
        let inv_prefix = format!("{}-inv-", id);
        let inv_keys: Vec<String> = self
            .task_cancels
            .lock()
            .await
            .keys()
            .filter(|k| k.starts_with(&inv_prefix))
            .cloned()
            .collect();
        for k in inv_keys {
            if let Some(c) = self.task_cancels.lock().await.remove(&k) {
                c.notify_waiters();
            }
        }

        let was_latent = self.precompiled.lock().await.remove(&id).is_some();

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
        // Latent tasks may have in-flight invocations running under the
        // {id}-inv-* task keys. Cancelling the latent task must cancel those
        // invocations as well, otherwise they keep executing and may still
        // publish results after the stop.
        if was_latent {
            let invocation_prefix = format!("{}-inv-", id);
            let invocations: Vec<String> = tasks
                .keys()
                .filter(|tid| tid.starts_with(&invocation_prefix))
                .cloned()
                .collect();
            for tid in invocations {
                if let Some(handle) = tasks.remove(&tid) {
                    handle.abort();
                    info!(
                        "In-flight invocation {} aborted and removed from tasks",
                        tid
                    );
                }
            }
        }
        if let Some(handle) = tasks.remove(&id) {
            handle.abort();
            info!("Task {} aborted and removed from tasks", id);
            Ok(())
        } else if was_latent {
            info!("Latent task {} removed from precompiled cache", id);
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

    async fn precompile(&self, mut config: StartConfig) -> Result<()> {
        if !is_wasm_component(&config.wasm_binary) {
            return Err(anyhow::anyhow!(
                "latent tasks require a WASM component; task {} is not a component",
                config.id
            ));
        }

        info!("Precompiling latent component for task: {}", config.id);

        let component = component::Component::from_binary(&self.engine, &config.wasm_binary)
            .map_err(|e| anyhow::anyhow!("Failed to precompile WASM component: {e}"))?;

        // A latent task is invoked repeatedly without going through configure_wasi
        // again at deploy time, so a broken policy (e.g. a storage entry naming a
        // directory that does not exist) would otherwise only surface on the first
        // invoke, and again on every invoke after that. Validate it once here so
        // precompiling fails the task immediately instead.
        if config.wasi_security.is_some() {
            let mut probe = WasiCtxBuilder::new();
            configure_wasi(
                &mut probe,
                &config.id,
                config.env.iter().map(|(k, v)| (k.as_str(), v.as_str())),
                &self.preopened_dirs,
                config.wasi_security.as_ref(),
                true,
            )?;
        }

        let is_proxy = is_proxy_component(&config.wasm_binary);
        let wasm_binary = Arc::new(std::mem::take(&mut config.wasm_binary));
        let task_id = config.id.clone();

        self.precompiled.lock().await.insert(
            task_id.clone(),
            PrecompiledComponent {
                component,
                config,
                is_proxy,
                wasm_binary,
            },
        );

        info!("Precompiled latent component cached for task: {}", task_id);

        Ok(())
    }

    async fn invoke(
        &self,
        id: String,
        args: Vec<String>,
        env: HashMap<String, String>,
    ) -> Result<Vec<u8>> {
        let (component, mut config, is_proxy, wasm_binary) = {
            let precompiled = self.precompiled.lock().await;
            match precompiled.get(&id) {
                Some(entry) => (
                    entry.component.clone(),
                    entry.config.clone(),
                    entry.is_proxy,
                    entry.wasm_binary.clone(),
                ),
                None => {
                    return Err(anyhow::anyhow!(
                        "task {id} has not been precompiled; deploy it as a latent task first"
                    ))
                }
            }
        };

        if is_proxy {
            return Err(anyhow::anyhow!(
                "task {id} is an HTTP proxy component; it is served by its listener, not by direct invocation"
            ));
        }

        for (k, v) in env {
            config.env.insert(k, v);
        }
        if !args.is_empty() {
            config.args = args;
        }
        config.daemon = false;
        config.id = format!("{id}-inv-{}", uuid::Uuid::new_v4());

        let has_custom_export = !config.function_name.is_empty()
            && config.function_name != "_start"
            && !config.function_name.starts_with("fl-round-");

        info!(
            "Invoking latent task {} as {} (custom_export={})",
            id, config.id, has_custom_export
        );

        if has_custom_export {
            self.run_component_export(config, component, wasm_binary)
                .await
        } else {
            self.run_component_command(config, component).await
        }
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

        // WASI preview1 has no sockets, so network rules cannot be honoured here.
        // Nothing is granted, which is the safe direction, but the guest will simply have no network at all.
        if config
            .wasi_security
            .as_ref()
            .is_some_and(|p| p.has_network_rules())
        {
            warn!(
                "Task {}: WASI security policy declares network rules, but core modules run on WASI preview1 which has no socket support. No network access will be granted",
                config.id
            );
        }

        let mut wasi_builder = wasmtime_wasi::WasiCtxBuilder::new();
        configure_wasi(
            &mut wasi_builder,
            &config.id,
            config.env.iter().map(|(k, v)| (k.as_str(), v.as_str())),
            &self.preopened_dirs,
            config.wasi_security.as_ref(),
            true,
        )?;

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

        self.run_component_command(config, component).await
    }

    async fn run_component_command(
        &self,
        config: StartConfig,
        component: component::Component,
    ) -> Result<Vec<u8>> {
        let stdout_pipe = Arc::new(MemoryOutputPipe::new(INVOCATION_OUTPUT_CAPACITY));
        let stderr_pipe = Arc::new(MemoryOutputPipe::new(INVOCATION_OUTPUT_CAPACITY));

        let mut wasi_builder = WasiCtxBuilder::new();

        wasi_builder
            .stdin(tokio::io::empty())
            .stdout(stdout_pipe.clone())
            .stderr(stderr_pipe.clone());

        configure_wasi(
            &mut wasi_builder,
            &config.id,
            config.env.iter().map(|(k, v)| (k.as_str(), v.as_str())),
            &self.preopened_dirs,
            config.wasi_security.as_ref(),
            false,
        )?;

        let wasi = wasi_builder.build();

        // Per-task HAL: upstream SocketInterface/StorageInterface instances
        // carry per-instance registries whose accept/read paths hold internal
        // locks across .await — sharing one instance across guest tasks would
        // let the first guest parked in a TCP accept block every other
        // guest's host calls forever.
        let task_hal = self
            .hal_enabled
            .then(|| PropletHal::with_storage_path(task_hal_storage_path(&config)));
        let task_cancel = Arc::new(Notify::new());
        self.task_cancels
            .lock()
            .await
            .insert(config.id.clone(), task_cancel.clone());
        let storage = self.init_task_storage(&config).await;
        let store_data = StoreData {
            wasi,
            http: WasiHttpCtx::new(),
            usb: WasiUsbCtx::new(false),
            table: ResourceTable::new(),
            hal: task_hal,
            storage,
            cancel: task_cancel,
            http_hooks: CustomTlsHttpHooks {
                tls_config: self.http_tls_config.clone(),
                policy: config.wasi_security.clone().map(Arc::new),
            },
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
        let task_cancels = self.task_cancels.clone();
        let stdout_for_result = stdout_pipe.clone();
        let stderr_for_result = stderr_pipe.clone();

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
                        let mut output = stdout_for_result.contents().to_vec();
                        let stderr_bytes = stderr_for_result.contents();
                        if !stderr_bytes.is_empty() {
                            output.extend_from_slice(b"\n");
                            output.extend_from_slice(&stderr_bytes);
                        }
                        Ok::<Vec<u8>, anyhow::Error>(output)
                    }
                    Err(()) => Err(anyhow::anyhow!(
                        "Component for task {} exited with error",
                        task_id
                    )),
                }
            }
            .await;

            tasks.lock().await.remove(&task_id_for_cleanup);
            task_cancels.lock().await.remove(&task_id_for_cleanup);

            let _ = result_tx.send(result);
        });

        {
            let mut tasks_map = self.tasks.lock().await;
            tasks_map.insert(config.id.clone(), handle);
        }

        if config.daemon {
            info!(
                "Daemon component task {} started, returning immediately",
                config.id
            );
            Ok(Vec::new())
        } else {
            let result = match result_rx.await {
                Ok(result) => result,
                Err(_) => Err(anyhow::anyhow!("Task was cancelled or panicked")),
            };
            self.tasks.lock().await.remove(&config.id);
            result
        }
    }

    async fn start_app_component_export(&self, mut config: StartConfig) -> Result<Vec<u8>> {
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

        let wasm_binary = Arc::new(std::mem::take(&mut config.wasm_binary));

        self.run_component_export(config, component, wasm_binary)
            .await
    }

    async fn run_component_export(
        &self,
        config: StartConfig,
        component: component::Component,
        wasm_binary: Arc<Vec<u8>>,
    ) -> Result<Vec<u8>> {
        let mut wasi_builder = WasiCtxBuilder::new();
        configure_wasi(
            &mut wasi_builder,
            &config.id,
            config.env.iter().map(|(k, v)| (k.as_str(), v.as_str())),
            &self.preopened_dirs,
            config.wasi_security.as_ref(),
            true,
        )?;

        let wasi = wasi_builder.build();

        // Per-task HAL: upstream SocketInterface/StorageInterface instances
        // carry per-instance registries whose accept/read paths hold internal
        // locks across .await — sharing one instance across guest tasks would
        // let the first guest parked in a TCP accept block every other
        // guest's host calls forever.
        let task_hal = self
            .hal_enabled
            .then(|| PropletHal::with_storage_path(task_hal_storage_path(&config)));
        let task_cancel = Arc::new(Notify::new());
        self.task_cancels
            .lock()
            .await
            .insert(config.id.clone(), task_cancel.clone());
        let storage = self.init_task_storage(&config).await;
        let store_data = StoreData {
            wasi,
            http: WasiHttpCtx::new(),
            usb: WasiUsbCtx::new(false),
            table: ResourceTable::new(),
            hal: task_hal,
            storage,
            cancel: task_cancel,
            http_hooks: CustomTlsHttpHooks {
                tls_config: self.http_tls_config.clone(),
                policy: config.wasi_security.clone().map(Arc::new),
            },
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
        let task_cancels = self.task_cancels.clone();

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
            task_cancels.lock().await.remove(&task_id_for_cleanup);

            let _ = result_tx.send(result);
        });

        {
            let mut tasks_map = self.tasks.lock().await;
            tasks_map.insert(config.id.clone(), handle);
        }

        if config.daemon {
            info!(
                "Daemon component export task {} started, returning immediately",
                config.id
            );
            Ok(Vec::new())
        } else {
            let result = match result_rx.await {
                Ok(result) => result,
                Err(_) => Err(anyhow::anyhow!("Task was cancelled or panicked")),
            };
            self.tasks.lock().await.remove(&config.id);
            result
        }
    }

    async fn start_app_proxy(&self, config: StartConfig) -> Result<Vec<u8>> {
        if !self.http_enabled {
            return Err(anyhow::anyhow!(
                "HTTP proxy is disabled. Enable it by setting http_enabled=true in configuration"
            ));
        }

        let preferred_port = config
            .cli_args
            .windows(2)
            .find(|w| w[0] == "--port" || w[0] == "--addr")
            .and_then(|w| w[1].split(':').next_back()?.parse().ok())
            .or_else(|| {
                config
                    .cli_args
                    .iter()
                    .find_map(|a| a.strip_prefix("--port=").or(a.strip_prefix("--addr=")))
                    .and_then(|a| a.split(':').next_back()?.parse().ok())
            });

        let policy = config.wasi_security.clone().map(Arc::new);

        // Bind the port up front and keep it claimed to eliminate the TOCTOU
        // window between probing and the real bind. When the user explicitly
        // provided --port/--addr, use that port directly and fail if busy;
        // otherwise fall back to auto-probing.
        let (socket, port) = match preferred_port {
            Some(p) => {
                let addr: SocketAddr = ([0, 0, 0, 0], p).into();
                if !proxy_bind_allowed(policy.as_deref(), addr) {
                    return Err(anyhow::anyhow!(
                        "wasi_security: task {} is not granted bind/listen on {addr} \
                         (add it to the policy's [network] bind list)",
                        config.id
                    ));
                }
                let socket = Socket::new(Domain::IPV4, Type::STREAM, Some(Protocol::TCP))?;
                socket.set_reuse_address(true)?;
                socket
                    .bind(&addr.into())
                    .with_context(|| format!("Failed to bind HTTP proxy port {addr}"))?;
                (socket, p)
            }
            None => find_available_port(self.proxy_port, policy.as_deref())?,
        };

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

        // Each connection builds its own WASI context, so validate the policy once up front.
        // Otherwise a broken policy would start the proxy and then fail every request instead of failing the task.
        if policy.is_some() {
            let mut probe = WasiCtxBuilder::new();
            configure_wasi(
                &mut probe,
                &config.id,
                env.iter().map(|(k, v)| (k.as_str(), v.as_str())),
                &preopened_dirs,
                policy.as_deref(),
                true,
            )?;
        }

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
                }
                drop(tasks_map);

                // The socket from find_available_port is already bound,
                // confirming the port is free.  The old task's entry was
                // stale — no rebind needed.
            }

            socket.set_nonblocking(true)?;
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
                let conn_policy = policy.clone();
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
                                let req_policy = conn_policy.clone();
                                let task_id_req = task_id_conn.clone();
                                let tls_cfg = tls_config.clone();
                                async move {
                                    handle_proxy_request(
                                        pre,
                                        env,
                                        dirs,
                                        req_policy,
                                        req,
                                        task_id_req,
                                        tls_cfg,
                                    )
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

        Ok(format!("started at port {port}").into_bytes())
    }

    async fn run_core_instance(
        &self,
        config: StartConfig,
        mut store: Store<wasmtime_wasi::p1::WasiP1Ctx>,
        instance: Instance,
    ) -> Result<Vec<u8>> {
        let task_id = config.id.clone();
        let task_id_for_cleanup = task_id.clone();
        let function_name = config.function_name.clone();
        let args = config.args.clone();
        let tasks = self.tasks.clone();
        let task_cancels = self.task_cancels.clone();

        let (result_tx, result_rx) = tokio::sync::oneshot::channel::<Result<Vec<u8>>>();

        let handle = tokio::task::spawn(async move {
            let task_id_for_blocking = task_id.clone();
            let result = tokio::task::spawn_blocking(move || {
                if let Some(init_func) = instance.get_func(&mut store, "_initialize") {
                    info!(
                        "Found _initialize function, initializing WASM runtime for task: {}",
                        task_id_for_blocking
                    );
                    init_func
                        .call(&mut store, &[], &mut [])
                        .map_err(|e| anyhow::anyhow!("Failed to initialize WASM runtime via _initialize: {e}"))?;
                    info!(
                        "WASM runtime initialized successfully for task: {}",
                        task_id_for_blocking
                    );
                } else {
                    info!(
                        "No _initialize function found, skipping initialization for task: {}",
                        task_id_for_blocking
                    );
                }

                let exports: Vec<String> = instance
                    .exports(&mut store)
                    .map(|export: Export<'_>| export.name().to_string())
                    .collect();
                info!(
                    "WASM module exports for task {}: requested='{}', available={:?}",
                    task_id_for_blocking, function_name, exports
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
                    task_id_for_blocking,
                    result_bytes.len()
                );

                Ok::<Vec<u8>, anyhow::Error>(result_bytes)
            })
            .await;

            tasks.lock().await.remove(&task_id_for_cleanup);
            task_cancels.lock().await.remove(&task_id_for_cleanup);

            match result {
                Ok(Ok(data)) => {
                    info!(
                        "Task {} completed, result size: {} bytes",
                        task_id,
                        data.len()
                    );
                    let _ = result_tx.send(Ok(data));
                }
                Ok(Err(e)) => {
                    error!("Task {} failed: {}", task_id, e);
                    let _ = result_tx.send(Err(e));
                }
                Err(e) => {
                    error!("Task {} join error: {}", task_id, e);
                    let _ = result_tx.send(Err(anyhow::anyhow!("join error: {}", e)));
                }
            }
        });

        {
            let mut tasks_map = self.tasks.lock().await;
            tasks_map.insert(config.id.clone(), handle);
        }

        if config.daemon {
            info!("Daemon task {} started, returning immediately", config.id);
            Ok(Vec::new())
        } else {
            info!(
                "Running in synchronous mode, waiting for task: {}",
                config.id
            );
            let result = match result_rx.await {
                Ok(result) => result,
                Err(_) => Err(anyhow::anyhow!("Task was cancelled or panicked")),
            };
            self.tasks.lock().await.remove(&config.id);
            result
        }
    }
}

/// Handle one HTTP request by instantiating the proxy component and calling
/// `wasi:http/incoming-handler.handle`.
async fn handle_proxy_request(
    pre: Arc<ProxyPre<StoreData>>,
    env: Arc<Vec<(String, String)>>,
    preopened_dirs: Vec<String>,
    policy: Option<Arc<WasiSecurity>>,
    req: hyper::Request<hyper::body::Incoming>,
    task_id: String,
    http_tls_config: Arc<rustls::ClientConfig>,
) -> Result<hyper::Response<HyperOutgoingBody>> {
    let mut wasi_builder = WasiCtxBuilder::new();
    configure_wasi(
        &mut wasi_builder,
        &task_id,
        env.iter().map(|(k, v)| (k.as_str(), v.as_str())),
        &preopened_dirs,
        policy.as_deref(),
        true,
    )?;

    let store_data = StoreData {
        wasi: wasi_builder.build(),
        http: WasiHttpCtx::new(),
        usb: WasiUsbCtx::new(false),
        table: ResourceTable::new(),
        hal: None,
        storage: None,
        cancel: Arc::new(Notify::new()),
        http_hooks: CustomTlsHttpHooks {
            tls_config: http_tls_config,
            policy,
        },
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

    fn policy_from_toml(toml: &str) -> WasiSecurity {
        WasiSecurity::from_toml(toml).unwrap()
    }

    #[test]
    fn proxy_bind_allowed_needs_a_bind_grant_for_that_port() {
        let addr: SocketAddr = ([0, 0, 0, 0], 8081).into();
        let other: SocketAddr = ([0, 0, 0, 0], 8082).into();
        let bind = policy_from_toml("[network]\nbind = [\"tcp://0.0.0.0:8081\"]\n");
        assert!(proxy_bind_allowed(Some(&bind), addr));
        assert!(!proxy_bind_allowed(Some(&bind), other));
        assert!(!proxy_bind_allowed(Some(&policy_from_toml("")), addr));
        // A connect grant is not a bind grant, despite the implicit-bind allowance.
        let connect = policy_from_toml("[network]\nconnect = [\"tcp://0.0.0.0:8081\"]\n");
        assert!(!proxy_bind_allowed(Some(&connect), addr));
        assert!(proxy_bind_allowed(None, addr));
    }

    #[tokio::test]
    async fn enforce_http_egress_only_returns_granted_targets() {
        let uri: hyper::Uri = "http://127.0.0.1:8081/message".parse().unwrap();
        let other: hyper::Uri = "http://127.0.0.1:9999/message".parse().unwrap();
        let addr = SocketAddr::from(([127, 0, 0, 1], 8081));

        let connect = policy_from_toml("[network]\nconnect = [\"tcp://127.0.0.1:8081\"]\n");
        let allowed = enforce_http_egress(&uri, Some(&connect)).await.unwrap();
        assert_eq!(allowed, Some(vec![addr]));
        assert!(enforce_http_egress(&other, Some(&connect)).await.is_err());

        let bind = policy_from_toml("[network]\nbind = [\"tcp://127.0.0.1:8081\"]\n");
        assert!(enforce_http_egress(&uri, Some(&bind)).await.is_err());
        assert_eq!(enforce_http_egress(&uri, None).await.unwrap(), None);

        let bare: hyper::Uri = "http://127.0.0.1/message".parse().unwrap();
        assert_eq!(request_authority(&bare, false).unwrap(), "127.0.0.1:80");
        assert_eq!(request_authority(&bare, true).unwrap(), "127.0.0.1:443");
    }

    #[tokio::test]
    async fn enforce_http_egress_needs_allow_ip_name_lookup_for_a_name() {
        let rule = "connect = [\"tcp://0.0.0.0:8081\"]\n";
        let named: hyper::Uri = "http://localhost:8081/message".parse().unwrap();
        let literal: hyper::Uri = "http://127.0.0.1:8081/message".parse().unwrap();

        // An address target resolves no name, so the flag does not apply to it.
        let denied = policy_from_toml(&format!("[network]\n{rule}"));
        assert!(enforce_http_egress(&literal, Some(&denied)).await.is_ok());
        assert!(enforce_http_egress(&named, Some(&denied)).await.is_err());

        // With the flag the name is resolved, and the result still goes through the grants.
        let allowed = policy_from_toml(&format!("[network]\nallow_ip_name_lookup = true\n{rule}"));
        assert!(enforce_http_egress(&named, Some(&allowed)).await.is_ok());
    }

    #[test]
    fn ip_literal_detection_unwraps_ipv6_brackets() {
        assert!(is_ip_literal("[::1]"));
        assert!(is_ip_literal("127.0.0.1"));
        assert!(!is_ip_literal("batch.elastic.local"));
        assert!(!is_ip_literal("localhost"));
    }

    #[test]
    fn find_available_port_skips_ports_the_policy_does_not_grant() {
        let policy = policy_from_toml("[network]\nbind = [\"tcp://0.0.0.0:34503\"]\n");
        let (_socket, port) = find_available_port(34501, Some(&policy)).unwrap();
        assert_eq!(port, 34503);

        let policy = policy_from_toml("[network]\nbind = [\"tcp://0.0.0.0:1\"]\n");
        assert!(find_available_port(34501, Some(&policy)).is_err());
    }

    #[test]
    fn configure_wasi_without_policy_grants_global_preopens() {
        let dir = std::env::temp_dir();
        let global = vec![dir.to_string_lossy().into_owned()];

        let mut builder = WasiCtxBuilder::new();
        configure_wasi(
            &mut builder,
            "task-1",
            [("KEY", "value")],
            &global,
            None,
            true,
        )
        .unwrap();

        // Building succeeds, which means every preopen resolved.
        let _ = builder.build();
    }

    /// `WasiCtxBuilder::env` appends without deduplicating, so a key the policy also sets
    /// must replace the task's value rather than adding a second entry for it.
    #[test]
    fn merge_env_lets_policy_override_task_env() {
        let policy_env = HashMap::from([
            ("SHARED".to_string(), "from-policy".to_string()),
            ("POLICY_ONLY".to_string(), "policy".to_string()),
        ]);

        let mut merged = merge_env(
            [
                ("SHARED", "from-task"),
                ("TASK_ONLY", "task"),
                ("DUP", "first"),
                ("DUP", "second"),
            ],
            Some(&policy_env),
        );
        merged.sort();

        assert_eq!(
            merged,
            vec![
                ("DUP", "second"),
                ("POLICY_ONLY", "policy"),
                ("SHARED", "from-policy"),
                ("TASK_ONLY", "task"),
            ]
        );
    }

    /// A policy naming a directory that does not exist must fail the task
    /// rather than silently running with fewer capabilities than requested.
    #[test]
    fn configure_wasi_fails_when_policy_dir_is_missing() {
        let policy = policy_from_toml(
            r#"
            [storage]
            mount = ["/definitely/not/a/real/path::/data"]
            "#,
        );

        let mut builder = WasiCtxBuilder::new();
        let result = configure_wasi(&mut builder, "task-1", [], &[], Some(&policy), true);

        assert!(result.is_err());
    }

    /// A policy replaces the global preopens, so a bad global entry that would
    /// normally only warn is not consulted at all.
    #[test]
    fn configure_wasi_with_policy_ignores_global_preopens() {
        let global = vec!["/definitely/not/a/real/path".to_string()];
        let policy = policy_from_toml("[network]\nconnect = [\"tcp://127.0.0.1:9000\"]\n");

        let mut builder = WasiCtxBuilder::new();
        configure_wasi(&mut builder, "task-1", [], &global, Some(&policy), true).unwrap();

        let _ = builder.build();
    }

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

    #[test]
    fn test_preserve_host_header_adds_host_from_authority() {
        let request = hyper::Request::builder()
            .uri("https://example.local/path")
            .body(HyperOutgoingBody::default())
            .unwrap();
        let mut request = request;
        assert!(!request.headers().contains_key(hyper::http::header::HOST));

        preserve_host_header(&mut request);

        assert_eq!(
            request
                .headers()
                .get(hyper::http::header::HOST)
                .and_then(|v| v.to_str().ok()),
            Some("example.local")
        );
    }

    #[test]
    fn test_preserve_host_header_does_not_override_existing() {
        let request = hyper::Request::builder()
            .uri("https://example.local/path")
            .header(hyper::http::header::HOST, "custom-host")
            .body(HyperOutgoingBody::default())
            .unwrap();
        let mut request = request;

        preserve_host_header(&mut request);

        assert_eq!(
            request
                .headers()
                .get(hyper::http::header::HOST)
                .and_then(|v| v.to_str().ok()),
            Some("custom-host")
        );
    }

    #[test]
    fn test_preserve_host_header_keeps_port_in_authority() {
        let request = hyper::Request::builder()
            .uri("https://example.local:8443/path")
            .body(HyperOutgoingBody::default())
            .unwrap();
        let mut request = request;

        preserve_host_header(&mut request);

        assert_eq!(
            request
                .headers()
                .get(hyper::http::header::HOST)
                .and_then(|v| v.to_str().ok()),
            Some("example.local:8443")
        );
    }

    #[test]
    fn test_preserve_host_header_skips_request_without_authority() {
        let request = hyper::Request::builder()
            .uri("/path")
            .body(HyperOutgoingBody::default())
            .unwrap();
        let mut request = request;

        preserve_host_header(&mut request);

        assert!(!request.headers().contains_key(hyper::http::header::HOST));
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

        let runtime =
            WasmtimeRuntime::new_with_options(false, false, false, Vec::new(), 8222, None, false)
                .unwrap();
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
            wasi_security: None,
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

    fn latent_config(id: &str, function_name: &str, wasm_binary: Vec<u8>) -> StartConfig {
        StartConfig {
            id: id.to_string(),
            function_name: function_name.to_string(),
            daemon: false,
            wasm_binary,
            cli_args: Vec::new(),
            env: HashMap::new(),
            args: Vec::new(),
            mode: None,
            hal_storage_path: None,
            wasi_security: None,
        }
    }

    fn greet_component() -> Option<Vec<u8>> {
        let wasm_path = concat!(
            env!("CARGO_MANIFEST_DIR"),
            "/../examples/greet-component/target/wasm32-wasip2/release/greet_component.wasm"
        );
        match std::fs::read(wasm_path) {
            Ok(b) => Some(b),
            Err(e) => {
                eprintln!(
                    "Skipping: WASM binary not found at {wasm_path}: {e}. \
                     Build it with: make greet-component"
                );
                None
            }
        }
    }

    #[tokio::test]
    async fn test_precompile_rejects_non_component() {
        let runtime =
            WasmtimeRuntime::new_with_options(false, false, false, Vec::new(), 8222, None, false)
                .unwrap();
        let config = latent_config(
            &uuid::Uuid::new_v4().to_string(),
            "run",
            vec![0xFF, 0xFF, 0xFF, 0xFF],
        );
        assert!(runtime.precompile(config).await.is_err());
    }

    /// A policy naming a directory that does not exist must fail at precompile
    /// time, not on the first (or every) subsequent invoke.
    #[tokio::test]
    async fn test_precompile_rejects_bad_wasi_security_policy() {
        let Some(wasm_binary) = greet_component() else {
            return;
        };
        let runtime =
            WasmtimeRuntime::new_with_options(false, false, false, Vec::new(), 8222, None, false)
                .unwrap();

        let mut config = latent_config(&uuid::Uuid::new_v4().to_string(), "greet", wasm_binary);
        config.wasi_security = Some(
            WasiSecurity::from_toml(
                r#"
                [storage]
                mount = ["/definitely/not/a/real/path::/data"]
                "#,
            )
            .unwrap(),
        );

        let result = runtime.precompile(config).await;
        assert!(
            result.is_err(),
            "precompile should reject a policy whose storage entry cannot be preopened"
        );
    }

    #[tokio::test]
    async fn test_invoke_unknown_task_errors() {
        let runtime =
            WasmtimeRuntime::new_with_options(false, false, false, Vec::new(), 8222, None, false)
                .unwrap();
        let result = runtime
            .invoke("does-not-exist".to_string(), Vec::new(), HashMap::new())
            .await;
        assert!(result.is_err());
    }

    #[tokio::test]
    async fn test_precompile_then_invoke_component_export() {
        let Some(wasm_binary) = greet_component() else {
            return;
        };
        let runtime =
            WasmtimeRuntime::new_with_options(false, false, false, Vec::new(), 8222, None, false)
                .unwrap();
        let task_id = uuid::Uuid::new_v4().to_string();

        runtime
            .precompile(latent_config(&task_id, "greet", wasm_binary))
            .await
            .expect("precompile should succeed");

        let output = runtime
            .invoke(
                task_id.clone(),
                vec!["\"world\"".to_string()],
                HashMap::new(),
            )
            .await
            .expect("invoke should succeed");
        assert!(
            String::from_utf8_lossy(&output).contains("Hello, world"),
            "unexpected invoke output: {}",
            String::from_utf8_lossy(&output)
        );

        let output2 = runtime
            .invoke(task_id, vec!["\"again\"".to_string()], HashMap::new())
            .await
            .expect("second invoke should succeed");
        assert!(String::from_utf8_lossy(&output2).contains("Hello, again"));
    }

    #[tokio::test]
    async fn test_concurrent_invocations_run_in_parallel() {
        let Some(wasm_binary) = greet_component() else {
            return;
        };
        let runtime = Arc::new(
            WasmtimeRuntime::new_with_options(false, false, false, Vec::new(), 8222, None, false)
                .unwrap(),
        );
        let task_id = uuid::Uuid::new_v4().to_string();

        runtime
            .precompile(latent_config(&task_id, "greet", wasm_binary))
            .await
            .expect("precompile should succeed");

        let mut handles = Vec::new();
        for i in 0..8 {
            let runtime = runtime.clone();
            let task_id = task_id.clone();
            handles.push(tokio::spawn(async move {
                runtime
                    .invoke(task_id, vec![format!("\"caller-{i}\"")], HashMap::new())
                    .await
            }));
        }

        for (i, handle) in handles.into_iter().enumerate() {
            let output = handle
                .await
                .expect("task join")
                .expect("invoke should succeed");
            assert!(
                String::from_utf8_lossy(&output).contains(&format!("Hello, caller-{i}")),
                "invocation {i} returned unexpected output: {}",
                String::from_utf8_lossy(&output)
            );
        }
    }
}
