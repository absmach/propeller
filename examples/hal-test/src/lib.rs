wit_bindgen::generate!({ world: "hal-test", generate_all });

use elastic::hal::{clock, crypto, platform, random};

fn hex(bytes: &[u8]) -> String {
    bytes.iter().map(|b| format!("{b:02x}")).collect()
}

struct Component;

impl Guest for Component {
    fn run_hal_test() -> String {
        let mut out = String::new();

        let info = platform::get_platform_info();
        out.push_str(&format!(
            "platform-info: type={} version={}\n",
            info.platform_type, info.version
        ));

        let caps = platform::list_capabilities();
        out.push_str(&format!("list-capabilities: {} entries\n", caps.len()));

        match random::get_random_bytes(32) {
            Ok(b) => out.push_str(&format!("random(32): {}\n", hex(&b))),
            Err(e) => out.push_str(&format!("random: error {e}\n")),
        }

        match clock::get_system_time() {
            Ok(t) => out.push_str(&format!(
                "system-time: {}s {}ns\n",
                t.seconds, t.nanoseconds
            )),
            Err(e) => out.push_str(&format!("clock: error {e}\n")),
        }

        // Deterministic value — used by proplet's e2e assertion.
        match crypto::hash(b"hello", crypto::HashAlgorithm::Sha256) {
            Ok(d) => out.push_str(&format!("sha256(hello)={}\n", hex(&d))),
            Err(e) => out.push_str(&format!("hash: error {e}\n")),
        }

        match crypto::generate_keypair() {
            Ok(kp) => out.push_str(&format!(
                "generate-keypair: ok (pub={}B priv={}B)\n",
                kp.public_key.len(),
                kp.private_key.len()
            )),
            Err(e) => out.push_str(&format!("generate-keypair: error {e}\n")),
        }

        out
    }
}

export!(Component);
