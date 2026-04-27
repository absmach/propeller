mod config;
mod hal;
mod hal_linker;
mod metrics;
mod monitoring;
mod mqtt;
mod plugin;
mod runtime;
mod service;
mod task_handler;
mod tee_detection;
mod types;

use crate::config::PropletConfig;
use crate::mqtt::{process_mqtt_events, MqttConfig, PubSub};
use crate::plugin::registry::PluginRegistry;
use crate::runtime::host::HostRuntime;
use crate::runtime::tee_runtime::TeeWasmRuntime;
use crate::runtime::wasmtime_runtime::WasmtimeRuntime;
use crate::runtime::Runtime;
use crate::service::PropletService;
use anyhow::Result;
use std::sync::Arc;
use tokio::sync::mpsc;
use tracing::{debug, info, warn, Level};
use tracing_subscriber::FmtSubscriber;

#[tokio::main]
async fn main() -> Result<()> {
    let config =
        PropletConfig::load().map_err(|e| anyhow::anyhow!("Failed to load configuration: {e}"))?;

    let log_level = match config.log_level.to_lowercase().as_str() {
        "trace" => Level::TRACE,
        "debug" => Level::DEBUG,
        "info" => Level::INFO,
        "warn" => Level::WARN,
        "error" => Level::ERROR,
        _ => Level::INFO,
    };

    let subscriber = FmtSubscriber::builder()
        .with_max_level(log_level)
        .with_target(false)
        .with_thread_ids(false)
        .with_file(false)
        .with_line_number(false)
        .finish();

    tracing::subscriber::set_global_default(subscriber)?;

    debug!("Proplet configuration: {:?}", config);

    info!("Starting Proplet (Rust) - Client ID: {}", config.client_id);

    let mqtt_config = MqttConfig {
        address: config.mqtt_address.clone(),
        client_id: config.client_id.clone(),
        timeout: config.mqtt_timeout(),
        qos: config.qos(),
        keep_alive: config.mqtt_keep_alive(),
        max_packet_size: config.mqtt_max_packet_size,
        inflight: config.mqtt_inflight,
        request_channel_capacity: config.mqtt_request_channel_capacity,
        username: config.client_id.clone(),
        password: config.client_key.clone(),
    };

    let (pubsub, eventloop) = PubSub::new(mqtt_config).await?;
    let pubsub_clone = pubsub.clone();

    // Bounded channel for backpressure to prevent overwhelming the task executor
    let (tx, rx) = mpsc::channel(128);

    tokio::spawn(async move {
        process_mqtt_events(eventloop, tx).await;
    });

    let runtime: Arc<dyn Runtime> = if let Some(external_runtime) = &config.external_wasm_runtime {
        info!("Using external Wasm runtime: {}", external_runtime);
        Arc::new(HostRuntime::new(external_runtime.clone()))
    } else {
        info!("Using Wasmtime runtime");
        Arc::new(WasmtimeRuntime::new_with_options(
            config.hal_enabled,
            config.http_enabled,
            config.preopened_dirs.clone(),
            config.http_proxy_port,
        )?)
    };

    let plugin_registry = if let Some(ref dir) = config.plugin_dir {
        match PluginRegistry::load_directory(dir) {
            Ok(registry) => {
                if !registry.is_empty() {
                    info!("Proplet plugin registry loaded from '{}'", dir);
                }
                Some(Arc::new(registry))
            }
            Err(e) => {
                warn!("Failed to load proplet plugin directory '{}': {}", dir, e);
                None
            }
        }
    } else {
        None
    };

    let service = if config.tee_enabled {
        match TeeWasmRuntime::new(&config).await {
            Ok(tee_runtime) => {
                info!("TEE runtime initialized successfully");
                Arc::new(PropletService::with_tee_runtime(
                    config.clone(),
                    pubsub,
                    runtime,
                    Arc::new(tee_runtime),
                    plugin_registry,
                ))
            }
            Err(e) => {
                tracing::error!("Failed to initialize TEE runtime: {:#}", e);
                return Err(anyhow::anyhow!(
                    "TEE detected but TEE runtime failed to initialize: {:#}",
                    e
                ));
            }
        }
    } else {
        Arc::new(PropletService::new(
            config.clone(),
            pubsub,
            runtime,
            plugin_registry,
        ))
    };

    let shutdown_handle = tokio::spawn(async move {
        tokio::signal::ctrl_c()
            .await
            .expect("Failed to listen for ctrl-c");

        info!("Received shutdown signal, cleaning up...");

        if let Err(e) = pubsub_clone.disconnect().await {
            tracing::error!("Failed to disconnect gracefully: {}", e);
        }

        info!("Graceful shutdown complete");
        std::process::exit(0);
    });

    tokio::select! {
        result = service.run(rx) => {
            result?;
        }
        _ = shutdown_handle => {
        }
    }

    Ok(())
}
