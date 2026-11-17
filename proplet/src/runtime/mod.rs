pub mod host;
pub mod tee_runtime;
pub mod wasmtime_runtime;

use anyhow::Result;
use async_trait::async_trait;
use elastic_wasi_security::WasiSecurity;
use std::collections::HashMap;

#[derive(Clone, Debug)]
pub struct StartConfig {
    pub id: String,
    pub function_name: String,
    pub daemon: bool,
    pub wasm_binary: Vec<u8>,
    pub cli_args: Vec<String>,
    pub env: HashMap<String, String>,
    pub args: Vec<String>,
    #[allow(dead_code)]
    pub mode: Option<String>,
    /// Per-task filesystem base for the ELASTIC TEE HAL `storage` interface.
    /// Each task gets its own root so concurrent workloads can't collide on
    /// `container_<n>/` directories. When `None`, the runtime derives a
    /// default of `/tmp/proplet/hal-storage/<task-id>`.
    pub hal_storage_path: Option<String>,
    /// Policy parsed from `metadata.wasi_security` before the task started.
    pub wasi_security: Option<WasiSecurity>,
}

#[async_trait]
pub trait Runtime: Send + Sync {
    async fn start_app(&self, ctx: RuntimeContext, config: StartConfig) -> Result<Vec<u8>>;

    async fn stop_app(&self, id: String) -> Result<()>;

    /// Returns the process ID for the task with the given id.
    ///
    /// For runtimes that spawn separate OS processes (e.g., HostRuntime),
    /// this returns the PID of the task's dedicated process.
    /// For in-process runtimes (e.g., WasmtimeRuntime), this returns
    /// the PID of the parent runtime process.
    ///
    /// Returns `None` if:
    /// - The task does not exist or is not running
    /// - The platform does not support PID retrieval
    async fn get_pid(&self, id: &str) -> Result<Option<u32>>;

    async fn precompile(&self, _config: StartConfig) -> Result<()> {
        Err(anyhow::anyhow!(
            "latent tasks are not supported by this runtime"
        ))
    }

    async fn invoke(
        &self,
        _id: String,
        _args: Vec<String>,
        _env: HashMap<String, String>,
    ) -> Result<Vec<u8>> {
        Err(anyhow::anyhow!(
            "latent task invocation is not supported by this runtime"
        ))
    }
}

#[derive(Clone)]
pub struct RuntimeContext {
    #[allow(dead_code)]
    pub proplet_id: String,
}

#[cfg(test)]
mod tests {
    use uuid::Uuid;

    use super::*;

    #[test]
    fn test_runtime_context_creation() {
        let id = Uuid::new_v4();
        let ctx = RuntimeContext {
            proplet_id: id.to_string(),
        };

        assert_eq!(ctx.proplet_id, id.to_string());
    }

    #[test]
    fn test_runtime_context_clone() {
        let id = Uuid::new_v4();
        let ctx1 = RuntimeContext {
            proplet_id: id.to_string(),
        };
        let ctx2 = ctx1.clone();

        assert_eq!(ctx1.proplet_id, ctx2.proplet_id);
    }

    #[test]
    fn test_example_wasi_security_policy_parses() {
        use elastic_wasi_security::NetworkRuleProtocol;

        let raw = include_str!("../../../examples/wasi-security/policy.toml");
        let policy = WasiSecurity::from_toml(raw).expect("example policy should parse");

        assert_eq!(policy.arguments, Some(vec!["--verbose".to_string()]));
        assert_eq!(
            policy.env.as_ref().and_then(|env| env.get("LOG_LEVEL")),
            Some(&"debug".to_string())
        );
        assert_eq!(
            policy.storage_readonly,
            vec![("/srv/models".to_string(), "/models".to_string())]
        );
        assert_eq!(
            policy.storage_mount,
            vec![("/var/lib/task".to_string(), "/data".to_string())]
        );
        assert!(!policy.allow_ip_name_lookup);

        assert_eq!(policy.network_bind.len(), 1);
        assert_eq!(policy.network_bind[0].protocol, NetworkRuleProtocol::Tcp);
        assert_eq!(
            policy.network_bind[0].socket,
            "0.0.0.0:8080".parse().unwrap()
        );

        assert_eq!(policy.network_connect.len(), 1);
        assert_eq!(policy.network_connect[0].protocol, NetworkRuleProtocol::Tcp);
        assert_eq!(
            policy.network_connect[0].socket,
            "10.0.0.5:5432".parse().unwrap()
        );
    }
}
