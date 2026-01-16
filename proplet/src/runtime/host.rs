use super::{Runtime, RuntimeContext, StartConfig};
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
use tracing::{debug, error, info, warn};

pub struct HostRuntime {
    runtime_path: String,
    processes: Arc<Mutex<HashMap<String, Child>>>,
    pids: Arc<Mutex<HashMap<String, u32>>>,
}

impl HostRuntime {
    pub fn new(runtime_path: String) -> Self {
        Self {
            runtime_path,
            processes: Arc::new(Mutex::new(HashMap::new())),
            pids: Arc::new(Mutex::new(HashMap::new())),
        }
    }

    async fn create_temp_wasm_file(&self, id: &str, wasm_binary: &[u8]) -> Result<PathBuf> {
        let temp_dir = std::env::temp_dir();
        let file_path = temp_dir.join(format!("proplet_{id}.wasm"));

        let mut file = fs::File::create(&file_path)
            .await
            .context("Failed to create temporary wasm file")?;

        file.write_all(wasm_binary)
            .await
            .context("Failed to write wasm binary to temp file")?;

        file.flush().await?;

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
    async fn start_app(&self, _ctx: RuntimeContext, config: StartConfig) -> Result<Vec<u8>> {
        info!(
            "Starting Host runtime app: task_id={}, function={}, daemon={}, wasm_size={}",
            config.id,
            config.function_name,
            config.daemon,
            config.wasm_binary.len()
        );

        let temp_file = self
            .create_temp_wasm_file(&config.id, &config.wasm_binary)
            .await?;

        let mut cmd = Command::new(&self.runtime_path);

        // Add wasmtime CLI arguments
        cmd.arg("run");

        // If a specific function is requested (and not _start), use --invoke
        // For WASI command modules, _start is the default entry point
        if !config.function_name.is_empty() 
            && config.function_name != "_start" 
            && !config.function_name.starts_with("fl-round-") {
            // This is a specific function name, use --invoke
            cmd.arg("--invoke").arg(&config.function_name);
        }

        // Add any additional CLI args from config
        for arg in &config.cli_args {
            cmd.arg(arg);
        }

        // Pass environment variables to the WASI guest via --env flags.
        // NOTE: These must be passed BEFORE the wasm file argument.
        if !config.env.is_empty() {
            info!("Setting {} environment variables for task {}", config.env.len(), config.id);
            for (key, value) in &config.env {
                debug!("  {}={}", key, value);
                cmd.arg("--env");
                cmd.arg(format!("{}={}", key, value));
            }
        } else {
            warn!("No environment variables provided for task {}", config.id);
        }

        // Add the WASM file
        cmd.arg(&temp_file);

        // Add function arguments (if any). These are passed to the module itself.
        for arg in &config.args {
            cmd.arg(arg.to_string());
        }

        // Also set environment variables for the host process (wasmtime itself)
        // This is less critical for the guest but good for consistency.
        cmd.envs(&config.env);

        cmd.stdout(Stdio::piped())
            .stderr(Stdio::piped())
            .stdin(Stdio::null());

        let child = cmd.spawn().context(format!(
            "Failed to spawn host runtime process: {}. Command: {} {:?}",
            self.runtime_path, self.runtime_path, config.cli_args
        ))?;

        let pid = child.id();
        info!("Process spawned with PID: {:?}", pid);

        if let Some(pid_val) = pid {
            let mut pids = self.pids.lock().await;
            pids.insert(config.id.clone(), pid_val);
        }

        let mut processes = self.processes.lock().await;
        processes.insert(config.id.clone(), child);
        drop(processes);

        if config.daemon {
            info!("Running in daemon mode for task: {}", config.id);

            let processes = self.processes.clone();
            let pids = self.pids.clone();
            let temp_file_clone = temp_file.clone();
            let task_id = config.id.clone();

            tokio::spawn(async move {
                loop {
                    tokio::time::sleep(tokio::time::Duration::from_millis(100)).await;

                    let mut should_cleanup = false;
                    {
                        let mut processes_guard = processes.lock().await;
                        if let Some(process) = processes_guard.get_mut(&task_id) {
                            match process.try_wait() {
                                Ok(Some(status)) => {
                                    info!("Daemon task {} exited with status: {}", task_id, status);
                                    should_cleanup = true;
                                }
                                Ok(None) => {
                                }
                                Err(e) => {
                                    error!("Daemon task {} try_wait error: {}", task_id, e);
                                    should_cleanup = true;
                                }
                            }
                        } else {
                            break;
                        }
                    }

                    if should_cleanup {
                        processes.lock().await.remove(&task_id);
                        pids.lock().await.remove(&task_id);
                        break;
                    }
                }

                let _ = fs::remove_file(temp_file_clone).await;
            });

            info!("Daemon task {} started, returning immediately", config.id);
            Ok(Vec::new())
        } else {
            info!(
                "Running in synchronous mode, waiting for task: {}",
                config.id
            );

            let output = loop {
                tokio::time::sleep(tokio::time::Duration::from_millis(100)).await;

                let mut processes = self.processes.lock().await;
                if let Some(child) = processes.get_mut(&config.id) {
                    match child.try_wait() {
                        Ok(Some(status)) => {
                            drop(processes);

                            let mut child = {
                                let mut processes = self.processes.lock().await;
                                processes.remove(&config.id).ok_or_else(|| {
                                    anyhow::anyhow!("Failed to retrieve child process")
                                })?
                            };

                            let stdout = if let Some(mut stdout_reader) = child.stdout.take() {
                                use tokio::io::AsyncReadExt;
                                let mut buf = Vec::new();
                                stdout_reader
                                    .read_to_end(&mut buf)
                                    .await
                                    .unwrap_or_default();
                                buf
                            } else {
                                Vec::new()
                            };

                            let stderr = if let Some(mut stderr_reader) = child.stderr.take() {
                                use tokio::io::AsyncReadExt;
                                let mut buf = Vec::new();
                                stderr_reader
                                    .read_to_end(&mut buf)
                                    .await
                                    .unwrap_or_default();
                                buf
                            } else {
                                Vec::new()
                            };

                            break std::process::Output {
                                status,
                                stdout,
                                stderr,
                            };
                        }
                        Ok(None) => {
                        }
                        Err(e) => {
                            drop(processes);
                            return Err(anyhow::anyhow!("Failed to check process status: {}", e));
                        }
                    }
                } else {
                    drop(processes);
                    self.cleanup_temp_file(temp_file).await?;
                    return Err(anyhow::anyhow!(
                        "Task {} was stopped before completion",
                        config.id
                    ));
                }
            };

            info!("Process completed for task: {}", config.id);

            self.pids.lock().await.remove(&config.id);
            self.cleanup_temp_file(temp_file).await?;

            if !output.status.success() {
                let stderr = String::from_utf8_lossy(&output.stderr);
                error!("Task {} failed with stderr: {}", config.id, stderr);
                return Err(anyhow::anyhow!(
                    "Process exited with status: {}, stderr: {}",
                    output.status,
                    stderr
                ));
            }

            Ok(output.stdout)
        }
    }

    async fn stop_app(&self, id: String) -> Result<()> {
        info!("Stopping Host runtime app: task_id={}", id);

        self.pids.lock().await.remove(&id);

        let mut processes = self.processes.lock().await;
        if let Some(mut child) = processes.remove(&id) {
            child.kill().await.context("Failed to kill process")?;
            debug!("Process for task {} killed", id);
            Ok(())
        } else {
            Err(anyhow::anyhow!("Task {id} not found"))
        }
    }

    async fn get_pid(&self, id: &str) -> Result<Option<u32>> {
        let pids = self.pids.lock().await;
        if let Some(&pid) = pids.get(id) {
            return Ok(Some(pid));
        }
        drop(pids);

        let processes = self.processes.lock().await;
        if let Some(child) = processes.get(id) {
            Ok(child.id())
        } else {
            Ok(None)
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_host_runtime_new() {
        let runtime = HostRuntime::new("/usr/bin/wasmtime".to_string());
        assert_eq!(runtime.runtime_path, "/usr/bin/wasmtime");
    }

    #[test]
    fn test_temp_file_path_generation() {
        let temp_dir = std::env::temp_dir();
        let task_id = "task-123";
        let expected_path = temp_dir.join(format!("proplet_{}.wasm", task_id));

        assert!(expected_path
            .to_string_lossy()
            .contains("proplet_task-123.wasm"));
    }

    #[test]
    fn test_temp_file_path_with_special_chars() {
        let temp_dir = std::env::temp_dir();
        let task_id = "task-with-dashes-123";
        let file_path = temp_dir.join(format!("proplet_{}.wasm", task_id));

        assert!(file_path
            .to_string_lossy()
            .contains("proplet_task-with-dashes-123.wasm"));
    }

    #[tokio::test]
    async fn test_create_and_cleanup_temp_file() {
        let runtime = HostRuntime::new("/usr/bin/wasmtime".to_string());
        let task_id = "test-cleanup-task";
        let wasm_data = vec![0x00, 0x61, 0x73, 0x6d]; // WASM magic number

        let file_path = runtime
            .create_temp_wasm_file(task_id, &wasm_data)
            .await
            .unwrap();

        assert!(file_path.exists());

        let content = tokio::fs::read(&file_path).await.unwrap();
        assert_eq!(content, wasm_data);

        runtime.cleanup_temp_file(file_path.clone()).await.unwrap();

        assert!(!file_path.exists());
    }

    #[tokio::test]
    async fn test_cleanup_nonexistent_file() {
        let runtime = HostRuntime::new("/usr/bin/wasmtime".to_string());
        let fake_path = std::env::temp_dir().join("nonexistent-file.wasm");

        let result = runtime.cleanup_temp_file(fake_path).await;
        assert!(result.is_ok());
    }

    #[tokio::test]
    async fn test_create_temp_file_with_empty_data() {
        let runtime = HostRuntime::new("/usr/bin/wasmtime".to_string());
        let task_id = "empty-task";
        let wasm_data = vec![];

        let file_path = runtime
            .create_temp_wasm_file(task_id, &wasm_data)
            .await
            .unwrap();

        assert!(file_path.exists());

        let content = tokio::fs::read(&file_path).await.unwrap();
        assert_eq!(content.len(), 0);

        runtime.cleanup_temp_file(file_path).await.unwrap();
    }

    #[tokio::test]
    async fn test_create_temp_file_with_large_data() {
        let runtime = HostRuntime::new("/usr/bin/wasmtime".to_string());
        let task_id = "large-task";
        let wasm_data = vec![0xAB; 1024 * 1024]; // 1 MB of data

        let file_path = runtime
            .create_temp_wasm_file(task_id, &wasm_data)
            .await
            .unwrap();

        assert!(file_path.exists());

        let content = tokio::fs::read(&file_path).await.unwrap();
        assert_eq!(content.len(), 1024 * 1024);

        runtime.cleanup_temp_file(file_path).await.unwrap();
    }
}
