#[link(wasm_import_module = "elastic:tee-hal/platform")]
unsafe extern "C" {
    #[link_name = "platform-info"]
    fn get_platform_info(out_ptr: i32, out_len_ptr: i32) -> i32;

    fn attestation(
        report_data_ptr: i32,
        report_data_len: i32,
        out_ptr: i32,
        out_len_ptr: i32,
    ) -> i32;
}

const BUF: usize = 4096;

fn hex(bytes: &[u8]) -> String {
    bytes.iter().map(|b| format!("{b:02x}")).collect()
}

fn run() -> i32 {
    let mut ok = true;

    {
        let mut buf = [0u8; BUF];
        let mut len: u32 = 0;
        let rc =
            unsafe { get_platform_info(buf.as_mut_ptr() as i32, &mut len as *mut u32 as i32) };
        if rc < 0 {
            eprintln!("get-platform-info: FAILED (rc={})", rc);
            ok = false;
        } else {
            println!(
                "get-platform-info: {}",
                String::from_utf8_lossy(&buf[..len as usize])
            );
        }
    }

    {
        let report_data = [0x42u8; 32];
        let mut buf = [0u8; BUF];
        let mut len: u32 = 0;
        let rc = unsafe {
            attestation(
                report_data.as_ptr() as i32,
                report_data.len() as i32,
                buf.as_mut_ptr() as i32,
                &mut len as *mut u32 as i32,
            )
        };
        if rc < 0 {
            eprintln!(
                "attestation: FAILED (rc={}) msg={}",
                rc,
                String::from_utf8_lossy(&buf[..len as usize])
            );
            ok = false;
        } else {
            println!("attestation: ok (evidence len={})", len);
            println!("attestation evidence: {}", hex(&buf[..len as usize]));
        }
    }

    if ok { 0 } else { 1 }
}

#[no_mangle]
pub extern "C" fn run_export() -> i32 {
    run()
}

fn main() {
    let result = run();
    if result != 0 {
        eprintln!("platform-test failed with exit code: {}", result);
    }
}
