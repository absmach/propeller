use std::collections::HashMap;
use std::mem;

use serde::{Deserialize, Serialize};

#[derive(Deserialize)]
struct TaskInfo {
    #[serde(default)]
    name: String,
    #[serde(default)]
    env: Option<HashMap<String, String>>,
}

#[derive(Deserialize)]
struct AuthContext {
    #[serde(default)]
    user_id: String,
    #[serde(default)]
    action: String,
}

#[derive(Deserialize)]
struct AuthorizeRequest {
    context: AuthContext,
    task: TaskInfo,
}

#[derive(Serialize)]
struct AuthorizeResponse {
    allow: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    reason: Option<String>,
}

#[derive(Deserialize)]
struct EnrichRequest {
    task: TaskInfo,
}

#[derive(Serialize)]
struct EnrichResponse {
    env: HashMap<String, String>,
}

#[no_mangle]
pub extern "C" fn plugin_alloc(len: u32) -> u32 {
    let mut buf: Vec<u8> = Vec::with_capacity(len as usize);
    let ptr = buf.as_mut_ptr();
    mem::forget(buf);

    ptr as usize as u32
}

#[no_mangle]
pub unsafe extern "C" fn plugin_free(ptr: u32, len: u32) {
    if ptr == 0 {
        return;
    }
    let _ = Vec::from_raw_parts(ptr as *mut u8, 0, len as usize);
}

unsafe fn read_input(ptr: u32, len: u32) -> Vec<u8> {
    let slice = std::slice::from_raw_parts(ptr as *const u8, len as usize);
    slice.to_vec()
}

fn write_output<T: Serialize>(value: &T) -> u64 {
    let json = serde_json::to_vec(value).unwrap_or_else(|_| b"{}".to_vec());
    let len = json.len() as u32;
    let ptr = plugin_alloc(len);
    unsafe {
        std::ptr::copy_nonoverlapping(json.as_ptr(), ptr as *mut u8, json.len());
    }

    ((ptr as u64) << 32) | (len as u64)
}

#[no_mangle]
pub unsafe extern "C" fn authorize(ptr: u32, len: u32) -> u64 {
    let bytes = read_input(ptr, len);
    let req: AuthorizeRequest = match serde_json::from_slice(&bytes) {
        Ok(v) => v,
        Err(e) => {
            return write_output(&AuthorizeResponse {
                allow: false,
                reason: Some(format!("invalid request: {}", e)),
            });
        }
    };

    if req.task.name.is_empty() {
        return write_output(&AuthorizeResponse {
            allow: false,
            reason: Some("task name is required".to_string()),
        });
    }

    if req.context.action == "create" && req.context.user_id.is_empty() {
        return write_output(&AuthorizeResponse {
            allow: false,
            reason: Some("user_id is required to create tasks".to_string()),
        });
    }

    write_output(&AuthorizeResponse {
        allow: true,
        reason: None,
    })
}

#[no_mangle]
pub unsafe extern "C" fn enrich_task(ptr: u32, len: u32) -> u64 {
    let bytes = read_input(ptr, len);
    let req: EnrichRequest = match serde_json::from_slice(&bytes) {
        Ok(v) => v,
        Err(_) => {
            return write_output(&EnrichResponse {
                env: HashMap::new(),
            });
        }
    };

    let mut env = req.task.env.unwrap_or_default();
    env.insert(
        "PROPELLER_PLUGIN_AUTHZ".to_string(),
        "plugin-auth".to_string(),
    );

    write_output(&EnrichResponse { env })
}
