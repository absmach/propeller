use lazy_static::lazy_static;
use prometheus::{
    register_counter, register_counter_vec, register_gauge, register_histogram_vec, Counter,
    CounterVec, Encoder, Gauge, HistogramVec, TextEncoder,
};

lazy_static! {
    /// Total number of tasks executed successfully
    pub static ref TASKS_EXECUTED_TOTAL: Counter = register_counter!(
        "proplet_tasks_executed_total",
        "Total number of tasks executed successfully"
    )
    .unwrap();

    /// Total number of tasks that failed
    pub static ref TASKS_FAILED_TOTAL: Counter = register_counter!(
        "proplet_tasks_failed_total",
        "Total number of tasks that failed"
    )
    .unwrap();

    /// Number of currently active tasks
    pub static ref ACTIVE_TASKS: Gauge = register_gauge!(
        "proplet_active_tasks",
        "Number of currently running tasks"
    )
    .unwrap();

    /// WASM execution duration in seconds
    pub static ref WASM_EXECUTION_DURATION: HistogramVec = register_histogram_vec!(
        "proplet_wasm_execution_duration_seconds",
        "Duration of WASM task execution in seconds",
        &["task_name"],
        vec![0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0, 30.0, 60.0]
    )
    .unwrap();

    /// WASM memory usage in bytes
    pub static ref WASM_MEMORY_BYTES: Gauge = register_gauge!(
        "proplet_wasm_memory_bytes",
        "Current WASM memory usage in bytes"
    )
    .unwrap();

    /// Total chunks received from registry
    pub static ref CHUNKS_RECEIVED_TOTAL: Counter = register_counter!(
        "proplet_chunks_received_total",
        "Total number of chunks received from registry"
    )
    .unwrap();

    /// Total binaries assembled from chunks
    pub static ref BINARIES_ASSEMBLED_TOTAL: Counter = register_counter!(
        "proplet_binaries_assembled_total",
        "Total number of WASM binaries assembled from chunks"
    )
    .unwrap();

    /// MQTT messages received by topic type
    pub static ref MQTT_MESSAGES_RECEIVED: CounterVec = register_counter_vec!(
        "proplet_mqtt_messages_received_total",
        "Total MQTT messages received by topic type",
        &["topic_type"]
    )
    .unwrap();

    /// MQTT messages published by topic type
    pub static ref MQTT_MESSAGES_PUBLISHED: CounterVec = register_counter_vec!(
        "proplet_mqtt_messages_published_total",
        "Total MQTT messages published by topic type",
        &["topic_type"]
    )
    .unwrap();

    /// Errors by type
    pub static ref ERRORS_TOTAL: CounterVec = register_counter_vec!(
        "proplet_errors_total",
        "Total errors by type",
        &["error_type"]
    )
    .unwrap();
}

// Topic type constants for labeling
pub const TOPIC_CONTROL: &str = "control";
pub const TOPIC_REGISTRY: &str = "registry";
pub const TOPIC_RESULTS: &str = "results";

// Error type constants
pub const ERROR_DECODE: &str = "decode";
pub const ERROR_RUNTIME: &str = "runtime";
pub const ERROR_MQTT: &str = "mqtt";
pub const ERROR_CHUNK: &str = "chunk";

/// Record a task starting
pub fn record_task_started() {
    ACTIVE_TASKS.inc();
}

/// Record a task completing successfully
pub fn record_task_completed() {
    ACTIVE_TASKS.dec();
    TASKS_EXECUTED_TOTAL.inc();
}

/// Record a task failing
pub fn record_task_failed() {
    ACTIVE_TASKS.dec();
    TASKS_FAILED_TOTAL.inc();
}

/// Observe WASM execution duration
pub fn observe_wasm_execution(task_name: &str, duration_secs: f64) {
    WASM_EXECUTION_DURATION
        .with_label_values(&[task_name])
        .observe(duration_secs);
}

/// Update WASM memory gauge
#[allow(dead_code)]
pub fn set_wasm_memory(bytes: f64) {
    WASM_MEMORY_BYTES.set(bytes);
}

/// Record chunk received
pub fn record_chunk_received() {
    CHUNKS_RECEIVED_TOTAL.inc();
}

/// Record binary assembled
pub fn record_binary_assembled() {
    BINARIES_ASSEMBLED_TOTAL.inc();
}

/// Record MQTT message received
pub fn record_mqtt_received(topic_type: &str) {
    MQTT_MESSAGES_RECEIVED
        .with_label_values(&[topic_type])
        .inc();
}

/// Record MQTT message published
pub fn record_mqtt_published(topic_type: &str) {
    MQTT_MESSAGES_PUBLISHED
        .with_label_values(&[topic_type])
        .inc();
}

/// Record an error
pub fn record_error(error_type: &str) {
    ERRORS_TOTAL.with_label_values(&[error_type]).inc();
}

/// Encode metrics as Prometheus text format
pub fn encode_metrics() -> Result<String, prometheus::Error> {
    let encoder = TextEncoder::new();
    let metric_families = prometheus::gather();
    let mut buffer = Vec::new();
    encoder.encode(&metric_families, &mut buffer)?;
    Ok(String::from_utf8(buffer).unwrap_or_default())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_task_metrics() {
        record_task_started();
        record_task_completed();
        record_task_started();
        record_task_failed();
    }

    #[test]
    fn test_wasm_execution() {
        observe_wasm_execution("test_task", 1.5);
    }

    #[test]
    fn test_encode_metrics() {
        let result = encode_metrics();
        assert!(result.is_ok());
    }
}
