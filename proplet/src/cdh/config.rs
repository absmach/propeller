#![allow(dead_code)]

use crate::cdh::error::{Error, Result};
use image_rs::config::ImageConfig;
use serde::{Deserialize, Serialize};
use std::fs;

#[derive(Clone, Deserialize, Serialize, Debug, PartialEq)]
pub struct CdhConfig {
    pub enabled: bool,

    pub tee: TeeConfig,

    pub kbc: KbcConfig,

    #[serde(default, skip_serializing)]
    pub image: ImageConfig,

    #[serde(default)]
    pub credentials: Vec<CredentialConfig>,

    #[serde(default)]
    pub attestation: AttestationConfig,

    #[serde(default)]
    pub resource: ResourceConfig,
}

impl CdhConfig {
    pub fn from_proplet_config(config: &crate::config::PropletConfig) -> Result<Self> {
        let cdh_enabled = config.cdh_enabled.unwrap_or(false);

        let tee_config = TeeConfig {
            override_tee: std::env::var("PROPLET_TEE_OVERRIDE").ok(),
            strict_mode: !matches!(
                std::env::var("PROPLET_TEE_STRICT_MODE").as_deref(),
                Ok("false")
            ),
        };

        let kbc_config = if let Ok(aa_kbc_params) = std::env::var("AA_KBC_PARAMS") {
            let parts: Vec<&str> = aa_kbc_params.split("::").collect();
            if parts.len() == 2 {
                KbcConfig {
                    name: parts[0].to_string(),
                    url: parts[1].to_string(),
                    kbs_cert: std::env::var("KBS_CERT").ok(),
                }
            } else {
                return Err(Error::Configuration(
                    "Invalid AA_KBC_PARAMS format. Expected: kbc_type::url".to_string(),
                ));
            }
        } else {
            KbcConfig {
                name: std::env::var("PROPLET_CDH_KBC_NAME")
                    .unwrap_or_else(|_| "cc_kbc".to_string()),
                url: std::env::var("PROPLET_CDH_KBC_URL")
                    .unwrap_or_else(|_| "https://kbs.example.com:8080".to_string()),
                kbs_cert: std::env::var("PROPLET_CDH_KBS_CERT").ok(),
            }
        };

        let image_config = ImageConfig::from_kernel_cmdline();

        let attestation_config = AttestationConfig {
            aa_socket: std::env::var("PROPLET_CDH_AA_SOCKET").unwrap_or_else(|_| {
                "unix:///run/confidential-containers/attestation-agent/attestation-agent.sock"
                    .to_string()
            }),
            timeout: std::env::var("PROPLET_CDH_AA_TIMEOUT")
                .ok()
                .and_then(|t| t.parse().ok())
                .unwrap_or(10),
        };

        let resource_config = ResourceConfig {
            cache_size: std::env::var("PROPLET_CDH_RESOURCE_CACHE_SIZE")
                .ok()
                .and_then(|s| s.parse().ok())
                .unwrap_or(100),
            cache_ttl: std::env::var("PROPLET_CDH_RESOURCE_CACHE_TTL")
                .ok()
                .and_then(|s| s.parse().ok())
                .unwrap_or(3600),
        };

        Ok(CdhConfig {
            enabled: cdh_enabled,
            tee: tee_config,
            kbc: kbc_config,
            image: image_config,
            credentials: Vec::new(),
            attestation: attestation_config,
            resource: resource_config,
        })
    }

    #[allow(dead_code)]
    pub fn load_from_file(path: &str) -> Result<Self> {
        let content = fs::read_to_string(path).map_err(|e| {
            Error::Configuration(format!("Failed to read config file {}: {}", path, e))
        })?;

        let config: CdhConfig = toml::from_str(&content).map_err(|e| {
            Error::Configuration(format!("Failed to parse config file {}: {}", path, e))
        })?;

        Ok(config)
    }

    #[allow(dead_code)]
    pub fn save_to_file(&self, path: &str) -> Result<()> {
        let content = toml::to_string_pretty(self)
            .map_err(|e| Error::Configuration(format!("Failed to serialize config: {}", e)))?;

        fs::write(path, content).map_err(|e| {
            Error::Configuration(format!("Failed to write config file {}: {}", path, e))
        })?;

        Ok(())
    }

    pub fn set_env_vars(&self) {
        std::env::set_var(
            "AA_KBC_PARAMS",
            format!("{}::{}", self.kbc.name, self.kbc.url),
        );

        if let Some(kbs_cert) = &self.kbc.kbs_cert {
            std::env::set_var("KBS_CERT", kbs_cert);
        }
    }
}

#[derive(Clone, Deserialize, Serialize, Debug, PartialEq)]
pub struct TeeConfig {
    pub override_tee: Option<String>,

    #[serde(default = "default_strict_mode")]
    pub strict_mode: bool,
}

fn default_strict_mode() -> bool {
    true
}

#[derive(Clone, Deserialize, Serialize, Debug, PartialEq)]
pub struct KbcConfig {
    pub name: String,

    pub url: String,

    #[serde(default)]
    pub kbs_cert: Option<String>,
}

#[derive(Clone, Deserialize, Serialize, Debug, PartialEq)]
pub struct CredentialConfig {
    pub path: String,

    pub resource_uri: String,
}

#[derive(Clone, Deserialize, Serialize, Debug, PartialEq)]
pub struct AttestationConfig {
    #[serde(default = "default_aa_socket")]
    pub aa_socket: String,

    #[serde(default = "default_aa_timeout")]
    pub timeout: u64,
}

impl Default for AttestationConfig {
    fn default() -> Self {
        Self {
            aa_socket: default_aa_socket(),
            timeout: default_aa_timeout(),
        }
    }
}

fn default_aa_socket() -> String {
    "unix:///run/confidential-containers/attestation-agent/attestation-agent.sock".to_string()
}

fn default_aa_timeout() -> u64 {
    10
}

#[derive(Clone, Deserialize, Serialize, Debug, PartialEq)]
pub struct ResourceConfig {
    #[serde(default = "default_cache_size")]
    pub cache_size: usize,

    #[serde(default = "default_cache_ttl")]
    pub cache_ttl: u64,
}

impl Default for ResourceConfig {
    fn default() -> Self {
        Self {
            cache_size: default_cache_size(),
            cache_ttl: default_cache_ttl(),
        }
    }
}

fn default_cache_size() -> usize {
    100
}

fn default_cache_ttl() -> u64 {
    3600
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_default_config() {
        let config = CdhConfig {
            enabled: false,
            tee: TeeConfig {
                override_tee: None,
                strict_mode: true,
            },
            kbc: KbcConfig {
                name: "cc_kbc".to_string(),
                url: "https://kbs.example.com:8080".to_string(),
                kbs_cert: None,
            },
            image: ImageConfig::default(),
            credentials: vec![],
            attestation: AttestationConfig {
                aa_socket:
                    "unix:///run/confidential-containers/attestation-agent/attestation-agent.sock"
                        .to_string(),
                timeout: 10,
            },
            resource: ResourceConfig {
                cache_size: 100,
                cache_ttl: 3600,
            },
        };

        let toml_str = toml::to_string_pretty(&config).unwrap();
        let parsed: CdhConfig = toml::from_str(&toml_str).unwrap();

        assert_eq!(config, parsed);
    }
}
