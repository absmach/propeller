#[link(wasm_import_module = "elastic:tee-hal/crypto")]
unsafe extern "C" {
    fn hash(
        data_ptr: i32,
        data_len: i32,
        algo_ptr: i32,
        algo_len: i32,
        out_ptr: i32,
        out_len_ptr: i32,
    ) -> i32;

    #[link_name = "generate-keypair"]
    fn generate_keypair(out_ptr: i32, out_len_ptr: i32) -> i32;
}

#[link(wasm_import_module = "elastic:tee-hal/random")]
unsafe extern "C" {
    #[link_name = "get-random-bytes"]
    fn get_random_bytes(length: i32, out_ptr: i32, out_len_ptr: i32) -> i32;
}

#[link(wasm_import_module = "elastic:tee-hal/clock")]
unsafe extern "C" {
    #[link_name = "system-time"]
    fn system_time(out_secs_ptr: i32, out_ns_ptr: i32) -> i32;
}

#[link(wasm_import_module = "elastic:tee-hal/platform")]
unsafe extern "C" {
    #[link_name = "platform-info"]
    fn platform_info(out_ptr: i32, out_len_ptr: i32) -> i32;
}

#[link(wasm_import_module = "elastic:tee-hal/capabilities")]
unsafe extern "C" {
    #[link_name = "list-capabilities"]
    fn list_capabilities(out_ptr: i32, out_len_ptr: i32) -> i32;
}

const BUF: usize = 4096;

fn hex(bytes: &[u8]) -> String {
    bytes.iter().map(|b| format!("{b:02x}")).collect()
}

fn run() -> i32 {
    let mut ok = true;

    let mut buf = [0u8; BUF];
    let mut len: u32 = 0;
    let rc = unsafe { platform_info(buf.as_mut_ptr() as i32, &mut len as *mut u32 as i32) };
    if rc < 0 {
        ok = false;
    } else {
        println!(
            "platform-info: {}",
            String::from_utf8_lossy(&buf[..len as usize])
        );
    }

    let mut buf = [0u8; BUF];
    let mut len: u32 = 0;
    let rc = unsafe { list_capabilities(buf.as_mut_ptr() as i32, &mut len as *mut u32 as i32) };
    if rc < 0 {
        ok = false;
    } else {
        println!(
            "list-capabilities: {}",
            String::from_utf8_lossy(&buf[..len as usize])
        );
    }

    let mut buf = [0u8; BUF];
    let mut len: u32 = 0;
    let rc = unsafe { get_random_bytes(32, buf.as_mut_ptr() as i32, &mut len as *mut u32 as i32) };
    if rc < 0 || len != 32 {
        ok = false;
    } else {
        println!("random(32): {}", hex(&buf[..32]));
    }

    let mut secs: u64 = 0;
    let mut ns: u32 = 0;
    let rc = unsafe { system_time(&mut secs as *mut u64 as i32, &mut ns as *mut u32 as i32) };
    if rc < 0 {
        ok = false;
    } else {
        println!("system-time: {secs}s {ns}ns");
    }

    let data = b"proplet-hal-test";
    let algo = b"SHA-256";
    let mut buf = [0u8; BUF];
    let mut len: u32 = 0;
    let rc = unsafe {
        hash(
            data.as_ptr() as i32,
            data.len() as i32,
            algo.as_ptr() as i32,
            algo.len() as i32,
            buf.as_mut_ptr() as i32,
            &mut len as *mut u32 as i32,
        )
    };
    if rc < 0 || len != 32 {
        ok = false;
    } else {
        println!(
            "sha256(\"{}\")={}",
            String::from_utf8_lossy(data),
            hex(&buf[..32])
        );
    }

    let mut buf = [0u8; BUF];
    let mut len: u32 = 0;
    let rc = unsafe { generate_keypair(buf.as_mut_ptr() as i32, &mut len as *mut u32 as i32) };
    if rc < 0 {
        ok = false;
    } else {
        println!("generate-keypair: ok (json len={})", len);
    }

    if ok {
        0
    } else {
        1
    }
}

#[no_mangle]
pub extern "C" fn run_export() -> i32 {
    run()
}

fn main() {
    std::process::exit(run() as i32);
}
