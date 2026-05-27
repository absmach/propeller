use super::{AuthorizeResponse, EnrichResponse, TaskInfo, TaskResult, WasmPlugin};
use anyhow::Result;
use std::sync::Arc;
use tracing::{info, warn};
use wasmtime::{Config, Engine};

pub struct PluginRegistry {
    _engine: Engine,
    plugins: Vec<WasmPlugin>,
}

impl PluginRegistry {
    pub fn load_directory(dir: &str) -> Result<Self> {
        let mut config = Config::new();
        config.wasm_component_model(true);
        let engine = Engine::new(&config)?;

        let path = std::path::Path::new(dir);
        if !path.exists() {
            return Ok(Self {
                _engine: engine,
                plugins: Vec::new(),
            });
        }

        let mut plugins = Vec::new();
        let mut entries: Vec<_> = std::fs::read_dir(path)?
            .filter_map(|e| e.ok())
            .filter(|e| e.path().extension().is_some_and(|ext| ext == "wasm"))
            .collect();
        entries.sort_by_key(|e| e.file_name());

        for entry in entries {
            let wasm_path = entry.path();
            match std::fs::read(&wasm_path) {
                Ok(bytes) => match WasmPlugin::load(&engine, &bytes) {
                    Ok(plugin) => {
                        info!("Loaded proplet plugin: {}", wasm_path.display());
                        plugins.push(plugin);
                    }
                    Err(e) => {
                        warn!(
                            "Skipping plugin {} (load error): {}",
                            wasm_path.display(),
                            e
                        );
                    }
                },
                Err(e) => {
                    warn!(
                        "Skipping plugin {} (read error): {}",
                        wasm_path.display(),
                        e
                    );
                }
            }
        }

        if plugins.is_empty() {
            info!("No proplet plugins found in '{}'", dir);
        }

        Ok(Self {
            _engine: engine,
            plugins,
        })
    }

    pub fn is_empty(&self) -> bool {
        self.plugins.is_empty()
    }

    /// Returns None if all plugins allow, or the first denial reason.
    pub fn authorize(&self, task: &TaskInfo) -> Result<Option<String>> {
        let plugins = self.plugins.len();
        if plugins == 0 {
            return Ok(None);
        }

        let task = task.clone();
        let plugins: Vec<_> = self.plugins.iter().collect();
        tokio::task::block_in_place(|| {
            for plugin in plugins {
                match plugin.authorize(task.clone()) {
                    Ok(AuthorizeResponse {
                        allow: false,
                        reason,
                    }) => {
                        return Ok(Some(
                            reason.unwrap_or_else(|| "denied by plugin".to_string()),
                        ));
                    }
                    Ok(_) => {}
                    Err(e) => {
                        warn!("Plugin authorize error (failing closed): {}", e);
                        return Ok(Some(format!("plugin error: {e}")));
                    }
                }
            }
            Ok(None)
        })
    }

    /// Merges env additions from all plugins.
    pub fn enrich(&self, task: &TaskInfo) -> Result<Vec<(String, String)>> {
        if self.plugins.is_empty() {
            return Ok(Vec::new());
        }

        let task = task.clone();
        let plugins: Vec<_> = self.plugins.iter().collect();
        tokio::task::block_in_place(|| {
            let mut env: Vec<(String, String)> = Vec::new();
            for plugin in plugins {
                match plugin.enrich(task.clone()) {
                    Ok(EnrichResponse { env: additions }) => env.extend(additions),
                    Err(e) => warn!("Plugin enrich error (skipping): {}", e),
                }
            }
            Ok(env)
        })
    }

    /// Notifies all plugins that a task started. The notification runs on a
    /// blocking thread but the returned JoinHandle is intentionally dropped
    /// (fire-and-forget); callers who need graceful shutdown should join the
    /// handle themselves.
    pub fn notify_task_start(registry: Arc<Self>, task: TaskInfo) -> tokio::task::JoinHandle<()> {
        tokio::task::spawn_blocking(move || {
            for plugin in &registry.plugins {
                plugin.on_task_start(task.clone());
            }
        })
    }

    /// Notifies all plugins that a task completed. The notification runs on a
    /// blocking thread but the returned JoinHandle is intentionally dropped
    /// (fire-and-forget); callers who need graceful shutdown should join the
    /// handle themselves.
    pub fn notify_task_complete(
        registry: Arc<Self>,
        result: TaskResult,
    ) -> tokio::task::JoinHandle<()> {
        tokio::task::spawn_blocking(move || {
            for plugin in &registry.plugins {
                plugin.on_task_complete(result.clone());
            }
        })
    }
}
