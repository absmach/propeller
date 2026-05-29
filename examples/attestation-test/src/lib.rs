wit_bindgen::generate!({ world: "attestation-test", generate_all });

use elastic::hal::{attestation, platform, random};

fn hex(bytes: &[u8]) -> String {
    bytes.iter().map(|b| format!("{b:02x}")).collect()
}

struct Component;

impl Guest for Component {
    fn run_attestation() -> String {
        let mut out = String::new();

        let info = platform::get_platform_info();
        out.push_str(&format!(
            "platform-info: type={} version={}\n",
            info.platform_type, info.version
        ));

        // Use secure randomness as the attestation report-data (nonce),
        // falling back to a fixed value if the host RNG is unavailable.
        let report_data = match random::get_secure_random(32) {
            Ok(b) => b,
            Err(e) => {
                out.push_str(&format!("random: failed ({e}), using fixed nonce\n"));
                vec![0x42u8; 32]
            }
        };

        match attestation::attestation(&report_data) {
            Ok(evidence) => {
                out.push_str(&format!("attestation: ok (evidence len={})\n", evidence.len()));
                out.push_str(&format!("evidence: {}\n", hex(&evidence)));
            }
            Err(e) => out.push_str(&format!("attestation: failed ({e})\n")),
        }

        out
    }
}

export!(Component);
