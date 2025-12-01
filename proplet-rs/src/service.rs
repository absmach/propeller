use crate::config::PropletConfig;
use crate::mqtt::{build_topic, MqttMessage, PubSub};
use crate::runtime::{Runtime, RuntimeContext};
use crate::types::*;
use anyhow::Result;
use std::collections::HashMap;
use std::sync::Arc;
use tokio::sync::{mpsc, Mutex};
use tracing::{debug, error, info};
use uuid::Uuid;

pub struct PropletService {
    config: PropletConfig,
    proplet: Arc<Mutex<Proplet>>,
    pubsub: PubSub,
    runtime: Arc<dyn Runtime>,
    chunks: Arc<Mutex<HashMap<Uuid, Vec<Option<Vec<u8>>>>>>,
    chunk_metadata: Arc<Mutex<HashMap<Uuid, ChunkMetadata>>>,
    running_tasks: Arc<Mutex<HashMap<Uuid, TaskState>>>,
}

impl PropletService {
    pub fn new(
        config: PropletConfig,
        pubsub: PubSub,
        runtime: Arc<dyn Runtime>,
    ) -> Self {
        let proplet = Proplet::new(config.instance_id, "proplet-rs".to_string());

        Self {
            config,
            proplet: Arc::new(Mutex::new(proplet)),
            pubsub,
            runtime,
            chunks: Arc::new(Mutex::new(HashMap::new())),
            chunk_metadata: Arc::new(Mutex::new(HashMap::new())),
            running_tasks: Arc::new(Mutex::new(HashMap::new())),
        }
    }

    pub async fn run(self: Arc<Self>, mut mqtt_rx: mpsc::UnboundedReceiver<MqttMessage>) -> Result<()> {
        info!("Starting PropletService");

        // Publish discovery message
        self.publish_discovery().await?;

        // Subscribe to topics
        self.subscribe_topics().await?;

        // Start liveliness updates
        let service = self.clone();
        tokio::spawn(async move {
            service.start_liveliness_updates().await;
        });

        // Process MQTT messages
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
        let start_topic = build_topic(
            &self.config.domain_id,
            &self.config.channel_id,
            "control/manager/start",
        );
        self.pubsub.subscribe(&start_topic).await?;

        let stop_topic = build_topic(
            &self.config.domain_id,
            &self.config.channel_id,
            "control/manager/stop",
        );
        self.pubsub.subscribe(&stop_topic).await?;

        let chunk_topic = build_topic(
            &self.config.domain_id,
            &self.config.channel_id,
            "registry/server",
        );
        self.pubsub.subscribe(&chunk_topic).await?;

        Ok(())
    }

    async fn publish_discovery(&self) -> Result<()> {
        let proplet = self.proplet.lock().await;
        let discovery = DiscoveryMessage {
            proplet_id: proplet.id,
            name: proplet.name.clone(),
        };

        let topic = build_topic(
            &self.config.domain_id,
            &self.config.channel_id,
            "control/proplet/create",
        );

        self.pubsub.publish(&topic, &discovery).await?;
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
            proplet_id: proplet.id,
            alive: proplet.alive,
            task_count: proplet.task_count,
        };

        let topic = build_topic(
            &self.config.domain_id,
            &self.config.channel_id,
            "control/proplet/alive",
        );

        self.pubsub.publish(&topic, &liveliness).await?;
        debug!("Published liveliness update");

        Ok(())
    }

    async fn handle_message(&self, msg: MqttMessage) -> Result<()> {
        debug!("Handling message from topic: {}", msg.topic);

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
        let req: StartRequest = msg.decode()?;
        req.validate()?;

        info!("Received start command for task: {}", req.id);

        // Update running tasks
        {
            let mut tasks = self.running_tasks.lock().await;
            tasks.insert(req.id, TaskState::Running);
        }

        let wasm_binary = if !req.wasm_file.is_empty() {
            // Use provided wasm file
            req.wasm_file.clone()
        } else if !req.image_url.is_empty() {
            // Request binary from registry
            info!("Requesting binary from registry: {}", req.image_url);
            self.request_binary_from_registry(req.id).await?;

            // Wait for chunks to be assembled
            match self.wait_for_binary(req.id).await {
                Ok(binary) => binary,
                Err(e) => {
                    error!("Failed to get binary for task {}: {}", req.id, e);
                    self.publish_result(req.id, Vec::new(), Some(e.to_string())).await?;
                    return Err(e);
                }
            }
        } else {
            return Err(anyhow::anyhow!("No wasm binary or image URL provided"));
        };

        // Execute the task
        let runtime = self.runtime.clone();
        let pubsub = self.pubsub.clone();
        let running_tasks = self.running_tasks.clone();
        let domain_id = self.config.domain_id.clone();
        let channel_id = self.config.channel_id.clone();
        let proplet_id = self.proplet.lock().await.id;

        tokio::spawn(async move {
            let ctx = RuntimeContext { proplet_id };

            let result = runtime
                .start_app(
                    ctx,
                    wasm_binary,
                    req.cli_args,
                    req.id,
                    req.function_name,
                    req.daemon,
                    req.env,
                    req.params,
                )
                .await;

            // Publish result
            let topic = build_topic(&domain_id, &channel_id, "control/proplet/results");

            let (result_data, error) = match result {
                Ok(data) => (data, None),
                Err(e) => {
                    error!("Task {} failed: {}", req.id, e);
                    (Vec::new(), Some(e.to_string()))
                }
            };

            let result_msg = ResultMessage {
                task_id: req.id,
                proplet_id,
                result: result_data,
                error,
            };

            if let Err(e) = pubsub.publish(&topic, &result_msg).await {
                error!("Failed to publish result for task {}: {}", req.id, e);
            }

            // Remove from running tasks
            running_tasks.lock().await.remove(&req.id);
        });

        Ok(())
    }

    async fn handle_stop_command(&self, msg: MqttMessage) -> Result<()> {
        let req: StopRequest = msg.decode()?;
        req.validate()?;

        info!("Received stop command for task: {}", req.id);

        // Stop the task
        self.runtime.stop_app(req.id).await?;

        // Remove from running tasks
        self.running_tasks.lock().await.remove(&req.id);

        Ok(())
    }

    async fn handle_chunk(&self, msg: MqttMessage) -> Result<()> {
        let chunk: Chunk = msg.decode()?;

        debug!("Received chunk {} for task {}", chunk.sequence, chunk.id);

        let mut chunks = self.chunks.lock().await;
        let metadata = self.chunk_metadata.lock().await;

        if let Some(meta) = metadata.get(&chunk.id) {
            let task_chunks = chunks.entry(chunk.id).or_insert_with(|| {
                vec![None; meta.total]
            });

            if chunk.sequence < task_chunks.len() {
                task_chunks[chunk.sequence] = Some(chunk.data);
            }
        }

        Ok(())
    }

    async fn request_binary_from_registry(&self, task_id: Uuid) -> Result<()> {
        let topic = build_topic(
            &self.config.domain_id,
            &self.config.channel_id,
            "registry/proplet",
        );

        #[derive(serde::Serialize)]
        struct RegistryRequest {
            task_id: Uuid,
        }

        let req = RegistryRequest { task_id };
        self.pubsub.publish(&topic, &req).await?;

        debug!("Requested binary from registry for task: {}", task_id);
        Ok(())
    }

    async fn wait_for_binary(&self, task_id: Uuid) -> Result<Vec<u8>> {
        // Wait up to 60 seconds for all chunks
        let timeout = tokio::time::Duration::from_secs(60);
        let start = tokio::time::Instant::now();

        loop {
            if start.elapsed() > timeout {
                return Err(anyhow::anyhow!("Timeout waiting for binary chunks"));
            }

            let assembled = self.try_assemble_chunks(task_id).await?;
            if let Some(binary) = assembled {
                return Ok(binary);
            }

            tokio::time::sleep(tokio::time::Duration::from_millis(100)).await;
        }
    }

    async fn try_assemble_chunks(&self, task_id: Uuid) -> Result<Option<Vec<u8>>> {
        let chunks = self.chunks.lock().await;

        if let Some(task_chunks) = chunks.get(&task_id) {
            // Check if all chunks are present
            if task_chunks.iter().all(|c| c.is_some()) {
                // Assemble binary
                let mut binary = Vec::new();
                for chunk in task_chunks {
                    if let Some(data) = chunk {
                        binary.extend_from_slice(data);
                    }
                }

                debug!("Assembled binary for task {}, size: {} bytes", task_id, binary.len());
                return Ok(Some(binary));
            }
        }

        Ok(None)
    }

    async fn publish_result(&self, task_id: Uuid, result: Vec<u8>, error: Option<String>) -> Result<()> {
        let proplet_id = self.proplet.lock().await.id;

        let result_msg = ResultMessage {
            task_id,
            proplet_id,
            result,
            error,
        };

        let topic = build_topic(
            &self.config.domain_id,
            &self.config.channel_id,
            "control/proplet/results",
        );

        self.pubsub.publish(&topic, &result_msg).await?;
        Ok(())
    }
}
