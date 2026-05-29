//! WASI P2 (component-model) HAL bindings.
//!
//! The sole HAL integration: generates typed host bindings from
//! `wit/hal/hal.wit` (world `hal-imports`) with
//! `wasmtime::component::bindgen!` against wasmtime 44, and bridges the
//! generated `Host` traits to the `elastic_tee_hal` providers. Wired into the
//! component runtime paths in `runtime::wasmtime_runtime` when `hal_enabled` is
//! set. P1 core modules receive WASI but no HAL.
//!
//! v1 scope: platform, attestation, crypto, clock, random — the interfaces with
//! a real provider backing. Stub-only interfaces (sockets/gpu/resources/events/
//! communication/storage) are intentionally omitted; see `wit/hal/hal.wit`.

use anyhow::Result;
use elastic_tee_hal::interfaces::{
    CapabilitiesInterface, ClockInterface, CryptoInterface, RandomInterface,
};
use elastic_tee_hal::providers::{
    DefaultCapabilitiesProvider, DefaultClockProvider, DefaultCryptoProvider, DefaultRandomProvider,
};
use tracing::warn;

use crate::runtime::wasmtime_runtime::StoreData;

wasmtime::component::bindgen!({
    world: "hal-imports",
    path: "wit/hal",
});

use elastic::hal::{attestation, clock, crypto, platform, random};

/// Register all HAL import interfaces on a component linker.
pub fn add_to_linker(linker: &mut wasmtime::component::Linker<StoreData>) -> Result<()> {
    HalImports::add_to_linker::<_, wasmtime::component::HasSelf<_>>(linker, |state| state)?;
    Ok(())
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
        match self.hal.platform_info() {
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
            Err(e) => {
                warn!("HAL platform/list-capabilities error: {}", e);
                Vec::new()
            }
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
impl attestation::Host for StoreData {
    fn attestation(&mut self, report_data: Vec<u8>) -> Result<Vec<u8>, String> {
        match self.hal.try_attest(&report_data) {
            Some(report) => Ok(report),
            None => Ok(b"{}".to_vec()),
        }
    }
}

// ============================================================================
// Crypto
// ============================================================================
impl crypto::Host for StoreData {
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
}

// ============================================================================
// Clock
// ============================================================================
impl clock::Host for StoreData {
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
impl random::Host for StoreData {
    fn get_random_bytes(&mut self, length: u32) -> Result<Vec<u8>, String> {
        DefaultRandomProvider::default().get_random_bytes(length)
    }

    fn get_secure_random(&mut self, length: u32) -> Result<Vec<u8>, String> {
        DefaultRandomProvider::default().get_secure_random(length)
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
