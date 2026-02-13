use anyhow::{anyhow, Result};
use elastic_tee_hal::interfaces::HalProvider;
use std::sync::Arc;
use tracing::warn;

pub struct PropletHal {
    provider: HalProvider,
}

#[allow(dead_code)]
impl PropletHal {
    pub fn new() -> Arc<Self> {
        Arc::new(Self {
            provider: HalProvider::with_defaults(),
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
        Self {
            provider: HalProvider::with_defaults(),
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
}
