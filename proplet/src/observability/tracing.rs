//! OpenTelemetry tracing support for the proplet.
//!
//! This module provides OTLP tracing export when the `otlp` feature is enabled.

use tracing::info;

/// Tracing configuration.
#[derive(Debug, Clone)]
#[allow(dead_code)]
pub struct TracingConfig {
    /// Jaeger endpoint (e.g., "http://localhost:14268/api/traces")
    pub jaeger_endpoint: Option<String>,
    /// Service name for tracing
    pub service_name: String,
}

impl Default for TracingConfig {
    fn default() -> Self {
        Self {
            jaeger_endpoint: None,
            service_name: "proplet".to_string(),
        }
    }
}

/// Initialize tracing with optional OTLP export.
///
/// When the `otlp` feature is enabled and an endpoint is configured,
/// traces will be exported to the OpenTelemetry Collector.
#[cfg(feature = "otlp")]
pub fn init_tracing(config: TracingConfig) -> anyhow::Result<()> {
    use opentelemetry_sdk::{runtime, trace as sdktrace, Resource};
    use tracing_subscriber::{layer::SubscriberExt, util::SubscriberInitExt, EnvFilter};

    let env_filter = EnvFilter::try_from_default_env().unwrap_or_else(|_| EnvFilter::new("info"));

    if let Some(endpoint) = config.jaeger_endpoint {
        let tracer = opentelemetry_jaeger::new_collector_pipeline()
            .with_endpoint(&endpoint)
            .with_reqwest()
            .with_service_name(config.service_name.clone())
            .with_trace_config(
                sdktrace::Config::default().with_resource(Resource::new(vec![
                    opentelemetry::KeyValue::new("service.name", config.service_name.clone()),
                ])),
            )
            .install_batch(runtime::Tokio)?;

        let telemetry_layer = tracing_opentelemetry::layer().with_tracer(tracer);

        tracing_subscriber::registry()
            .with(env_filter)
            .with(tracing_subscriber::fmt::layer())
            .with(telemetry_layer)
            .init();

        info!("Jaeger tracing initialized, endpoint: {}", endpoint);
    } else {
        // Standard tracing without Jaeger
        tracing_subscriber::registry()
            .with(env_filter)
            .with(tracing_subscriber::fmt::layer())
            .init();

        info!("Tracing initialized (Jaeger disabled)");
    }

    Ok(())
}

/// Initialize tracing without OTLP export (when feature is disabled).
#[cfg(not(feature = "otlp"))]
pub fn init_tracing(_config: TracingConfig) -> anyhow::Result<()> {
    use tracing_subscriber::{layer::SubscriberExt, util::SubscriberInitExt, EnvFilter};

    let env_filter = EnvFilter::try_from_default_env().unwrap_or_else(|_| EnvFilter::new("info"));

    tracing_subscriber::registry()
        .with(env_filter)
        .with(tracing_subscriber::fmt::layer())
        .init();

    info!("Tracing initialized (OTLP feature not enabled)");

    Ok(())
}

/// Shutdown tracing and flush pending spans.
#[cfg(feature = "otlp")]
pub fn shutdown_tracing() {
    opentelemetry::global::shutdown_tracer_provider();
    info!("OTLP tracing shutdown complete");
}

/// Shutdown tracing (no-op when OTLP is disabled).
#[cfg(not(feature = "otlp"))]
pub fn shutdown_tracing() {
    // No-op when OTLP is disabled
}
