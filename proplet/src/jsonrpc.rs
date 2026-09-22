use serde::{Deserialize, Serialize};
use serde_json::Value;

pub const VERSION: &str = "2.0";

pub const CODE_INVALID_REQUEST: i32 = -32600;
pub const CODE_METHOD_NOT_FOUND: i32 = -32601;
pub const CODE_INVALID_PARAMS: i32 = -32602;
pub const CODE_INTERNAL_ERROR: i32 = -32603;

pub const METHOD_TASK_START: &str = "task.start";
pub const METHOD_TASK_STOP: &str = "task.stop";

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Request {
    pub jsonrpc: String,
    pub method: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub params: Option<Value>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub id: Option<Value>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Error {
    pub code: i32,
    pub message: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub data: Option<Value>,
}

impl Error {
    pub fn new(code: i32, message: impl Into<String>) -> Self {
        Self {
            code,
            message: message.into(),
            data: None,
        }
    }

    pub fn method_not_found(method: &str) -> Self {
        Self {
            code: CODE_METHOD_NOT_FOUND,
            message: "Method not found".to_string(),
            data: Some(Value::String(method.to_string())),
        }
    }

    pub fn invalid_params(detail: impl Into<String>) -> Self {
        Self {
            code: CODE_INVALID_PARAMS,
            message: "Invalid params".to_string(),
            data: Some(Value::String(detail.into())),
        }
    }

    pub fn internal(detail: impl Into<String>) -> Self {
        Self {
            code: CODE_INTERNAL_ERROR,
            message: "Internal error".to_string(),
            data: Some(Value::String(detail.into())),
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Response {
    pub jsonrpc: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub result: Option<Value>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub error: Option<Error>,
    pub id: Value,
}

impl Response {
    pub fn success(id: Option<Value>, result: Value) -> Self {
        Self {
            jsonrpc: VERSION.to_string(),
            result: Some(result),
            error: None,
            id: id.unwrap_or(Value::Null),
        }
    }

    pub fn failure(id: Option<Value>, error: Error) -> Self {
        Self {
            jsonrpc: VERSION.to_string(),
            result: None,
            error: Some(error),
            id: id.unwrap_or(Value::Null),
        }
    }
}

pub struct Envelope {
    pub payload: Value,
    pub id: Option<Value>,
    pub method: Option<String>,
}

pub fn unwrap(value: Value) -> Envelope {
    let is_envelope = value
        .get("jsonrpc")
        .and_then(Value::as_str)
        .is_some_and(|v| v == VERSION);

    if !is_envelope {
        return Envelope {
            payload: value,
            id: None,
            method: None,
        };
    }

    let id = value.get("id").cloned();
    let method = value
        .get("method")
        .and_then(Value::as_str)
        .map(str::to_string);

    let payload = value
        .get("params")
        .or_else(|| value.get("result"))
        .cloned()
        .unwrap_or(Value::Null);

    Envelope {
        payload,
        id,
        method,
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn test_unwrap_passes_through_a_flat_payload() {
        let flat = json!({"id": "task-1", "name": "fn"});
        let envelope = unwrap(flat.clone());

        assert_eq!(envelope.payload, flat);
        assert!(envelope.id.is_none());
        assert!(envelope.method.is_none());
    }

    #[test]
    fn test_unwrap_extracts_params_from_an_envelope() {
        let wrapped = json!({
            "jsonrpc": "2.0",
            "method": "task.start",
            "params": {"id": "task-1", "name": "fn"},
            "id": "task-1"
        });
        let envelope = unwrap(wrapped);

        assert_eq!(envelope.payload, json!({"id": "task-1", "name": "fn"}));
        assert_eq!(envelope.id, Some(json!("task-1")));
        assert_eq!(envelope.method.as_deref(), Some(METHOD_TASK_START));
    }

    #[test]
    fn test_unwrap_extracts_a_result() {
        let wrapped = json!({
            "jsonrpc": "2.0",
            "result": {"ok": true},
            "id": 7
        });
        let envelope = unwrap(wrapped);

        assert_eq!(envelope.payload, json!({"ok": true}));
        assert_eq!(envelope.id, Some(json!(7)));
    }

    #[test]
    fn test_unwrap_ignores_a_wrong_version() {
        let wrapped = json!({"jsonrpc": "1.0", "method": "task.start", "params": {"id": "x"}});
        let envelope = unwrap(wrapped.clone());

        assert_eq!(envelope.payload, wrapped);
        assert!(envelope.method.is_none());
    }

    #[test]
    fn test_unwrap_envelope_without_params_yields_null() {
        let wrapped = json!({"jsonrpc": "2.0", "method": "task.stop", "id": "t"});
        let envelope = unwrap(wrapped);

        assert_eq!(envelope.payload, Value::Null);
        assert_eq!(envelope.method.as_deref(), Some(METHOD_TASK_STOP));
    }

    #[test]
    fn test_response_success_serialisation() {
        let resp = Response::success(Some(json!("t-1")), json!({"ok": true}));
        let encoded = serde_json::to_value(&resp).unwrap();

        assert_eq!(
            encoded,
            json!({"jsonrpc": "2.0", "result": {"ok": true}, "id": "t-1"})
        );
    }

    #[test]
    fn test_response_failure_serialisation() {
        let resp = Response::failure(None, Error::method_not_found("nope"));
        let encoded = serde_json::to_value(&resp).unwrap();

        assert_eq!(
            encoded,
            json!({
                "jsonrpc": "2.0",
                "error": {"code": -32601, "message": "Method not found", "data": "nope"},
                "id": null
            })
        );
    }

    #[test]
    fn test_error_constructors_carry_their_codes() {
        assert_eq!(Error::invalid_params("bad").code, CODE_INVALID_PARAMS);
        assert_eq!(Error::internal("boom").code, CODE_INTERNAL_ERROR);
        assert_eq!(
            Error::new(CODE_INVALID_REQUEST, "bad request").code,
            CODE_INVALID_REQUEST
        );
    }
}
