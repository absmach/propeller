pub mod error;

#[cfg(feature = "cdh")]
pub mod attestation;
#[cfg(feature = "cdh")]
pub mod config;
#[cfg(feature = "cdh")]
pub mod core;
#[cfg(feature = "cdh")]
pub mod image;
#[cfg(feature = "cdh")]
pub mod resource;
#[cfg(feature = "cdh")]
pub mod secret;
#[cfg(feature = "cdh")]
pub mod tee;

#[cfg(feature = "cdh")]
use crate::cdh::error::Result;
#[cfg(feature = "cdh")]
use std::path::Path;

#[cfg(feature = "cdh")]
pub use config::CdhConfig;

#[cfg(feature = "cdh")]
pub use core::{CdhHub, CdhHubBuilder};

#[cfg(feature = "cdh")]
#[async_trait::async_trait]
pub trait ConfidentialDataHub: Send + Sync {
    #[allow(dead_code)]
    async fn unseal_secret(&self, secret: &[u8]) -> Result<Vec<u8>>;

    #[allow(dead_code)]
    async fn unwrap_key(&self, annotation_packet: &[u8]) -> Result<Vec<u8>>;

    #[allow(dead_code)]
    async fn get_resource(&self, uri: &str) -> Result<Vec<u8>>;

    #[allow(dead_code)]
    async fn pull_encrypted_image(&self, image_url: &str, output_path: &Path) -> Result<String>;
}
