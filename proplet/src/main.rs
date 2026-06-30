mod config;
mod hal;
mod hal_component;
mod metrics;
mod monitoring;
mod mqtt;
mod plugin;
mod runtime;
mod service;
mod task_handler;
mod tee_detection;
mod telemetry;
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
use opentelemetry::trace::TracerProvider as _;
use opentelemetry_otlp::WithExportConfig;
use opentelemetry_sdk::Resource;
use std::sync::Arc;
use tokio::sync::{mpsc, oneshot};
use tracing::{info, warn, Level};
use tracing_subscriber::layer::SubscriberExt;
use tracing_subscriber::util::SubscriberInitExt;

fn init_tracer(endpoint: &str, ratio: f64) -> Result<opentelemetry_sdk::trace::SdkTracerProvider> {
    let exporter = opentelemetry_otlp::SpanExporter::builder()
        .with_http()
        .with_endpoint(endpoint)
        .build()?;

    let provider = opentelemetry_sdk::trace::SdkTracerProvider::builder()
        .with_batch_exporter(exporter)
        .with_sampler(opentelemetry_sdk::trace::Sampler::TraceIdRatioBased(ratio))
        .with_resource(Resource::builder().with_service_name("proplet").build())
        .build();

    Ok(provider)
}

#[tokio::main]
async fn main() -> Result<()> {
    let _ = rustls::crypto::ring::default_provider().install_default();
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

    let fmt_layer = tracing_subscriber::fmt::layer()
        .json()
        .with_target(false)
        .with_thread_ids(false)
        .with_file(false)
        .with_line_number(false);

    let tracer_provider = if let Some(url) = &config.otel_url {
        let provider = init_tracer(url, config.trace_ratio)
            .map_err(|e| anyhow::anyhow!("Failed to initialize OTLP tracer: {e}"))?;
        Some(provider)
    } else {
        None
    };

    let otel_layer = tracer_provider.as_ref().map(|provider| {
        let tracer = provider.tracer("proplet");
        opentelemetry::global::set_tracer_provider(provider.clone());
        tracing_opentelemetry::layer().with_tracer(tracer)
    });

    tracing_subscriber::registry()
        .with(tracing_subscriber::filter::LevelFilter::from(log_level))
        .with(fmt_layer)
        .with(otel_layer)
        .try_init()
        .map_err(|e| anyhow::anyhow!("Failed to set tracing subscriber: {e}"))?;

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
        tls_ca_cert: config.mqtt_tls_ca_cert.clone(),
        tls_client_cert: config.mqtt_tls_client_cert.clone(),
        tls_client_key: config.mqtt_tls_client_key.clone(),
        tls_insecure_skip_verify: config.mqtt_tls_insecure_skip_verify,
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
        Arc::new(HostRuntime::new(
            external_runtime.clone(),
            config.http_proxy_port,
        ))
    } else {
        info!("Using Wasmtime runtime");
        Arc::new(WasmtimeRuntime::new_with_options(
            config.hal_enabled,
            config.http_enabled,
            config.usb_enabled,
            config.preopened_dirs.clone(),
            config.http_proxy_port,
            config.http_tls_ca_cert.as_deref(),
            config.http_tls_insecure_skip_verify,
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

    let proplet_metrics = Arc::new(
        telemetry::PropletMetrics::new()
            .map_err(|e| anyhow::anyhow!("Failed to initialize metrics: {e}"))?,
    );

    let telemetry_shutdown = if config.metrics_enabled {
        let metrics_clone = proplet_metrics.clone();
        let metrics_port = config.metrics_port;
        let (tx, rx) = oneshot::channel::<()>();
        tokio::spawn(async move {
            telemetry::serve_telemetry(metrics_port, metrics_clone, rx).await;
        });
        Some(tx)
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
                    proplet_metrics,
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
            proplet_metrics,
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
    });

    tokio::select! {
        result = service.run(rx) => {
            result?;
        }
        _ = shutdown_handle => {
        }
    }

    if let Some(tx) = telemetry_shutdown {
        let _ = tx.send(());
    }

    if let Some(provider) = tracer_provider {
        let _ = provider.shutdown();
    }

    Ok(())
}
