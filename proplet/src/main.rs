mod config;
mod hal;
mod hal_linker;
mod metrics;
mod monitoring;
mod mqtt;
mod runtime;
mod service;
mod task_handler;
mod tee_detection;
mod types;

use crate::config::PropletConfig;
use crate::mqtt::{process_mqtt_events, MqttConfig, PubSub};
use crate::runtime::host::HostRuntime;
use crate::runtime::tee_runtime::TeeWasmRuntime;
use crate::runtime::wasmtime_runtime::WasmtimeRuntime;
use crate::runtime::Runtime;
use crate::service::PropletService;
use anyhow::Result;
use std::sync::Arc;
use tokio::sync::mpsc;
use tracing::{debug, info, Level};
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

    info!(
        "Starting Proplet (Rust) - Instance ID: {}",
        config.instance_id
    );

    let mqtt_config = MqttConfig {
        address: config.mqtt_address.clone(),
        client_id: config.instance_id.to_string(),
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
        Arc::new(WasmtimeRuntime::new(config.hal_enabled)?)
    };

    let service = if config.tee_enabled {
        match TeeWasmRuntime::new(&config).await {
            Ok(tee_runtime) => Arc::new(PropletService::with_tee_runtime(
                config.clone(),
                pubsub,
                runtime,
                Arc::new(tee_runtime),
            )),
            Err(_) => Arc::new(PropletService::new(config.clone(), pubsub, runtime)),
        }
    } else {
        Arc::new(PropletService::new(config.clone(), pubsub, runtime))
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
