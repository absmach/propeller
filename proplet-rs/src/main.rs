mod config;
mod mqtt;
mod runtime;
mod service;
mod types;

use crate::config::PropletConfig;
use crate::mqtt::{process_mqtt_events, MqttConfig, PubSub};
use crate::runtime::host::HostRuntime;
use crate::runtime::wasmtime_runtime::WasmtimeRuntime;
use crate::runtime::Runtime;
use crate::service::PropletService;
use anyhow::Result;
use std::sync::Arc;
use tokio::sync::mpsc;
use tracing::{info, Level};
use tracing_subscriber::FmtSubscriber;

#[tokio::main]
async fn main() -> Result<()> {
    // Load configuration
    let config = PropletConfig::from_env();

    // Initialize logging
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

    info!("Starting Proplet (Rust) - Instance ID: {}", config.instance_id);
    info!("MQTT Address: {}", config.mqtt_address);
    info!("Domain ID: {}", config.domain_id);
    info!("Channel ID: {}", config.channel_id);

    // Create MQTT client
    let mqtt_config = MqttConfig {
        address: config.mqtt_address.clone(),
        client_id: config.instance_id.to_string(),
        timeout: config.mqtt_timeout(),
        qos: config.qos(),
    };

    let (pubsub, eventloop) = PubSub::new(mqtt_config).await?;

    // Create message channel
    let (tx, rx) = mpsc::unbounded_channel();

    // Start MQTT event processing
    tokio::spawn(async move {
        process_mqtt_events(eventloop, tx).await;
    });

    // Create runtime based on configuration
    let runtime: Arc<dyn Runtime> = if let Some(external_runtime) = &config.external_wasm_runtime {
        info!("Using external Wasm runtime: {}", external_runtime);
        Arc::new(HostRuntime::new(external_runtime.clone()))
    } else {
        info!("Using Wasmtime runtime");
        Arc::new(WasmtimeRuntime::new())
    };

    // Create and run service
    let service = Arc::new(PropletService::new(config.clone(), pubsub, runtime));

    // Handle shutdown signal
    tokio::spawn(async move {
        tokio::signal::ctrl_c()
            .await
            .expect("Failed to listen for ctrl-c");

        info!("Received shutdown signal, exiting...");
        std::process::exit(0);
    });

    // Run the service
    service.run(rx).await?;

    Ok(())
}
