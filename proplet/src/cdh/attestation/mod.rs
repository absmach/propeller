#![allow(dead_code)]

use crate::cdh::config::AttestationConfig;
use crate::cdh::error::{Error, Result};
use crate::cdh::tee::TeeType;
use std::path::Path;
use std::sync::Arc;
use tracing::{debug, info, warn};

pub struct AttestationClient {
    socket_path: String,
    timeout: u64,
    aa_client: Option<Arc<AttestationAgentClient>>,
}

impl AttestationClient {
    pub async fn new(config: &AttestationConfig) -> Result<Self> {
        info!(
            "Initializing AttestationClient with gRPC endpoint: {}",
            config.aa_socket
        );

        let aa_client = if config.aa_socket.starts_with("unix://") {
            let socket_path = config.aa_socket.trim_start_matches("unix://");

            if Path::new(socket_path).exists() {
                info!("AA socket found, will attempt to connect via gRPC");
                Some(Arc::new(AttestationAgentClient::new_grpc_unix(
                    socket_path.to_string(),
                )?))
            } else {
                warn!(
                    "AA socket not found at: {}, attestation will be unavailable",
                    socket_path
                );
                None
            }
        } else if config.aa_socket.starts_with("http://")
            || config.aa_socket.starts_with("https://")
        {
            info!("AA HTTP endpoint found, will attempt to connect via gRPC");
            Some(Arc::new(AttestationAgentClient::new_grpc_http(
                config.aa_socket.clone(),
            )?))
        } else {
            warn!(
                "Invalid AA endpoint format: {}, attestation will be unavailable",
                config.aa_socket
            );
            None
        };

        Ok(Self {
            socket_path: config.aa_socket.clone(),
            timeout: config.timeout,
            aa_client,
        })
    }

    pub async fn get_evidence(&self, nonce: Option<Vec<u8>>) -> Result<AttestationReport> {
        info!("Requesting attestation evidence");

        if let Some(client) = &self.aa_client {
            let report = client.get_evidence(nonce, self.timeout).await?;
            info!("Attestation evidence generated successfully");
            Ok(report)
        } else {
            warn!("AA client not available, cannot generate attestation evidence");
            Err(Error::Attestation(
                "Attestation Agent endpoint not available".to_string(),
            ))
        }
    }

    pub async fn extend_runtime_measurement(&self, event: RuntimeMeasurementEvent) -> Result<()> {
        debug!(
            "Extending runtime measurement: {}::{}",
            event.domain, event.operation
        );

        if let Some(client) = &self.aa_client {
            client
                .extend_runtime_measurement(event, self.timeout)
                .await?;
            debug!("Runtime measurement extended successfully");
            Ok(())
        } else {
            debug!("AA client not available, skipping runtime measurement extension");
            Ok(())
        }
    }

    pub fn is_available(&self) -> bool {
        self.aa_client.is_some()
    }
}

#[derive(Debug, Clone)]
pub struct AttestationRequest {
    pub report_data: Vec<u8>,
    pub nonce: Option<Vec<u8>>,
}

#[derive(Debug, Clone)]
pub struct AttestationReport {
    pub report: Vec<u8>,
    pub platform: AttestationPlatform,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum AttestationPlatform {
    TDX,
    SNP,
    SEV,
    SE,
    AzureTDXVtpm,
    AzureSNPVtpm,
    None,
}

impl AttestationPlatform {
    pub fn from_tee_type(tee_type: TeeType) -> Self {
        match tee_type {
            TeeType::Tdx => AttestationPlatform::TDX,
            TeeType::Snp => AttestationPlatform::SNP,
            TeeType::Sev => AttestationPlatform::SEV,
            TeeType::Se => AttestationPlatform::SE,
            TeeType::AzureTdxVtpm => AttestationPlatform::AzureTDXVtpm,
            TeeType::AzureSnpVtpm => AttestationPlatform::AzureSNPVtpm,
            TeeType::None => AttestationPlatform::None,
            TeeType::Mock => AttestationPlatform::None,
        }
    }

    pub fn name(&self) -> &'static str {
        match self {
            AttestationPlatform::TDX => "TDX",
            AttestationPlatform::SNP => "SNP",
            AttestationPlatform::SEV => "SEV",
            AttestationPlatform::SE => "SE",
            AttestationPlatform::AzureTDXVtpm => "Azure-TDX-vTPM",
            AttestationPlatform::AzureSNPVtpm => "Azure-SNP-vTPM",
            AttestationPlatform::None => "None",
        }
    }
}

#[derive(Debug, Clone)]
pub struct RuntimeMeasurementEvent {
    pub domain: String,
    pub operation: String,
    pub content: serde_json::Value,
}

impl RuntimeMeasurementEvent {
    pub fn new(
        domain: impl Into<String>,
        operation: impl Into<String>,
        content: serde_json::Value,
    ) -> Self {
        Self {
            domain: domain.into(),
            operation: operation.into(),
            content,
        }
    }

    pub fn for_image_pull(image_url: &str, digest: &str) -> Self {
        Self::new(
            "github.com/absmach/propeller",
            "PullEncryptedImage",
            serde_json::json!({
                "image_url": image_url,
                "digest": digest
            }),
        )
    }

    pub fn for_wasm_execution(task_id: &str, wasm_hash: &str) -> Self {
        Self::new(
            "github.com/absmach/propeller",
            "ExecuteWasm",
            serde_json::json!({
                "task_id": task_id,
                "wasm_hash": wasm_hash
            }),
        )
    }
}

struct AttestationAgentClient {
    #[cfg(feature = "cdh")]
    _grpc_client: Option<()>,
    endpoint_type: EndpointType,
}

#[derive(Debug, Clone, Copy)]
enum EndpointType {
    UnixSocket,
    Http,
}

impl AttestationAgentClient {
    fn new_grpc_unix(_socket_path: String) -> Result<Self> {
        #[cfg(feature = "cdh")]
        {
            Ok(Self {
                _grpc_client: None,
                endpoint_type: EndpointType::UnixSocket,
            })
        }

        #[cfg(not(feature = "cdh"))]
        {
            Err(Error::Attestation("CDH feature not enabled".to_string()))
        }
    }

    fn new_grpc_http(_url: String) -> Result<Self> {
        #[cfg(feature = "cdh")]
        {
            Ok(Self {
                _grpc_client: None,
                endpoint_type: EndpointType::Http,
            })
        }

        #[cfg(not(feature = "cdh"))]
        {
            Err(Error::Attestation("CDH feature not enabled".to_string()))
        }
    }

    async fn get_evidence(
        &self,
        _nonce: Option<Vec<u8>>,
        _timeout: u64,
    ) -> Result<AttestationReport> {
        #[cfg(feature = "cdh")]
        {
            warn!("gRPC evidence generation requires Attestation Agent with gRPC support. Using placeholder implementation.");

            Ok(AttestationReport {
                report: vec![],
                platform: AttestationPlatform::None,
            })
        }

        #[cfg(not(feature = "cdh"))]
        {
            Err(Error::Attestation("CDH feature not enabled".to_string()))
        }
    }

    async fn extend_runtime_measurement(
        &self,
        event: RuntimeMeasurementEvent,
        _timeout: u64,
    ) -> Result<()> {
        #[cfg(feature = "cdh")]
        {
            debug!(
                "Runtime measurement: {}::{} - {}",
                event.domain, event.operation, event.content
            );
            warn!("gRPC runtime measurement extension requires Attestation Agent with gRPC support. Skipping.");
            Ok(())
        }

        #[cfg(not(feature = "cdh"))]
        {
            Err(Error::Attestation("CDH feature not enabled".to_string()))
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_attestation_platform_from_tee() {
        assert_eq!(
            AttestationPlatform::from_tee_type(TeeType::Tdx),
            AttestationPlatform::TDX
        );
        assert_eq!(
            AttestationPlatform::from_tee_type(TeeType::Snp),
            AttestationPlatform::SNP
        );
    }

    #[test]
    fn test_runtime_measurement_event() {
        let event =
            RuntimeMeasurementEvent::for_image_pull("ghcr.io/test/app:encrypted", "sha256:abc123");

        assert_eq!(event.domain, "github.com/absmach/propeller");
        assert_eq!(event.operation, "PullEncryptedImage");
    }
}
