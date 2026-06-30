use super::{Runtime, RuntimeContext, StartConfig};
use anyhow::{Context, Result};
use async_trait::async_trait;
use std::collections::HashMap;
use std::net::SocketAddr;
use std::path::PathBuf;
use std::process::Stdio;
use std::sync::Arc;
use tokio::fs;
use tokio::io::AsyncWriteExt;
use tokio::net::TcpListener;
use tokio::process::{Child, Command};
use tokio::sync::Mutex;
use tracing::{debug, error, info, warn};

pub struct HostRuntime {
    runtime_path: String,
    http_proxy_port: u16,
    processes: Arc<Mutex<HashMap<String, Child>>>,
    pids: Arc<Mutex<HashMap<String, u32>>>,
}

impl HostRuntime {
    pub fn new(runtime_path: String, http_proxy_port: u16) -> Self {
        Self {
            runtime_path,
            http_proxy_port,
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

    fn is_wasm_component(binary: &[u8]) -> bool {
        // Component model binary layer marker: 0x0a (older wasm-tools) or 0x0d (wasm-tools >= 0.200)
        binary.len() >= 8
            && (binary[4..8] == [0x0a, 0x00, 0x01, 0x00]
                || binary[4..8] == [0x0d, 0x00, 0x01, 0x00])
    }

    fn is_proxy_component(bytes: &[u8]) -> bool {
        bytes
            .windows(b"wasi:http/incoming-handler".len())
            .any(|w| w == b"wasi:http/incoming-handler")
    }

    async fn find_available_port(start_port: u16) -> Result<(u16, TcpListener)> {
        let max_attempts = 100u16;
        for port in start_port..start_port.saturating_add(max_attempts) {
            let addr: SocketAddr = ([0, 0, 0, 0], port).into();
            match TcpListener::bind(addr).await {
                Ok(listener) => return Ok((port, listener)),
                Err(_) => continue,
            }
        }
        let end_port = start_port.saturating_add(max_attempts - 1);
        Err(anyhow::anyhow!(
            "No available port found in range {}-{}",
            start_port,
            end_port,
        ))
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

        let is_component = Self::is_wasm_component(&config.wasm_binary);
        let is_proxy = is_component && Self::is_proxy_component(&config.wasm_binary);

        if is_proxy {
            info!(
                "Detected proxy component for task {}, using 'wasmtime serve'",
                config.id
            );
            cmd.arg("serve");

            let has_http_flag = config.cli_args.iter().any(|a| a == "-Shttp");
            if !has_http_flag {
                cmd.arg("-Shttp");
            }

            let has_addr = config
                .cli_args
                .iter()
                .any(|a| a == "--addr" || a.starts_with("--addr="));

            let mut _port_holder: Option<(u16, TcpListener)> = None;

            let actual_port = if has_addr {
                // User provided --addr, parse port from cli_args if possible
                config
                    .cli_args
                    .windows(2)
                    .find(|w| w[0] == "--addr")
                    .and_then(|w| w[1].split(':').next_back()?.parse().ok())
                    .or_else(|| {
                        config
                            .cli_args
                            .iter()
                            .find_map(|a| a.strip_prefix("--addr="))
                            .and_then(|a| a.split(':').next_back()?.parse().ok())
                    })
                    .unwrap_or(self.http_proxy_port)
            } else {
                let (port, listener) = Self::find_available_port(self.http_proxy_port).await?;
                cmd.arg("--addr").arg(format!("0.0.0.0:{port}"));
                _port_holder = Some((port, listener));
                port
            };

            for arg in &config.cli_args {
                cmd.arg(arg);
            }

            info!(
                "Proxy component task {}, using port {}, config port: {}, cli_args: {:?}",
                config.id, actual_port, self.http_proxy_port, config.cli_args
            );

            if !config.env.is_empty() {
                info!(
                    "Setting {} environment variables for task {}",
                    config.env.len(),
                    config.id
                );
                for (key, value) in &config.env {
                    debug!("  {}={}", key, value);
                    cmd.arg("--env");
                    cmd.arg(format!("{}={}", key, value));
                }
            } else {
                warn!("No environment variables provided for task {}", config.id);
            }

            cmd.arg(&temp_file);

            cmd.envs(&config.env);

            // Release the probed port right before wasmtime serve binds to it,
            // minimising (but not eliminating) the TOCTOU window.
            drop(_port_holder);

            cmd.stdout(Stdio::piped())
                .stderr(Stdio::piped())
                .stdin(Stdio::null());

            let child = cmd.spawn().context(format!(
                "Failed to spawn host runtime process: {}. Command: {} {:?}",
                self.runtime_path, self.runtime_path, config.cli_args
            ))?;

            let pid = child.id();
            info!(
                "Proxy component 'wasmtime serve' spawned with PID: {:?}",
                pid
            );

            if let Some(pid_val) = pid {
                let mut pids = self.pids.lock().await;
                pids.insert(config.id.clone(), pid_val);
            }

            let mut processes = self.processes.lock().await;
            processes.insert(config.id.clone(), child);
            drop(processes);

            info!(
                "Proxy component task {} started in daemon mode, returning immediately",
                config.id
            );

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
                                    info!("Proxy task {} exited with status: {}", task_id, status);
                                    should_cleanup = true;
                                }
                                Ok(None) => {}
                                Err(e) => {
                                    error!("Proxy task {} try_wait error: {}", task_id, e);
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

            Ok(format!("started at port {actual_port}").into_bytes())
        } else {
            cmd.arg("run");

            let cli_args_has_invoke = config.cli_args.iter().any(|a| a == "--invoke");
            let has_custom_export = !config.function_name.is_empty()
                && config.function_name != "_start"
                && !config.function_name.starts_with("fl-round-")
                && !cli_args_has_invoke;

            if has_custom_export {
                if is_component {
                    let wave_call = format!("{}({})", config.function_name, config.args.join(", "));
                    cmd.arg("--invoke").arg(&wave_call);
                } else {
                    cmd.arg("--invoke").arg(&config.function_name);
                }
            }

            for arg in &config.cli_args {
                cmd.arg(arg);
            }

            if !config.env.is_empty() {
                info!(
                    "Setting {} environment variables for task {}",
                    config.env.len(),
                    config.id
                );
                for (key, value) in &config.env {
                    debug!("  {}={}", key, value);
                    cmd.arg("--env");
                    cmd.arg(format!("{}={}", key, value));
                }
            } else {
                warn!("No environment variables provided for task {}", config.id);
            }

            cmd.arg(&temp_file);

            if !is_component || !has_custom_export {
                for arg in &config.args {
                    cmd.arg(arg);
                }
            }

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
                                        info!(
                                            "Daemon task {} exited with status: {}",
                                            task_id, status
                                        );
                                        should_cleanup = true;
                                    }
                                    Ok(None) => {}
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
                            Ok(None) => {}
                            Err(e) => {
                                drop(processes);
                                return Err(anyhow::anyhow!(
                                    "Failed to check process status: {}",
                                    e
                                ));
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
        let runtime = HostRuntime::new("/usr/bin/wasmtime".to_string(), 8222);
        assert_eq!(runtime.runtime_path, "/usr/bin/wasmtime");
        assert_eq!(runtime.http_proxy_port, 8222);
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
        let runtime = HostRuntime::new("/usr/bin/wasmtime".to_string(), 8222);
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
        let runtime = HostRuntime::new("/usr/bin/wasmtime".to_string(), 8222);
        let fake_path = std::env::temp_dir().join("nonexistent-file.wasm");

        let result = runtime.cleanup_temp_file(fake_path).await;
        assert!(result.is_ok());
    }

    #[tokio::test]
    async fn test_create_temp_file_with_empty_data() {
        let runtime = HostRuntime::new("/usr/bin/wasmtime".to_string(), 8222);
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
        let runtime = HostRuntime::new("/usr/bin/wasmtime".to_string(), 8222);
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

    #[test]
    fn test_is_wasm_component_old_format() {
        let binary = [0x00, 0x61, 0x73, 0x6d, 0x0a, 0x00, 0x01, 0x00, 0x00];
        assert!(HostRuntime::is_wasm_component(&binary));
    }

    #[test]
    fn test_is_wasm_component_new_format() {
        let binary = [0x00, 0x61, 0x73, 0x6d, 0x0d, 0x00, 0x01, 0x00, 0x00];
        assert!(HostRuntime::is_wasm_component(&binary));
    }

    #[test]
    fn test_is_wasm_component_rejects_core_module() {
        let binary = [0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00, 0x00];
        assert!(!HostRuntime::is_wasm_component(&binary));
    }

    #[test]
    fn test_is_wasm_component_rejects_too_short() {
        let binary = [0x00, 0x61, 0x73, 0x6d];
        assert!(!HostRuntime::is_wasm_component(&binary));
    }

    #[test]
    fn test_is_proxy_component_detects_proxy() {
        let binary = b"some bytes wasi:http/incoming-handler more bytes";
        assert!(HostRuntime::is_proxy_component(binary));
    }

    #[test]
    fn test_is_proxy_component_rejects_non_proxy() {
        let binary = b"some bytes without the proxy marker";
        assert!(!HostRuntime::is_proxy_component(binary));
    }

    #[test]
    fn test_is_proxy_component_rejects_empty() {
        let binary: &[u8] = &[];
        assert!(!HostRuntime::is_proxy_component(binary));
    }

    #[test]
    fn test_is_proxy_component_rejects_partial_match() {
        let binary = b"wasi:http/incoming-handle";
        assert!(!HostRuntime::is_proxy_component(binary));
    }

    #[test]
    fn test_is_proxy_component_with_wasm_binary() {
        let mut binary = vec![0x00, 0x61, 0x73, 0x6d, 0x0d, 0x00, 0x01, 0x00];
        binary.extend_from_slice(b"wasi:http/incoming-handler");
        binary.extend_from_slice(b"more wasm content");
        assert!(HostRuntime::is_proxy_component(&binary));
    }

    #[test]
    fn test_proxy_component_is_also_wasm_component() {
        let mut binary = vec![0x00, 0x61, 0x73, 0x6d, 0x0d, 0x00, 0x01, 0x00];
        binary.extend_from_slice(b"wasi:http/incoming-handler");
        assert!(HostRuntime::is_wasm_component(&binary));
        assert!(HostRuntime::is_proxy_component(&binary));
    }

    #[test]
    fn test_core_module_is_not_proxy() {
        let binary = [0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00];
        assert!(!HostRuntime::is_proxy_component(&binary));
    }
}
