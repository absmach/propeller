#![allow(dead_code)]

use crate::cdh::config::KbcConfig;
use crate::cdh::error::{Error, Result};
use async_trait::async_trait;
use kbs_protocol::evidence_provider::NativeEvidenceProvider;
use kbs_protocol::{KbsClientBuilder, KbsClientCapabilities};
use lru::LruCache;
use std::sync::Arc;
use std::time::{Duration, SystemTime};
use tokio::sync::Mutex;
use tracing::{debug, info};

pub struct ResourceHandler {
    kbs_config: KbcConfig,
    cache: Arc<Mutex<LruCache<String, CachedResource>>>,
    cache_ttl: Duration,
}

#[derive(Clone)]
struct CachedResource {
    data: Vec<u8>,
    retrieved_at: SystemTime,
}

impl ResourceHandler {
    pub async fn new(
        kbs_config: KbcConfig,
        cache_size: usize,
        cache_ttl_seconds: u64,
    ) -> Result<Self> {
        info!(
            "Initializing ResourceHandler with cache size: {}, TTL: {}s",
            cache_size, cache_ttl_seconds
        );

        Ok(Self {
            kbs_config,
            cache: Arc::new(Mutex::new(LruCache::new(cache_size.try_into().unwrap()))),
            cache_ttl: Duration::from_secs(cache_ttl_seconds),
        })
    }

    pub async fn get_resource(&self, uri: &str) -> Result<Vec<u8>> {
        info!("Getting resource from KBS: {}", uri);

        if let Some(cached) = self.get_from_cache(uri).await {
            debug!("Resource cache hit for: {}", uri);
            return Ok(cached);
        }

        debug!("Resource cache miss for: {}, fetching from KBS", uri);

        let data = self.fetch_from_kbs(uri).await?;

        self.cache_resource(uri, &data).await;

        Ok(data)
    }

    pub async fn attest_and_get_resource(
        &self,
        uri: &str,
        _attestation_report: Vec<u8>,
    ) -> Result<Vec<u8>> {
        info!("Getting resource from KBS with attestation: {}", uri);

        let evidence_provider =
            Box::new(NativeEvidenceProvider::new().map_err(|e| {
                Error::KbsClient(format!("Failed to create evidence provider: {}", e))
            })?);

        let mut client =
            KbsClientBuilder::with_evidence_provider(evidence_provider, &self.kbs_config.url)
                .build()
                .map_err(|e| Error::KbsClient(format!("Failed to create KBS client: {}", e)))?;

        let resource_uri: resource_uri::ResourceUri = uri
            .try_into()
            .map_err(|e| Error::KbsClient(format!("Invalid resource URI: {}", e)))?;

        let data = client
            .get_resource(resource_uri)
            .await
            .map_err(|e| Error::KbsClient(format!("Failed to get resource from KBS: {}", e)))?;

        self.cache_resource(uri, &data).await;

        Ok(data)
    }

    fn is_cache_valid(&self, cached: &CachedResource) -> bool {
        match cached.retrieved_at.elapsed() {
            Ok(elapsed) => elapsed < self.cache_ttl,
            Err(_) => false,
        }
    }

    async fn get_from_cache(&self, uri: &str) -> Option<Vec<u8>> {
        let mut cache = self.cache.lock().await;
        if let Some(cached) = cache.get(uri) {
            if self.is_cache_valid(cached) {
                return Some(cached.data.clone());
            } else {
                debug!("Cache expired for: {}", uri);
                cache.pop(uri);
            }
        }
        None
    }

    async fn cache_resource(&self, uri: &str, data: &[u8]) {
        let mut cache = self.cache.lock().await;
        let cached = CachedResource {
            data: data.to_vec(),
            retrieved_at: SystemTime::now(),
        };
        cache.put(uri.to_string(), cached);
        debug!("Cached resource: {}", uri);
    }

    fn clear_cache(&self) {
        let cache = self.cache.clone();
        tokio::spawn(async move {
            let mut cache = cache.lock().await;
            cache.clear();
            info!("Resource cache cleared");
        });
    }

    async fn fetch_from_kbs(&self, uri: &str) -> Result<Vec<u8>> {
        let evidence_provider =
            Box::new(NativeEvidenceProvider::new().map_err(|e| {
                Error::KbsClient(format!("Failed to create evidence provider: {}", e))
            })?);

        let mut client =
            KbsClientBuilder::with_evidence_provider(evidence_provider, &self.kbs_config.url)
                .build()
                .map_err(|e| Error::KbsClient(format!("Failed to create KBS client: {}", e)))?;

        let resource_uri: resource_uri::ResourceUri = uri
            .try_into()
            .map_err(|e| Error::KbsClient(format!("Invalid resource URI: {}", e)))?;

        let data = client
            .get_resource(resource_uri)
            .await
            .map_err(|e| Error::KbsClient(format!("Failed to get resource from KBS: {}", e)))?;

        Ok(data)
    }
}

#[async_trait]
pub trait KeyBrokerClient: Send + Sync {
    async fn get_resource(&self, uri: &str) -> Result<Vec<u8>>;
    async fn attest_and_get_resource(
        &self,
        uri: &str,
        attestation_report: Vec<u8>,
    ) -> Result<Vec<u8>>;
}

#[async_trait]
impl KeyBrokerClient for ResourceHandler {
    async fn get_resource(&self, uri: &str) -> Result<Vec<u8>> {
        self.get_resource(uri).await
    }

    async fn attest_and_get_resource(
        &self,
        uri: &str,
        attestation_report: Vec<u8>,
    ) -> Result<Vec<u8>> {
        self.attest_and_get_resource(uri, attestation_report).await
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn test_cache_operations() {
        let kbc_config = KbcConfig {
            name: "cc_kbc".to_string(),
            url: "https://kbs.example.com:8080".to_string(),
            kbs_cert: None,
        };

        let handler = ResourceHandler::new(kbc_config, 10, 3600).await.unwrap();

        handler.cache_resource("test-uri", b"test-data").await;

        let cached = handler.get_from_cache("test-uri").await;
        assert!(cached.is_some());
        assert_eq!(cached.unwrap(), b"test-data");
    }
}
