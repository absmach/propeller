use super::{Runtime, RuntimeContext};
use anyhow::{Context, Result};
use async_trait::async_trait;
use std::collections::HashMap;
use std::path::PathBuf;
use std::process::Stdio;
use std::sync::Arc;
use tokio::fs;
use tokio::io::AsyncWriteExt;
use tokio::process::{Child, Command};
use tokio::sync::Mutex;
use tracing::{debug, error, info};
use uuid::Uuid;

pub struct HostRuntime {
    runtime_path: String,
    processes: Arc<Mutex<HashMap<Uuid, Child>>>,
}

impl HostRuntime {
    pub fn new(runtime_path: String) -> Self {
        Self {
            runtime_path,
            processes: Arc::new(Mutex::new(HashMap::new())),
        }
    }

    async fn create_temp_wasm_file(&self, id: Uuid, wasm_binary: &[u8]) -> Result<PathBuf> {
        let temp_dir = std::env::temp_dir();
        let file_path = temp_dir.join(format!("proplet_{}.wasm", id));

        let mut file = fs::File::create(&file_path)
            .await
            .context("Failed to create temporary wasm file")?;

        file.write_all(wasm_binary)
            .await
            .context("Failed to write wasm binary to temp file")?;

        file.flush().await?;

        debug!("Created temporary wasm file: {:?}", file_path);
        Ok(file_path)
    }

    async fn cleanup_temp_file(&self, file_path: PathBuf) -> Result<()> {
        if file_path.exists() {
            fs::remove_file(&file_path)
                .await
                .context("Failed to remove temporary wasm file")?;
            debug!("Cleaned up temporary file: {:?}", file_path);
        }
        Ok(())
    }
}

#[async_trait]
impl Runtime for HostRuntime {
    async fn start_app(
        &self,
        _ctx: RuntimeContext,
        wasm_binary: Vec<u8>,
        cli_args: Vec<String>,
        id: Uuid,
        function_name: String,
        daemon: bool,
        env: HashMap<String, String>,
        args: Vec<f64>,
    ) -> Result<Vec<u8>> {
        info!(
            "Starting Host runtime app: task_id={}, function={}, daemon={}",
            id, function_name, daemon
        );

        // Create temporary wasm file
        let temp_file = self.create_temp_wasm_file(id, &wasm_binary).await?;

        // Build command
        let mut cmd = Command::new(&self.runtime_path);

        // Add wasm file as first argument
        cmd.arg(&temp_file);

        // Add CLI arguments
        for arg in &cli_args {
            cmd.arg(arg);
        }

        // Add numeric parameters as additional arguments
        for arg in &args {
            cmd.arg(arg.to_string());
        }

        // Set environment variables
        cmd.envs(&env);

        // Configure stdio
        cmd.stdout(Stdio::piped())
            .stderr(Stdio::piped())
            .stdin(Stdio::null());

        // Spawn the process
        let mut child = cmd
            .spawn()
            .context("Failed to spawn host runtime process")?;

        debug!("Process spawned with PID: {:?}", child.id());

        if daemon {
            // For daemon mode, store the process and return immediately
            let mut processes = self.processes.lock().await;
            processes.insert(id, child);

            // Spawn a background task to wait for the process and cleanup
            let processes = self.processes.clone();
            let temp_file_clone = temp_file.clone();
            let task_id = id;

            tokio::spawn(async move {
                // Wait for the process to complete
                if let Some(mut process) = processes.lock().await.remove(&task_id) {
                    match process.wait().await {
                        Ok(status) => {
                            info!("Daemon task {} exited with status: {}", task_id, status);
                        }
                        Err(e) => {
                            error!("Daemon task {} wait error: {}", task_id, e);
                        }
                    }
                }

                // Cleanup temp file
                let _ = fs::remove_file(temp_file_clone).await;
            });

            Ok(Vec::new())
        } else {
            // Wait for process to complete
            let output = child
                .wait_with_output()
                .await
                .context("Failed to wait for host runtime process")?;

            // Cleanup temp file
            self.cleanup_temp_file(temp_file).await?;

            if !output.status.success() {
                let stderr = String::from_utf8_lossy(&output.stderr);
                error!("Task {} failed: {}", id, stderr);
                return Err(anyhow::anyhow!(
                    "Process exited with status: {}, stderr: {}",
                    output.status,
                    stderr
                ));
            }

            debug!(
                "Task {} completed successfully, output size: {} bytes",
                id,
                output.stdout.len()
            );

            Ok(output.stdout)
        }
    }

    async fn stop_app(&self, id: Uuid) -> Result<()> {
        info!("Stopping Host runtime app: task_id={}", id);

        let mut processes = self.processes.lock().await;
        if let Some(mut child) = processes.remove(&id) {
            child.kill().await.context("Failed to kill process")?;
            debug!("Process for task {} killed", id);
            Ok(())
        } else {
            Err(anyhow::anyhow!("Task {} not found", id))
        }
    }
}
