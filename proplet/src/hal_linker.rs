use anyhow::Result;
use elastic_tee_hal::{
    interfaces::{
        CapabilitiesInterface, ClockInterface, CryptoInterface, HalProvider, RandomInterface,
    },
    providers::{
        DefaultCapabilitiesProvider, DefaultClockProvider, DefaultCryptoProvider,
        DefaultRandomProvider,
    },
};
use std::collections::HashMap;
use std::sync::{Arc, Mutex};
use tracing::{debug, warn};
use wasmtime::{Caller, Linker};
use wasmtime_wasi::p1::WasiP1Ctx;

type StorageMap = Arc<Mutex<HashMap<(u64, String), Vec<u8>>>>;

fn read_mem(caller: &mut Caller<'_, WasiP1Ctx>, ptr: i32, len: i32) -> Vec<u8> {
    let memory = match caller.get_export("memory") {
        Some(wasmtime::Extern::Memory(m)) => m,
        _ => return Vec::new(),
    };
    let data = memory.data(caller);
    let start = ptr as usize;
    let end = start.saturating_add(len as usize);
    if end > data.len() {
        return Vec::new();
    }
    data[start..end].to_vec()
}

fn write_mem(caller: &mut Caller<'_, WasiP1Ctx>, ptr: i32, len_ptr: i32, bytes: &[u8]) -> i32 {
    let memory = match caller.get_export("memory") {
        Some(wasmtime::Extern::Memory(m)) => m,
        _ => return -1,
    };
    let mem_size = memory.data_size(&mut *caller);
    let start = ptr as usize;
    let end = start.saturating_add(bytes.len());
    if end > mem_size {
        warn!(
            "HAL write_mem: buffer overflow (need {}, have {})",
            end, mem_size
        );
        return -1;
    }
    {
        let data = memory.data_mut(caller);
        data[start..end].copy_from_slice(bytes);
        let len_bytes = (bytes.len() as u32).to_le_bytes();
        let ls = len_ptr as usize;
        data[ls..ls + 4].copy_from_slice(&len_bytes);
    }
    bytes.len() as i32
}

fn read_str(caller: &mut Caller<'_, WasiP1Ctx>, ptr: i32, len: i32) -> String {
    String::from_utf8_lossy(&read_mem(caller, ptr, len)).into_owned()
}

// TODO: replace with elastic_tee_hal::wasmtime_bindings::add_to_linker(linker)
//       once wasmhal ships that module.
pub fn add_to_linker(linker: &mut Linker<WasiP1Ctx>, provider: Arc<HalProvider>) -> Result<()> {
    add_platform(linker, provider.clone())?;
    add_capabilities(linker, provider.clone())?;
    add_crypto(linker)?;
    add_storage(linker)?;
    add_sockets(linker)?;
    add_gpu(linker)?;
    add_resources(linker)?;
    add_events(linker)?;
    add_communication(linker)?;
    add_clock(linker)?;
    add_random(linker)?;
    Ok(())
}

fn add_platform(linker: &mut Linker<WasiP1Ctx>, provider: Arc<HalProvider>) -> Result<()> {
    const NS: &str = "elastic:tee-hal/platform";

    {
        let p = provider.clone();
        linker.func_wrap(
            NS,
            "attestation",
            move |mut caller: Caller<'_, WasiP1Ctx>,
                  data_ptr: i32,
                  data_len: i32,
                  out_ptr: i32,
                  out_len_ptr: i32|
                  -> i32 {
                let report_data = read_mem(&mut caller, data_ptr, data_len);
                let bytes = match p.platform.as_ref() {
                    Some(platform) => match platform.attestation(&report_data) {
                        Ok(b) => b,
                        Err(e) => {
                            warn!("HAL platform/attestation error: {}", e);
                            return -1;
                        }
                    },
                    None => {
                        debug!("HAL platform/attestation: no TEE platform, returning stub");
                        b"{}".to_vec()
                    }
                };
                write_mem(&mut caller, out_ptr, out_len_ptr, &bytes)
            },
        )?;
    }

    {
        let p = provider.clone();
        linker.func_wrap(
            NS,
            "platform-info",
            move |mut caller: Caller<'_, WasiP1Ctx>, out_ptr: i32, out_len_ptr: i32| -> i32 {
                let json = match p.platform.as_ref() {
                    Some(platform) => match platform.platform_info() {
                        Ok((pt, ver, attest)) => format!(
                            r#"{{"platform_type":"{}","version":"{}","attestation_support":{}}}"#,
                            pt, ver, attest
                        ),
                        Err(e) => {
                            warn!("HAL platform/platform-info error: {}", e);
                            return -1;
                        }
                    },
                    None => {
                        r#"{"platform_type":"None","version":"0.0.0","attestation_support":false}"#
                            .to_string()
                    }
                };
                write_mem(&mut caller, out_ptr, out_len_ptr, json.as_bytes())
            },
        )?;
    }

    Ok(())
}

fn add_capabilities(linker: &mut Linker<WasiP1Ctx>, _provider: Arc<HalProvider>) -> Result<()> {
    const NS: &str = "elastic:tee-hal/capabilities";

    linker.func_wrap(
        NS,
        "list-capabilities",
        move |mut caller: Caller<'_, WasiP1Ctx>, out_ptr: i32, out_len_ptr: i32| -> i32 {
            let caps_provider = DefaultCapabilitiesProvider::new();
            let list = match caps_provider.list_capabilities() {
                Ok(l) => l,
                Err(e) => {
                    warn!("HAL capabilities/list-capabilities error: {}", e);
                    return -1;
                }
            };
            let items: Vec<String> = list
                .iter()
                .map(|(name, supported, ver)| {
                    format!(
                        r#"{{"feature_name":"{}","supported":{},"version":"{}"}}"#,
                        name, supported, ver
                    )
                })
                .collect();
            let json = format!("[{}]", items.join(","));
            write_mem(&mut caller, out_ptr, out_len_ptr, json.as_bytes())
        },
    )?;

    linker.func_wrap(
        NS,
        "has-capability",
        move |mut caller: Caller<'_, WasiP1Ctx>, name_ptr: i32, name_len: i32| -> i32 {
            let name = read_str(&mut caller, name_ptr, name_len);
            let caps_provider = DefaultCapabilitiesProvider::new();
            match caps_provider.has_capability(&name) {
                Ok(has) => has as i32,
                Err(e) => {
                    warn!("HAL capabilities/has-capability error: {}", e);
                    -1
                }
            }
        },
    )?;

    Ok(())
}

fn add_crypto(linker: &mut Linker<WasiP1Ctx>) -> Result<()> {
    const NS: &str = "elastic:tee-hal/crypto";

    linker.func_wrap(
        NS,
        "hash",
        |mut caller: Caller<'_, WasiP1Ctx>,
         data_ptr: i32,
         data_len: i32,
         algo_ptr: i32,
         algo_len: i32,
         out_ptr: i32,
         out_len_ptr: i32|
         -> i32 {
            let data = read_mem(&mut caller, data_ptr, data_len);
            let algo = read_str(&mut caller, algo_ptr, algo_len);
            let crypto = DefaultCryptoProvider::new();
            match crypto.hash(&data, &algo) {
                Ok(bytes) => write_mem(&mut caller, out_ptr, out_len_ptr, &bytes),
                Err(e) => {
                    warn!("HAL crypto/hash error: {}", e);
                    -1
                }
            }
        },
    )?;

    linker.func_wrap(
        NS,
        "encrypt",
        |mut caller: Caller<'_, WasiP1Ctx>,
         data_ptr: i32,
         data_len: i32,
         key_ptr: i32,
         key_len: i32,
         algo_ptr: i32,
         algo_len: i32,
         out_ptr: i32,
         out_len_ptr: i32|
         -> i32 {
            let data = read_mem(&mut caller, data_ptr, data_len);
            let key = read_mem(&mut caller, key_ptr, key_len);
            let algo = read_str(&mut caller, algo_ptr, algo_len);
            let crypto = DefaultCryptoProvider::new();
            match crypto.encrypt(&data, &key, &algo) {
                Ok(bytes) => write_mem(&mut caller, out_ptr, out_len_ptr, &bytes),
                Err(e) => {
                    warn!("HAL crypto/encrypt error: {}", e);
                    -1
                }
            }
        },
    )?;

    linker.func_wrap(
        NS,
        "decrypt",
        |mut caller: Caller<'_, WasiP1Ctx>,
         data_ptr: i32,
         data_len: i32,
         key_ptr: i32,
         key_len: i32,
         algo_ptr: i32,
         algo_len: i32,
         out_ptr: i32,
         out_len_ptr: i32|
         -> i32 {
            let data = read_mem(&mut caller, data_ptr, data_len);
            let key = read_mem(&mut caller, key_ptr, key_len);
            let algo = read_str(&mut caller, algo_ptr, algo_len);
            let crypto = DefaultCryptoProvider::new();
            match crypto.decrypt(&data, &key, &algo) {
                Ok(bytes) => write_mem(&mut caller, out_ptr, out_len_ptr, &bytes),
                Err(e) => {
                    warn!("HAL crypto/decrypt error: {}", e);
                    -1
                }
            }
        },
    )?;

    linker.func_wrap(
        NS,
        "generate-keypair",
        |mut caller: Caller<'_, WasiP1Ctx>, out_ptr: i32, out_len_ptr: i32| -> i32 {
            let crypto = DefaultCryptoProvider::new();
            match crypto.generate_keypair() {
                Ok((pub_key, priv_key)) => {
                    let json = format!(
                        r#"{{"public_key":"{}","private_key":"{}"}}"#,
                        hex::encode(&pub_key),
                        hex::encode(&priv_key)
                    );
                    write_mem(&mut caller, out_ptr, out_len_ptr, json.as_bytes())
                }
                Err(e) => {
                    warn!("HAL crypto/generate-keypair error: {}", e);
                    -1
                }
            }
        },
    )?;

    linker.func_wrap(
        NS,
        "sign",
        |mut caller: Caller<'_, WasiP1Ctx>,
         data_ptr: i32,
         data_len: i32,
         key_ptr: i32,
         key_len: i32,
         out_ptr: i32,
         out_len_ptr: i32|
         -> i32 {
            let data = read_mem(&mut caller, data_ptr, data_len);
            let key = read_mem(&mut caller, key_ptr, key_len);
            let crypto = DefaultCryptoProvider::new();
            match crypto.sign(&data, &key) {
                Ok(sig) => write_mem(&mut caller, out_ptr, out_len_ptr, &sig),
                Err(e) => {
                    warn!("HAL crypto/sign error: {}", e);
                    -1
                }
            }
        },
    )?;

    linker.func_wrap(
        NS,
        "verify",
        |mut caller: Caller<'_, WasiP1Ctx>,
         data_ptr: i32,
         data_len: i32,
         sig_ptr: i32,
         sig_len: i32,
         key_ptr: i32,
         key_len: i32|
         -> i32 {
            let data = read_mem(&mut caller, data_ptr, data_len);
            let sig = read_mem(&mut caller, sig_ptr, sig_len);
            let key = read_mem(&mut caller, key_ptr, key_len);
            let crypto = DefaultCryptoProvider::new();
            match crypto.verify(&data, &sig, &key) {
                Ok(valid) => valid as i32,
                Err(e) => {
                    warn!("HAL crypto/verify error: {}", e);
                    -1
                }
            }
        },
    )?;

    Ok(())
}

fn add_storage(linker: &mut Linker<WasiP1Ctx>) -> Result<()> {
    const NS: &str = "elastic:tee-hal/storage";

    let store: StorageMap = Arc::new(Mutex::new(HashMap::new()));
    let next_handle: Arc<Mutex<u64>> = Arc::new(Mutex::new(1));

    {
        let nh = next_handle.clone();
        linker.func_wrap(
            NS,
            "create-container",
            move |mut caller: Caller<'_, WasiP1Ctx>,
                  _name_ptr: i32,
                  _name_len: i32,
                  out_handle_ptr: i32|
                  -> i32 {
                let mut h = nh.lock().unwrap();
                let handle = *h;
                *h += 1;
                let memory = match caller.get_export("memory") {
                    Some(wasmtime::Extern::Memory(m)) => m,
                    _ => return -1,
                };
                let data = memory.data_mut(&mut caller);
                let s = out_handle_ptr as usize;
                data[s..s + 8].copy_from_slice(&handle.to_le_bytes());
                0
            },
        )?;
    }

    {
        let nh = next_handle.clone();
        linker.func_wrap(
            NS,
            "open-container",
            move |mut caller: Caller<'_, WasiP1Ctx>,
                  _name_ptr: i32,
                  _name_len: i32,
                  out_handle_ptr: i32|
                  -> i32 {
                let mut h = nh.lock().unwrap();
                let handle = *h;
                *h += 1;
                let memory = match caller.get_export("memory") {
                    Some(wasmtime::Extern::Memory(m)) => m,
                    _ => return -1,
                };
                let data = memory.data_mut(&mut caller);
                let s = out_handle_ptr as usize;
                data[s..s + 8].copy_from_slice(&handle.to_le_bytes());
                0
            },
        )?;
    }

    linker.func_wrap(
        NS,
        "delete-container",
        |_: Caller<'_, WasiP1Ctx>, _: i64| -> i32 { 0 },
    )?;

    {
        let s = store.clone();
        linker.func_wrap(
            NS,
            "store-object",
            move |mut caller: Caller<'_, WasiP1Ctx>,
                  container: i64,
                  key_ptr: i32,
                  key_len: i32,
                  data_ptr: i32,
                  data_len: i32,
                  out_obj_ptr: i32|
                  -> i32 {
                let key = read_str(&mut caller, key_ptr, key_len);
                let data = read_mem(&mut caller, data_ptr, data_len);
                s.lock().unwrap().insert((container as u64, key), data);
                let memory = match caller.get_export("memory") {
                    Some(wasmtime::Extern::Memory(m)) => m,
                    _ => return -1,
                };
                let mem = memory.data_mut(&mut caller);
                mem[out_obj_ptr as usize..out_obj_ptr as usize + 8]
                    .copy_from_slice(&1u64.to_le_bytes());
                0
            },
        )?;
    }

    {
        let s = store.clone();
        linker.func_wrap(
            NS,
            "retrieve-object",
            move |mut caller: Caller<'_, WasiP1Ctx>,
                  container: i64,
                  key_ptr: i32,
                  key_len: i32,
                  out_ptr: i32,
                  out_len_ptr: i32|
                  -> i32 {
                let key = read_str(&mut caller, key_ptr, key_len);
                match s.lock().unwrap().get(&(container as u64, key)) {
                    Some(data) => {
                        let data = data.clone();
                        write_mem(&mut caller, out_ptr, out_len_ptr, &data)
                    }
                    None => -1,
                }
            },
        )?;
    }

    {
        let s = store.clone();
        linker.func_wrap(
            NS,
            "delete-object",
            move |mut caller: Caller<'_, WasiP1Ctx>,
                  container: i64,
                  key_ptr: i32,
                  key_len: i32|
                  -> i32 {
                let key = read_str(&mut caller, key_ptr, key_len);
                s.lock().unwrap().remove(&(container as u64, key));
                0
            },
        )?;
    }

    {
        let s = store.clone();
        linker.func_wrap(
            NS,
            "list-objects",
            move |mut caller: Caller<'_, WasiP1Ctx>,
                  container: i64,
                  out_ptr: i32,
                  out_len_ptr: i32|
                  -> i32 {
                let keys: Vec<String> = s
                    .lock()
                    .unwrap()
                    .keys()
                    .filter(|(c, _)| *c == container as u64)
                    .map(|(_, k)| format!("\"{}\"", k))
                    .collect();
                let json = format!("[{}]", keys.join(","));
                write_mem(&mut caller, out_ptr, out_len_ptr, json.as_bytes())
            },
        )?;
    }

    {
        let s = store.clone();
        linker.func_wrap(
            NS,
            "get-metadata",
            move |mut caller: Caller<'_, WasiP1Ctx>,
                  container: i64,
                  key_ptr: i32,
                  key_len: i32,
                  out_ptr: i32,
                  out_len_ptr: i32|
                  -> i32 {
                let key = read_str(&mut caller, key_ptr, key_len);
                let size = s
                    .lock()
                    .unwrap()
                    .get(&(container as u64, key))
                    .map(|v| v.len())
                    .unwrap_or(0);
                let json = format!(
                    r#"{{"size":{},"content_type":"application/octet-stream"}}"#,
                    size
                );
                write_mem(&mut caller, out_ptr, out_len_ptr, json.as_bytes())
            },
        )?;
    }

    Ok(())
}

fn add_sockets(linker: &mut Linker<WasiP1Ctx>) -> Result<()> {
    const NS: &str = "elastic:tee-hal/sockets";
    linker.func_wrap(
        NS,
        "create-socket",
        |_: Caller<'_, WasiP1Ctx>, _: i32| -> i32 { -1 },
    )?;
    linker.func_wrap(
        NS,
        "bind",
        |_: Caller<'_, WasiP1Ctx>, _: i64, _: i32, _: i32, _: i32, _: i32| -> i32 { -1 },
    )?;
    linker.func_wrap(
        NS,
        "listen",
        |_: Caller<'_, WasiP1Ctx>, _: i64, _: i32| -> i32 { -1 },
    )?;
    linker.func_wrap(
        NS,
        "connect",
        |_: Caller<'_, WasiP1Ctx>, _: i64, _: i32, _: i32, _: i32, _: i32| -> i32 { -1 },
    )?;
    linker.func_wrap(NS, "accept", |_: Caller<'_, WasiP1Ctx>, _: i64| -> i32 {
        -1
    })?;
    linker.func_wrap(
        NS,
        "send",
        |_: Caller<'_, WasiP1Ctx>, _: i64, _: i32, _: i32| -> i32 { -1 },
    )?;
    linker.func_wrap(
        NS,
        "receive",
        |_: Caller<'_, WasiP1Ctx>, _: i64, _: i32, _: i32, _: i32| -> i32 { -1 },
    )?;
    linker.func_wrap(NS, "close", |_: Caller<'_, WasiP1Ctx>, _: i64| -> i32 { 0 })?;
    Ok(())
}

fn add_gpu(linker: &mut Linker<WasiP1Ctx>) -> Result<()> {
    const NS: &str = "elastic:tee-hal/gpu";
    linker.func_wrap(
        NS,
        "list-adapters",
        |_: Caller<'_, WasiP1Ctx>, _: i32, _: i32| -> i32 { -1 },
    )?;
    linker.func_wrap(
        NS,
        "get-adapter-info",
        |_: Caller<'_, WasiP1Ctx>, _: i64, _: i32, _: i32| -> i32 { -1 },
    )?;
    linker.func_wrap(
        NS,
        "create-device",
        |_: Caller<'_, WasiP1Ctx>, _: i64| -> i32 { -1 },
    )?;
    linker.func_wrap(
        NS,
        "create-buffer",
        |_: Caller<'_, WasiP1Ctx>, _: i64, _: i32, _: i64, _: i32| -> i32 { -1 },
    )?;
    linker.func_wrap(
        NS,
        "write-buffer",
        |_: Caller<'_, WasiP1Ctx>, _: i64, _: i64, _: i32, _: i32| -> i32 { -1 },
    )?;
    linker.func_wrap(
        NS,
        "read-buffer",
        |_: Caller<'_, WasiP1Ctx>, _: i64, _: i64, _: i64, _: i32, _: i32| -> i32 { -1 },
    )?;
    linker.func_wrap(
        NS,
        "create-compute-pipeline",
        |_: Caller<'_, WasiP1Ctx>, _: i64, _: i32, _: i32| -> i32 { -1 },
    )?;
    linker.func_wrap(
        NS,
        "dispatch",
        |_: Caller<'_, WasiP1Ctx>, _: i64, _: i64, _: i32, _: i32, _: i32| -> i32 { -1 },
    )?;
    Ok(())
}

fn add_resources(linker: &mut Linker<WasiP1Ctx>) -> Result<()> {
    const NS: &str = "elastic:tee-hal/resources";

    linker.func_wrap(
        NS,
        "allocate",
        |mut caller: Caller<'_, WasiP1Ctx>,
         _rt: i32,
         amount: i64,
         _prio: i32,
         out_id_ptr: i32,
         out_id_len_ptr: i32,
         out_granted_ptr: i32|
         -> i32 {
            write_mem(&mut caller, out_id_ptr, out_id_len_ptr, b"alloc-ok");
            let memory = match caller.get_export("memory") {
                Some(wasmtime::Extern::Memory(m)) => m,
                _ => return -1,
            };
            let data = memory.data_mut(&mut caller);
            data[out_granted_ptr as usize..out_granted_ptr as usize + 8]
                .copy_from_slice(&(amount as u64).to_le_bytes());
            0
        },
    )?;

    linker.func_wrap(
        NS,
        "deallocate",
        |_: Caller<'_, WasiP1Ctx>, _: i32, _: i32| -> i32 { 0 },
    )?;

    linker.func_wrap(
        NS,
        "query-available",
        |mut caller: Caller<'_, WasiP1Ctx>, _rt: i32, out_ptr: i32| -> i32 {
            let memory = match caller.get_export("memory") {
                Some(wasmtime::Extern::Memory(m)) => m,
                _ => return -1,
            };
            let data = memory.data_mut(&mut caller);
            data[out_ptr as usize..out_ptr as usize + 8].copy_from_slice(&u64::MAX.to_le_bytes());
            0
        },
    )?;

    Ok(())
}

fn add_events(linker: &mut Linker<WasiP1Ctx>) -> Result<()> {
    const NS: &str = "elastic:tee-hal/events";

    linker.func_wrap(
        NS,
        "subscribe",
        |mut caller: Caller<'_, WasiP1Ctx>, _et: i32, out_ptr: i32| -> i32 {
            let memory = match caller.get_export("memory") {
                Some(wasmtime::Extern::Memory(m)) => m,
                _ => return -1,
            };
            let data = memory.data_mut(&mut caller);
            data[out_ptr as usize..out_ptr as usize + 8].copy_from_slice(&1u64.to_le_bytes());
            0
        },
    )?;

    linker.func_wrap(
        NS,
        "unsubscribe",
        |_: Caller<'_, WasiP1Ctx>, _: i64| -> i32 { 0 },
    )?;

    linker.func_wrap(
        NS,
        "poll-events",
        |mut caller: Caller<'_, WasiP1Ctx>, _handle: i64, out_ptr: i32, out_len_ptr: i32| -> i32 {
            write_mem(&mut caller, out_ptr, out_len_ptr, b"[]")
        },
    )?;

    Ok(())
}

fn add_communication(linker: &mut Linker<WasiP1Ctx>) -> Result<()> {
    const NS: &str = "elastic:tee-hal/communication";

    linker.func_wrap(
        NS,
        "send-message",
        |_: Caller<'_, WasiP1Ctx>, _: i32, _: i32, _: i32, _: i32, _: i32, _: i32| -> i32 { 0 },
    )?;

    linker.func_wrap(
        NS,
        "receive-message",
        |mut caller: Caller<'_, WasiP1Ctx>, out_ptr: i32, out_len_ptr: i32| -> i32 {
            write_mem(&mut caller, out_ptr, out_len_ptr, b"null")
        },
    )?;

    linker.func_wrap(
        NS,
        "list-workloads",
        |mut caller: Caller<'_, WasiP1Ctx>, out_ptr: i32, out_len_ptr: i32| -> i32 {
            write_mem(&mut caller, out_ptr, out_len_ptr, b"[]")
        },
    )?;

    Ok(())
}

fn add_clock(linker: &mut Linker<WasiP1Ctx>) -> Result<()> {
    const NS: &str = "elastic:tee-hal/clock";

    linker.func_wrap(
        NS,
        "system-time",
        |mut caller: Caller<'_, WasiP1Ctx>, out_secs_ptr: i32, out_ns_ptr: i32| -> i32 {
            let clock = DefaultClockProvider::new();
            match clock.system_time() {
                Ok((secs, ns)) => {
                    let memory = match caller.get_export("memory") {
                        Some(wasmtime::Extern::Memory(m)) => m,
                        _ => return -1,
                    };
                    let data = memory.data_mut(&mut caller);
                    data[out_secs_ptr as usize..out_secs_ptr as usize + 8]
                        .copy_from_slice(&secs.to_le_bytes());
                    data[out_ns_ptr as usize..out_ns_ptr as usize + 4]
                        .copy_from_slice(&ns.to_le_bytes());
                    0
                }
                Err(e) => {
                    warn!("HAL clock/system-time error: {}", e);
                    -1
                }
            }
        },
    )?;

    linker.func_wrap(
        NS,
        "monotonic-time",
        |mut caller: Caller<'_, WasiP1Ctx>, out_secs_ptr: i32, out_ns_ptr: i32| -> i32 {
            let clock = DefaultClockProvider::new();
            match clock.monotonic_time() {
                Ok((secs, ns)) => {
                    let memory = match caller.get_export("memory") {
                        Some(wasmtime::Extern::Memory(m)) => m,
                        _ => return -1,
                    };
                    let data = memory.data_mut(&mut caller);
                    data[out_secs_ptr as usize..out_secs_ptr as usize + 8]
                        .copy_from_slice(&secs.to_le_bytes());
                    data[out_ns_ptr as usize..out_ns_ptr as usize + 4]
                        .copy_from_slice(&ns.to_le_bytes());
                    0
                }
                Err(e) => {
                    warn!("HAL clock/monotonic-time error: {}", e);
                    -1
                }
            }
        },
    )?;

    linker.func_wrap(
        NS,
        "resolution",
        |mut caller: Caller<'_, WasiP1Ctx>, out_ptr: i32| -> i32 {
            let clock = DefaultClockProvider::new();
            match clock.resolution() {
                Ok(ns) => {
                    let memory = match caller.get_export("memory") {
                        Some(wasmtime::Extern::Memory(m)) => m,
                        _ => return -1,
                    };
                    let data = memory.data_mut(&mut caller);
                    data[out_ptr as usize..out_ptr as usize + 8].copy_from_slice(&ns.to_le_bytes());
                    0
                }
                Err(e) => {
                    warn!("HAL clock/resolution error: {}", e);
                    -1
                }
            }
        },
    )?;

    linker.func_wrap(
        NS,
        "sleep",
        |_: Caller<'_, WasiP1Ctx>, duration_ns: i64| -> i32 {
            let clock = DefaultClockProvider::new();
            match clock.sleep(duration_ns as u64) {
                Ok(()) => 0,
                Err(e) => {
                    warn!("HAL clock/sleep error: {}", e);
                    -1
                }
            }
        },
    )?;

    Ok(())
}

fn add_random(linker: &mut Linker<WasiP1Ctx>) -> Result<()> {
    const NS: &str = "elastic:tee-hal/random";

    linker.func_wrap(
        NS,
        "get-random-bytes",
        |mut caller: Caller<'_, WasiP1Ctx>, length: i32, out_ptr: i32, out_len_ptr: i32| -> i32 {
            let rng = DefaultRandomProvider::new();
            match rng.get_random_bytes(length as u32) {
                Ok(bytes) => write_mem(&mut caller, out_ptr, out_len_ptr, &bytes),
                Err(e) => {
                    warn!("HAL random/get-random-bytes error: {}", e);
                    -1
                }
            }
        },
    )?;

    linker.func_wrap(
        NS,
        "get-secure-random",
        |mut caller: Caller<'_, WasiP1Ctx>, length: i32, out_ptr: i32, out_len_ptr: i32| -> i32 {
            let rng = DefaultRandomProvider::new();
            match rng.get_secure_random(length as u32) {
                Ok(bytes) => write_mem(&mut caller, out_ptr, out_len_ptr, &bytes),
                Err(e) => {
                    warn!("HAL random/get-secure-random error: {}", e);
                    -1
                }
            }
        },
    )?;

    linker.func_wrap(
        NS,
        "get-entropy-info",
        |mut caller: Caller<'_, WasiP1Ctx>, out_ptr: i32, out_len_ptr: i32| -> i32 {
            let hw = elastic_tee_hal::random::hardware_rng::is_hardware_rng_available();
            let json = format!(
                r#"{{"source":"{}","quality":255,"available_bytes":1048576}}"#,
                if hw { "hardware" } else { "platform" }
            );
            write_mem(&mut caller, out_ptr, out_len_ptr, json.as_bytes())
        },
    )?;

    linker.func_wrap(
        NS,
        "reseed",
        |_: Caller<'_, WasiP1Ctx>, _: i32, _: i32| -> i32 { 0 },
    )?;

    Ok(())
}
