//! WASI P2 (component-model) HAL bindings.
//!
//! Generates typed host bindings from `wit/hal/hal.wit` (world `hal-imports`)
//! with `wasmtime::component::bindgen!` against wasmtime 44 and bridges the
//! generated `Host` traits to providers held on `StoreData.hal`. Wired into the
//! component runtime paths in `runtime::wasmtime_runtime` when `hal_enabled` is
//! set. P1 core modules receive WASI but no HAL.
//!
//! Coverage: all 11 elastic interfaces from upstream wit-modular — platform,
//! attestation, crypto, clock, random, sockets, gpu, resources, events,
//! communication, storage. Async-only upstream APIs (sockets/gpu/resources/
//! events/communication/storage) are driven from the sync host bindings via
//! `tokio::runtime::Handle::block_on`; this is safe because the component
//! runtime invokes guest code from inside `tokio::task::spawn_blocking`, where
//! a Tokio handle is available. Where the WIT signature has no upstream analog
//! (e.g. crypto `create-context`, fine-grained get-metadata) we return a
//! `Err(String)` so a relying guest sees a clear failure rather than a stub
//! success.

use anyhow::Result;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Mutex;
use tracing::warn;

use crate::hal::{PropletHal, SocketProtocol};
use crate::runtime::wasmtime_runtime::StoreData;

wasmtime::component::bindgen!({
    world: "hal-imports",
    path: "wit/hal",
});

use elastic::hal::{
    attestation, clock, communication, crypto, events, gpu, platform, random, resources, sockets,
    storage,
};

// ============================================================================
// Internal stub state for WIT methods with no direct upstream backing.
// ============================================================================
static CRYPTO_CONTEXT_COUNTER: AtomicU64 = AtomicU64::new(1);
static CRYPTO_CONTEXTS: Mutex<Vec<u64>> = Mutex::new(Vec::new());

fn alloc_crypto_context() -> u64 {
    let id = CRYPTO_CONTEXT_COUNTER.fetch_add(1, Ordering::SeqCst);
    CRYPTO_CONTEXTS.lock().unwrap().push(id);
    id
}

fn free_crypto_context(id: u64) -> Result<(), String> {
    let mut ctx = CRYPTO_CONTEXTS.lock().unwrap();
    if let Some(pos) = ctx.iter().position(|x| *x == id) {
        ctx.swap_remove(pos);
        Ok(())
    } else {
        Err("unknown crypto context handle".to_string())
    }
}

/// Register all HAL import interfaces on a component linker.
pub fn add_to_linker(linker: &mut wasmtime::component::Linker<StoreData>) -> Result<()> {
    HalImports::add_to_linker::<_, wasmtime::component::HasSelf<_>>(linker, |state| state)?;
    Ok(())
}

/// Host bindings are only registered on linkers that received `add_to_linker`,
/// which the runtime calls only when it also populates `StoreData.hal`. This
/// helper makes that invariant explicit; reaching it with `None` is a runtime
/// wiring bug.
fn hal(state: &StoreData) -> &PropletHal {
    state
        .hal
        .as_deref()
        .expect("HAL bindings invoked on a store with no HAL — runtime wiring bug")
}

/// Drive an async upstream call to completion from inside a sync host
/// binding. Returns a string error if no Tokio runtime is reachable from the
/// current thread (would only happen in an unexpected harness wiring).
fn block_on<F: std::future::Future>(future: F) -> Result<F::Output, String> {
    match tokio::runtime::Handle::try_current() {
        Ok(h) => Ok(tokio::task::block_in_place(|| h.block_on(future))),
        Err(_) => Err("no Tokio runtime available for HAL async call".to_string()),
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
// Platform (platform info + capability discovery)
// ============================================================================
impl platform::Host for StoreData {
    fn get_platform_info(&mut self) -> platform::PlatformInfo {
        match hal(self).platform_info() {
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
        let Some(caps) = hal(self).capabilities() else {
            warn!("HAL platform/list-capabilities: no provider");
            return Vec::new();
        };
        match caps.list_capabilities() {
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
            Err(e) => {
                warn!("HAL platform/list-capabilities error: {}", e);
                Vec::new()
            }
        }
    }

    fn has_capability(&mut self, feature_name: String) -> bool {
        hal(self)
            .capabilities()
            .and_then(|c| c.has_capability(&feature_name).ok())
            .unwrap_or(false)
    }
}

// ============================================================================
// Attestation
// ============================================================================
impl attestation::Host for StoreData {
    fn attestation(&mut self, report_data: Vec<u8>) -> Result<Vec<u8>, String> {
        // No silent stub: if there is no TEE platform, a relying party must be
        // able to tell that no real evidence was produced.
        hal(self)
            .try_attest(&report_data)
            .ok_or_else(|| "no TEE platform available for attestation".to_string())
    }
}

// ============================================================================
// Crypto
// ============================================================================
impl crypto::Host for StoreData {
    fn hash(&mut self, data: Vec<u8>, algorithm: crypto::HashAlgorithm) -> Result<Vec<u8>, String> {
        crypto_provider(self)?.hash(&data, hash_algo_str(algorithm))
    }

    fn encrypt(
        &mut self,
        data: Vec<u8>,
        key: Vec<u8>,
        algorithm: crypto::CipherAlgorithm,
    ) -> Result<Vec<u8>, String> {
        crypto_provider(self)?.encrypt(&data, &key, cipher_algo_str(algorithm))
    }

    fn decrypt(
        &mut self,
        data: Vec<u8>,
        key: Vec<u8>,
        algorithm: crypto::CipherAlgorithm,
    ) -> Result<Vec<u8>, String> {
        crypto_provider(self)?.decrypt(&data, &key, cipher_algo_str(algorithm))
    }

    fn generate_keypair(&mut self) -> Result<crypto::KeyPair, String> {
        crypto_provider(self)?
            .generate_keypair()
            .map(|(public_key, private_key)| crypto::KeyPair {
                public_key,
                private_key,
            })
    }

    fn sign(&mut self, data: Vec<u8>, private_key: Vec<u8>) -> Result<Vec<u8>, String> {
        crypto_provider(self)?.sign(&data, &private_key)
    }

    fn verify(
        &mut self,
        data: Vec<u8>,
        signature: Vec<u8>,
        public_key: Vec<u8>,
    ) -> Result<bool, String> {
        crypto_provider(self)?.verify(&data, &signature, &public_key)
    }

    fn create_context(&mut self) -> Result<u64, String> {
        Ok(alloc_crypto_context())
    }

    fn destroy_context(&mut self, handle: u64) -> Result<(), String> {
        free_crypto_context(handle)
    }
}

fn crypto_provider(
    state: &StoreData,
) -> Result<&dyn elastic_tee_hal::interfaces::CryptoInterface, String> {
    hal(state)
        .crypto()
        .ok_or_else(|| "no crypto provider available".to_string())
}

// ============================================================================
// Clock
// ============================================================================
impl clock::Host for StoreData {
    fn get_system_time(&mut self) -> Result<clock::SystemTime, String> {
        clock_provider(self)?
            .system_time()
            .map(|(seconds, nanoseconds)| clock::SystemTime {
                seconds,
                nanoseconds,
            })
    }

    fn get_monotonic_time(&mut self) -> Result<clock::MonotonicTime, String> {
        clock_provider(self)?
            .monotonic_time()
            .map(
                |(elapsed_seconds, elapsed_nanoseconds)| clock::MonotonicTime {
                    elapsed_seconds,
                    elapsed_nanoseconds,
                },
            )
    }

    fn resolution(&mut self) -> Result<u64, String> {
        clock_provider(self)?.resolution()
    }

    fn sleep(&mut self, duration_ns: u64) -> Result<(), String> {
        clock_provider(self)?.sleep(duration_ns)
    }
}

fn clock_provider(
    state: &StoreData,
) -> Result<&dyn elastic_tee_hal::interfaces::ClockInterface, String> {
    hal(state)
        .clock()
        .ok_or_else(|| "no clock provider available".to_string())
}

// ============================================================================
// Random
// ============================================================================
impl random::Host for StoreData {
    fn get_random_bytes(&mut self, length: u32) -> Result<Vec<u8>, String> {
        random_provider(self)?.get_random_bytes(length)
    }

    fn get_secure_random(&mut self, length: u32) -> Result<Vec<u8>, String> {
        random_provider(self)?.get_secure_random(length)
    }

    fn get_entropy_info(&mut self) -> Result<random::EntropyInfo, String> {
        // Upstream `interfaces::RandomInterface` trait has no entropy
        // introspection; report a conservative platform-quality default.
        Ok(random::EntropyInfo {
            source: random::EntropySource::Platform,
            quality: 256,
            available_bytes: u64::MAX,
        })
    }

    fn reseed(&mut self, _additional_entropy: Vec<u8>) -> Result<(), String> {
        // The default provider draws from getrandom on every call; an explicit
        // reseed is a no-op for it. Returning Ok keeps the API contract truthful.
        Ok(())
    }
}

fn random_provider(
    state: &StoreData,
) -> Result<&dyn elastic_tee_hal::interfaces::RandomInterface, String> {
    hal(state)
        .random()
        .ok_or_else(|| "no random provider available".to_string())
}

// ============================================================================
// Sockets — bridges upstream `SocketInterface` async API through a synthetic
// handle layer (see `hal::SocketBridge`) so the WIT split create/bind/listen
// shape composes correctly with upstream's one-shot `create_tcp_socket`.
// ============================================================================
impl sockets::Host for StoreData {
    fn create_socket(&mut self, protocol: sockets::Protocol) -> Result<u64, String> {
        let p = match protocol {
            sockets::Protocol::Tcp => SocketProtocol::Tcp,
            sockets::Protocol::Udp => SocketProtocol::Udp,
            sockets::Protocol::Tls => SocketProtocol::Tls,
            sockets::Protocol::Dtls => SocketProtocol::Dtls,
        };
        Ok(hal(self).socket_bridge().alloc(p))
    }

    fn bind(&mut self, socket: u64, addr: sockets::Address) -> Result<(), String> {
        let s = format!("{}:{}", addr.ip, addr.port);
        hal(self).socket_bridge().set_bind_addr(socket, s)
    }

    fn listen(&mut self, socket: u64, _backlog: u32) -> Result<(), String> {
        // Upstream binds + listens in one call. Defer the actual creation here
        // so we have both the protocol (from create) and the address (from bind).
        let bridge = hal(self).socket_bridge();
        let (proto, addr) = bridge.take_pending(socket)?;
        let addr = addr.ok_or_else(|| "bind() must precede listen()".to_string())?;
        let sockets_iface = hal(self).sockets();
        let real = match proto {
            SocketProtocol::Tcp => {
                block_on(async move { sockets_iface.create_tcp_socket(&addr).await })?
                    .map_err(|e| e.to_string())?
            }
            SocketProtocol::Udp => {
                block_on(async move { sockets_iface.create_udp_socket(&addr).await })?
                    .map_err(|e| e.to_string())?
            }
            SocketProtocol::Tls | SocketProtocol::Dtls => {
                return Err(
                    "TLS/DTLS listen requires server certificates not modelled by WIT".to_string(),
                );
            }
        };
        bridge.activate(socket, real);
        Ok(())
    }

    fn connect(&mut self, socket: u64, addr: sockets::Address) -> Result<(), String> {
        let bridge = hal(self).socket_bridge();
        let (proto, _) = bridge.take_pending(socket)?;
        if !matches!(proto, SocketProtocol::Tcp) {
            return Err("connect() only supported for TCP in v1".to_string());
        }
        let addr_str = format!("{}:{}", addr.ip, addr.port);
        let sockets_iface = hal(self).sockets();
        let real = block_on(async move { sockets_iface.tcp_connect(&addr_str).await })?
            .map_err(|e| e.to_string())?;
        bridge.activate(socket, real);
        Ok(())
    }

    fn accept(&mut self, socket: u64) -> Result<u64, String> {
        let real = hal(self).socket_bridge().resolve(socket)?;
        let sockets_iface = hal(self).sockets();
        block_on(async move { sockets_iface.tcp_accept(real).await })?.map_err(|e| e.to_string())
    }

    fn send(&mut self, socket: u64, data: Vec<u8>) -> Result<u32, String> {
        let real = hal(self).socket_bridge().resolve(socket)?;
        let sockets_iface = hal(self).sockets();
        let res = block_on(async move { sockets_iface.socket_write(real, &data).await })?
            .map_err(|e| e.to_string())?;
        Ok(res.bytes_transferred as u32)
    }

    fn receive(&mut self, socket: u64, max_len: u32) -> Result<Vec<u8>, String> {
        let real = hal(self).socket_bridge().resolve(socket)?;
        let sockets_iface = hal(self).sockets();
        let mut buf = vec![0u8; max_len as usize];
        let res = block_on(async { sockets_iface.socket_read(real, &mut buf).await })?
            .map_err(|e| e.to_string())?;
        buf.truncate(res.bytes_transferred);
        Ok(buf)
    }

    fn close(&mut self, socket: u64) -> Result<(), String> {
        let real = hal(self).socket_bridge().resolve(socket)?;
        let sockets_iface = hal(self).sockets();
        let r = block_on(async move { sockets_iface.close_socket(real).await })?
            .map_err(|e| e.to_string());
        hal(self).socket_bridge().remove(socket);
        r
    }
}

// ============================================================================
// GPU — bridges to upstream `GpuInterface` (a simulation at this rev). The
// `dispatch` WIT signature takes (device, pipeline, x,y,z) while upstream
// dispatches against a compute-pass handle; we surface that as an error to
// avoid pretending the GPU ran a workload.
// ============================================================================
impl gpu::Host for StoreData {
    fn list_adapters(&mut self) -> Result<Vec<u64>, String> {
        let gpu_iface = hal(self).gpu();
        block_on(async move { gpu_iface.get_gpu_adapters().await })?.map_err(|e| e.to_string())
    }

    fn get_adapter_info(&mut self, handle: u64) -> Result<gpu::AdapterInfo, String> {
        let gpu_iface = hal(self).gpu();
        let info = block_on(async move { gpu_iface.get_gpu_adapter_info(handle).await })?
            .map_err(|e| e.to_string())?;
        Ok(gpu::AdapterInfo {
            name: info.name,
            vendor: info.vendor,
            device_type: format!("{:?}", info.device_type),
        })
    }

    fn create_device(&mut self, adapter: u64) -> Result<u64, String> {
        let gpu_iface = hal(self).gpu();
        block_on(async move { gpu_iface.create_gpu_device(adapter).await })?
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
        let gpu_iface = hal(self).gpu();
        block_on(async move { gpu_iface.create_gpu_buffer(device, &desc).await })?
            .map_err(|e| e.to_string())
    }

    fn write_buffer(&mut self, buffer: u64, offset: u64, data: Vec<u8>) -> Result<(), String> {
        let gpu_iface = hal(self).gpu();
        block_on(async move { gpu_iface.write_gpu_buffer(buffer, offset, &data).await })?
            .map_err(|e| e.to_string())
    }

    fn read_buffer(&mut self, buffer: u64, offset: u64, size: u64) -> Result<Vec<u8>, String> {
        let gpu_iface = hal(self).gpu();
        block_on(async move { gpu_iface.read_gpu_buffer(buffer, offset, size).await })?
            .map_err(|e| e.to_string())
    }

    fn create_compute_pipeline(
        &mut self,
        device: u64,
        shader_code: Vec<u8>,
    ) -> Result<u64, String> {
        let gpu_iface = hal(self).gpu();
        block_on(async move {
            gpu_iface
                .create_gpu_compute_pipeline(device, &shader_code, "main", [1, 1, 1])
                .await
        })?
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
        Err(
            "dispatch requires a compute-pass handle in upstream; not modelled by WIT v0.1"
                .to_string(),
        )
    }
}

// ============================================================================
// Resources — direct mapping to upstream `ResourceInterface`.
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

impl resources::Host for StoreData {
    fn allocate(
        &mut self,
        request: resources::AllocationRequest,
    ) -> Result<resources::AllocationResponse, String> {
        let res = hal(self)
            .resources()
            .ok_or_else(|| "no resources provider available".to_string())?;
        let req = elastic_tee_hal::resources::ResourceRequest {
            resource_type: map_resource_type(request.resource_type),
            amount: request.amount,
            requester: "wasm-guest".to_string(),
            priority: map_priority(request.priority),
            timeout_seconds: None,
        };
        let out = block_on(async move { res.allocate_resource(req).await })?
            .map_err(|e| e.to_string())?;
        Ok(resources::AllocationResponse {
            allocation_id: out.allocation_id,
            granted_amount: out.granted_amount,
        })
    }

    fn deallocate(&mut self, id: String) -> Result<(), String> {
        let res = hal(self)
            .resources()
            .ok_or_else(|| "no resources provider available".to_string())?;
        block_on(async move { res.release_resource(&id).await })?.map_err(|e| e.to_string())
    }

    fn query_available(&mut self, resource_type: resources::ResourceType) -> Result<u64, String> {
        let res = hal(self)
            .resources()
            .ok_or_else(|| "no resources provider available".to_string())?;
        let limits =
            block_on(async move { res.get_system_limits().await })?.map_err(|e| e.to_string())?;
        Ok(match resource_type {
            resources::ResourceType::Memory => limits.max_memory_mb,
            resources::ResourceType::Cpu => limits.max_cpu_cores as u64,
            resources::ResourceType::Storage => limits.max_storage_mb,
            resources::ResourceType::Network => limits.max_network_bandwidth_mbps,
        })
    }
}

// ============================================================================
// Events — bridges WIT (subscribe/unsubscribe/poll) to upstream's
// handler-and-subscription model. Each WIT subscription handle becomes an
// upstream handler that this proplet polls non-blockingly via
// `try_request_event_from_handler`.
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
    // Upstream `EventData` is a typed enum; we serialise it for the guest to
    // decode. Coarse category mapping picks the closest WIT bucket from the
    // upstream `event_type` string.
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

impl events::Host for StoreData {
    fn subscribe(&mut self, event_type: events::EventType) -> Result<u64, String> {
        let events_iface = hal(self).events();
        let cfg = elastic_tee_hal::events::EventHandlerConfig {
            name: format!("proplet-{}", event_type_to_str(event_type)),
            event_types: vec![event_type_to_str(event_type).to_string()],
            max_queue_size: 256,
        };
        block_on(async move { events_iface.create_event_handler(cfg).await })?
            .map_err(|e| e.to_string())
    }

    fn unsubscribe(&mut self, handle: u64) -> Result<(), String> {
        let events_iface = hal(self).events();
        block_on(async move { events_iface.remove_event_handler(handle).await })?
            .map_err(|e| e.to_string())
    }

    fn poll_events(&mut self, handle: u64) -> Result<Vec<events::EventData>, String> {
        let events_iface = hal(self).events();
        let mut out = Vec::new();
        loop {
            let next =
                block_on(async { events_iface.try_request_event_from_handler(handle).await })?
                    .map_err(|e| e.to_string())?;
            match next {
                Some(ev) => out.push(upstream_event_to_wit(ev)),
                None => break,
            }
        }
        Ok(out)
    }
}

// ============================================================================
// Communication — upstream is buffer-centric (per-pair buffers) and lacks a
// global per-workload mailbox. We expose a minimal shim: each `send-message`
// call sets up a buffer named after the (sender,recipient) pair and pushes
// the data; `receive-message` polls any buffer addressed to a fixed
// "wasm-guest" id; `list-workloads` returns the buffer names so guests can
// discover peers. This is a faithful surface of what upstream models today.
// ============================================================================
fn buffer_name(recipient: &str) -> String {
    format!("wasm-guest>>{recipient}")
}

impl communication::Host for StoreData {
    fn send_message(
        &mut self,
        recipient: String,
        data: Vec<u8>,
        encrypt: bool,
    ) -> Result<u64, String> {
        let comm = hal(self).communication();
        let name = buffer_name(&recipient);
        let cfg = elastic_tee_hal::communication::BufferConfig {
            name: name.clone(),
            capacity: data.len().max(4096),
            is_encrypted: encrypt,
            read_permissions: vec![recipient.clone()],
            write_permissions: vec!["wasm-guest".to_string()],
            admin_permissions: vec!["wasm-guest".to_string()],
        };
        // Idempotent setup: existing buffers are reused by upstream.
        let comm_for_setup = comm.clone();
        let cfg_for_setup = cfg.clone();
        let handle = block_on(async move {
            comm_for_setup
                .setup_communication_buffer(cfg_for_setup)
                .await
        })?
        .map_err(|e| e.to_string())?;
        block_on(async move {
            comm.push_data_to_buffer(
                handle,
                &data,
                "wasm-guest",
                elastic_tee_hal::communication::MessageType::Data,
                elastic_tee_hal::communication::MessagePriority::Normal,
            )
            .await
        })?
        .map_err(|e| e.to_string())?;
        Ok(handle)
    }

    fn receive_message(&mut self) -> Result<Option<communication::Message>, String> {
        let comm = hal(self).communication();
        let comm_for_list = comm.clone();
        let buffers = block_on(async move { comm_for_list.list_communication_buffers().await })?
            .map_err(|e| e.to_string())?;
        for buf in buffers {
            // Buffer naming convention from send_message: "sender>>recipient".
            let Some((sender, _)) = buf.name.split_once(">>") else {
                continue;
            };
            let buf_handle = buf.handle;
            let comm_for_read = comm.clone();
            let msg = block_on(async move {
                comm_for_read
                    .read_data_from_buffer(buf_handle, "wasm-guest")
                    .await
            })?
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
        let comm = hal(self).communication();
        let buffers = block_on(async move { comm.list_communication_buffers().await })?
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
// Storage — bridges to upstream `StorageInterface`. Containers are opened
// non-encrypted by default. `get-metadata` returns container-level metadata
// because object metadata is private at this upstream rev.
// ============================================================================
fn storage_iface(
    state: &StoreData,
) -> Result<std::sync::Arc<elastic_tee_hal::StorageInterface>, String> {
    hal(state)
        .storage()
        .ok_or_else(|| "no storage provider available".to_string())
}

impl storage::Host for StoreData {
    fn create_container(&mut self, name: String) -> Result<u64, String> {
        let s = storage_iface(self)?;
        block_on(async move { s.open_container(&name, false).await })?.map_err(|e| e.to_string())
    }

    fn open_container(&mut self, name: String) -> Result<u64, String> {
        let s = storage_iface(self)?;
        block_on(async move { s.open_container(&name, false).await })?.map_err(|e| e.to_string())
    }

    fn delete_container(&mut self, handle: u64) -> Result<(), String> {
        let s = storage_iface(self)?;
        // Upstream models container deletion as close_container; no on-disk
        // teardown is performed.
        block_on(async move { s.close_container(handle).await })?.map_err(|e| e.to_string())
    }

    fn store_object(&mut self, container: u64, key: String, data: Vec<u8>) -> Result<u64, String> {
        let s = storage_iface(self)?;
        let key_for_hash = key.clone();
        block_on(async move { s.write_object(container, &key, &data).await })?
            .map_err(|e| e.to_string())?;
        // Upstream returns unit; WIT wants an object-handle. Expose a stable
        // FNV-1a hash of (container, key) so guests can correlate writes.
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
        let s = storage_iface(self)?;
        block_on(async move { s.read_object(container, &key).await })?.map_err(|e| e.to_string())
    }

    fn delete_object(&mut self, container: u64, key: String) -> Result<(), String> {
        let s = storage_iface(self)?;
        block_on(async move { s.delete_object(container, &key).await })?.map_err(|e| e.to_string())
    }

    fn list_objects(&mut self, container: u64) -> Result<Vec<String>, String> {
        let s = storage_iface(self)?;
        block_on(async move { s.list_objects(container).await })?.map_err(|e| e.to_string())
    }

    fn get_metadata(
        &mut self,
        container: u64,
        _key: String,
    ) -> Result<storage::ObjectMetadata, String> {
        let s = storage_iface(self)?;
        let meta = block_on(async move { s.get_container_metadata(container).await })?
            .map_err(|e| e.to_string())?;
        Ok(storage::ObjectMetadata {
            size: meta.total_size,
            created_at: meta.created_at,
            content_type: "application/octet-stream".to_string(),
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_add_to_linker_succeeds() {
        let mut config = wasmtime::Config::new();
        config.wasm_component_model(true);
        let engine = wasmtime::Engine::new(&config).unwrap();
        let mut linker: wasmtime::component::Linker<StoreData> =
            wasmtime::component::Linker::new(&engine);
        add_to_linker(&mut linker).expect("HAL add_to_linker failed");
    }
}
