use std::collections::HashMap;

use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Serialize, Deserialize, Default)]
pub struct TaskInfo {
    #[serde(default)]
    pub id: String,
    #[serde(default)]
    pub name: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub kind: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub image_url: String,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub inputs: Vec<String>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub cli_args: Vec<String>,
    #[serde(default, skip_serializing_if = "HashMap::is_empty")]
    pub env: HashMap<String, String>,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub proplet_id: String,
    #[serde(default)]
    pub priority: i32,
    #[serde(default)]
    pub daemon: bool,
    #[serde(default)]
    pub encrypted: bool,
}

#[derive(Debug, Clone, Serialize, Deserialize, Default)]
pub struct AuthContext {
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub user_id: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub token: String,
    #[serde(default)]
    pub action: String,
}

#[derive(Debug, Clone, Serialize, Deserialize, Default)]
pub struct AuthorizeRequest {
    pub context: AuthContext,
    pub task: TaskInfo,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct AuthorizeResponse {
    pub allow: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub reason: Option<String>,
}

impl Default for AuthorizeResponse {
    fn default() -> Self {
        Self { allow: true, reason: None }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize, Default)]
pub struct EnrichRequest {
    pub task: TaskInfo,
}

#[derive(Debug, Clone, Serialize, Deserialize, Default)]
pub struct EnrichResponse {
    #[serde(default, skip_serializing_if = "HashMap::is_empty")]
    pub env: HashMap<String, String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub priority: Option<i32>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub inputs: Vec<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize, Default)]
pub struct TaskEvent {
    pub task: TaskInfo,
    #[serde(default)]
    pub success: bool,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub result: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub error: String,
}

pub trait Plugin: Default + 'static {
    fn authorize(&self, req: AuthorizeRequest) -> AuthorizeResponse {
        let _ = req;
        AuthorizeResponse::default()
    }

    fn enrich(&self, req: EnrichRequest) -> EnrichResponse {
        let _ = req;
        EnrichResponse::default()
    }

    fn on_task_start(&self, evt: TaskEvent) {
        let _ = evt;
    }

    fn on_task_complete(&self, evt: TaskEvent) {
        let _ = evt;
    }
}

#[doc(hidden)]
pub mod __private {
    pub use serde_json;

    pub fn alloc_impl(len: u32) -> u32 {
        let mut buf: Vec<u8> = Vec::with_capacity(len as usize);
        let ptr = buf.as_mut_ptr() as u32;
        std::mem::forget(buf);
        ptr
    }

    pub unsafe fn read_bytes(ptr: u32, len: u32) -> Vec<u8> {
        std::slice::from_raw_parts(ptr as *const u8, len as usize).to_vec()
    }

    pub fn write_bytes(bytes: &[u8]) -> u64 {
        let len = bytes.len() as u32;
        let ptr = alloc_impl(len);
        unsafe {
            std::ptr::copy_nonoverlapping(bytes.as_ptr(), ptr as *mut u8, bytes.len());
        }
        ((ptr as u64) << 32) | (len as u64)
    }
}

/// Register a type as a Propeller plugin.
///
/// The type must implement [`Plugin`] and [`Default`]. The macro emits all required
/// WASM exports (`plugin_alloc`, `plugin_free`) and hook exports (`authorize`,
/// `enrich_task`, `on_task_start`, `on_task_complete`).
///
/// ```ignore
/// #[derive(Default)]
/// struct MyPlugin;
///
/// impl Plugin for MyPlugin {
///     fn authorize(&self, req: AuthorizeRequest) -> AuthorizeResponse { ... }
/// }
///
/// register_plugin!(MyPlugin);
/// ```
#[macro_export]
macro_rules! register_plugin {
    ($plugin_type:ty) => {
        static _PROPELLER_PLUGIN: ::std::sync::OnceLock<$plugin_type> =
            ::std::sync::OnceLock::new();

        fn _propeller_plugin() -> &'static $plugin_type {
            _PROPELLER_PLUGIN.get_or_init(::std::default::Default::default)
        }

        #[no_mangle]
        pub extern "C" fn plugin_alloc(len: u32) -> u32 {
            $crate::__private::alloc_impl(len)
        }

        #[no_mangle]
        pub unsafe extern "C" fn plugin_free(ptr: u32, len: u32) {
            if ptr != 0 {
                let _ = ::std::vec::Vec::from_raw_parts(ptr as *mut u8, 0, len as usize);
            }
        }

        #[no_mangle]
        pub unsafe extern "C" fn authorize(ptr: u32, len: u32) -> u64 {
            let bytes = $crate::__private::read_bytes(ptr, len);
            let resp =
                match $crate::__private::serde_json::from_slice::<$crate::AuthorizeRequest>(
                    &bytes,
                ) {
                    Ok(req) => $crate::Plugin::authorize(_propeller_plugin(), req),
                    Err(e) => $crate::AuthorizeResponse {
                        allow: false,
                        reason: Some(format!("invalid request: {e}")),
                    },
                };
            let out = $crate::__private::serde_json::to_vec(&resp)
                .unwrap_or_else(|_| b"{}".to_vec());
            $crate::__private::write_bytes(&out)
        }

        #[no_mangle]
        pub unsafe extern "C" fn enrich_task(ptr: u32, len: u32) -> u64 {
            let bytes = $crate::__private::read_bytes(ptr, len);
            let resp =
                match $crate::__private::serde_json::from_slice::<$crate::EnrichRequest>(&bytes) {
                    Ok(req) => $crate::Plugin::enrich(_propeller_plugin(), req),
                    Err(_) => $crate::EnrichResponse::default(),
                };
            let out = $crate::__private::serde_json::to_vec(&resp)
                .unwrap_or_else(|_| b"{}".to_vec());
            $crate::__private::write_bytes(&out)
        }

        #[no_mangle]
        pub unsafe extern "C" fn on_task_start(ptr: u32, len: u32) {
            let bytes = $crate::__private::read_bytes(ptr, len);
            if let Ok(evt) =
                $crate::__private::serde_json::from_slice::<$crate::TaskEvent>(&bytes)
            {
                $crate::Plugin::on_task_start(_propeller_plugin(), evt);
            }
        }

        #[no_mangle]
        pub unsafe extern "C" fn on_task_complete(ptr: u32, len: u32) {
            let bytes = $crate::__private::read_bytes(ptr, len);
            if let Ok(evt) =
                $crate::__private::serde_json::from_slice::<$crate::TaskEvent>(&bytes)
            {
                $crate::Plugin::on_task_complete(_propeller_plugin(), evt);
            }
        }
    };
}
