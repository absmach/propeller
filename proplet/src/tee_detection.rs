use std::fs;
use std::path::Path;

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum TeeType {
    Tdx,
    Sev,
    Sgx,
    None,
}

impl TeeType {
    pub fn is_tee(&self) -> bool {
        !matches!(self, TeeType::None)
    }

    #[allow(dead_code)]
    pub fn as_str(&self) -> &str {
        match self {
            TeeType::Tdx => "TDX",
            TeeType::Sev => "SEV",
            TeeType::Sgx => "SGX",
            TeeType::None => "None",
        }
    }
}

#[derive(Debug, Clone)]
pub struct TeeDetection {
    pub tee_type: TeeType,
    #[allow(dead_code)]
    pub detection_method: String,
    #[allow(dead_code)]
    pub details: Option<String>,
}

impl TeeDetection {
    pub fn new(tee_type: TeeType, detection_method: &str) -> Self {
        Self {
            tee_type,
            detection_method: detection_method.to_string(),
            details: None,
        }
    }

    pub fn with_details(tee_type: TeeType, detection_method: &str, details: &str) -> Self {
        Self {
            tee_type,
            detection_method: detection_method.to_string(),
            details: Some(details.to_string()),
        }
    }

    pub fn is_tee(&self) -> bool {
        self.tee_type.is_tee()
    }
}

/// Detects if the system is running inside a Trusted Execution Environment (TEE)
///
/// This function performs automatic detection by checking:
/// 1. Device files specific to each TEE type
/// 2. System files in /sys
/// 3. CPU information
/// 4. Kernel boot messages (dmesg)
///
/// Returns a `TeeDetection` struct with the detected TEE type and detection method.
pub fn detect_tee() -> TeeDetection {
    if let Some(detection) = detect_tdx() {
        return detection;
    }

    if let Some(detection) = detect_sev() {
        return detection;
    }

    if let Some(detection) = detect_sgx() {
        return detection;
    }

    TeeDetection::new(TeeType::None, "no_tee_detected")
}

fn detect_tdx() -> Option<TeeDetection> {
    if Path::new("/dev/tdx_guest").exists() {
        return Some(TeeDetection::with_details(
            TeeType::Tdx,
            "device_file",
            "/dev/tdx_guest exists",
        ));
    }

    if Path::new("/sys/firmware/tdx_guest").exists() {
        return Some(TeeDetection::with_details(
            TeeType::Tdx,
            "sysfs",
            "/sys/firmware/tdx_guest exists",
        ));
    }

    if let Ok(cpuinfo) = fs::read_to_string("/proc/cpuinfo") {
        if cpuinfo.contains("tdx_guest") {
            return Some(TeeDetection::with_details(
                TeeType::Tdx,
                "cpuinfo",
                "tdx_guest flag found in /proc/cpuinfo",
            ));
        }
    }

    if let Ok(output) = std::process::Command::new("dmesg").arg("--kernel").output() {
        if let Ok(dmesg) = String::from_utf8(output.stdout) {
            if dmesg.contains("tdx: Guest initialized") || dmesg.contains("virt/tdx") {
                return Some(TeeDetection::with_details(
                    TeeType::Tdx,
                    "dmesg",
                    "TDX initialization found in kernel messages",
                ));
            }
        }
    }

    None
}

fn detect_sev() -> Option<TeeDetection> {
    if Path::new("/dev/sev").exists() {
        return Some(TeeDetection::with_details(
            TeeType::Sev,
            "device_file",
            "/dev/sev exists",
        ));
    }

    if let Ok(sev_status) = fs::read_to_string(
        "/sys/firmware/efi/efivars/SevStatus-00000000-0000-0000-0000-000000000000",
    ) {
        if !sev_status.is_empty() {
            return Some(TeeDetection::with_details(
                TeeType::Sev,
                "sysfs",
                "SEV status found in EFI variables",
            ));
        }
    }

    if let Ok(cpuinfo) = fs::read_to_string("/proc/cpuinfo") {
        if cpuinfo.contains("sev") || cpuinfo.contains("sev_es") || cpuinfo.contains("sev_snp") {
            let detail = if cpuinfo.contains("sev_snp") {
                "SEV-SNP detected in /proc/cpuinfo"
            } else if cpuinfo.contains("sev_es") {
                "SEV-ES detected in /proc/cpuinfo"
            } else {
                "SEV detected in /proc/cpuinfo"
            };
            return Some(TeeDetection::with_details(TeeType::Sev, "cpuinfo", detail));
        }
    }

    if let Ok(output) = std::process::Command::new("dmesg").arg("--kernel").output() {
        if let Ok(dmesg) = String::from_utf8(output.stdout) {
            if dmesg.contains("AMD Memory Encryption Features active: SEV")
                || dmesg.contains("SEV enabled")
                || dmesg.contains("SEV-SNP")
            {
                return Some(TeeDetection::with_details(
                    TeeType::Sev,
                    "dmesg",
                    "SEV initialization found in kernel messages",
                ));
            }
        }
    }

    None
}

fn detect_sgx() -> Option<TeeDetection> {
    if Path::new("/dev/sgx_enclave").exists() || Path::new("/dev/sgx/enclave").exists() {
        return Some(TeeDetection::with_details(
            TeeType::Sgx,
            "device_file",
            "SGX device file exists",
        ));
    }

    if Path::new("/dev/isgx").exists() {
        return Some(TeeDetection::with_details(
            TeeType::Sgx,
            "device_file",
            "/dev/isgx exists (legacy driver)",
        ));
    }

    if let Ok(cpuinfo) = fs::read_to_string("/proc/cpuinfo") {
        if cpuinfo.contains("sgx") {
            return Some(TeeDetection::with_details(
                TeeType::Sgx,
                "cpuinfo",
                "SGX flag found in /proc/cpuinfo",
            ));
        }
    }

    if let Ok(output) = std::process::Command::new("dmesg").arg("--kernel").output() {
        if let Ok(dmesg) = String::from_utf8(output.stdout) {
            if dmesg.contains("sgx: EPC section") || dmesg.contains("intel_sgx") {
                return Some(TeeDetection::with_details(
                    TeeType::Sgx,
                    "dmesg",
                    "SGX initialization found in kernel messages",
                ));
            }
        }
    }

    None
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_tee_type_is_tee() {
        assert!(TeeType::Tdx.is_tee());
        assert!(TeeType::Sev.is_tee());
        assert!(TeeType::Sgx.is_tee());
        assert!(!TeeType::None.is_tee());
    }

    #[test]
    fn test_tee_type_as_str() {
        assert_eq!(TeeType::Tdx.as_str(), "TDX");
        assert_eq!(TeeType::Sev.as_str(), "SEV");
        assert_eq!(TeeType::Sgx.as_str(), "SGX");
        assert_eq!(TeeType::None.as_str(), "None");
    }

    #[test]
    fn test_tee_detection_new() {
        let detection = TeeDetection::new(TeeType::Tdx, "test_method");
        assert_eq!(detection.tee_type, TeeType::Tdx);
        assert_eq!(detection.detection_method, "test_method");
        assert!(detection.details.is_none());
        assert!(detection.is_tee());
    }

    #[test]
    fn test_tee_detection_with_details() {
        let detection = TeeDetection::with_details(TeeType::Sev, "device_file", "/dev/sev exists");
        assert_eq!(detection.tee_type, TeeType::Sev);
        assert_eq!(detection.detection_method, "device_file");
        assert_eq!(detection.details, Some("/dev/sev exists".to_string()));
        assert!(detection.is_tee());
    }

    #[test]
    fn test_tee_detection_none() {
        let detection = TeeDetection::new(TeeType::None, "no_detection");
        assert_eq!(detection.tee_type, TeeType::None);
        assert!(!detection.is_tee());
    }

    #[test]
    fn test_detect_tee_returns_result() {
        let detection = detect_tee();
        assert!(matches!(
            detection.tee_type,
            TeeType::Tdx | TeeType::Sev | TeeType::Sgx | TeeType::None
        ));
    }

    #[test]
    fn test_detect_tdx_no_tee() {
        if let Some(detection) = detect_tdx() {
            assert_eq!(detection.tee_type, TeeType::Tdx);
        }
    }

    #[test]
    fn test_detect_sev_no_tee() {
        if let Some(detection) = detect_sev() {
            assert_eq!(detection.tee_type, TeeType::Sev);
        }
    }

    #[test]
    fn test_detect_sgx_no_tee() {
        if let Some(detection) = detect_sgx() {
            assert_eq!(detection.tee_type, TeeType::Sgx);
        }
    }
}
