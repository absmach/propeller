use crate::runtime::StartConfig;
use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use tracing::{debug, info};

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Default)]
pub enum MLBackend {
    #[serde(rename = "standard")]
    Standard,
    #[serde(rename = "tinyml")]
    TinyML,
    #[serde(rename = "auto")]
    #[default]
    Auto,
}

#[allow(dead_code)]
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct FLTaskConfig {
    pub round_id: String,
    pub model_ref: String,
    pub hyperparams: Option<HashMap<String, serde_json::Value>>,
    pub backend: Option<MLBackend>,
    pub proplet_id: Option<String>,
}

#[allow(dead_code)]
pub struct TaskHandler;

#[allow(dead_code)]
impl TaskHandler {
    pub fn parse_and_select_backend(
        task_config: &FLTaskConfig,
        env: &HashMap<String, String>,
    ) -> (MLBackend, StartConfig) {
        let backend = task_config
            .backend
            .clone()
            .unwrap_or_else(|| Self::select_backend_auto(task_config, env));

        info!(
            "Task handler selected backend: {:?} for round: {}",
            backend, task_config.round_id
        );

        let mut task_env = env.clone();
        task_env.insert("ROUND_ID".to_string(), task_config.round_id.clone());
        task_env.insert("MODEL_URI".to_string(), task_config.model_ref.clone());

        if let Some(hyperparams) = &task_config.hyperparams {
            if let Ok(hyperparams_json) = serde_json::to_string(hyperparams) {
                task_env.insert("HYPERPARAMS".to_string(), hyperparams_json);
            }
        }

        task_env.insert(
            "ML_BACKEND".to_string(),
            format!("{:?}", backend).to_lowercase(),
        );

        let start_config = StartConfig {
            id: format!(
                "fl-{}-{}",
                task_config.round_id,
                task_config
                    .proplet_id
                    .as_ref()
                    .unwrap_or(&"unknown".to_string())
            ),
            function_name: "run".to_string(),
            daemon: false,
            wasm_binary: Vec::new(), // Will be set by caller
            cli_args: Vec::new(),
            env: task_env,
            args: Vec::new(),
            mode: Some("train".to_string()),
        };

        (backend, start_config)
    }

    fn select_backend_auto(task_config: &FLTaskConfig, env: &HashMap<String, String>) -> MLBackend {
        if let Some(backend_str) = env.get("ML_BACKEND") {
            match backend_str.to_lowercase().as_str() {
                "standard" => return MLBackend::Standard,
                "tinyml" => return MLBackend::TinyML,
                _ => {}
            }
        }

        if let Some(hyperparams) = &task_config.hyperparams {
            if let Some(batch_size) = hyperparams.get("batch_size") {
                if let Some(bs) = batch_size.as_u64() {
                    if bs <= 8 {
                        debug!("Small batch size detected, selecting TinyML backend");
                        return MLBackend::TinyML;
                    }
                } else if let Some(bs) = batch_size.as_f64() {
                    if bs <= 8.0 {
                        debug!("Small batch size detected, selecting TinyML backend");
                        return MLBackend::TinyML;
                    }
                }
            }
        }

        MLBackend::Standard
    }

    pub fn get_backend_config(backend: &MLBackend) -> HashMap<String, String> {
        let mut config = HashMap::new();

        match backend {
            MLBackend::Standard => {
                config.insert("backend_type".to_string(), "standard".to_string());
                config.insert("max_memory_mb".to_string(), "512".to_string());
                config.insert("supports_gpu".to_string(), "true".to_string());
            }
            MLBackend::TinyML => {
                config.insert("backend_type".to_string(), "tinyml".to_string());
                config.insert("max_memory_mb".to_string(), "64".to_string());
                config.insert("supports_gpu".to_string(), "false".to_string());
            }
            MLBackend::Auto => {}
        }

        config
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_backend_selection_auto() {
        let task_config = FLTaskConfig {
            round_id: "r-001".to_string(),
            model_ref: "fl/models/global_model_v0".to_string(),
            hyperparams: Some({
                let mut h = HashMap::new();
                h.insert(
                    "batch_size".to_string(),
                    serde_json::Value::Number(4.into()),
                );
                h
            }),
            backend: None,
            proplet_id: Some("proplet-1".to_string()),
        };

        let env = HashMap::new();
        let (backend, _) = TaskHandler::parse_and_select_backend(&task_config, &env);

        assert_eq!(backend, MLBackend::TinyML);
    }

    #[test]
    fn test_backend_selection_explicit() {
        let task_config = FLTaskConfig {
            round_id: "r-001".to_string(),
            model_ref: "fl/models/global_model_v0".to_string(),
            hyperparams: None,
            backend: Some(MLBackend::Standard),
            proplet_id: Some("proplet-1".to_string()),
        };

        let env = HashMap::new();
        let (backend, _) = TaskHandler::parse_and_select_backend(&task_config, &env);

        assert_eq!(backend, MLBackend::Standard);
    }
}
