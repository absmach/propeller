#[link(wasm_import_module = "elastic:attestation/hal")]
unsafe extern "C" {
    #[link_name = "get-attestation"]
    fn get_attestation(
        nonce_ptr: i32,
        nonce_len: i32,
        out_ptr: i32,
        out_len_ptr: i32,
    ) -> i32;
}

const BUF: usize = 4096;

fn hex(bytes: &[u8]) -> String {
    bytes.iter().map(|b| format!("{b:02x}")).collect()
}

fn run() -> i32 {
    let nonce: [u8; 16] = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16];
    let mut buf = [0u8; BUF];
    let mut len: u32 = 0;
    let rc = unsafe {
        get_attestation(
            nonce.as_ptr() as i32,
            nonce.len() as i32,
            buf.as_mut_ptr() as i32,
            &mut len as *mut u32 as i32,
        )
    };
    if rc < 0 {
        eprintln!(
            "get-attestation: FAILED (rc={}) msg={}",
            rc,
            String::from_utf8_lossy(&buf[..len as usize])
        );
        return 1;
    }
    println!("get-attestation: ok (report len={})", len);
    println!("get-attestation report: {}", hex(&buf[..len as usize]));
    0
}

#[no_mangle]
pub extern "C" fn run_export() -> i32 {
    run()
}

fn main() {
    let result = run();
    if result != 0 {
        eprintln!("attestation-test failed with exit code: {}", result);
    }
}
