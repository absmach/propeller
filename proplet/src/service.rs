use crate::config::PropletConfig;
use crate::metrics::MetricsCollector;
use crate::monitoring::{system::SystemMonitor, ProcessMonitor};
use crate::mqtt::{build_topic, MqttMessage, PubSub};
use crate::runtime::{Runtime, RuntimeContext, StartConfig};
use crate::types::*;
use anyhow::Result;
use std::collections::{BTreeMap, HashMap};
use std::sync::Arc;
use std::time::SystemTime;
use tokio::sync::{mpsc, Mutex};
use tokio::time::Instant;
use tracing::{debug, error, info, warn};

#[cfg(feature = "cdh")]
use crate::cdh::{CdhHub, ConfidentialDataHub};

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
    chunk_assembly: Arc<Mutex<HashMap<String, ChunkAssemblyState>>>,
    running_tasks: Arc<Mutex<HashMap<String, TaskState>>>,
    monitor: Arc<SystemMonitor>,
    metrics_collector: Arc<Mutex<MetricsCollector>>,
    #[cfg(feature = "cdh")]
    cdh_hub: Option<Arc<CdhHub>>,
}

impl PropletService {
    pub async fn new(config: PropletConfig, pubsub: PubSub, runtime: Arc<dyn Runtime>) -> Self {
        let proplet = Proplet::new(config.instance_id.clone(), "proplet".to_string());
        let monitor = Arc::new(SystemMonitor::new(MonitoringProfile::default()));
        let metrics_collector = Arc::new(Mutex::new(MetricsCollector::new()));

        #[cfg(feature = "cdh")]
        let cdh_hub = if config.cdh_enabled.unwrap_or(false) {
            info!("CDH enabled, initializing CdhHub");
            match crate::cdh::CdhConfig::from_proplet_config(&config) {
                Ok(cdh_config) => {
                    match crate::cdh::CdhHubBuilder::new()
                        .with_config(cdh_config)
                        .build()
                        .await
                    {
                        Ok(hub) => {
                            info!("CdhHub initialized successfully");
                            Some(Arc::new(hub))
                        }
                        Err(e) => {
                            warn!("Failed to initialize CdhHub: {}, continuing without CDH", e);
                            None
                        }
                    }
                }
                Err(e) => {
                    warn!("Failed to load CDH config: {}, continuing without CDH", e);
                    None
                }
            }
        } else {
            info!("CDH disabled");
            None
        };

        let service = Self {
            config,
            proplet: Arc::new(Mutex::new(proplet)),
            pubsub,
            runtime,
            chunk_assembly: Arc::new(Mutex::new(HashMap::new())),
            running_tasks: Arc::new(Mutex::new(HashMap::new())),
            monitor,
            metrics_collector,
            #[cfg(feature = "cdh")]
            cdh_hub,
        };

        service.start_chunk_expiry_task();

        service
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

        // Start proplet metrics updates if enabled
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

        {
            let mut tasks = self.running_tasks.lock().await;
            tasks.insert(req.id.clone(), TaskState::Running);
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
            #[cfg(feature = "cdh")]
            if self.cdh_hub.is_some() && req.encrypted {
                info!("Processing encrypted image with CDH: {}", req.image_url);

                let cdh_hub = self.cdh_hub.as_ref().unwrap().clone();

                let temp_dir = std::env::temp_dir();
                let output_path = temp_dir.join(format!("task-{}.wasm", req.id));

                match cdh_hub
                    .pull_encrypted_image(&req.image_url, &output_path)
                    .await
                {
                    Ok(digest) => {
                        info!("Successfully pulled encrypted image, digest: {}", digest);

                        match tokio::fs::read(&output_path).await {
                            Ok(binary) => {
                                info!("Read decrypted wasm binary, size: {} bytes", binary.len());
                                tokio::fs::remove_file(&output_path).await.ok();
                                binary
                            }
                            Err(e) => {
                                error!("Failed to read decrypted wasm binary: {}", e);
                                self.running_tasks.lock().await.remove(&req.id);
                                self.publish_result(&req.id, Vec::new(), Some(e.to_string()))
                                    .await?;
                                return Err(e.into());
                            }
                        }
                    }
                    Err(e) => {
                        error!("Failed to pull encrypted image: {}", e);
                        self.running_tasks.lock().await.remove(&req.id);
                        self.publish_result(&req.id, Vec::new(), Some(e.to_string()))
                            .await?;
                        return Err(e.into());
                    }
                }
            } else {
                info!("Requesting binary from registry: {}", req.image_url);
                info!("cdh_hub.is_some() = {}", self.cdh_hub.is_some());
                info!("req.encrypted = {}", req.encrypted);
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

            #[cfg(not(feature = "cdh"))]
            {
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

        let runtime = self.runtime.clone();
        let pubsub = self.pubsub.clone();
        let running_tasks = self.running_tasks.clone();
        let monitor = self.monitor.clone();
        let domain_id = self.config.domain_id.clone();
        let channel_id = self.config.channel_id.clone();
        let qos = self.config.qos();
        let proplet_id = self.proplet.lock().await.id.clone();
        let task_id = req.id.clone();
        let task_name = req.name.clone();
        let env = req.env.unwrap_or_default();
        let daemon = req.daemon;
        let cli_args = req.cli_args.clone();
        let inputs = req.inputs.clone();

        let export_metrics = monitoring_profile.enabled && monitoring_profile.export_to_mqtt;

        tokio::spawn(async move {
            let ctx = RuntimeContext {
                proplet_id: proplet_id.clone(),
            };

            info!("Executing task {} in spawned task", task_id);

            let config = StartConfig {
                id: task_id.clone(),
                function_name: task_name.clone(),
                daemon,
                wasm_binary,
                cli_args,
                env,
                args: inputs,
            };

            // Start monitoring setup (if enabled)
            if export_metrics {
                if let Err(e) = monitor
                    .start_monitoring(&task_id, monitoring_profile.clone())
                    .await
                {
                    error!("Failed to start monitoring for task {}: {}", task_id, e);
                }
            }

            // Spawn the monitoring attachment in a separate task to run concurrently
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
                    error!("Task {} failed: {}", task_id, e);
                    (String::new(), Some(e.to_string()))
                }
            };

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
        let proplet_id = self.proplet.lock().await.id.clone();
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
}
