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
        for plugin in &self.plugins {
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
                    warn!("Plugin authorize error (treating as allow): {}", e);
                }
            }
        }
        Ok(None)
    }

    /// Merges env additions from all plugins.
    pub fn enrich(&self, task: &TaskInfo) -> Result<Vec<(String, String)>> {
        let mut env: Vec<(String, String)> = Vec::new();
        for plugin in &self.plugins {
            match plugin.enrich(task.clone()) {
                Ok(EnrichResponse { env: additions }) => env.extend(additions),
                Err(e) => warn!("Plugin enrich error (skipping): {}", e),
            }
        }
        Ok(env)
    }

    /// Fire-and-forget: notifies all plugins that a task started.
    pub fn notify_task_start(registry: Arc<Self>, task: TaskInfo) {
        tokio::task::spawn_blocking(move || {
            for plugin in &registry.plugins {
                plugin.on_task_start(task.clone());
            }
        });
    }

    /// Fire-and-forget: notifies all plugins that a task completed.
    pub fn notify_task_complete(registry: Arc<Self>, result: TaskResult) {
        tokio::task::spawn_blocking(move || {
            for plugin in &registry.plugins {
                plugin.on_task_complete(result.clone());
            }
        });
    }
}
