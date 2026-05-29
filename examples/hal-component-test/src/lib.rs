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
            "platform: type={} version={}\n",
            info.platform_type, info.version
        ));

        match crypto::hash(b"hello", crypto::HashAlgorithm::Sha256) {
            Ok(d) => out.push_str(&format!("sha256(hello)={}\n", hex(&d))),
            Err(e) => out.push_str(&format!("hash error: {e}\n")),
        }

        match random::get_secure_random(16) {
            Ok(b) => out.push_str(&format!("random16={}\n", hex(&b))),
            Err(e) => out.push_str(&format!("random error: {e}\n")),
        }

        match clock::get_system_time() {
            Ok(t) => out.push_str(&format!("time: {}s {}ns\n", t.seconds, t.nanoseconds)),
            Err(e) => out.push_str(&format!("clock error: {e}\n")),
        }

        out
    }
}

export!(Component);
