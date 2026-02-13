use anyhow::Result;
use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::time::{Duration, SystemTime};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub enum TaskState {
    Running,
    Completed,
    Failed,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Proplet {
    pub id: String,
    pub name: String,
    pub task_count: usize,
    pub alive: bool,
    pub alive_history: Vec<SystemTime>,
}

impl Proplet {
    pub fn new(id: String, name: String) -> Self {
        Self {
            id,
            name,
            task_count: 0,
            alive: false,
            alive_history: Vec::new(),
        }
    }

    pub fn set_alive(&mut self, alive: bool) {
        self.alive = alive;
        if alive {
            self.alive_history.push(SystemTime::now());
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct StartRequest {
    pub id: String,
    #[serde(default, deserialize_with = "deserialize_null_default")]
    pub cli_args: Vec<String>,
    pub name: String,
    #[serde(default)]
    pub state: u8,
    #[serde(default, deserialize_with = "deserialize_null_default")]
    pub file: String,
    #[serde(default, deserialize_with = "deserialize_null_default")]
    pub image_url: String,
    #[serde(default, deserialize_with = "deserialize_null_default")]
    pub inputs: Vec<u64>,
    #[serde(default)]
    pub daemon: bool,
    #[serde(default)]
    pub env: Option<HashMap<String, String>>,
    #[serde(rename = "monitoringProfile", default)]
    pub monitoring_profile: Option<MonitoringProfile>,
    #[serde(default)]
    pub encrypted: bool,
    #[serde(default)]
    pub kbs_resource_path: Option<String>,
    #[serde(default)]
    pub mode: Option<String>,
    #[serde(default)]
    pub proplet_id: Option<String>,
}

fn deserialize_null_default<'de, D, T>(deserializer: D) -> std::result::Result<T, D::Error>
where
    T: Default + Deserialize<'de>,
    D: serde::Deserializer<'de>,
{
    let opt = Option::deserialize(deserializer)?;
    Ok(opt.unwrap_or_default())
}

impl StartRequest {
    pub fn validate(&self) -> Result<()> {
        if self.id.is_empty() {
            return Err(anyhow::anyhow!("id is required"));
        }
        if self.name.is_empty() {
            return Err(anyhow::anyhow!("function name is required"));
        }

        if self.encrypted {
            if self.image_url.is_empty() {
                return Err(anyhow::anyhow!(
                    "image_url is required for encrypted workloads"
                ));
            }
            if !self.file.is_empty() {
                return Err(anyhow::anyhow!(
                    "encrypted workloads should only use image_url, not file"
                ));
            }
        } else if self.file.is_empty() && self.image_url.is_empty() {
            return Err(anyhow::anyhow!("either file or image_url must be provided"));
        }
        Ok(())
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct StopRequest {
    pub id: String,
}

impl StopRequest {
    pub fn validate(&self) -> Result<()> {
        if self.id.is_empty() {
            return Err(anyhow::anyhow!("id is required"));
        }

        Ok(())
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Chunk {
    pub app_name: String,
    pub chunk_idx: usize,
    pub total_chunks: usize,
    #[serde(deserialize_with = "deserialize_base64")]
    pub data: Vec<u8>,
}

fn deserialize_base64<'de, D>(deserializer: D) -> std::result::Result<Vec<u8>, D::Error>
where
    D: serde::Deserializer<'de>,
{
    use base64::{engine::general_purpose::STANDARD, Engine};
    use serde::de::Error;

    let s = String::deserialize(deserializer)?;
    STANDARD.decode(&s).map_err(Error::custom)
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct LivelinessMessage {
    pub proplet_id: String,
    pub status: String,
    pub namespace: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DiscoveryMessage {
    pub proplet_id: String,
    pub namespace: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ResultMessage {
    pub task_id: String,
    pub proplet_id: String,
    pub results: String,
    pub error: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct MonitoringProfile {
    pub enabled: bool,
    #[serde(with = "serde_duration")]
    pub interval: Duration,
    pub collect_cpu: bool,
    pub collect_memory: bool,
    pub collect_disk_io: bool,
    pub collect_threads: bool,
    pub collect_file_descriptors: bool,
    pub export_to_mqtt: bool,
    pub retain_history: bool,
    pub history_size: usize,
}

impl Default for MonitoringProfile {
    fn default() -> Self {
        Self {
            enabled: true,
            interval: Duration::from_secs(10),
            collect_cpu: true,
            collect_memory: true,
            collect_disk_io: true,
            collect_threads: true,
            collect_file_descriptors: true,
            export_to_mqtt: true,
            retain_history: true,
            history_size: 100,
        }
    }
}

mod serde_duration {
    use serde::{Deserialize, Deserializer, Serializer};
    use std::time::Duration;

    pub fn serialize<S>(duration: &Duration, serializer: S) -> Result<S::Ok, S::Error>
    where
        S: Serializer,
    {
        serializer.serialize_u64(duration.as_secs())
    }

    pub fn deserialize<'de, D>(deserializer: D) -> Result<Duration, D::Error>
    where
        D: Deserializer<'de>,
    {
        let secs = u64::deserialize(deserializer)?;
        Ok(Duration::from_secs(secs))
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct MetricsMessage {
    pub task_id: String,
    pub proplet_id: String,
    pub metrics: crate::monitoring::metrics::ProcessMetrics,
    pub aggregated: Option<crate::monitoring::metrics::AggregatedMetrics>,
    pub timestamp: SystemTime,
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;
    use uuid::Uuid;

    #[test]
    fn test_proplet_new() {
        let id = Uuid::new_v4();
        let proplet = Proplet::new(id.to_string(), "test-proplet".to_string());

        assert_eq!(proplet.id, id.to_string());
        assert_eq!(proplet.name, "test-proplet");
        assert_eq!(proplet.task_count, 0);
        assert!(!proplet.alive);
        assert!(proplet.alive_history.is_empty());
    }

    #[test]
    fn test_proplet_set_alive_true() {
        let id = Uuid::new_v4();
        let mut proplet = Proplet::new(id.to_string(), "test".to_string());

        proplet.set_alive(true);

        assert!(proplet.alive);
        assert_eq!(proplet.alive_history.len(), 1);
    }

    #[test]
    fn test_proplet_set_alive_false() {
        let id = Uuid::new_v4();
        let mut proplet = Proplet::new(id.to_string(), "test".to_string());

        proplet.set_alive(false);

        assert!(!proplet.alive);
        assert!(proplet.alive_history.is_empty());
    }

    #[test]
    fn test_proplet_set_alive_multiple_times() {
        let id = Uuid::new_v4();
        let mut proplet = Proplet::new(id.to_string(), "test".to_string());

        proplet.set_alive(true);
        proplet.set_alive(false);
        proplet.set_alive(true);

        assert!(proplet.alive);
        assert_eq!(proplet.alive_history.len(), 2);
    }

    #[test]
    fn test_start_request_validate_success_with_file() {
        let req = StartRequest {
            id: "task-123".to_string(),
            cli_args: vec!["--invoke".to_string(), "main".to_string()],
            name: "test_function".to_string(),
            state: 0,
            file: "base64encodeddata".to_string(),
            image_url: String::new(),
            inputs: vec![1, 2, 3],
            daemon: false,
            env: Some(HashMap::new()),
            monitoring_profile: None,
            encrypted: false,
            kbs_resource_path: None,
            mode: None,
            proplet_id: None,
        };

        assert!(req.validate().is_ok());
    }

    #[test]
    fn test_start_request_validate_success_with_image_url() {
        let req = StartRequest {
            id: "task-456".to_string(),
            cli_args: vec![],
            name: "test_function".to_string(),
            state: 0,
            file: String::new(),
            image_url: "registry.example.com/app:v1".to_string(),
            inputs: vec![],
            daemon: true,
            env: None,
            monitoring_profile: None,
            encrypted: false,
            kbs_resource_path: None,
            mode: None,
            proplet_id: None,
        };

        assert!(req.validate().is_ok());
    }

    #[test]
    fn test_start_request_validate_empty_id() {
        let req = StartRequest {
            id: String::new(),
            cli_args: vec![],
            name: "test".to_string(),
            state: 0,
            file: "data".to_string(),
            image_url: String::new(),
            inputs: vec![],
            daemon: false,
            env: None,
            monitoring_profile: None,
            encrypted: false,
            kbs_resource_path: None,
            mode: None,
            proplet_id: None,
        };

        let result = req.validate();
        assert!(result.is_err());
        assert_eq!(result.unwrap_err().to_string(), "id is required");
    }

    #[test]
    fn test_start_request_validate_empty_name() {
        let req = StartRequest {
            id: "task-123".to_string(),
            cli_args: vec![],
            name: String::new(),
            state: 0,
            file: "data".to_string(),
            image_url: String::new(),
            inputs: vec![],
            daemon: false,
            env: None,
            monitoring_profile: None,
            encrypted: false,
            kbs_resource_path: None,
            mode: None,
            proplet_id: None,
        };

        let result = req.validate();
        assert!(result.is_err());
        assert_eq!(result.unwrap_err().to_string(), "function name is required");
    }

    #[test]
    fn test_start_request_validate_no_file_or_image() {
        let req = StartRequest {
            id: "task-123".to_string(),
            cli_args: vec![],
            name: "test".to_string(),
            state: 0,
            file: String::new(),
            image_url: String::new(),
            inputs: vec![],
            daemon: false,
            env: None,
            monitoring_profile: None,
            encrypted: false,
            kbs_resource_path: None,
            mode: None,
            proplet_id: None,
        };

        let result = req.validate();
        assert!(result.is_err());
        assert_eq!(
            result.unwrap_err().to_string(),
            "either file or image_url must be provided"
        );
    }

    #[test]
    fn test_start_request_validate_encrypted_with_valid_image_url() {
        let req = StartRequest {
            id: "task-encrypted-1".to_string(),
            cli_args: vec![],
            name: "encrypted_function".to_string(),
            state: 0,
            file: String::new(),
            image_url: "docker.io/user/wasm:encrypted".to_string(),
            inputs: vec![],
            daemon: false,
            env: None,
            monitoring_profile: None,
            encrypted: true,
            kbs_resource_path: Some("default/key1/value".to_string()),
            mode: None,
            proplet_id: None,
        };

        assert!(req.validate().is_ok());
    }

    #[test]
    fn test_start_request_validate_encrypted_requires_image_url() {
        let req = StartRequest {
            id: "task-encrypted-2".to_string(),
            cli_args: vec![],
            name: "encrypted_function".to_string(),
            state: 0,
            file: String::new(),
            image_url: String::new(),
            inputs: vec![],
            daemon: false,
            env: None,
            monitoring_profile: None,
            encrypted: true,
            kbs_resource_path: Some("default/key1/value".to_string()),
            mode: None,
            proplet_id: None,
        };

        let result = req.validate();
        assert!(result.is_err());
        assert_eq!(
            result.unwrap_err().to_string(),
            "image_url is required for encrypted workloads"
        );
    }

    #[test]
    fn test_start_request_validate_encrypted_rejects_file() {
        let req = StartRequest {
            id: "task-encrypted-3".to_string(),
            cli_args: vec![],
            name: "encrypted_function".to_string(),
            state: 0,
            file: "base64data".to_string(),
            image_url: "docker.io/user/wasm:encrypted".to_string(),
            inputs: vec![],
            daemon: false,
            env: None,
            monitoring_profile: None,
            encrypted: true,
            kbs_resource_path: Some("default/key1/value".to_string()),
            mode: None,
            proplet_id: None,
        };

        let result = req.validate();
        assert!(result.is_err());
        assert_eq!(
            result.unwrap_err().to_string(),
            "encrypted workloads should only use image_url, not file"
        );
    }

    #[test]
    fn test_start_request_deserialize_with_nulls() {
        let json_data = json!({
            "id": "task-789",
            "name": "test_func",
            "cli_args": null,
            "file": null,
            "image_url": "registry.example.com/app:v1",
            "inputs": null,
            "daemon": false,
            "env": null,
            "state": 1
        });

        let req: StartRequest = serde_json::from_value(json_data).unwrap();

        assert_eq!(req.id, "task-789");
        assert_eq!(req.name, "test_func");
        assert!(req.cli_args.is_empty());
        assert!(req.file.is_empty());
        assert_eq!(req.image_url, "registry.example.com/app:v1");
        assert!(req.inputs.is_empty());
        assert!(!req.daemon);
        assert!(req.env.is_none());
        assert_eq!(req.state, 1);
    }

    #[test]
    fn test_start_request_deserialize_complete() {
        let json_data = json!({
            "id": "task-complete",
            "name": "complete_func",
            "cli_args": ["--arg1", "value1"],
            "file": "ZGF0YQ==",
            "image_url": "",
            "inputs": [10, 20, 30],
            "daemon": true,
            "env": {
                "KEY1": "value1",
                "KEY2": "value2"
            },
            "state": 2
        });

        let req: StartRequest = serde_json::from_value(json_data).unwrap();

        assert_eq!(req.id, "task-complete");
        assert_eq!(req.cli_args.len(), 2);
        assert_eq!(req.inputs, vec![10, 20, 30]);
        assert!(req.daemon);
        assert_eq!(req.env.as_ref().unwrap().len(), 2);
    }

    #[test]
    fn test_stop_request_validate_success() {
        let req = StopRequest {
            id: "task-stop-123".to_string(),
        };

        assert!(req.validate().is_ok());
    }

    #[test]
    fn test_stop_request_validate_empty_id() {
        let req = StopRequest { id: String::new() };

        let result = req.validate();
        assert!(result.is_err());
        assert_eq!(result.unwrap_err().to_string(), "id is required");
    }

    #[test]
    fn test_chunk_deserialize_with_base64() {
        let json_data = json!({
            "app_name": "my-app",
            "chunk_idx": 2,
            "total_chunks": 10,
            "data": "aGVsbG8gd29ybGQ="  // "hello world" in base64
        });

        let chunk: Chunk = serde_json::from_value(json_data).unwrap();

        assert_eq!(chunk.app_name, "my-app");
        assert_eq!(chunk.chunk_idx, 2);
        assert_eq!(chunk.total_chunks, 10);
        assert_eq!(chunk.data, b"hello world");
    }

    #[test]
    fn test_chunk_deserialize_invalid_base64() {
        let json_data = json!({
            "app_name": "my-app",
            "chunk_idx": 0,
            "total_chunks": 1,
            "data": "not-valid-base64!@#$"
        });

        let result: Result<Chunk, _> = serde_json::from_value(json_data);
        assert!(result.is_err());
    }

    #[test]
    fn test_liveliness_message_serialization() {
        let msg = LivelinessMessage {
            proplet_id: "proplet-123".to_string(),
            status: "alive".to_string(),
            namespace: "default".to_string(),
        };

        let json = serde_json::to_string(&msg).unwrap();
        let deserialized: LivelinessMessage = serde_json::from_str(&json).unwrap();

        assert_eq!(deserialized.proplet_id, "proplet-123");
        assert_eq!(deserialized.status, "alive");
        assert_eq!(deserialized.namespace, "default");
    }

    #[test]
    fn test_discovery_message_serialization() {
        let msg = DiscoveryMessage {
            proplet_id: "proplet-456".to_string(),
            namespace: "prod".to_string(),
        };

        let json = serde_json::to_string(&msg).unwrap();
        let deserialized: DiscoveryMessage = serde_json::from_str(&json).unwrap();

        assert_eq!(deserialized.proplet_id, "proplet-456");
        assert_eq!(deserialized.namespace, "prod");
    }

    #[test]
    fn test_result_message_with_success() {
        let msg = ResultMessage {
            task_id: "task-result-1".to_string(),
            proplet_id: Uuid::new_v4().to_string(),
            results: String::from("hello world"),
            error: None,
        };

        let json = serde_json::to_string(&msg).unwrap();
        let deserialized: ResultMessage = serde_json::from_str(&json).unwrap();

        assert_eq!(deserialized.task_id, "task-result-1");
        assert_eq!(deserialized.results, "hello world");
        assert!(deserialized.error.is_none());
    }

    #[test]
    fn test_result_message_with_error() {
        let msg = ResultMessage {
            task_id: "task-result-2".to_string(),
            proplet_id: Uuid::new_v4().to_string(),
            results: String::new(),
            error: Some("Execution failed".to_string()),
        };

        let json = serde_json::to_string(&msg).unwrap();
        let deserialized: ResultMessage = serde_json::from_str(&json).unwrap();

        assert_eq!(deserialized.task_id, "task-result-2");
        assert!(deserialized.results.is_empty());
        assert_eq!(deserialized.error, Some("Execution failed".to_string()));
    }

    #[test]
    fn test_task_state_serialization() {
        let states = vec![TaskState::Running, TaskState::Completed, TaskState::Failed];

        for state in states {
            let json = serde_json::to_string(&state).unwrap();
            let deserialized: TaskState = serde_json::from_str(&json).unwrap();

            match (state, deserialized) {
                (TaskState::Running, TaskState::Running) => (),
                (TaskState::Completed, TaskState::Completed) => (),
                (TaskState::Failed, TaskState::Failed) => (),
                _ => panic!("Serialization round-trip failed"),
            }
        }
    }

    #[test]
    fn test_deserialize_null_default_with_string() {
        let json_data = json!({
            "id": "test",
            "name": "func",
            "file": null,
            "image_url": "url",
            "state": 0
        });

        let req: StartRequest = serde_json::from_value(json_data).unwrap();
        assert!(req.file.is_empty());
    }

    #[test]
    fn test_deserialize_null_default_with_vec() {
        let json_data = json!({
            "id": "test",
            "name": "func",
            "cli_args": null,
            "file": "data",
            "image_url": "",
            "inputs": null,
            "state": 0
        });

        let req: StartRequest = serde_json::from_value(json_data).unwrap();
        assert!(req.cli_args.is_empty());
        assert!(req.inputs.is_empty());
    }

    #[test]
    fn test_start_request_with_env_vars() {
        let mut env = HashMap::new();
        env.insert("VAR1".to_string(), "value1".to_string());
        env.insert("VAR2".to_string(), "value2".to_string());

        let req = StartRequest {
            id: "task-env".to_string(),
            cli_args: vec![],
            name: "env_test".to_string(),
            state: 0,
            file: "data".to_string(),
            image_url: String::new(),
            inputs: vec![],
            daemon: false,
            env: Some(env.clone()),
            monitoring_profile: None,
            encrypted: false,
            kbs_resource_path: None,
            mode: None,
            proplet_id: None,
        };

        assert_eq!(req.env.as_ref().unwrap().len(), 2);
        assert_eq!(
            req.env.as_ref().unwrap().get("VAR1"),
            Some(&"value1".to_string())
        );
    }

    #[test]
    fn test_chunk_with_empty_data() {
        let json_data = json!({
            "app_name": "empty-app",
            "chunk_idx": 0,
            "total_chunks": 1,
            "data": ""  // empty base64 string
        });

        let chunk: Chunk = serde_json::from_value(json_data).unwrap();
        assert!(chunk.data.is_empty());
    }

    #[test]
    fn test_chunk_with_large_index() {
        let json_data = json!({
            "app_name": "large-app",
            "chunk_idx": 999,
            "total_chunks": 1000,
            "data": "dGVzdA=="
        });

        let chunk: Chunk = serde_json::from_value(json_data).unwrap();
        assert_eq!(chunk.chunk_idx, 999);
        assert_eq!(chunk.total_chunks, 1000);
    }

    #[test]
    fn test_start_request_cli_args_json_roundtrip() {
        let cli_args = vec![
            "-S".to_string(),
            "nn".to_string(),
            "--dir=/home/proplet/fixture::fixture".to_string(),
        ];

        let mut env = HashMap::new();
        env.insert("OPENVINO_DIR".to_string(), "/opt/intel/openvino".to_string());

        let req = StartRequest {
            id: "wasi-nn-task".to_string(),
            cli_args: cli_args.clone(),
            name: "inference".to_string(),
            state: 0,
            file: String::new(),
            image_url: "registry.example.com/wasi-nn:v1".to_string(),
            inputs: vec![],
            daemon: false,
            env: Some(env.clone()),
            monitoring_profile: None,
            encrypted: false,
            kbs_resource_path: None,
            mode: None,
            proplet_id: None,
        };

        let json = serde_json::to_string(&req).unwrap();
        let deserialized: StartRequest = serde_json::from_str(&json).unwrap();

        assert_eq!(deserialized.cli_args, cli_args);
        assert_eq!(
            deserialized.env.as_ref().unwrap().get("OPENVINO_DIR"),
            Some(&"/opt/intel/openvino".to_string())
        );
    }
}
