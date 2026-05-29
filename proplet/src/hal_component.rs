//! WASI P2 (component-model) HAL bindings.
//!
//! The sole HAL integration: generates typed host bindings from
//! `wit/hal/hal.wit` (world `hal-imports`) with
//! `wasmtime::component::bindgen!` against wasmtime 44, and bridges the
//! generated `Host` traits to the `elastic_tee_hal` providers held on
//! `StoreData.hal`. Wired into the component runtime paths in
//! `runtime::wasmtime_runtime` when `hal_enabled` is set. P1 core modules
//! receive WASI but no HAL.
//!
//! v1 scope: platform, attestation, crypto, clock, random — the interfaces with
//! a real provider backing. Stub-only interfaces (sockets/gpu/resources/events/
//! communication/storage) are intentionally omitted; see `wit/hal/hal.wit`.

use anyhow::Result;
use tracing::warn;

use crate::hal::PropletHal;
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
}

fn random_provider(
    state: &StoreData,
) -> Result<&dyn elastic_tee_hal::interfaces::RandomInterface, String> {
    hal(state)
        .random()
        .ok_or_else(|| "no random provider available".to_string())
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
