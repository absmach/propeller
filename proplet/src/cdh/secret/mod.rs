#![allow(dead_code)]

use crate::cdh::error::{Error, Result};
use crate::cdh::resource::ResourceHandler;
use base64::{engine::general_purpose::URL_SAFE_NO_PAD as b64, Engine};
use serde::{Deserialize, Serialize};
use std::sync::Arc;
use tracing::{debug, info};

pub struct SecretHandler {
    resource_handler: Arc<ResourceHandler>,
}

impl SecretHandler {
    pub fn new(resource_handler: Arc<ResourceHandler>) -> Self {
        Self { resource_handler }
    }

    pub async fn unseal_secret(&self, secret: &[u8]) -> Result<Vec<u8>> {
        info!("Unsealing sealed secret");

        let secret_string = String::from_utf8(secret.to_vec())
            .map_err(|_| Error::SecretParse("Secret string must be UTF-8".to_string()))?;

        let sealed_secret = SealedSecret::parse(&secret_string)?;

        sealed_secret.unseal(&self.resource_handler).await
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SealedSecret {
    pub version: String,

    #[serde(flatten)]
    pub content: SecretContent,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(tag = "type", rename_all = "lowercase")]
pub enum SecretContent {
    Envelope(EnvelopeSecret),
    Vault(VaultSecret),
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct EnvelopeSecret {
    pub provider: String,
    pub key_id: String,
    pub encrypted_key: String,
    pub encrypted_data: String,
    #[serde(default)]
    pub wrap_type: WrapType,
    pub iv: String,
    #[serde(default)]
    pub provider_settings: serde_json::Value,
    #[serde(default)]
    pub annotations: serde_json::Value,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct VaultSecret {
    pub provider: String,
    pub name: String,
    #[serde(default)]
    pub provider_settings: serde_json::Value,
    #[serde(default)]
    pub annotations: serde_json::Value,
}

#[derive(Debug, Clone, Copy, Serialize, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum WrapType {
    Aes256Gcm,
    #[serde(other)]
    Unknown,
}

impl Default for WrapType {
    fn default() -> Self {
        WrapType::Aes256Gcm
    }
}

pub const SECRET_VERSION: &str = "0.1.0";

impl SealedSecret {
    pub fn parse(secret: &str) -> Result<Self> {
        let sections: Vec<&str> = secret.split('.').collect();

        if sections.len() != 4 {
            return Err(Error::SecretParse(
                "Invalid sealed secret format. Expected: sealed.header.payload.signature"
                    .to_string(),
            ));
        }

        if sections[0] != "sealed" {
            return Err(Error::SecretParse(
                "Sealed secret must start with 'sealed' prefix".to_string(),
            ));
        }

        let payload_json = b64
            .decode(sections[2])
            .map_err(|e| Error::SecretParse(format!("Failed to decode payload: {}", e)))?;

        let secret: SealedSecret = serde_json::from_slice(&payload_json)
            .map_err(|e| Error::SecretParse(format!("Failed to parse secret JSON: {}", e)))?;

        if secret.version != SECRET_VERSION {
            return Err(Error::SecretParse(format!(
                "Unsupported secret version: {}. Expected: {}",
                secret.version, SECRET_VERSION
            )));
        }

        Ok(secret)
    }

    async fn unseal(&self, resource_handler: &ResourceHandler) -> Result<Vec<u8>> {
        match &self.content {
            SecretContent::Envelope(env) => env.unseal(resource_handler).await,
            SecretContent::Vault(vault) => vault.unseal(resource_handler).await,
        }
    }
}

impl EnvelopeSecret {
    async fn unseal(&self, resource_handler: &ResourceHandler) -> Result<Vec<u8>> {
        debug!("Unsealing envelope secret");

        let kek = resource_handler.get_resource(&self.key_id).await?;

        let encrypted_key = b64
            .decode(&self.encrypted_key)
            .map_err(|e| Error::SecretUnseal(format!("Failed to decode encrypted_key: {}", e)))?;

        let encrypted_data = b64
            .decode(&self.encrypted_data)
            .map_err(|e| Error::SecretUnseal(format!("Failed to decode encrypted_data: {}", e)))?;

        let iv = b64
            .decode(&self.iv)
            .map_err(|e| Error::SecretUnseal(format!("Failed to decode IV: {}", e)))?;

        let dek = self.decrypt_key(&kek, &encrypted_key, &iv)?;

        let plaintext = self.decrypt_data(&dek, &encrypted_data, &iv)?;

        Ok(plaintext)
    }

    fn decrypt_key(&self, kek: &[u8], encrypted_key: &[u8], iv: &[u8]) -> Result<Vec<u8>> {
        use aes_gcm::aead::{Aead, Payload};
        use aes_gcm::{Aes256Gcm, KeyInit};

        let cipher = Aes256Gcm::new(
            kek.try_into()
                .map_err(|_| Error::Crypto("KEK must be 32 bytes for AES-256-GCM".to_string()))?,
        );

        let payload = Payload {
            msg: encrypted_key,
            aad: b"",
        };

        cipher
            .decrypt(
                iv.try_into().map_err(|_| {
                    Error::Crypto("IV must be 12 bytes for AES-256-GCM".to_string())
                })?,
                payload,
            )
            .map_err(|e| Error::SecretUnseal(format!("Failed to decrypt DEK: {}", e)))
    }

    fn decrypt_data(&self, dek: &[u8], encrypted_data: &[u8], iv: &[u8]) -> Result<Vec<u8>> {
        use aes_gcm::aead::{Aead, Payload};
        use aes_gcm::{Aes256Gcm, KeyInit};

        let cipher = Aes256Gcm::new(
            dek.try_into()
                .map_err(|_| Error::Crypto("DEK must be 32 bytes for AES-256-GCM".to_string()))?,
        );

        let payload = Payload {
            msg: encrypted_data,
            aad: b"",
        };

        cipher
            .decrypt(
                iv.try_into().map_err(|_| {
                    Error::Crypto("IV must be 12 bytes for AES-256-GCM".to_string())
                })?,
                payload,
            )
            .map_err(|e| Error::SecretUnseal(format!("Failed to decrypt secret data: {}", e)))
    }
}

impl VaultSecret {
    async fn unseal(&self, resource_handler: &ResourceHandler) -> Result<Vec<u8>> {
        debug!("Unsealing vault secret from: {}", self.name);

        let secret = resource_handler.get_resource(&self.name).await?;

        Ok(secret)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_parse_vault_secret() {
        let secret_str = "sealed.fakejwsheader.eyJ2ZXJzaW9uIjoiMC4xLjAiLCJ0eXBlIjoidmF1bHQiLCJwcm92aWRlciI6ImticyIsIm5hbWUiOiJrYnM6Ly8vZGVmYXVsdC9zZWNyZXQvMSJ9.fakesignature";

        let secret = SealedSecret::parse(secret_str);
        assert!(secret.is_ok());

        let secret = secret.unwrap();
        assert_eq!(secret.version, "0.1.0");

        match secret.content {
            SecretContent::Vault(v) => {
                assert_eq!(v.name, "kbs:///default/secret/1");
            }
            _ => panic!("Expected Vault secret"),
        }
    }

    #[test]
    fn test_parse_invalid_secret() {
        let secret_str = "invalid.secret.format";

        let result = SealedSecret::parse(secret_str);
        assert!(result.is_err());
    }

    #[test]
    fn test_parse_envelope_secret() {
        let secret_json = serde_json::json!({
            "version": "0.1.0",
            "type": "envelope",
            "provider": "kbs",
            "key_id": "kbs:///default/keys/kek-1",
            "encrypted_key": "encrypted_key_data",
            "encrypted_data": "encrypted_secret_data",
            "wrap_type": "AES_256_GCM",
            "iv": "iv_data",
            "provider_settings": {},
            "annotations": {}
        });

        let secret_str = format!(
            "sealed.fakejwsheader.{}.fakesignature",
            b64.encode(serde_json::to_vec(&secret_json).unwrap())
        );

        let secret = SealedSecret::parse(&secret_str);
        assert!(secret.is_ok());

        let secret = secret.unwrap();
        match secret.content {
            SecretContent::Envelope(e) => {
                assert_eq!(e.key_id, "kbs:///default/keys/kek-1");
                assert_eq!(e.provider, "kbs");
            }
            _ => panic!("Expected Envelope secret"),
        }
    }
}
