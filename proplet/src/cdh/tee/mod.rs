#![allow(dead_code)]

use serde::{Deserialize, Serialize};
use tracing::{info, warn};

pub use tee_detection::detect_tee;
pub use workload_validator::{WorkloadType, WorkloadValidator};

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum TeeType {
    Tdx,
    Snp,
    Sev,
    Se,
    AzureTdxVtpm,
    AzureSnpVtpm,
    None,
    Mock,
}

impl TeeType {
    #[allow(dead_code)]
    pub fn is_tee(&self) -> bool {
        !matches!(self, TeeType::None | TeeType::Mock)
    }

    pub fn name(&self) -> &'static str {
        match self {
            TeeType::Tdx => "Intel TDX",
            TeeType::Snp => "AMD SEV-SNP",
            TeeType::Sev => "AMD SEV",
            TeeType::Se => "IBM Secure Execution",
            TeeType::AzureTdxVtpm => "Azure TDX with vTPM",
            TeeType::AzureSnpVtpm => "Azure SNP with vTPM",
            TeeType::None => "No TEE",
            TeeType::Mock => "Mock TEE (testing only)",
        }
    }
}

impl std::fmt::Display for TeeType {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{}", self.name())
    }
}

pub struct TeeDetector;

impl TeeDetector {
    pub fn detect() -> TeeType {
        info!("Detecting TEE environment...");

        if let Ok(override_tee) = std::env::var("PROPLET_TEE_OVERRIDE") {
            match override_tee.to_lowercase().as_str() {
                "tdx" => {
                    warn!("TEE override: Using Intel TDX (FOR TESTING ONLY)");
                    return TeeType::Tdx;
                }
                "snp" => {
                    warn!("TEE override: Using AMD SEV-SNP (FOR TESTING ONLY)");
                    return TeeType::Snp;
                }
                "sev" => {
                    warn!("TEE override: Using AMD SEV (FOR TESTING ONLY)");
                    return TeeType::Sev;
                }
                "none" => {
                    warn!("TEE override: No TEE (FOR TESTING ONLY)");
                    return TeeType::None;
                }
                "mock" => {
                    warn!("TEE override: Using Mock TEE (FOR TESTING ONLY)");
                    return TeeType::Mock;
                }
                _ => {
                    warn!(
                        "Unknown TEE override value: {}, using auto-detection",
                        override_tee
                    );
                }
            }
        }

        let tee_type = detect_tee();
        info!("Detected TEE type: {}", tee_type);
        tee_type
    }
}

mod tee_detection {
    use super::TeeType;
    use std::path::Path;
    use tracing::debug;

    pub fn detect_tee() -> TeeType {
        if is_tdx() {
            return TeeType::Tdx;
        }

        if is_snp() {
            return TeeType::Snp;
        }

        if is_sev() {
            return TeeType::Sev;
        }

        if is_azure_tdx_vtpm() {
            return TeeType::AzureTdxVtpm;
        }

        if is_azure_snp_vtpm() {
            return TeeType::AzureSnpVtpm;
        }

        if is_se() {
            return TeeType::Se;
        }

        debug!("No TEE detected");
        TeeType::None
    }

    fn is_tdx() -> bool {
        Path::new("/dev/tdx-guest").exists() || has_cpu_flag("tdx") || has_cmdline_flag("tdx")
    }

    fn is_snp() -> bool {
        Path::new("/dev/sev-guest").exists() || has_cpu_flag("sev_snp") || has_cmdline_flag("snp")
    }

    fn is_sev() -> bool {
        has_cpu_flag("sev") || has_cmdline_flag("sev")
    }

    fn is_azure_tdx_vtpm() -> bool {
        Path::new("/dev/tdx-guest").exists()
            && has_cmdline_flag("azure")
            && Path::new("/dev/tpm0").exists()
    }

    fn is_azure_snp_vtpm() -> bool {
        Path::new("/dev/sev-guest").exists()
            && has_cmdline_flag("azure")
            && Path::new("/dev/tpm0").exists()
    }

    fn is_se() -> bool {
        has_cpu_flag("s390") || has_cmdline_flag("prot_virt")
    }

    fn has_cpu_flag(flag: &str) -> bool {
        if let Ok(cpuinfo) = std::fs::read_to_string("/proc/cpuinfo") {
            cpuinfo.to_lowercase().contains(flag)
        } else {
            false
        }
    }

    fn has_cmdline_flag(flag: &str) -> bool {
        if let Ok(cmdline) = std::fs::read_to_string("/proc/cmdline") {
            cmdline.to_lowercase().contains(flag)
        } else {
            false
        }
    }
}

mod workload_validator {
    use super::TeeType;
    use crate::cdh::error::{Error, Result};
    use tracing::{error, info, warn};

    #[derive(Debug, Clone, Copy, PartialEq, Eq)]
    pub enum WorkloadType {
        Encrypted,
        NonEncrypted,
    }

    pub struct WorkloadValidator {
        tee_type: TeeType,
        strict_mode: bool,
    }

    impl WorkloadValidator {
        pub fn new(tee_type: TeeType, strict_mode: bool) -> Self {
            Self {
                tee_type,
                strict_mode,
            }
        }

        pub fn validate(&self, workload_type: WorkloadType) -> Result<()> {
            info!(
                "Validating workload: type={:?}, tee_type={}, strict_mode={}",
                workload_type, self.tee_type, self.strict_mode
            );

            match (workload_type, self.tee_type) {
                (WorkloadType::Encrypted, TeeType::Mock) => {
                    if self.strict_mode {
                        error!("SECURITY VIOLATION: Mock TEE is not allowed in strict mode for encrypted workloads");
                        return Err(Error::TeeValidation(
                            "Encrypted workload detected in Mock TEE with strict mode enabled"
                                .to_string(),
                        ));
                    }
                    warn!("WARNING: Encrypted workload running in Mock TEE (testing only)");
                }
                (WorkloadType::Encrypted, TeeType::None) => {
                    error!("SECURITY VIOLATION: Encrypted workload detected outside TEE");
                    return Err(Error::TeeValidation(
                        "Encrypted workloads MUST execute inside a TEE. Current environment: No TEE".to_string()
                    ));
                }
                (WorkloadType::Encrypted, _) => {
                    info!("Encrypted workload validated in TEE: {}", self.tee_type);
                }
                (WorkloadType::NonEncrypted, _) => {
                    info!("Non-encrypted workload allowed in any environment");
                }
            }

            Ok(())
        }
    }

    impl Default for WorkloadValidator {
        fn default() -> Self {
            Self::new(TeeType::None, true)
        }
    }
}
