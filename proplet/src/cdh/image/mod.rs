#![allow(dead_code)]

use crate::cdh::error::{Error, Result};
use crate::cdh::resource::ResourceHandler;
use base64::Engine;
use image_rs::{builder::ClientBuilder, config::ImageConfig, image::ImageClient};
use serde::{Deserialize, Serialize};
use std::path::Path;
use std::sync::Arc;
use tracing::{debug, info};

pub struct ImageHandler {
    config: ImageConfig,
    resource_handler: Arc<ResourceHandler>,
}

impl ImageHandler {
    pub async fn new(config: ImageConfig, resource_handler: Arc<ResourceHandler>) -> Result<Self> {
        info!("Initializing ImageHandler");

        Ok(Self {
            config,
            resource_handler,
        })
    }

    async fn get_client(&self) -> Result<ImageClient> {
        debug!("Creating new ImageClient");

        let client: ImageClient = Into::<ClientBuilder>::into(self.config.clone())
            .build()
            .await
            .map_err(|e| Error::ImageHandling(format!("Failed to create image client: {}", e)))?;

        Ok(client)
    }

    pub async fn pull_encrypted_image(
        &self,
        image_url: &str,
        output_path: &Path,
    ) -> Result<ImageInfo> {
        info!("Pulling encrypted image: {}", image_url);

        let mut client = self.get_client().await?;
        let image_info = client
            .pull_image(image_url, output_path, &None, &None)
            .await
            .map_err(|e| Error::ImageHandling(format!("Failed to pull image: {}", e)))?;

        Ok(ImageInfo {
            manifest_digest: image_info.manifest_digest,
            image_url: image_url.to_string(),
            output_path: output_path.to_path_buf(),
        })
    }

    pub fn is_encrypted_image(&self, image_url: &str) -> bool {
        image_url.contains("@") || image_url.contains("encrypted")
    }

    pub async fn unwrap_layer_key(&self, annotation_packet: &[u8]) -> Result<Vec<u8>> {
        debug!("Unwrapping layer key from annotation packet");

        let annotation: AnnotationPacket = serde_json::from_slice(annotation_packet)
            .map_err(|e| Error::KeyUnwrap(format!("Failed to parse annotation packet: {}", e)))?;

        annotation.unwrap_key(&self.resource_handler).await
    }

    pub async fn decrypt_layer(
        &self,
        encrypted_layer: &[u8],
        key: &[u8],
        iv: &[u8],
    ) -> Result<Vec<u8>> {
        use aes_gcm::aead::{Aead, Payload};
        use aes_gcm::{Aes256Gcm, KeyInit};

        debug!("Decrypting layer");

        let cipher = Aes256Gcm::new(
            key.try_into()
                .map_err(|_| Error::Crypto("Key must be 32 bytes".to_string()))?,
        );

        let payload = Payload {
            msg: encrypted_layer,
            aad: b"",
        };

        let decrypted = cipher
            .decrypt(
                iv.try_into()
                    .map_err(|_| Error::Crypto("IV must be 12 bytes".to_string()))?,
                payload,
            )
            .map_err(|e| Error::ImageHandling(format!("Failed to decrypt layer: {}", e)))?;

        Ok(decrypted)
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct AnnotationPacket {
    pub kid: String,
    pub wrapped_data: String,
    pub iv: String,
    #[serde(default)]
    pub wrap_type: WrapType,
    #[serde(default)]
    pub annotations: serde_json::Value,
}

#[derive(Debug, Clone, Copy, Serialize, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
#[allow(dead_code)]
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

impl AnnotationPacket {
    #[allow(dead_code)]
    pub async fn unwrap_key(&self, resource_handler: &ResourceHandler) -> Result<Vec<u8>> {
        debug!("Unwrapping key from KBS: {}", self.kid);

        let wrapped_key = base64::engine::general_purpose::STANDARD
            .decode(&self.wrapped_data)
            .map_err(|e| Error::KeyUnwrap(format!("Failed to decode wrapped key: {}", e)))?;

        let iv = base64::engine::general_purpose::STANDARD
            .decode(&self.iv)
            .map_err(|e| Error::KeyUnwrap(format!("Failed to decode IV: {}", e)))?;

        let lek = resource_handler.get_resource(&self.kid).await?;

        self.decrypt_key(&lek, &wrapped_key, &iv)
    }

    #[allow(dead_code)]
    fn decrypt_key(&self, lek: &[u8], wrapped_key: &[u8], iv: &[u8]) -> Result<Vec<u8>> {
        use aes_gcm::aead::{Aead, Payload};
        use aes_gcm::{Aes256Gcm, KeyInit};

        let cipher = Aes256Gcm::new(
            lek.try_into()
                .map_err(|_| Error::Crypto("LEK must be 32 bytes".to_string()))?,
        );

        let payload = Payload {
            msg: wrapped_key,
            aad: b"",
        };

        let decrypted = cipher
            .decrypt(
                iv.try_into()
                    .map_err(|_| Error::Crypto("IV must be 12 bytes".to_string()))?,
                payload,
            )
            .map_err(|e| Error::KeyUnwrap(format!("Failed to unwrap key: {}", e)))?;

        Ok(decrypted)
    }
}

#[derive(Debug, Clone)]
#[allow(dead_code)]
pub struct ImageInfo {
    pub manifest_digest: String,
    pub image_url: String,
    pub output_path: std::path::PathBuf,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[allow(dead_code)]
pub struct EncryptedImageInfo {
    pub image_url: String,
    pub manifest_digest: String,
    pub encrypted_layers: Vec<EncryptedLayer>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[allow(dead_code)]
pub struct EncryptedLayer {
    pub digest: String,
    pub encryption_metadata: AnnotationPacket,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_parse_annotation_packet() {
        let packet_json = serde_json::json!({
            "kid": "kbs:///default/keys/image-key",
            "wrapped_data": "wrapped_key_base64",
            "iv": "iv_base64",
            "wrap_type": "AES_256_GCM",
            "annotations": {}
        });

        let packet: AnnotationPacket = serde_json::from_value(packet_json).unwrap();
        assert_eq!(packet.kid, "kbs:///default/keys/image-key");
    }

    #[test]
    fn test_encrypted_image_info() {
        let info = EncryptedImageInfo {
            image_url: "ghcr.io/test/app:encrypted@sha256:abc123".to_string(),
            manifest_digest: "sha256:abc123".to_string(),
            encrypted_layers: vec![EncryptedLayer {
                digest: "sha256:layer1".to_string(),
                encryption_metadata: AnnotationPacket {
                    kid: "kbs:///default/keys/key1".to_string(),
                    wrapped_data: "wrapped1".to_string(),
                    iv: "iv1".to_string(),
                    wrap_type: WrapType::Aes256Gcm,
                    annotations: serde_json::Value::Null,
                },
            }],
        };

        let json = serde_json::to_string(&info).unwrap();
        let parsed: EncryptedImageInfo = serde_json::from_str(&json).unwrap();
        assert_eq!(parsed.image_url, info.image_url);
    }
}
