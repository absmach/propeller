use crate::jsonrpc::{Error as RpcError, Request as RpcRequest, Response as RpcResponse};
use crate::runtime::{Runtime, RuntimeContext, StartConfig};
use http_body_util::{BodyExt, Full};
use hyper::body::{Bytes, Incoming};
use hyper::server::conn::http1;
use hyper::service::service_fn;
use hyper::{Request, Response};
use hyper_util::rt::{TokioIo, TokioTimer};
use serde_json::Value;
use std::collections::HashMap;
use std::sync::Arc;
use std::time::Duration;
use tokio::net::TcpListener;
use tokio::sync::{oneshot, Mutex};
use tracing::{info, warn};

const MAX_BODY_BYTES: u64 = 1024 * 1024;
const CONNECTION_TIMEOUT_SECS: u64 = 120;

#[derive(Clone)]
pub struct DeployedFunction {
    pub task_id: String,
    pub wasm_binary: Arc<Vec<u8>>,
}

pub struct RpcState {
    pub functions: Mutex<HashMap<String, DeployedFunction>>,
    pub runtime: Arc<dyn Runtime>,
    pub proplet_id: String,
    pub token: Option<String>,
}

impl RpcState {
    pub fn new(runtime: Arc<dyn Runtime>, proplet_id: String, token: Option<String>) -> Self {
        Self {
            functions: Mutex::new(HashMap::new()),
            runtime,
            proplet_id,
            token,
        }
    }

    pub async fn register(&self, name: String, function: DeployedFunction) {
        self.functions.lock().await.insert(name, function);
    }

    pub async fn deregister(&self, task_id: &str) {
        self.functions
            .lock()
            .await
            .retain(|_, f| f.task_id != task_id);
    }

    async fn lookup(&self, method: &str) -> Option<DeployedFunction> {
        self.functions.lock().await.get(method).cloned()
    }
}

pub async fn serve_rpc(
    port: u16,
    bind_address: String,
    state: Arc<RpcState>,
    mut shutdown: oneshot::Receiver<()>,
) {
    let listener = match TcpListener::bind(format!("{bind_address}:{port}")).await {
        Ok(l) => l,
        Err(e) => {
            warn!("Failed to bind RPC server on {bind_address}:{port}: {e}");
            return;
        }
    };
    info!("RPC server listening on {bind_address}:{port}");

    loop {
        tokio::select! {
            _ = &mut shutdown => {
                info!("RPC server shutting down");
                return;
            }
            accept = listener.accept() => {
                match accept {
                    Ok((stream, _)) => {
                        let state = state.clone();
                        tokio::spawn(async move {
                            let io = TokioIo::new(stream);
                            let svc = service_fn(move |req: Request<Incoming>| {
                                let state = state.clone();
                                async move { handle(req, state).await }
                            });
                            let conn = http1::Builder::new()
                                .keep_alive(true)
                                .timer(TokioTimer::new())
                                .serve_connection(io, svc);
                            match tokio::time::timeout(
                                Duration::from_secs(CONNECTION_TIMEOUT_SECS), conn
                            ).await {
                                Ok(Err(e)) => warn!("RPC connection error: {e}"),
                                Err(_elapsed) => tracing::debug!("RPC connection idle timeout"),
                                Ok(Ok(())) => {}
                            }
                        });
                    }
                    Err(e) => warn!("RPC accept error: {e}"),
                }
            }
        }
    }
}

fn json_response(status: u16, body: String) -> Response<Full<Bytes>> {
    Response::builder()
        .status(status)
        .header("Content-Type", "application/json")
        .body(Full::new(Bytes::from(body)))
        .unwrap_or_else(|_| Response::new(Full::new(Bytes::from("{}"))))
}

fn error_response(id: Option<Value>, error: RpcError) -> Response<Full<Bytes>> {
    let response = RpcResponse::failure(id, error);
    let body = serde_json::to_string(&response).unwrap_or_else(|_| "{}".to_string());

    json_response(200, body)
}

fn authorized(req: &Request<Incoming>, token: &Option<String>) -> bool {
    let Some(expected) = token else {
        return true;
    };

    req.headers()
        .get("authorization")
        .and_then(|v| v.to_str().ok())
        .and_then(|v| v.strip_prefix("Bearer "))
        .is_some_and(|presented| presented == expected)
}

async fn handle(
    req: Request<Incoming>,
    state: Arc<RpcState>,
) -> Result<Response<Full<Bytes>>, std::convert::Infallible> {
    if req.uri().path() != "/rpc" {
        return Ok(json_response(404, r#"{"error":"not found"}"#.to_string()));
    }
    if req.method() != hyper::Method::POST {
        return Ok(json_response(
            405,
            r#"{"error":"method not allowed"}"#.to_string(),
        ));
    }
    if !authorized(&req, &state.token) {
        return Ok(json_response(
            401,
            r#"{"error":"unauthorized"}"#.to_string(),
        ));
    }

    let body = match req.into_body().collect().await {
        Ok(collected) => collected.to_bytes(),
        Err(e) => {
            return Ok(error_response(
                None,
                RpcError::internal(format!("body read failed: {e}")),
            ))
        }
    };
    if body.len() as u64 > MAX_BODY_BYTES {
        return Ok(error_response(
            None,
            RpcError::invalid_params("request body too large"),
        ));
    }

    let rpc_req: RpcRequest = match serde_json::from_slice(&body) {
        Ok(r) => r,
        Err(e) => {
            return Ok(error_response(
                None,
                RpcError::new(crate::jsonrpc::CODE_INVALID_REQUEST, e.to_string()),
            ))
        }
    };
    if rpc_req.jsonrpc != crate::jsonrpc::VERSION {
        return Ok(error_response(
            rpc_req.id,
            RpcError::new(
                crate::jsonrpc::CODE_INVALID_REQUEST,
                "jsonrpc version must be 2.0",
            ),
        ));
    }

    let Some(function) = state.lookup(&rpc_req.method).await else {
        return Ok(error_response(
            rpc_req.id,
            RpcError::method_not_found(&rpc_req.method),
        ));
    };

    let args = match params_to_args(rpc_req.params) {
        Ok(a) => a,
        Err(e) => return Ok(error_response(rpc_req.id, e)),
    };

    let config = StartConfig {
        id: format!("{}-rpc-{}", function.task_id, uuid::Uuid::new_v4()),
        function_name: rpc_req.method.clone(),
        daemon: false,
        wasm_binary: (*function.wasm_binary).clone(),
        cli_args: Vec::new(),
        env: HashMap::new(),
        args,
        mode: None,
        hal_storage_path: None,
    };

    let ctx = RuntimeContext {
        proplet_id: state.proplet_id.clone(),
    };

    match state.runtime.start_app(ctx, config).await {
        Ok(output) => {
            let text = String::from_utf8_lossy(&output).to_string();
            let result = serde_json::from_str::<Value>(&text).unwrap_or(Value::String(text));
            let response = RpcResponse::success(rpc_req.id, result);
            let body = serde_json::to_string(&response).unwrap_or_else(|_| "{}".to_string());

            Ok(json_response(200, body))
        }
        Err(e) => Ok(error_response(
            rpc_req.id,
            RpcError::internal(format!("{e:#}")),
        )),
    }
}

fn params_to_args(params: Option<Value>) -> Result<Vec<String>, RpcError> {
    match params {
        None | Some(Value::Null) => Ok(Vec::new()),
        Some(Value::Array(values)) => values
            .into_iter()
            .map(|v| serde_json::to_string(&v).map_err(|e| RpcError::invalid_params(e.to_string())))
            .collect(),
        Some(Value::Object(map)) => map
            .into_iter()
            .map(|(_, v)| {
                serde_json::to_string(&v).map_err(|e| RpcError::invalid_params(e.to_string()))
            })
            .collect(),
        Some(_) => Err(RpcError::invalid_params(
            "params must be an array or an object",
        )),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn test_params_to_args_accepts_an_array() {
        let args = params_to_args(Some(json!(["world", 5]))).unwrap();
        assert_eq!(args, vec!["\"world\"".to_string(), "5".to_string()]);
    }

    #[test]
    fn test_params_to_args_accepts_absent_params() {
        assert!(params_to_args(None).unwrap().is_empty());
        assert!(params_to_args(Some(Value::Null)).unwrap().is_empty());
    }

    #[test]
    fn test_params_to_args_rejects_a_scalar() {
        let err = params_to_args(Some(json!(5))).unwrap_err();
        assert_eq!(err.code, crate::jsonrpc::CODE_INVALID_PARAMS);
    }

    #[tokio::test]
    async fn test_register_and_deregister() {
        let state = RpcState::new(
            Arc::new(
                crate::runtime::wasmtime_runtime::WasmtimeRuntime::new_with_options(
                    false,
                    false,
                    false,
                    Vec::new(),
                    8222,
                    None,
                    false,
                )
                .unwrap(),
            ),
            "proplet-1".to_string(),
            None,
        );

        state
            .register(
                "greet".to_string(),
                DeployedFunction {
                    task_id: "task-1".to_string(),
                    wasm_binary: Arc::new(Vec::new()),
                },
            )
            .await;
        assert!(state.lookup("greet").await.is_some());

        state.deregister("task-1").await;
        assert!(state.lookup("greet").await.is_none());
    }
}
