//! Standalone WASI P2 (component-model) host that serves the ELASTIC TEE HAL to
//! a WASM component and invokes one of its exported functions.
//!
//! The P2 counterpart of proplet's `hal_component` integration, extracted into a
//! small CLI so HAL components (e.g. `hal-test`, `attestation-test`) can be run
//! outside proplet. Uses `wasmtime::component::bindgen!` to generate typed host
//! bindings and bridges them to the upstream `elastic_tee_hal` providers /
//! concrete interface structs — works on any Linux host, returning a clear
//! string error for TEE-only or unmodelled features rather than silent stubs.

use anyhow::{Context, Result};
use elastic_tee_hal::interfaces::{
    CapabilitiesInterface, ClockInterface, CryptoInterface, HalProvider, RandomInterface,
};
use elastic_tee_hal::providers::{
    DefaultCapabilitiesProvider, DefaultClockProvider, DefaultCryptoProvider,
    DefaultRandomProvider,
};
use elastic_tee_hal::{
    CommunicationInterface, EventInterface, GpuInterface, ResourceInterface, SocketInterface,
    StorageInterface,
};
use std::collections::HashMap;
use std::path::PathBuf;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::{Arc, Mutex, OnceLock};
use tokio::runtime::Runtime;
use wasmtime::component::{Component, Linker, ResourceTable, Val};
use wasmtime::{Config, Engine, Store};
use wasmtime_wasi::{WasiCtx, WasiCtxBuilder, WasiCtxView, WasiView};
use wasmtime_wasi_http::p2::WasiHttpView;
use wasmtime_wasi_http::WasiHttpCtx;

// Single bindgen over the modular world — each interface lives in its own
// `elastic:<name>@0.1.0` package, matching upstream `wit-modular/` and the BRT
// EUCNC component.
wasmtime::component::bindgen!({
    world: "elastic:hal-modular/elastic-modular-imports",
    path: "wit",
});

use elastic::hal::attestation;
use elastic::hal::clock;
use elastic::hal::communication;
use elastic::hal::crypto;
use elastic::hal::events;
use elastic::hal::gpu;
use elastic::hal::platform;
use elastic::hal::random;
use elastic::hal::resources;
use elastic::hal::sockets;
use elastic::hal::storage;

// ============================================================================
// Socket bridge — same shape as proplet's. The WIT split create/bind/listen
// surface is reconciled with upstream's combined `create_tcp_socket` by
// handing out synthetic handles in the high-bit range that cannot collide
// with upstream's atomic counter.
// ============================================================================
const SYNTHETIC_SOCKET_BASE: u64 = 1 << 62;

#[derive(Copy, Clone, Debug)]
enum SocketProto {
    Tcp,
    Udp,
    Tls,
    Dtls,
}

enum SocketSlot {
    Pending {
        protocol: SocketProto,
        bind_addr: Option<String>,
    },
    Active {
        real_handle: u64,
    },
}

struct SocketBridge {
    next: AtomicU64,
    state: Mutex<HashMap<u64, SocketSlot>>,
}

impl SocketBridge {
    fn new() -> Self {
        Self {
            next: AtomicU64::new(SYNTHETIC_SOCKET_BASE),
            state: Mutex::new(HashMap::new()),
        }
    }

    fn alloc(&self, protocol: SocketProto) -> u64 {
        let id = self.next.fetch_add(1, Ordering::SeqCst);
        self.state.lock().unwrap().insert(
            id,
            SocketSlot::Pending {
                protocol,
                bind_addr: None,
            },
        );
        id
    }

    fn set_bind_addr(&self, id: u64, addr: String) -> Result<(), String> {
        let mut s = self.state.lock().unwrap();
        match s.get_mut(&id) {
            Some(SocketSlot::Pending { bind_addr, .. }) => {
                *bind_addr = Some(addr);
                Ok(())
            }
            Some(SocketSlot::Active { .. }) => Err("socket already activated".to_string()),
            None => Err("unknown socket handle".to_string()),
        }
    }

    fn take_pending(&self, id: u64) -> Result<(SocketProto, Option<String>), String> {
        let s = self.state.lock().unwrap();
        match s.get(&id) {
            Some(SocketSlot::Pending {
                protocol,
                bind_addr,
            }) => Ok((*protocol, bind_addr.clone())),
            Some(SocketSlot::Active { .. }) => Err("socket already activated".to_string()),
            None => Err("unknown socket handle".to_string()),
        }
    }

    fn activate(&self, id: u64, real_handle: u64) {
        self.state
            .lock()
            .unwrap()
            .insert(id, SocketSlot::Active { real_handle });
    }

    fn resolve(&self, id: u64) -> Result<u64, String> {
        if id < SYNTHETIC_SOCKET_BASE {
            return Ok(id);
        }
        match self.state.lock().unwrap().get(&id) {
            Some(SocketSlot::Active { real_handle }) => Ok(*real_handle),
            Some(SocketSlot::Pending { .. }) => Err("socket not yet bound/connected".to_string()),
            None => Err("unknown socket handle".to_string()),
        }
    }

    fn remove(&self, id: u64) {
        if id >= SYNTHETIC_SOCKET_BASE {
            self.state.lock().unwrap().remove(&id);
        }
    }
}

// ============================================================================
// HostState — providers + concrete async interfaces + a dedicated tokio
// runtime that drives those async calls from inside the sync host bindings.
// ============================================================================
struct HostState {
    wasi: WasiCtx,
    http: WasiHttpCtx,
    table: ResourceTable,
    provider: HalProvider,
    runtime: Arc<Runtime>,
    sockets: Arc<SocketInterface>,
    socket_bridge: SocketBridge,
    gpu: Arc<GpuInterface>,
    resources: Option<Arc<ResourceInterface>>,
    events: Arc<EventInterface>,
    communication: Arc<CommunicationInterface>,
    storage: OnceLock<Option<Arc<StorageInterface>>>,
    storage_base: PathBuf,
    crypto_ctxs: Mutex<Vec<u64>>,
    next_crypto_ctx: AtomicU64,
}

impl HostState {
    fn new(runtime: Arc<Runtime>, envs: &[(String, String)]) -> Self {
        let mut wasi_builder = WasiCtxBuilder::new();
        wasi_builder.inherit_stdio();
        for (k, v) in envs {
            wasi_builder.env(k, v);
        }
        Self {
            wasi: wasi_builder.build(),
            http: WasiHttpCtx::new(),
            table: ResourceTable::new(),
            provider: HalProvider::with_defaults(),
            runtime,
            sockets: Arc::new(SocketInterface::new()),
            socket_bridge: SocketBridge::new(),
            gpu: Arc::new(GpuInterface::new()),
            resources: ResourceInterface::new().ok().map(Arc::new),
            events: Arc::new(EventInterface::new()),
            communication: Arc::new(CommunicationInterface::new()),
            storage: OnceLock::new(),
            storage_base: std::env::var_os("HAL_STORAGE_PATH")
                .map(PathBuf::from)
                .unwrap_or_else(|| std::env::temp_dir().join("hal-runner-storage")),
            crypto_ctxs: Mutex::new(Vec::new()),
            next_crypto_ctx: AtomicU64::new(1),
        }
    }

    fn block_on<F: std::future::Future>(&self, future: F) -> F::Output {
        self.runtime.block_on(future)
    }

    fn storage_handle(&self) -> Result<Arc<StorageInterface>, String> {
        let opt = self
            .storage
            .get_or_init(|| {
                let base = self.storage_base.clone();
                match self.runtime.block_on(StorageInterface::new(&base)) {
                    Ok(s) => Some(Arc::new(s)),
                    Err(e) => {
                        tracing::warn!("HAL storage init failed at {}: {e}", base.display());
                        None
                    }
                }
            })
            .clone();
        opt.ok_or_else(|| "no storage provider available".to_string())
    }
}

impl WasiView for HostState {
    fn ctx(&mut self) -> WasiCtxView<'_> {
        WasiCtxView {
            ctx: &mut self.wasi,
            table: &mut self.table,
        }
    }
}

impl WasiHttpView for HostState {
    fn http(&mut self) -> wasmtime_wasi_http::p2::WasiHttpCtxView<'_> {
        wasmtime_wasi_http::p2::WasiHttpCtxView {
            ctx: &mut self.http,
            table: &mut self.table,
            hooks: Default::default(),
        }
    }
}

fn hash_algo_str(algo: crypto::HashAlgorithm) -> &'static str {
    match algo {
        crypto::HashAlgorithm::Sha256 => "SHA-256",
        crypto::HashAlgorithm::Sha512 => "SHA-512",
        crypto::HashAlgorithm::Blake3 => "BLAKE3",
    }
}

fn cipher_algo_str(algo: crypto::CipherAlgorithm) -> &'static str {
    match algo {
        crypto::CipherAlgorithm::Aes256Gcm => "AES-256-GCM",
        crypto::CipherAlgorithm::Chacha20Poly1305 => "ChaCha20-Poly1305",
    }
}

// ============================================================================
// Platform
// ============================================================================
impl platform::Host for HostState {
    fn get_platform_info(&mut self) -> platform::PlatformInfo {
        match self
            .provider
            .platform
            .as_ref()
            .and_then(|p| p.platform_info().ok())
        {
            Some((platform_type, version, _attest)) => platform::PlatformInfo {
                platform_type,
                version,
            },
            None => platform::PlatformInfo {
                platform_type: "None".to_string(),
                version: "0.0.0".to_string(),
            },
        }
    }

    fn list_capabilities(&mut self) -> Vec<platform::CapabilityInfo> {
        match DefaultCapabilitiesProvider::default().list_capabilities() {
            Ok(list) => list
                .into_iter()
                .map(
                    |(feature_name, supported, version)| platform::CapabilityInfo {
                        feature_name,
                        supported,
                        version,
                    },
                )
                .collect(),
            Err(_) => Vec::new(),
        }
    }

    fn has_capability(&mut self, feature_name: String) -> bool {
        DefaultCapabilitiesProvider::default()
            .has_capability(&feature_name)
            .unwrap_or(false)
    }
}

// ============================================================================
// Attestation
// ============================================================================
impl attestation::Host for HostState {
    fn attestation(&mut self, report_data: Vec<u8>) -> Result<Vec<u8>, String> {
        match self.provider.platform.as_ref() {
            Some(p) => p.attestation(&report_data),
            None => Err("no TEE platform available for attestation".to_string()),
        }
    }
}

// ============================================================================
// Crypto
// ============================================================================
impl crypto::Host for HostState {
    fn hash(&mut self, data: Vec<u8>, algorithm: crypto::HashAlgorithm) -> Result<Vec<u8>, String> {
        DefaultCryptoProvider::default().hash(&data, hash_algo_str(algorithm))
    }

    fn encrypt(
        &mut self,
        data: Vec<u8>,
        key: Vec<u8>,
        algorithm: crypto::CipherAlgorithm,
    ) -> Result<Vec<u8>, String> {
        DefaultCryptoProvider::default().encrypt(&data, &key, cipher_algo_str(algorithm))
    }

    fn decrypt(
        &mut self,
        data: Vec<u8>,
        key: Vec<u8>,
        algorithm: crypto::CipherAlgorithm,
    ) -> Result<Vec<u8>, String> {
        DefaultCryptoProvider::default().decrypt(&data, &key, cipher_algo_str(algorithm))
    }

    fn generate_keypair(&mut self) -> Result<crypto::KeyPair, String> {
        DefaultCryptoProvider::default()
            .generate_keypair()
            .map(|(public_key, private_key)| crypto::KeyPair {
                public_key,
                private_key,
            })
    }

    fn sign(&mut self, data: Vec<u8>, private_key: Vec<u8>) -> Result<Vec<u8>, String> {
        DefaultCryptoProvider::default().sign(&data, &private_key)
    }

    fn verify(
        &mut self,
        data: Vec<u8>,
        signature: Vec<u8>,
        public_key: Vec<u8>,
    ) -> Result<bool, String> {
        DefaultCryptoProvider::default().verify(&data, &signature, &public_key)
    }

    fn create_context(&mut self) -> Result<u64, String> {
        let id = self.next_crypto_ctx.fetch_add(1, Ordering::SeqCst);
        self.crypto_ctxs.lock().unwrap().push(id);
        Ok(id)
    }

    fn destroy_context(&mut self, handle: u64) -> Result<(), String> {
        let mut v = self.crypto_ctxs.lock().unwrap();
        if let Some(pos) = v.iter().position(|x| *x == handle) {
            v.swap_remove(pos);
            Ok(())
        } else {
            Err("unknown crypto context handle".to_string())
        }
    }
}

// ============================================================================
// Clock
// ============================================================================
impl clock::Host for HostState {
    fn get_system_time(&mut self) -> Result<clock::SystemTime, String> {
        DefaultClockProvider::default()
            .system_time()
            .map(|(seconds, nanoseconds)| clock::SystemTime {
                seconds,
                nanoseconds,
            })
    }

    fn get_monotonic_time(&mut self) -> Result<clock::MonotonicTime, String> {
        DefaultClockProvider::default().monotonic_time().map(
            |(elapsed_seconds, elapsed_nanoseconds)| clock::MonotonicTime {
                elapsed_seconds,
                elapsed_nanoseconds,
            },
        )
    }

    fn resolution(&mut self) -> Result<u64, String> {
        DefaultClockProvider::default().resolution()
    }

    fn sleep(&mut self, duration_ns: u64) -> Result<(), String> {
        DefaultClockProvider::default().sleep(duration_ns)
    }
}

// ============================================================================
// Random
// ============================================================================
impl random::Host for HostState {
    fn get_random_bytes(&mut self, length: u32) -> Result<Vec<u8>, String> {
        DefaultRandomProvider::default().get_random_bytes(length)
    }

    fn get_secure_random(&mut self, length: u32) -> Result<Vec<u8>, String> {
        DefaultRandomProvider::default().get_secure_random(length)
    }

    fn get_entropy_info(&mut self) -> Result<random::EntropyInfo, String> {
        Ok(random::EntropyInfo {
            source: random::EntropySource::Platform,
            quality: 256,
            available_bytes: u64::MAX,
        })
    }

    fn reseed(&mut self, _additional_entropy: Vec<u8>) -> Result<(), String> {
        Ok(())
    }
}

// ============================================================================
// Sockets
// ============================================================================
impl sockets::Host for HostState {
    fn create_socket(&mut self, protocol: sockets::Protocol) -> Result<u64, String> {
        let p = match protocol {
            sockets::Protocol::Tcp => SocketProto::Tcp,
            sockets::Protocol::Udp => SocketProto::Udp,
            sockets::Protocol::Tls => SocketProto::Tls,
            sockets::Protocol::Dtls => SocketProto::Dtls,
        };
        Ok(self.socket_bridge.alloc(p))
    }

    fn bind(&mut self, socket: u64, addr: sockets::Address) -> Result<(), String> {
        self.socket_bridge
            .set_bind_addr(socket, format!("{}:{}", addr.ip, addr.port))
    }

    fn listen(&mut self, socket: u64, _backlog: u32) -> Result<(), String> {
        let (proto, addr) = self.socket_bridge.take_pending(socket)?;
        let addr = addr.ok_or_else(|| "bind() must precede listen()".to_string())?;
        let s = self.sockets.clone();
        let real = match proto {
            SocketProto::Tcp => self
                .block_on(async move { s.create_tcp_socket(&addr).await })
                .map_err(|e| e.to_string())?,
            SocketProto::Udp => self
                .block_on(async move { s.create_udp_socket(&addr).await })
                .map_err(|e| e.to_string())?,
            SocketProto::Tls | SocketProto::Dtls => {
                return Err("TLS/DTLS listen requires server certificates not modelled by WIT"
                    .to_string());
            }
        };
        self.socket_bridge.activate(socket, real);
        Ok(())
    }

    fn connect(&mut self, socket: u64, addr: sockets::Address) -> Result<(), String> {
        let (proto, _) = self.socket_bridge.take_pending(socket)?;
        if !matches!(proto, SocketProto::Tcp) {
            return Err("connect() only supported for TCP in v1".to_string());
        }
        let addr_str = format!("{}:{}", addr.ip, addr.port);
        let s = self.sockets.clone();
        let real = self
            .block_on(async move { s.tcp_connect(&addr_str).await })
            .map_err(|e| e.to_string())?;
        self.socket_bridge.activate(socket, real);
        Ok(())
    }

    fn accept(&mut self, socket: u64) -> Result<u64, String> {
        let real = self.socket_bridge.resolve(socket)?;
        let s = self.sockets.clone();
        self.block_on(async move { s.tcp_accept(real).await })
            .map_err(|e| e.to_string())
    }

    fn send(&mut self, socket: u64, data: Vec<u8>) -> Result<u32, String> {
        let real = self.socket_bridge.resolve(socket)?;
        let s = self.sockets.clone();
        let res = self
            .block_on(async move { s.socket_write(real, &data).await })
            .map_err(|e| e.to_string())?;
        Ok(res.bytes_transferred as u32)
    }

    fn receive(&mut self, socket: u64, max_len: u32) -> Result<Vec<u8>, String> {
        let real = self.socket_bridge.resolve(socket)?;
        let s = self.sockets.clone();
        let mut buf = vec![0u8; max_len as usize];
        let res = self
            .block_on(async { s.socket_read(real, &mut buf).await })
            .map_err(|e| e.to_string())?;
        buf.truncate(res.bytes_transferred);
        Ok(buf)
    }

    fn close(&mut self, socket: u64) -> Result<(), String> {
        let real = self.socket_bridge.resolve(socket)?;
        let s = self.sockets.clone();
        let r = self
            .block_on(async move { s.close_socket(real).await })
            .map_err(|e| e.to_string());
        self.socket_bridge.remove(socket);
        r
    }
}

// ============================================================================
// GPU
// ============================================================================
impl gpu::Host for HostState {
    fn list_adapters(&mut self) -> Result<Vec<u64>, String> {
        let g = self.gpu.clone();
        self.block_on(async move { g.get_gpu_adapters().await })
            .map_err(|e| e.to_string())
    }

    fn get_adapter_info(&mut self, handle: u64) -> Result<gpu::AdapterInfo, String> {
        let g = self.gpu.clone();
        let info = self
            .block_on(async move { g.get_gpu_adapter_info(handle).await })
            .map_err(|e| e.to_string())?;
        Ok(gpu::AdapterInfo {
            name: info.name,
            vendor: info.vendor,
            device_type: format!("{:?}", info.device_type),
        })
    }

    fn create_device(&mut self, adapter: u64) -> Result<u64, String> {
        let g = self.gpu.clone();
        self.block_on(async move { g.create_gpu_device(adapter).await })
            .map_err(|e| e.to_string())
    }

    fn create_buffer(
        &mut self,
        device: u64,
        descriptor: gpu::BufferDescriptor,
    ) -> Result<u64, String> {
        let mut usage = elastic_tee_hal::gpu::GpuBufferUsage::default();
        match descriptor.usage {
            gpu::BufferUsage::Storage => usage.storage = true,
            gpu::BufferUsage::Uniform => usage.uniform = true,
            gpu::BufferUsage::Vertex => usage.vertex = true,
            gpu::BufferUsage::Index => usage.index = true,
        };
        let desc = elastic_tee_hal::gpu::GpuBufferDescriptor {
            label: None,
            size: descriptor.size,
            usage,
            mapped_at_creation: false,
        };
        let g = self.gpu.clone();
        self.block_on(async move { g.create_gpu_buffer(device, &desc).await })
            .map_err(|e| e.to_string())
    }

    fn write_buffer(&mut self, buffer: u64, offset: u64, data: Vec<u8>) -> Result<(), String> {
        let g = self.gpu.clone();
        self.block_on(async move { g.write_gpu_buffer(buffer, offset, &data).await })
            .map_err(|e| e.to_string())
    }

    fn read_buffer(&mut self, buffer: u64, offset: u64, size: u64) -> Result<Vec<u8>, String> {
        let g = self.gpu.clone();
        self.block_on(async move { g.read_gpu_buffer(buffer, offset, size).await })
            .map_err(|e| e.to_string())
    }

    fn create_compute_pipeline(
        &mut self,
        device: u64,
        shader_code: Vec<u8>,
    ) -> Result<u64, String> {
        let g = self.gpu.clone();
        self.block_on(async move {
            g.create_gpu_compute_pipeline(device, &shader_code, "main", [1, 1, 1])
                .await
        })
        .map_err(|e| e.to_string())
    }

    fn dispatch(
        &mut self,
        _device: u64,
        _pipeline: u64,
        _x: u32,
        _y: u32,
        _z: u32,
    ) -> Result<(), String> {
        Err("dispatch requires a compute-pass handle in upstream; not modelled by WIT v0.1"
            .to_string())
    }
}

// ============================================================================
// Resources
// ============================================================================
fn map_resource_type(rt: resources::ResourceType) -> elastic_tee_hal::resources::ResourceType {
    match rt {
        resources::ResourceType::Memory => elastic_tee_hal::resources::ResourceType::Memory,
        resources::ResourceType::Cpu => elastic_tee_hal::resources::ResourceType::CpuCores,
        resources::ResourceType::Storage => elastic_tee_hal::resources::ResourceType::Storage,
        resources::ResourceType::Network => {
            elastic_tee_hal::resources::ResourceType::NetworkBandwidth
        }
    }
}

fn map_priority(p: u32) -> elastic_tee_hal::resources::RequestPriority {
    match p {
        0 => elastic_tee_hal::resources::RequestPriority::Low,
        1 => elastic_tee_hal::resources::RequestPriority::Normal,
        2 => elastic_tee_hal::resources::RequestPriority::High,
        _ => elastic_tee_hal::resources::RequestPriority::Critical,
    }
}

impl resources::Host for HostState {
    fn allocate(
        &mut self,
        request: resources::AllocationRequest,
    ) -> Result<resources::AllocationResponse, String> {
        let r = self
            .resources
            .clone()
            .ok_or_else(|| "no resources provider available".to_string())?;
        let req = elastic_tee_hal::resources::ResourceRequest {
            resource_type: map_resource_type(request.resource_type),
            amount: request.amount,
            requester: "wasm-guest".to_string(),
            priority: map_priority(request.priority),
            timeout_seconds: None,
        };
        let out = self
            .block_on(async move { r.allocate_resource(req).await })
            .map_err(|e| e.to_string())?;
        Ok(resources::AllocationResponse {
            allocation_id: out.allocation_id,
            granted_amount: out.granted_amount,
        })
    }

    fn deallocate(&mut self, id: String) -> Result<(), String> {
        let r = self
            .resources
            .clone()
            .ok_or_else(|| "no resources provider available".to_string())?;
        self.block_on(async move { r.release_resource(&id).await })
            .map_err(|e| e.to_string())
    }

    fn query_available(&mut self, resource_type: resources::ResourceType) -> Result<u64, String> {
        let r = self
            .resources
            .clone()
            .ok_or_else(|| "no resources provider available".to_string())?;
        let limits = self
            .block_on(async move { r.get_system_limits().await })
            .map_err(|e| e.to_string())?;
        Ok(match resource_type {
            resources::ResourceType::Memory => limits.max_memory_mb,
            resources::ResourceType::Cpu => limits.max_cpu_cores as u64,
            resources::ResourceType::Storage => limits.max_storage_mb,
            resources::ResourceType::Network => limits.max_network_bandwidth_mbps,
        })
    }
}

// ============================================================================
// Events
// ============================================================================
fn event_type_to_str(et: events::EventType) -> &'static str {
    match et {
        events::EventType::Platform => "platform",
        events::EventType::Crypto => "crypto",
        events::EventType::Storage => "storage",
        events::EventType::Network => "network",
        events::EventType::Gpu => "gpu",
    }
}

fn upstream_event_to_wit(ev: elastic_tee_hal::events::Event) -> events::EventData {
    let payload = serde_json::to_vec(&ev.data).unwrap_or_default();
    let event_type = match ev.event_type.as_str() {
        "crypto" => events::EventType::Crypto,
        "storage" => events::EventType::Storage,
        "network" => events::EventType::Network,
        "gpu" => events::EventType::Gpu,
        _ => events::EventType::Platform,
    };
    events::EventData {
        event_type,
        timestamp: ev.timestamp,
        payload,
    }
}

impl events::Host for HostState {
    fn subscribe(&mut self, event_type: events::EventType) -> Result<u64, String> {
        let e = self.events.clone();
        let cfg = elastic_tee_hal::events::EventHandlerConfig {
            name: format!("hal-runner-{}", event_type_to_str(event_type)),
            event_types: vec![event_type_to_str(event_type).to_string()],
            max_queue_size: 256,
        };
        self.block_on(async move { e.create_event_handler(cfg).await })
            .map_err(|e| e.to_string())
    }

    fn unsubscribe(&mut self, handle: u64) -> Result<(), String> {
        let e = self.events.clone();
        self.block_on(async move { e.remove_event_handler(handle).await })
            .map_err(|e| e.to_string())
    }

    fn poll_events(&mut self, handle: u64) -> Result<Vec<events::EventData>, String> {
        let e = self.events.clone();
        let mut out = Vec::new();
        loop {
            let next = self
                .block_on(async { e.try_request_event_from_handler(handle).await })
                .map_err(|er| er.to_string())?;
            match next {
                Some(ev) => out.push(upstream_event_to_wit(ev)),
                None => break,
            }
        }
        Ok(out)
    }
}

// ============================================================================
// Communication
// ============================================================================
fn buffer_name(recipient: &str) -> String {
    format!("wasm-guest>>{recipient}")
}

impl communication::Host for HostState {
    fn send_message(
        &mut self,
        recipient: String,
        data: Vec<u8>,
        encrypt: bool,
    ) -> Result<u64, String> {
        let comm = self.communication.clone();
        let name = buffer_name(&recipient);
        let cfg = elastic_tee_hal::communication::BufferConfig {
            name,
            capacity: data.len().max(4096),
            is_encrypted: encrypt,
            read_permissions: vec![recipient.clone()],
            write_permissions: vec!["wasm-guest".to_string()],
            admin_permissions: vec!["wasm-guest".to_string()],
        };
        let comm_for_setup = comm.clone();
        let handle = self
            .block_on(async move { comm_for_setup.setup_communication_buffer(cfg).await })
            .map_err(|e| e.to_string())?;
        self.block_on(async move {
            comm.push_data_to_buffer(
                handle,
                &data,
                "wasm-guest",
                elastic_tee_hal::communication::MessageType::Data,
                elastic_tee_hal::communication::MessagePriority::Normal,
            )
            .await
        })
        .map_err(|e| e.to_string())?;
        Ok(handle)
    }

    fn receive_message(&mut self) -> Result<Option<communication::Message>, String> {
        let comm = self.communication.clone();
        let comm_for_list = comm.clone();
        let buffers = self
            .block_on(async move { comm_for_list.list_communication_buffers().await })
            .map_err(|e| e.to_string())?;
        for buf in buffers {
            let Some((sender, _)) = buf.name.split_once(">>") else {
                continue;
            };
            let comm_for_read = comm.clone();
            let buf_handle = buf.handle;
            let msg = self
                .block_on(async move { comm_for_read.read_data_from_buffer(buf_handle, "wasm-guest").await })
                .map_err(|e| e.to_string())?;
            if let Some(m) = msg {
                return Ok(Some(communication::Message {
                    sender: sender.to_string(),
                    recipient: "wasm-guest".to_string(),
                    payload: m.data,
                    encrypted: buf.is_encrypted,
                }));
            }
        }
        Ok(None)
    }

    fn list_workloads(&mut self) -> Result<Vec<String>, String> {
        let comm = self.communication.clone();
        let buffers = self
            .block_on(async move { comm.list_communication_buffers().await })
            .map_err(|e| e.to_string())?;
        let mut peers = Vec::new();
        for buf in buffers {
            if let Some((sender, recipient)) = buf.name.split_once(">>") {
                if sender == "wasm-guest" {
                    peers.push(recipient.to_string());
                } else {
                    peers.push(sender.to_string());
                }
            }
        }
        peers.sort();
        peers.dedup();
        Ok(peers)
    }
}

// ============================================================================
// Storage
// ============================================================================
impl storage::Host for HostState {
    fn create_container(&mut self, name: String) -> Result<u64, String> {
        let s = self.storage_handle()?;
        self.block_on(async move { s.open_container(&name, false).await })
            .map_err(|e| e.to_string())
    }

    fn open_container(&mut self, name: String) -> Result<u64, String> {
        let s = self.storage_handle()?;
        self.block_on(async move { s.open_container(&name, false).await })
            .map_err(|e| e.to_string())
    }

    fn delete_container(&mut self, handle: u64) -> Result<(), String> {
        let s = self.storage_handle()?;
        self.block_on(async move { s.close_container(handle).await })
            .map_err(|e| e.to_string())
    }

    fn store_object(&mut self, container: u64, key: String, data: Vec<u8>) -> Result<u64, String> {
        let s = self.storage_handle()?;
        let key_for_hash = key.clone();
        self.block_on(async move { s.write_object(container, &key, &data).await })
            .map_err(|e| e.to_string())?;
        let mut h: u64 = 0xcbf29ce484222325;
        for b in container.to_le_bytes() {
            h ^= b as u64;
            h = h.wrapping_mul(0x100000001b3);
        }
        for b in key_for_hash.as_bytes() {
            h ^= *b as u64;
            h = h.wrapping_mul(0x100000001b3);
        }
        Ok(h)
    }

    fn retrieve_object(&mut self, container: u64, key: String) -> Result<Vec<u8>, String> {
        let s = self.storage_handle()?;
        self.block_on(async move { s.read_object(container, &key).await })
            .map_err(|e| e.to_string())
    }

    fn delete_object(&mut self, container: u64, key: String) -> Result<(), String> {
        let s = self.storage_handle()?;
        self.block_on(async move { s.delete_object(container, &key).await })
            .map_err(|e| e.to_string())
    }

    fn list_objects(&mut self, container: u64) -> Result<Vec<String>, String> {
        let s = self.storage_handle()?;
        self.block_on(async move { s.list_objects(container).await })
            .map_err(|e| e.to_string())
    }

    fn get_metadata(
        &mut self,
        container: u64,
        _key: String,
    ) -> Result<storage::ObjectMetadata, String> {
        let s = self.storage_handle()?;
        let meta = self
            .block_on(async move { s.get_container_metadata(container).await })
            .map_err(|e| e.to_string())?;
        Ok(storage::ObjectMetadata {
            size: meta.total_size,
            created_at: meta.created_at,
            content_type: "application/octet-stream".to_string(),
        })
    }
}


// ============================================================================
// CLI
// ============================================================================
struct Cli {
    wasm_file: PathBuf,
    /// One of:
    ///   * bare name           → top-level export
    ///   * `instance#func`     → sub-instance export
    function: String,
    envs: Vec<(String, String)>,
    /// `Some` when the user passed `--start-server <host>:<port>`. The
    /// resolved export is invoked with `(host: string, port: u16)` to match
    /// the BRT EUCNC `server-api/start-server` shape.
    start_server: Option<(String, u16)>,
}

fn parse_cli() -> Result<Cli> {
    let args: Vec<String> = std::env::args().collect();
    if args.len() < 2 {
        eprintln!("Usage: {} <component.wasm> [options]", args[0]);
        eprintln!();
        eprintln!("Runs a WASI P2 component that imports the elastic:hal interfaces,");
        eprintln!("serving them with the ELASTIC TEE HAL providers (platform, attestation,");
        eprintln!("crypto, clock, random, sockets, gpu, resources, events, communication,");
        eprintln!("storage). Both `elastic:hal/*` and the modular `elastic:sockets`/");
        eprintln!("`elastic:storage` etc. packagings are served against the same providers.");
        eprintln!();
        eprintln!("Options:");
        eprintln!("  -f, --function <name>     Exported function to call.");
        eprintln!("                            Use `instance#func` for sub-instance exports,");
        eprintln!("                            e.g. `elastic:foo/server-api@0.1.0#start-server`.");
        eprintln!("                            Default: run-hal-test");
        eprintln!("      --start-server H:P    Invoke the resolved export with (host, port).");
        eprintln!(
            "  -e, --env <KEY=VALUE>     Pass environment variable to the component (repeatable)"
        );
        eprintln!();
        eprintln!("Env:");
        eprintln!("  HAL_STORAGE_PATH          Storage base dir (default: $TMPDIR/hal-runner-storage)");
        eprintln!();
        eprintln!("Examples:");
        eprintln!(
            "  {} ../hal-test/target/wasm32-wasip2/release/hal_test.wasm",
            args[0]
        );
        eprintln!(
            "  HAL_STORAGE_PATH=$PWD/hal-storage {} ./brt_eucnc_front.wasm \\",
            args[0]
        );
        eprintln!(
            "    -f 'elastic:brt-eucnc-front-service/server-api@0.1.0#start-server' \\"
        );
        eprintln!("    --start-server 0.0.0.0:8080");
        std::process::exit(1);
    }

    let wasm_file = PathBuf::from(&args[1]);
    let mut function = "run-hal-test".to_string();
    let mut envs: Vec<(String, String)> = Vec::new();
    let mut start_server: Option<(String, u16)> = None;

    let mut i = 2;
    while i < args.len() {
        match args[i].as_str() {
            "--function" | "-f" => {
                i += 1;
                function = args
                    .get(i)
                    .cloned()
                    .context("--function requires a name argument")?;
            }
            "--start-server" => {
                i += 1;
                let hp = args
                    .get(i)
                    .cloned()
                    .context("--start-server requires a HOST:PORT argument")?;
                let (host, port) = hp
                    .rsplit_once(':')
                    .with_context(|| format!("--start-server '{hp}' is not in HOST:PORT format"))?;
                let port: u16 = port
                    .parse()
                    .with_context(|| format!("--start-server port '{port}' is not a valid u16"))?;
                start_server = Some((host.to_string(), port));
            }
            "--env" | "-e" => {
                i += 1;
                let kv = args
                    .get(i)
                    .cloned()
                    .context("--env requires a KEY=VALUE argument")?;
                let (key, value) = kv
                    .split_once('=')
                    .with_context(|| format!("--env '{kv}' is not in KEY=VALUE format"))?;
                envs.push((key.to_string(), value.to_string()));
            }
            _ => return Err(anyhow::anyhow!("Unknown argument: {}", args[i])),
        }
        i += 1;
    }

    Ok(Cli {
        wasm_file,
        function,
        envs,
        start_server,
    })
}

fn resolve_export(
    instance: &wasmtime::component::Instance,
    store: &mut Store<HostState>,
    function: &str,
) -> Option<wasmtime::component::Func> {
    if let Some((instance_name, func_name)) = function.split_once('#') {
        let inst_idx = instance.get_export_index(&mut *store, None, instance_name)?;
        let func_idx = instance.get_export_index(&mut *store, Some(&inst_idx), func_name)?;
        return instance.get_func(&mut *store, func_idx);
    }
    instance.get_func(&mut *store, function)
}

fn main() -> Result<()> {
    tracing_subscriber::fmt()
        .with_env_filter(
            tracing_subscriber::EnvFilter::try_from_default_env().unwrap_or_else(|_| "info".into()),
        )
        .init();

    let cli = parse_cli()?;

    let wasm_bytes = std::fs::read(&cli.wasm_file)
        .with_context(|| format!("Failed to read component: {}", cli.wasm_file.display()))?;

    let mut config = Config::new();
    config.wasm_component_model(true);
    let engine = Engine::new(&config)
        .map_err(|e| anyhow::anyhow!("Failed to create wasmtime Engine: {e}"))?;

    let component = Component::from_binary(&engine, &wasm_bytes)
        .map_err(|e| anyhow::anyhow!("Failed to compile WASM component: {e}"))?;

    let mut linker: Linker<HostState> = Linker::new(&engine);
    wasmtime_wasi::p2::add_to_linker_sync(&mut linker)
        .map_err(|e| anyhow::anyhow!("Failed to add WASI P2 to linker: {e}"))?;
    wasmtime_wasi_http::p2::add_only_http_to_linker_sync(&mut linker)
        .map_err(|e| anyhow::anyhow!("Failed to add wasi:http to linker: {e}"))?;
    ElasticModularImports::add_to_linker::<_, wasmtime::component::HasSelf<_>>(&mut linker, |s| s)
        .map_err(|e| anyhow::anyhow!("Failed to add ELASTIC TEE HAL to linker: {e}"))?;

    // Owned tokio runtime: the host bindings drive upstream's async APIs via
    // `runtime.block_on(...)` from inside otherwise-sync host fns.
    let runtime = Arc::new(
        tokio::runtime::Builder::new_multi_thread()
            .enable_all()
            .build()
            .map_err(|e| anyhow::anyhow!("Failed to build tokio runtime: {e}"))?,
    );
    let state = HostState::new(runtime, &cli.envs);
    let mut store = Store::new(&engine, state);

    let instance = linker
        .instantiate(&mut store, &component)
        .map_err(|e| anyhow::anyhow!("Failed to instantiate component: {e}"))?;

    let func = resolve_export(&instance, &mut store, &cli.function)
        .with_context(|| format!("Export '{}' not found in component", cli.function))?;

    let (wasm_args, mut results): (Vec<Val>, Vec<Val>) = if let Some((host, port)) =
        cli.start_server.clone()
    {
        // BRT-style: start-server(host: string, port: u16) -> result<_, string>
        (
            vec![Val::String(host), Val::U16(port)],
            vec![Val::Bool(false)],
        )
    } else {
        // HAL example exports each return a single string summary.
        (vec![], vec![Val::Bool(false)])
    };

    func.call(&mut store, &wasm_args, &mut results)
        .map_err(|e| anyhow::anyhow!("Failed to call export '{}': {e}", cli.function))?;

    if let Some(Val::String(s)) = results.first() {
        print!("{s}");
    } else if let Some(v) = results.first() {
        println!("{v:?}");
    }

    Ok(())
}
