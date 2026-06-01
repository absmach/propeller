use anyhow::{anyhow, Result};
use elastic_tee_hal::interfaces::{
    CapabilitiesInterface, ClockInterface, CryptoInterface, HalProvider, RandomInterface,
};
use elastic_tee_hal::{
    CommunicationInterface, EventInterface, GpuInterface, ResourceInterface, SocketInterface,
    StorageInterface,
};
use std::collections::HashMap;
use std::path::PathBuf;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::{Arc, Mutex, OnceLock};
use tracing::warn;

/// State for the WIT `sockets` interface bridge. Upstream
/// `elastic_tee_hal::SocketInterface` requires the bind address up front in
/// `create_tcp_socket` / `create_udp_socket`, but the WIT shape exposes
/// `create-socket(protocol)` separately from `bind` / `listen` / `connect`. We
/// hand out synthetic handles (high-bit range so they cannot collide with
/// upstream's atomic counter) until enough info is gathered to call upstream,
/// then remember the mapping.
const SYNTHETIC_SOCKET_BASE: u64 = 1 << 62;

#[derive(Clone)]
pub enum SocketSlot {
    Pending {
        protocol: SocketProtocol,
        bind_addr: Option<String>,
    },
    Active {
        real_handle: u64,
    },
}

#[derive(Copy, Clone, Debug)]
pub enum SocketProtocol {
    Tcp,
    Udp,
    Tls,
    Dtls,
}

pub struct SocketBridge {
    next: AtomicU64,
    pub(crate) state: Mutex<HashMap<u64, SocketSlot>>,
}

impl SocketBridge {
    fn new() -> Self {
        Self {
            next: AtomicU64::new(SYNTHETIC_SOCKET_BASE),
            state: Mutex::new(HashMap::new()),
        }
    }

    pub fn alloc(&self, protocol: SocketProtocol) -> u64 {
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

    pub fn set_bind_addr(&self, id: u64, addr: String) -> Result<(), String> {
        let mut state = self.state.lock().unwrap();
        match state.get_mut(&id) {
            Some(SocketSlot::Pending { bind_addr, .. }) => {
                *bind_addr = Some(addr);
                Ok(())
            }
            Some(SocketSlot::Active { .. }) => {
                Err("socket already activated; cannot bind".to_string())
            }
            None => Err("unknown socket handle".to_string()),
        }
    }

    pub fn take_pending(&self, id: u64) -> Result<(SocketProtocol, Option<String>), String> {
        let state = self.state.lock().unwrap();
        match state.get(&id) {
            Some(SocketSlot::Pending {
                protocol,
                bind_addr,
            }) => Ok((*protocol, bind_addr.clone())),
            Some(SocketSlot::Active { .. }) => Err("socket already activated".to_string()),
            None => Err("unknown socket handle".to_string()),
        }
    }

    pub fn activate(&self, id: u64, real_handle: u64) {
        self.state
            .lock()
            .unwrap()
            .insert(id, SocketSlot::Active { real_handle });
    }

    pub fn resolve(&self, id: u64) -> Result<u64, String> {
        // Real upstream handles (< SYNTHETIC_SOCKET_BASE) are passed through
        // directly so handles returned by `accept` keep working without an
        // extra round-trip through the bridge.
        if id < SYNTHETIC_SOCKET_BASE {
            return Ok(id);
        }
        match self.state.lock().unwrap().get(&id) {
            Some(SocketSlot::Active { real_handle }) => Ok(*real_handle),
            Some(SocketSlot::Pending { .. }) => Err("socket not yet bound/connected".to_string()),
            None => Err("unknown socket handle".to_string()),
        }
    }

    pub fn remove(&self, id: u64) {
        if id >= SYNTHETIC_SOCKET_BASE {
            self.state.lock().unwrap().remove(&id);
        }
    }
}

pub struct PropletHal {
    provider: HalProvider,
    sockets: Arc<SocketInterface>,
    gpu: Arc<GpuInterface>,
    resources: Option<Arc<ResourceInterface>>,
    events: Arc<EventInterface>,
    communication: Arc<CommunicationInterface>,
    /// Lazily initialised: `StorageInterface::new` is async, so the first
    /// access (inside spawn_blocking with a tokio handle available) drives it
    /// to completion via `Handle::block_on`.
    storage: OnceLock<Option<Arc<StorageInterface>>>,
    storage_base: PathBuf,
    socket_bridge: SocketBridge,
}

#[allow(dead_code)]
impl PropletHal {
    pub fn new() -> Arc<Self> {
        Arc::new(Self::default())
    }

    /// Construct with an explicit storage base path. Callers (e.g.
    /// `WasmtimeRuntime`) pass the value from `PropletConfig.hal_storage_path`
    /// so the storage location is part of the proplet's typed configuration
    /// rather than a free-floating process env var.
    pub fn with_storage_path(storage_base: PathBuf) -> Arc<Self> {
        Arc::new(Self {
            provider: HalProvider::with_defaults(),
            sockets: Arc::new(SocketInterface::new()),
            gpu: Arc::new(GpuInterface::new()),
            resources: ResourceInterface::new().ok().map(Arc::new),
            events: Arc::new(EventInterface::new()),
            communication: Arc::new(CommunicationInterface::new()),
            storage: OnceLock::new(),
            storage_base,
            socket_bridge: SocketBridge::new(),
        })
    }

    pub fn has_tee(&self) -> bool {
        self.provider.platform.is_some()
    }

    pub fn try_attest(&self, nonce: &[u8]) -> Option<Vec<u8>> {
        let platform = self.provider.platform.as_ref()?;
        match platform.attestation(nonce) {
            Ok(report) => Some(report),
            Err(e) => {
                warn!("HAL attestation error: {}", e);
                None
            }
        }
    }

    pub fn capabilities(&self) -> Option<&dyn CapabilitiesInterface> {
        self.provider.capabilities.as_deref()
    }

    pub fn crypto(&self) -> Option<&dyn CryptoInterface> {
        self.provider.crypto.as_deref()
    }

    pub fn random(&self) -> Option<&dyn RandomInterface> {
        self.provider.random.as_deref()
    }

    pub fn clock(&self) -> Option<&dyn ClockInterface> {
        self.provider.clock.as_deref()
    }

    pub fn sockets(&self) -> Arc<SocketInterface> {
        self.sockets.clone()
    }

    pub fn socket_bridge(&self) -> &SocketBridge {
        &self.socket_bridge
    }

    pub fn gpu(&self) -> Arc<GpuInterface> {
        self.gpu.clone()
    }

    pub fn resources(&self) -> Option<Arc<ResourceInterface>> {
        self.resources.clone()
    }

    pub fn events(&self) -> Arc<EventInterface> {
        self.events.clone()
    }

    pub fn communication(&self) -> Arc<CommunicationInterface> {
        self.communication.clone()
    }

    /// Returns the shared storage interface, initialising it on first use.
    /// Must be called from inside a Tokio runtime (any of the proplet host
    /// binding entry points satisfy this — they run under `spawn_blocking`).
    pub fn storage(&self) -> Option<Arc<StorageInterface>> {
        self.storage
            .get_or_init(|| {
                let base = self.storage_base.clone();
                let handle = match tokio::runtime::Handle::try_current() {
                    Ok(h) => h,
                    Err(_) => {
                        warn!("HAL storage init outside Tokio runtime; storage unavailable");
                        return None;
                    }
                };
                match handle.block_on(StorageInterface::new(&base)) {
                    Ok(s) => Some(Arc::new(s)),
                    Err(e) => {
                        warn!("HAL storage init failed at {}: {}", base.display(), e);
                        None
                    }
                }
            })
            .clone()
    }

    pub fn sha256(&self, data: &[u8]) -> Result<Vec<u8>> {
        let crypto = self
            .provider
            .crypto
            .as_ref()
            .ok_or_else(|| anyhow!("HAL crypto provider not available"))?;
        crypto.hash(data, "SHA-256").map_err(|e| anyhow!(e))
    }

    pub fn encrypt(&self, data: &[u8], key: &[u8]) -> Result<Vec<u8>> {
        let crypto = self
            .provider
            .crypto
            .as_ref()
            .ok_or_else(|| anyhow!("HAL crypto provider not available"))?;
        crypto
            .encrypt(data, key, "AES-256-GCM")
            .map_err(|e| anyhow!(e))
    }

    pub fn decrypt(&self, data: &[u8], key: &[u8]) -> Result<Vec<u8>> {
        let crypto = self
            .provider
            .crypto
            .as_ref()
            .ok_or_else(|| anyhow!("HAL crypto provider not available"))?;
        crypto
            .decrypt(data, key, "AES-256-GCM")
            .map_err(|e| anyhow!(e))
    }

    pub fn random_bytes(&self, length: u32) -> Result<Vec<u8>> {
        let rng = self
            .provider
            .random
            .as_ref()
            .ok_or_else(|| anyhow!("HAL random provider not available"))?;
        rng.get_secure_random(length).map_err(|e| anyhow!(e))
    }

    pub fn platform_info(&self) -> Option<(String, String, bool)> {
        self.provider
            .platform
            .as_ref()
            .and_then(|p| p.platform_info().ok())
    }
}

impl Default for PropletHal {
    fn default() -> Self {
        // Use a stable, non-temp default so storage data survives between
        // runs without relying on the process env. Callers that need a
        // different path should use `with_storage_path` (the configured
        // entry-point used by `WasmtimeRuntime`).
        Self {
            provider: HalProvider::with_defaults(),
            sockets: Arc::new(SocketInterface::new()),
            gpu: Arc::new(GpuInterface::new()),
            resources: ResourceInterface::new().ok().map(Arc::new),
            events: Arc::new(EventInterface::new()),
            communication: Arc::new(CommunicationInterface::new()),
            storage: OnceLock::new(),
            storage_base: PathBuf::from("/tmp/proplet/hal-storage"),
            socket_bridge: SocketBridge::new(),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_hal_new_no_panic() {
        let hal = PropletHal::new();
        let _ = hal.has_tee();
    }

    #[test]
    fn test_sha256() {
        let hal = PropletHal::new();
        let digest = hal.sha256(b"hello").expect("sha256 failed");
        assert_eq!(digest.len(), 32);
    }

    #[test]
    fn test_random_bytes() {
        let hal = PropletHal::new();
        let bytes = hal.random_bytes(32).expect("random_bytes failed");
        assert_eq!(bytes.len(), 32);
    }

    #[test]
    fn test_encrypt_decrypt_roundtrip() {
        let hal = PropletHal::new();
        let key = hal.random_bytes(32).expect("key gen failed");
        let plaintext = b"proplet-secret";

        let ciphertext = hal.encrypt(plaintext, &key).expect("encrypt failed");
        let recovered = hal.decrypt(&ciphertext, &key).expect("decrypt failed");
        assert_eq!(recovered, plaintext);
    }

    #[test]
    fn test_attest_no_panic() {
        let hal = PropletHal::new();
        let _ = hal.try_attest(b"test-nonce");
    }

    #[test]
    fn test_new_providers_present() {
        let hal = PropletHal::new();
        let _ = hal.sockets();
        let _ = hal.gpu();
        let _ = hal.events();
        let _ = hal.communication();
        assert!(hal.resources().is_some());
    }
}
