#![allow(dead_code)]

use crate::cdh::tee::{TeeDetector, WorkloadType, WorkloadValidator};
use crate::cdh::{
    attestation::{AttestationClient, RuntimeMeasurementEvent},
    config::CdhConfig,
    error::{Error, Result},
    image::ImageHandler,
    resource::ResourceHandler,
    secret::SecretHandler,
    ConfidentialDataHub,
};
use async_trait::async_trait;
use std::path::Path;
use std::sync::Arc;
use tracing::info;

pub struct CdhHub {
    #[allow(dead_code)]
    config: CdhConfig,
    #[allow(dead_code)]
    resource_handler: Arc<ResourceHandler>,
    attestation_client: Arc<AttestationClient>,
    #[allow(dead_code)]
    secret_handler: Arc<SecretHandler>,
    image_handler: Arc<ImageHandler>,
    workload_validator: WorkloadValidator,
}

impl CdhHub {
    pub async fn new(config: CdhConfig) -> Result<Self> {
        info!("Initializing CdhHub");

        config.set_env_vars();

        let tee_type = TeeDetector::detect();
        let workload_validator = WorkloadValidator::new(tee_type, config.tee.strict_mode);

        let resource_handler = Arc::new(
            ResourceHandler::new(
                config.kbc.clone(),
                config.resource.cache_size,
                config.resource.cache_ttl,
            )
            .await?,
        );

        let attestation_client = Arc::new(AttestationClient::new(&config.attestation).await?);

        let secret_handler = Arc::new(SecretHandler::new(resource_handler.clone()));

        let image_handler =
            Arc::new(ImageHandler::new(config.image.clone(), resource_handler.clone()).await?);

        Ok(Self {
            config,
            resource_handler,
            attestation_client,
            secret_handler,
            image_handler,
            workload_validator,
        })
    }

    pub async fn validate_encrypted_workload(&self) -> Result<()> {
        self.workload_validator.validate(WorkloadType::Encrypted)
    }

    #[allow(dead_code)]
    pub async fn validate_non_encrypted_workload(&self) -> Result<()> {
        self.workload_validator.validate(WorkloadType::NonEncrypted)
    }

    pub async fn record_image_pull_event(&self, image_url: &str, digest: &str) -> Result<()> {
        let event = RuntimeMeasurementEvent::for_image_pull(image_url, digest);
        self.attestation_client
            .extend_runtime_measurement(event)
            .await
    }

    #[allow(dead_code)]
    pub async fn record_wasm_execution_event(&self, task_id: &str, wasm_hash: &str) -> Result<()> {
        let event = RuntimeMeasurementEvent::for_wasm_execution(task_id, wasm_hash);
        self.attestation_client
            .extend_runtime_measurement(event)
            .await
    }

    #[allow(dead_code)]
    pub fn is_attestation_available(&self) -> bool {
        self.attestation_client.is_available()
    }
}

#[async_trait]
impl ConfidentialDataHub for CdhHub {
    async fn unseal_secret(&self, secret: &[u8]) -> Result<Vec<u8>> {
        self.secret_handler.unseal_secret(secret).await
    }

    async fn unwrap_key(&self, annotation_packet: &[u8]) -> Result<Vec<u8>> {
        self.image_handler.unwrap_layer_key(annotation_packet).await
    }

    async fn get_resource(&self, uri: &str) -> Result<Vec<u8>> {
        self.resource_handler.get_resource(uri).await
    }

    async fn pull_encrypted_image(&self, image_url: &str, output_path: &Path) -> Result<String> {
        info!("Pulling encrypted image through CDH: {}", image_url);

        self.validate_encrypted_workload().await?;

        let image_info = self
            .image_handler
            .pull_encrypted_image(image_url, output_path)
            .await?;

        self.record_image_pull_event(image_url, &image_info.manifest_digest)
            .await?;

        Ok(image_info.manifest_digest)
    }
}

pub struct CdhHubBuilder {
    config: Option<CdhConfig>,
    resource_cache_size: Option<usize>,
    resource_cache_ttl: Option<u64>,
    attestation_timeout: Option<u64>,
}

impl CdhHubBuilder {
    pub fn new() -> Self {
        Self {
            config: None,
            resource_cache_size: None,
            resource_cache_ttl: None,
            attestation_timeout: None,
        }
    }

    pub fn with_config(mut self, config: CdhConfig) -> Self {
        self.config = Some(config);
        self
    }

    #[allow(dead_code)]
    pub fn with_resource_cache_size(mut self, size: usize) -> Self {
        self.resource_cache_size = Some(size);
        self
    }

    #[allow(dead_code)]
    pub fn with_resource_cache_ttl(mut self, ttl: u64) -> Self {
        self.resource_cache_ttl = Some(ttl);
        self
    }

    #[allow(dead_code)]
    pub fn with_attestation_timeout(mut self, timeout: u64) -> Self {
        self.attestation_timeout = Some(timeout);
        self
    }

    pub async fn build(self) -> Result<CdhHub> {
        let mut config = self
            .config
            .ok_or_else(|| Error::Configuration("CdhConfig not provided".to_string()))?;

        if let Some(size) = self.resource_cache_size {
            config.resource.cache_size = size;
        }

        if let Some(ttl) = self.resource_cache_ttl {
            config.resource.cache_ttl = ttl;
        }

        if let Some(timeout) = self.attestation_timeout {
            config.attestation.timeout = timeout;
        }

        CdhHub::new(config).await
    }
}

impl Default for CdhHubBuilder {
    fn default() -> Self {
        Self::new()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn test_cdh_hub_builder() {
        let config = CdhConfig::from_proplet_config(&crate::config::PropletConfig {
            instance_id: "test".to_string(),
            log_level: "info".to_string(),
            mqtt_address: "tcp://localhost:1883".to_string(),
            mqtt_timeout: Some(30),
            mqtt_keep_alive: Some(60),
            mqtt_max_packet_size: Some(1024),
            mqtt_inflight: 10,
            mqtt_request_channel_capacity: 128,
            liveliness_interval: 10,
            domain_id: "test-domain".to_string(),
            channel_id: "test-channel".to_string(),
            client_id: "test-client".to_string(),
            client_key: "test-key".to_string(),
            external_wasm_runtime: None,
            cdh_enabled: Some(false),
        })
        .unwrap();

        let hub = CdhHubBuilder::new()
            .with_config(config)
            .with_resource_cache_size(50)
            .with_resource_cache_ttl(1800)
            .build()
            .await;

        assert!(hub.is_ok() || hub.is_err());
    }
}
