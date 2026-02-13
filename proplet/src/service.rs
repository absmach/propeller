use crate::config::PropletConfig;
use crate::metrics::MetricsCollector;
use crate::monitoring::{system::SystemMonitor, ProcessMonitor};
use crate::mqtt::{build_topic, MqttMessage, PubSub};
use crate::runtime::{Runtime, RuntimeContext, StartConfig};
use crate::types::*;
use anyhow::{Context, Result};
use reqwest::Client as HttpClient;
use std::collections::{BTreeMap, HashMap};
use std::sync::Arc;
use std::time::SystemTime;
use tokio::sync::{mpsc, Mutex};
use tokio::time::Instant;
use tracing::{debug, error, info, warn};

#[derive(Debug)]
struct ChunkAssemblyState {
    chunks: BTreeMap<usize, Vec<u8>>,
    total_chunks: usize,
    created_at: Instant,
}

impl ChunkAssemblyState {
    fn new(total_chunks: usize) -> Self {
        Self {
            chunks: BTreeMap::new(),
            total_chunks,
            created_at: Instant::now(),
        }
    }

    fn is_complete(&self) -> bool {
        self.chunks.len() == self.total_chunks
    }

    fn is_expired(&self, ttl: tokio::time::Duration) -> bool {
        self.created_at.elapsed() > ttl
    }

    fn assemble(&self) -> Vec<u8> {
        let mut binary = Vec::new();
        for chunk_data in self.chunks.values() {
            binary.extend_from_slice(chunk_data);
        }
        binary
    }
}

pub struct PropletService {
    config: PropletConfig,
    proplet: Arc<Mutex<Proplet>>,
    pubsub: PubSub,
    runtime: Arc<dyn Runtime>,
    tee_runtime: Option<Arc<dyn Runtime>>,
    chunk_assembly: Arc<Mutex<HashMap<String, ChunkAssemblyState>>>,
    running_tasks: Arc<Mutex<HashMap<String, TaskState>>>,
    monitor: Arc<SystemMonitor>,
    metrics_collector: Arc<Mutex<MetricsCollector>>,
    http_client: HttpClient,
}

impl PropletService {
    pub fn new(config: PropletConfig, pubsub: PubSub, runtime: Arc<dyn Runtime>) -> Self {
        let proplet = Proplet::new(config.instance_id.clone(), "proplet".to_string());
        let monitor = Arc::new(SystemMonitor::new(MonitoringProfile::default()));
        let metrics_collector = Arc::new(Mutex::new(MetricsCollector::new()));
        let http_client = HttpClient::builder()
            .timeout(std::time::Duration::from_secs(30))
            .build()
            .unwrap_or_else(|e| {
                warn!("Failed to create HTTP client, using default: {}", e);
                HttpClient::new()
            });

        let service = Self {
            config,
            proplet: Arc::new(Mutex::new(proplet)),
            pubsub,
            runtime,
            tee_runtime: None,
            chunk_assembly: Arc::new(Mutex::new(HashMap::new())),
            running_tasks: Arc::new(Mutex::new(HashMap::new())),
            monitor,
            metrics_collector,
            http_client,
        };

        service.start_chunk_expiry_task();

        service
    }

    pub fn with_tee_runtime(
        config: PropletConfig,
        pubsub: PubSub,
        runtime: Arc<dyn Runtime>,
        tee_runtime: Arc<dyn Runtime>,
    ) -> Self {
        let proplet = Proplet::new(config.instance_id.clone(), "proplet".to_string());
        let monitor = Arc::new(SystemMonitor::new(MonitoringProfile::default()));
        let metrics_collector = Arc::new(Mutex::new(MetricsCollector::new()));
        let http_client = HttpClient::builder()
            .timeout(std::time::Duration::from_secs(30))
            .build()
            .unwrap_or_else(|e| {
                warn!("Failed to create HTTP client, using default: {}", e);
                HttpClient::new()
            });

        let service = Self {
            config,
            proplet: Arc::new(Mutex::new(proplet)),
            pubsub,
            runtime,
            tee_runtime: Some(tee_runtime),
            chunk_assembly: Arc::new(Mutex::new(HashMap::new())),
            running_tasks: Arc::new(Mutex::new(HashMap::new())),
            monitor,
            metrics_collector,
            http_client,
        };

        service.start_chunk_expiry_task();

        service
    }

    #[allow(dead_code)]
    pub fn set_tee_runtime(&mut self, tee_runtime: Arc<dyn Runtime>) {
        self.tee_runtime = Some(tee_runtime);
    }

    fn start_chunk_expiry_task(&self) {
        let chunk_assembly = self.chunk_assembly.clone();
        let ttl = tokio::time::Duration::from_secs(300); // 5 minutes TTL

        tokio::spawn(async move {
            let mut interval = tokio::time::interval(tokio::time::Duration::from_secs(60));
            loop {
                interval.tick().await;

                let mut assembly = chunk_assembly.lock().await;
                let expired: Vec<String> = assembly
                    .iter()
                    .filter(|(_, state)| state.is_expired(ttl))
                    .map(|(name, _)| name.clone())
                    .collect();

                for app_name in expired {
                    if let Some(state) = assembly.remove(&app_name) {
                        warn!(
                            "Expired incomplete chunk assembly for '{}': received {}/{} chunks",
                            app_name,
                            state.chunks.len(),
                            state.total_chunks
                        );
                    }
                }
            }
        });
    }

    pub async fn run(self: Arc<Self>, mut mqtt_rx: mpsc::Receiver<MqttMessage>) -> Result<()> {
        info!("Starting PropletService");

        self.publish_discovery().await?;

        self.subscribe_topics().await?;

        let service = self.clone();
        tokio::spawn(async move {
            service.start_liveliness_updates().await;
        });

        if self.config.enable_monitoring && self.config.metrics_interval > 0 {
            let service = self.clone();
            tokio::spawn(async move {
                service.start_metrics_updates().await;
            });
        }

        while let Some(msg) = mqtt_rx.recv().await {
            let service = self.clone();
            tokio::spawn(async move {
                if let Err(e) = service.handle_message(msg).await {
                    error!("Error handling message: {}", e);
                }
            });
        }

        Ok(())
    }

    async fn subscribe_topics(&self) -> Result<()> {
        let qos = self.config.qos();

        let start_topic = build_topic(
            &self.config.domain_id,
            &self.config.channel_id,
            "control/manager/start",
        );
        self.pubsub.subscribe(&start_topic, qos).await?;

        let stop_topic = build_topic(
            &self.config.domain_id,
            &self.config.channel_id,
            "control/manager/stop",
        );
        self.pubsub.subscribe(&stop_topic, qos).await?;

        let chunk_topic = build_topic(
            &self.config.domain_id,
            &self.config.channel_id,
            "registry/server",
        );
        self.pubsub.subscribe(&chunk_topic, qos).await?;

        Ok(())
    }

    async fn publish_discovery(&self) -> Result<()> {
        let discovery = DiscoveryMessage {
            proplet_id: self.config.client_id.clone(),
            namespace: self
                .config
                .k8s_namespace
                .clone()
                .unwrap_or_else(|| "default".to_string()),
        };

        let topic = build_topic(
            &self.config.domain_id,
            &self.config.channel_id,
            "control/proplet/create",
        );

        self.pubsub
            .publish(&topic, &discovery, self.config.qos())
            .await?;
        info!("Published discovery message");

        Ok(())
    }

    async fn start_liveliness_updates(&self) {
        let mut interval = tokio::time::interval(self.config.liveliness_interval());

        loop {
            interval.tick().await;

            if let Err(e) = self.publish_liveliness().await {
                error!("Failed to publish liveliness: {}", e);
            }
        }
    }

    async fn publish_liveliness(&self) -> Result<()> {
        let mut proplet = self.proplet.lock().await;
        proplet.set_alive(true);

        let running_tasks = self.running_tasks.lock().await;
        proplet.task_count = running_tasks.len();

        let liveliness = LivelinessMessage {
            proplet_id: self.config.client_id.clone(),
            status: "alive".to_string(),
            namespace: self
                .config
                .k8s_namespace
                .clone()
                .unwrap_or_else(|| "default".to_string()),
        };

        let topic = build_topic(
            &self.config.domain_id,
            &self.config.channel_id,
            "control/proplet/alive",
        );

        self.pubsub
            .publish(&topic, &liveliness, self.config.qos())
            .await?;
        debug!("Published liveliness update");

        Ok(())
    }

    async fn start_metrics_updates(&self) {
        let mut interval = tokio::time::interval(self.config.metrics_interval());

        loop {
            interval.tick().await;

            if let Err(e) = self.publish_proplet_metrics().await {
                error!("Failed to publish proplet metrics: {}", e);
            }
        }
    }

    async fn publish_proplet_metrics(&self) -> Result<()> {
        let (cpu_metrics, memory_metrics) = {
            let mut collector = self.metrics_collector.lock().await;
            collector.collect()
        };

        #[derive(serde::Serialize)]
        struct PropletMetricsMessage {
            proplet_id: String,
            namespace: String,
            timestamp: SystemTime,
            cpu_metrics: crate::metrics::CpuMetrics,
            memory_metrics: crate::metrics::MemoryMetrics,
        }

        let msg = PropletMetricsMessage {
            proplet_id: self.config.client_id.clone(),
            namespace: self
                .config
                .k8s_namespace
                .clone()
                .unwrap_or_else(|| "default".to_string()),
            timestamp: SystemTime::now(),
            cpu_metrics,
            memory_metrics,
        };

        let topic = build_topic(
            &self.config.domain_id,
            &self.config.channel_id,
            "control/proplet/metrics",
        );

        self.pubsub.publish(&topic, &msg, self.config.qos()).await?;
        debug!("Published proplet metrics");

        Ok(())
    }

    async fn handle_message(&self, msg: MqttMessage) -> Result<()> {
        debug!("Handling message from topic: {}", msg.topic);

        if let Ok(payload_str) = String::from_utf8(msg.payload.clone()) {
            debug!("Raw message payload: {}", payload_str);
        }

        if msg.topic.contains("control/manager/start") {
            self.handle_start_command(msg).await
        } else if msg.topic.contains("control/manager/stop") {
            self.handle_stop_command(msg).await
        } else if msg.topic.contains("registry/server") {
            self.handle_chunk(msg).await
        } else {
            debug!("Ignoring message from unknown topic: {}", msg.topic);
            Ok(())
        }
    }

    async fn handle_start_command(&self, msg: MqttMessage) -> Result<()> {
        let req: StartRequest = msg.decode().map_err(|e| {
            error!("Failed to decode start request: {}", e);
            if let Ok(payload_str) = String::from_utf8(msg.payload.clone()) {
                error!("Payload was: {}", payload_str);
            }
            e
        })?;
        req.validate()?;

        info!("Received start command for task: {}", req.id);

        let runtime = if req.encrypted {
            if let Some(ref tee_runtime) = self.tee_runtime {
                tee_runtime.clone()
            } else {
                error!("TEE runtime not available but encrypted workload requested");
                self.publish_result(
                    &req.id,
                    Vec::new(),
                    Some("TEE runtime not available".to_string()),
                )
                .await?;
                return Err(anyhow::anyhow!("TEE runtime not available"));
            }
        } else {
            self.runtime.clone()
        };

        {
            let mut tasks = self.running_tasks.lock().await;
            use std::collections::hash_map::Entry;
            if let Entry::Vacant(e) = tasks.entry(req.id.clone()) {
                e.insert(TaskState::Running);
            } else {
                warn!(
                    "Task {} is already running, ignoring duplicate start command",
                    req.id
                );
                return Ok(());
            }
        }

        let wasm_binary = if !req.file.is_empty() {
            use base64::{engine::general_purpose::STANDARD, Engine};
            match STANDARD.decode(&req.file) {
                Ok(decoded) => {
                    info!("Decoded wasm binary, size: {} bytes", decoded.len());
                    decoded
                }
                Err(e) => {
                    error!("Failed to decode base64 file for task {}: {}", req.id, e);
                    self.running_tasks.lock().await.remove(&req.id);
                    self.publish_result(&req.id, Vec::new(), Some(e.to_string()))
                        .await?;
                    return Err(e.into());
                }
            }
        } else if !req.image_url.is_empty() {
            if req.encrypted {
                info!("Encrypted workload with image_url: {}", req.image_url);
                Vec::new()
            } else {
                info!("Requesting binary from registry: {}", req.image_url);
                self.request_binary_from_registry(&req.image_url).await?;

                match self.wait_for_binary(&req.image_url).await {
                    Ok(binary) => binary,
                    Err(e) => {
                        error!("Failed to get binary for task {}: {}", req.id, e);
                        self.running_tasks.lock().await.remove(&req.id);
                        self.publish_result(&req.id, Vec::new(), Some(e.to_string()))
                            .await?;
                        return Err(e);
                    }
                }
            }
        } else {
            let err = anyhow::anyhow!("No wasm binary or image URL provided");
            error!("Validation error for task {}: {}", req.id, err);
            self.running_tasks.lock().await.remove(&req.id);
            self.publish_result(&req.id, Vec::new(), Some(err.to_string()))
                .await?;
            return Err(err);
        };

        let monitoring_profile = req.monitoring_profile.clone().unwrap_or_else(|| {
            if req.daemon {
                MonitoringProfile::long_running_daemon()
            } else {
                MonitoringProfile::standard()
            }
        });

        let pubsub = self.pubsub.clone();
        let running_tasks = self.running_tasks.clone();
        let monitor = self.monitor.clone();
        let domain_id = self.config.domain_id.clone();
        let channel_id = self.config.channel_id.clone();
        let qos = self.config.qos();
        let proplet_id = self.config.client_id.clone();
        let task_id = req.id.clone();
        let task_name = req.name.clone();
        let mut env = req.env.unwrap_or_default();
        if !env.is_empty() {
            info!(
                "Received {} environment variables for task {}",
                env.len(),
                task_id
            );
            for (key, value) in &env {
                debug!("  {}={}", key, value);
            }
        } else {
            warn!(
                "No environment variables in start request for task {}",
                task_id
            );
        }
        let daemon = req.daemon;
        let cli_args = req.cli_args.clone();
        let inputs = req.inputs.clone();
        let http_client = self.http_client.clone();

        let image_url = if req.encrypted && !req.image_url.is_empty() {
            Some(req.image_url.clone())
        } else {
            None
        };

        let export_metrics = monitoring_profile.enabled && monitoring_profile.export_to_mqtt;

        tokio::spawn(async move {
            let ctx = RuntimeContext {
                proplet_id: proplet_id.clone(),
            };

            info!("Executing task {} in spawned task", task_id);

            if !env.contains_key("PROPLET_ID") {
                env.insert("PROPLET_ID".to_string(), proplet_id.clone());
            }

            let mut cli_args = cli_args;
            if let Some(ref img_url) = image_url {
                cli_args.insert(0, img_url.clone());
            }

            // Note: We'll create config after fetching MODEL_DATA and DATASET_DATA
            // so they can be included in the environment variables
            let mut config = StartConfig {
                id: task_id.clone(),
                function_name: task_name.clone(),
                daemon,
                wasm_binary,
                cli_args,
                env: env.clone(), // Initial clone, will be updated after fetches
                args: inputs,
                mode: req.mode.clone(),
            };

            if export_metrics {
                if let Err(e) = monitor
                    .start_monitoring(&task_id, monitoring_profile.clone())
                    .await
                {
                    error!("Failed to start monitoring for task {}: {}", task_id, e);
                }
            }

            let monitor_handle = if export_metrics {
                let monitor_clone = monitor.clone();
                let runtime_clone = runtime.clone();
                let task_id_clone = task_id.clone();
                let pubsub_clone = pubsub.clone();
                let domain_clone = domain_id.clone();
                let channel_clone = channel_id.clone();
                let proplet_id_clone = proplet_id.clone();

                Some(tokio::spawn(async move {
                    tokio::time::sleep(tokio::time::Duration::from_millis(100)).await;

                    if let Ok(Some(pid)) = runtime_clone.get_pid(&task_id_clone).await {
                        let task_id_for_closure = task_id_clone.clone();
                        let proplet_id_for_closure = proplet_id_clone.clone();
                        monitor_clone
                            .attach_pid(&task_id_clone, pid, move |metrics, aggregated| {
                                let pubsub = pubsub_clone.clone();
                                let domain = domain_clone.clone();
                                let channel = channel_clone.clone();
                                let task_id = task_id_for_closure.clone();
                                let proplet_id = proplet_id_for_closure.clone();

                                tokio::spawn(async move {
                                    let metrics_msg = MetricsMessage {
                                        task_id,
                                        proplet_id,
                                        metrics,
                                        aggregated,
                                        timestamp: SystemTime::now(),
                                    };

                                    let topic = build_topic(
                                        &domain,
                                        &channel,
                                        "control/proplet/task_metrics",
                                    );
                                    if let Err(e) = pubsub.publish(&topic, &metrics_msg, qos).await
                                    {
                                        debug!("Failed to publish task metrics: {}", e);
                                    }
                                });
                            })
                            .await;
                    } else {
                        debug!(
                            "No PID available for task {}, skipping monitoring",
                            task_id_clone
                        );
                    }
                }))
            } else {
                None
            };

            // Fetch model if MODEL_URI is present (FML task)
            // Use environment variables only - no fallbacks, must be set in .env file
            if let Some(model_uri) = env.get("MODEL_URI") {
                let model_registry_url = match env
                    .get("MODEL_REGISTRY_URL")
                    .cloned()
                    .or_else(|| std::env::var("MODEL_REGISTRY_URL").ok())
                {
                    Some(url) => url,
                    None => {
                        error!("MODEL_REGISTRY_URL not set. Must be provided via environment variable in .env file.");
                        return;
                    }
                };

                let model_version = extract_model_version_from_uri(model_uri);
                let model_url = format!("{}/models/{}", model_registry_url, model_version);

                info!("Fetching model from registry: {}", model_url);
                match http_client.get(&model_url).send().await {
                    Ok(response) if response.status().is_success() => {
                        if let Ok(model_json) = response.json::<serde_json::Value>().await {
                            if let Ok(model_str) = serde_json::to_string(&model_json) {
                                env.insert("MODEL_DATA".to_string(), model_str);
                                info!(
                                    "Successfully fetched model v{} and passed to client",
                                    model_version
                                );
                            }
                        }
                    }
                    Ok(response) => {
                        warn!("Failed to fetch model: HTTP {}", response.status());
                    }
                    Err(e) => {
                        warn!("Failed to fetch model from registry: {}", e);
                    }
                }
            }

            // Fetch dataset if this is an FML task
            // Use environment variables only - no fallbacks, must be set in .env file
            let dataset_proplet_id = env.get("PROPLET_ID").cloned().unwrap_or_else(|| {
                warn!(
                    "PROPLET_ID not in environment, using proplet_id from config: {}",
                    proplet_id
                );
                proplet_id.clone()
            });

            let data_store_url = match env
                .get("DATA_STORE_URL")
                .cloned()
                .or_else(|| std::env::var("DATA_STORE_URL").ok())
            {
                Some(url) => url,
                None => {
                    warn!("DATA_STORE_URL not set. Dataset fetching will be skipped. Set it in .env file to enable dataset fetching.");
                    String::new() // Empty string to skip dataset fetching
                }
            };

            if !data_store_url.is_empty() {
                let dataset_url = format!("{}/datasets/{}", data_store_url, dataset_proplet_id);
                info!("Fetching dataset from Local Data Store: {}", dataset_url);
                match http_client.get(&dataset_url).send().await {
                    Ok(response) if response.status().is_success() => {
                        if let Ok(dataset_json) = response.json::<serde_json::Value>().await {
                            if let Ok(dataset_str) = serde_json::to_string(&dataset_json) {
                                env.insert("DATASET_DATA".to_string(), dataset_str);
                                if let Some(size) =
                                    dataset_json.get("size").and_then(|s| s.as_u64())
                                {
                                    info!("Successfully fetched dataset with {} samples and passed to client", size);
                                } else {
                                    info!("Successfully fetched dataset and passed to client");
                                }
                            }
                        }
                    }
                    Ok(response) => {
                        warn!(
                            "Failed to fetch dataset: HTTP {} (will use synthetic data)",
                            response.status()
                        );
                    }
                    Err(e) => {
                        warn!("Failed to fetch dataset from Local Data Store: {} (will use synthetic data)", e);
                    }
                }
            }

            // Update config.env with the latest env (including MODEL_DATA and DATASET_DATA)
            config.env = env.clone();

            let result = runtime.start_app(ctx, config).await;

            if let Some(handle) = monitor_handle {
                let _ = handle.await;
            }

            monitor.stop_monitoring(&task_id).await.ok();

            let (result_str, error) = match result {
                Ok(data) => {
                    let result_str = String::from_utf8_lossy(&data).to_string();
                    info!(
                        "Task {} completed successfully. Result: {}",
                        task_id, result_str
                    );
                    (result_str, None)
                }
                Err(e) => {
                    error!("Task {} failed: {:#}", task_id, e);
                    (String::new(), Some(e.to_string()))
                }
            };

            if let Some(round_id) = env.get("ROUND_ID") {
                // COORDINATOR_URL is required for FML tasks (when ROUND_ID is present)
                // Use environment variables only - no fallbacks, must be set in .env file
                let coordinator_url = match env
                    .get("COORDINATOR_URL")
                    .cloned()
                    .or_else(|| std::env::var("COORDINATOR_URL").ok())
                {
                    Some(url) => url,
                    None => {
                        error!("COORDINATOR_URL not set. Must be provided via environment variable in .env file for FML tasks.");
                        return;
                    }
                };

                if let Ok(update_json) = serde_json::from_str::<serde_json::Value>(&result_str) {
                    let update_url = format!("{}/update", coordinator_url);

                    match http_client
                        .post(&update_url)
                        .json(&update_json)
                        .send()
                        .await
                    {
                        Ok(response) if response.status().is_success() => {
                            info!(
                                "Successfully posted FL update to coordinator via HTTP: {}",
                                update_url
                            );
                        }
                        Ok(response) => {
                            warn!(
                                "Coordinator returned error status {} for update, falling back to MQTT",
                                response.status()
                            );
                            let fl_topic = format!("fl/rounds/{}/updates/{}", round_id, proplet_id);
                            if let Err(e) = pubsub.publish(&fl_topic, &update_json, qos).await {
                                error!("Failed to publish FL update to {}: {}", fl_topic, e);
                            }
                        }
                        Err(e) => {
                            warn!(
                                "Failed to POST update to coordinator via HTTP: {}, falling back to MQTT",
                                e
                            );
                            let fl_topic = format!("fl/rounds/{}/updates/{}", round_id, proplet_id);
                            if let Err(e) = pubsub.publish(&fl_topic, &update_json, qos).await {
                                error!("Failed to publish FL update to {}: {}", fl_topic, e);
                            } else {
                                info!(
                                    "Successfully published FL update to coordinator via MQTT: {}",
                                    fl_topic
                                );
                            }
                        }
                    }
                } else if error.is_none() {
                    warn!(
                        "Task {} output was not valid JSON, skipping FL update publish to coordinator",
                        task_id
                    );
                }
            }

            if env.contains_key("ROUND_ID") {
                let update_envelope =
                    build_fl_update_envelope(&task_id, &proplet_id, &result_str, &env);

                #[derive(serde::Serialize)]
                struct FLResultMessage {
                    task_id: String,
                    results: serde_json::Value,
                    error: Option<String>,
                }

                let fl_result = FLResultMessage {
                    task_id: task_id.clone(),
                    results: serde_json::to_value(&update_envelope).unwrap_or_default(),
                    error,
                };

                let topic = build_topic(&domain_id, &channel_id, "control/proplet/results");
                info!("Publishing FL update for task {}", task_id);

                if let Err(e) = pubsub.publish(&topic, &fl_result, qos).await {
                    error!("Failed to publish FL result for task {}: {}", task_id, e);
                } else {
                    info!("Successfully published FL update for task {}", task_id);
                }
            } else {
                let result_msg = ResultMessage {
                    task_id: task_id.clone(),
                    proplet_id,
                    results: result_str,
                    error,
                };

                let topic = build_topic(&domain_id, &channel_id, "control/proplet/results");

                info!("Publishing result for task {}", task_id);

                if let Err(e) = pubsub.publish(&topic, &result_msg, qos).await {
                    error!("Failed to publish result for task {}: {}", task_id, e);
                } else {
                    info!("Successfully published result for task {}", task_id);
                }
            }

            running_tasks.lock().await.remove(&task_id);
        });

        Ok(())
    }

    async fn handle_stop_command(&self, msg: MqttMessage) -> Result<()> {
        let req: StopRequest = msg.decode()?;
        req.validate()?;

        info!("Received stop command for task: {}", req.id);

        self.runtime.stop_app(req.id.clone()).await?;
        self.monitor.stop_monitoring(&req.id).await.ok();

        self.running_tasks.lock().await.remove(&req.id);

        Ok(())
    }

    async fn handle_chunk(&self, msg: MqttMessage) -> Result<()> {
        let chunk: Chunk = msg.decode()?;

        debug!(
            "Received chunk {}/{} for app '{}'",
            chunk.chunk_idx + 1,
            chunk.total_chunks,
            chunk.app_name
        );

        let mut assembly = self.chunk_assembly.lock().await;

        let state = assembly
            .entry(chunk.app_name.clone())
            .or_insert_with(|| ChunkAssemblyState::new(chunk.total_chunks));

        if state.total_chunks != chunk.total_chunks {
            warn!(
                "Chunk total_chunks mismatch for '{}': expected {}, got {}",
                chunk.app_name, state.total_chunks, chunk.total_chunks
            );
            return Err(anyhow::anyhow!(
                "Chunk total_chunks mismatch for '{}'",
                chunk.app_name
            ));
        }

        if let std::collections::btree_map::Entry::Vacant(e) = state.chunks.entry(chunk.chunk_idx) {
            e.insert(chunk.data);
            debug!(
                "Stored chunk {} for app '{}' ({}/{} chunks received)",
                chunk.chunk_idx,
                chunk.app_name,
                state.chunks.len(),
                state.total_chunks
            );
        } else {
            debug!(
                "Duplicate chunk {} for app '{}', ignoring",
                chunk.chunk_idx, chunk.app_name
            );
        }

        Ok(())
    }

    async fn request_binary_from_registry(&self, app_name: &str) -> Result<()> {
        let topic = build_topic(
            &self.config.domain_id,
            &self.config.channel_id,
            "registry/proplet",
        );

        #[derive(serde::Serialize)]
        struct RegistryRequest {
            app_name: String,
        }

        let req = RegistryRequest {
            app_name: app_name.to_string(),
        };
        self.pubsub.publish(&topic, &req, self.config.qos()).await?;

        debug!("Requested binary from registry for app: {}", app_name);
        Ok(())
    }

    async fn wait_for_binary(&self, app_name: &str) -> Result<Vec<u8>> {
        let timeout = tokio::time::Duration::from_secs(60);
        let start = tokio::time::Instant::now();
        let polling_interval = tokio::time::Duration::from_secs(5);

        loop {
            if start.elapsed() > timeout {
                return Err(anyhow::anyhow!("Timeout waiting for binary chunks"));
            }

            let assembled = self.try_assemble_chunks(app_name).await?;
            if let Some(binary) = assembled {
                return Ok(binary);
            }

            tokio::time::sleep(polling_interval).await;
        }
    }

    async fn try_assemble_chunks(&self, app_name: &str) -> Result<Option<Vec<u8>>> {
        let mut assembly = self.chunk_assembly.lock().await;

        if let Some(state) = assembly.get(app_name) {
            if state.is_complete() {
                let binary = state.assemble();

                info!(
                    "Assembled binary for app '{}', size: {} bytes from {} chunks",
                    app_name,
                    binary.len(),
                    state.total_chunks
                );

                assembly.remove(app_name);

                return Ok(Some(binary));
            }
        }

        Ok(None)
    }

    async fn publish_result(
        &self,
        task_id: &str,
        results: Vec<u8>,
        error: Option<String>,
    ) -> Result<()> {
        let proplet_id = self.config.client_id.clone();
        let result_str = String::from_utf8_lossy(&results).to_string();

        let result_msg = ResultMessage {
            task_id: task_id.to_string(),
            proplet_id,
            results: result_str,
            error,
        };

        let topic = build_topic(
            &self.config.domain_id,
            &self.config.channel_id,
            "control/proplet/results",
        );

        self.pubsub
            .publish(&topic, &result_msg, self.config.qos())
            .await?;
        Ok(())
    }

    #[allow(dead_code)]
    fn parse_http_requests_from_stderr(stderr: &str) -> Vec<HttpRequest> {
        let mut requests = Vec::new();

        for line in stderr.lines() {
            if let Some(json_str) = line.strip_prefix("MODEL_REQUEST:") {
                if let Ok(req) = serde_json::from_str::<serde_json::Value>(json_str.trim()) {
                    if let Some(url) = req.get("url").and_then(|u| u.as_str()) {
                        requests.push(HttpRequest {
                            action: "get_model".to_string(),
                            url: url.to_string(),
                            data: None,
                        });
                    }
                }
            } else if let Some(json_str) = line.strip_prefix("UPDATE_REQUEST:") {
                if let Ok(req) = serde_json::from_str::<serde_json::Value>(json_str.trim()) {
                    if let Some(url) = req.get("url").and_then(|u| u.as_str()) {
                        let data = req.get("data").cloned();
                        requests.push(HttpRequest {
                            action: "post_update".to_string(),
                            url: url.to_string(),
                            data,
                        });
                    }
                }
            } else if let Some(json_str) = line.strip_prefix("TASK_REQUEST:") {
                if let Ok(req) = serde_json::from_str::<serde_json::Value>(json_str.trim()) {
                    if let Some(url) = req.get("url").and_then(|u| u.as_str()) {
                        requests.push(HttpRequest {
                            action: "get_task".to_string(),
                            url: url.to_string(),
                            data: None,
                        });
                    }
                }
            }
        }

        requests
    }

    #[allow(dead_code)]
    async fn fetch_model_from_registry(&self, url: &str) -> Result<serde_json::Value> {
        info!("Fetching model from registry: {}", url);
        let response = self
            .http_client
            .get(url)
            .send()
            .await
            .context("Failed to send GET request to model registry")?;

        if !response.status().is_success() {
            return Err(anyhow::anyhow!(
                "Model registry returned error: {}",
                response.status()
            ));
        }

        let model: serde_json::Value = response
            .json()
            .await
            .context("Failed to parse model JSON from registry")?;

        info!("Successfully fetched model from registry");
        Ok(model)
    }

    #[allow(dead_code)]
    async fn post_update_to_coordinator(
        &self,
        url: &str,
        update_data: &serde_json::Value,
    ) -> Result<()> {
        info!("Posting update to coordinator: {}", url);
        let response = self
            .http_client
            .post(url)
            .json(update_data)
            .send()
            .await
            .context("Failed to send POST request to coordinator")?;

        if !response.status().is_success() {
            return Err(anyhow::anyhow!(
                "Coordinator returned error: {}",
                response.status()
            ));
        }

        info!("Successfully posted update to coordinator");
        Ok(())
    }

    #[allow(dead_code)]
    async fn get_task_from_coordinator(&self, url: &str) -> Result<serde_json::Value> {
        info!("Getting task from coordinator: {}", url);
        let response = self
            .http_client
            .get(url)
            .send()
            .await
            .context("Failed to send GET request to coordinator")?;

        if !response.status().is_success() {
            return Err(anyhow::anyhow!(
                "Coordinator returned error: {}",
                response.status()
            ));
        }

        let task: serde_json::Value = response
            .json()
            .await
            .context("Failed to parse task JSON from coordinator")?;

        info!("Successfully got task from coordinator");
        Ok(task)
    }
}

#[allow(dead_code)]
#[derive(Debug)]
struct HttpRequest {
    action: String,
    url: String,
    data: Option<serde_json::Value>,
}

fn extract_model_version_from_uri(uri: &str) -> i32 {
    if let Some(last_part) = uri.split('/').next_back() {
        if let Some(v_part) = last_part.strip_prefix("global_model_v") {
            if let Ok(version) = v_part.parse::<i32>() {
                return version;
            }
        }
        if let Ok(num_str) = last_part
            .chars()
            .rev()
            .take_while(|c| c.is_ascii_digit())
            .collect::<String>()
            .chars()
            .rev()
            .collect::<String>()
            .parse::<i32>()
        {
            return num_str;
        }
    }
    0
}

fn build_fl_update_envelope(
    task_id: &str,
    proplet_id: &str,
    result_str: &str,
    env: &HashMap<String, String>,
) -> serde_json::Value {
    use base64::{engine::general_purpose::STANDARD, Engine};

    let num_samples = env
        .get("FL_NUM_SAMPLES")
        .and_then(|s| s.parse::<u64>().ok())
        .unwrap_or(1);

    let update_format = env
        .get("FL_FORMAT")
        .cloned()
        .unwrap_or_else(|| "f32-delta".to_string());

    let round_id = env.get("ROUND_ID").cloned().unwrap_or_default();

    let update_b64 = STANDARD.encode(result_str.as_bytes());

    serde_json::json!({
        "task_id": task_id,
        "round_id": round_id,
        "proplet_id": proplet_id,
        "num_samples": num_samples,
        "update_b64": update_b64,
        "format": update_format,
        "metrics": {}
    })
}
