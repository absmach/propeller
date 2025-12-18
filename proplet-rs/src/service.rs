use crate::config::PropletConfig;
use crate::mqtt::{build_topic, MqttMessage, PubSub};
use crate::runtime::{Runtime, RuntimeContext, StartConfig};
use crate::types::*;
use aes_gcm::{aead::Aead, Aes256Gcm, Key, KeyInit, Nonce};
use anyhow::{Context, Result};
use sha2::{Digest, Sha256};
use std::collections::{BTreeMap, HashMap};
use std::sync::Arc;
use tokio::sync::{mpsc, Mutex};
use tokio::time::Instant;
use tracing::{debug, error, info, warn};

#[derive(Debug)]
struct ChunkAssemblyState {
    chunks: BTreeMap<usize, Vec<u8>>,
    total_chunks: usize,
    checksum: String,
    created_at: Instant,
}

impl ChunkAssemblyState {
    fn new(total_chunks: usize, checksum: String) -> Self {
        Self {
            chunks: BTreeMap::new(),
            total_chunks,
            checksum,
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
    workload_key: Key<Aes256Gcm>,
}

impl PropletService {
    pub fn new(config: PropletConfig, pubsub: PubSub, runtime: Arc<dyn Runtime>) -> Result<Self> {
        let proplet = Proplet::new(config.instance_id, "proplet-rs".to_string());

        let key_bytes = hex::decode(&config.workload_key)
            .context("Failed to decode PROPLET_WORKLOAD_KEY from hex")?;
        
        if key_bytes.len() != 32 {
            return Err(anyhow::anyhow!("PROPLET_WORKLOAD_KEY must be 32 bytes (AES-256)"));
        }
        
        let workload_key = *Key::<Aes256Gcm>::from_slice(&key_bytes);

        let service = Self {
            config,
            proplet: Arc::new(Mutex::new(proplet)),
            pubsub,
            runtime,
            chunk_assembly: Arc::new(Mutex::new(HashMap::new())),
            running_tasks: Arc::new(Mutex::new(HashMap::new())),
            workload_key,
        };

        service.start_chunk_expiry_task();

        Ok(service)
    }

    fn decrypt_payload(&self, ciphertext: &[u8]) -> Result<Vec<u8>> {
        let cipher = Aes256Gcm::new(&self.workload_key);
        
        if ciphertext.len() < 12 + 16 {
            return Err(anyhow::anyhow!("Ciphertext too short"));
        }

        let nonce_slice = &ciphertext[0..12];
        let nonce = Nonce::from_slice(nonce_slice);
        
        let encrypted_data = &ciphertext[12..];

        cipher.decrypt(nonce, encrypted_data)
            .map_err(|e| anyhow::anyhow!("Decryption failed: {}", e))
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
        let req: StartRequest = msg.decode().map_err(|e| {
            error!("Failed to decode start request: {}", e);
            if let Ok(payload_str) = String::from_utf8(msg.payload.clone()) {
                error!("Payload was: {}", payload_str);
            }
            e
        })?;
        req.validate()?;

        info!("Received start command for task: {}", req.id);

        let wasm_binary = if !req.file.is_empty() {
            use base64::{engine::general_purpose::STANDARD, Engine};
            match STANDARD.decode(&req.file) {
                Ok(encrypted_bytes) => {
                    info!("Decoded encrypted payload, size: {} bytes", encrypted_bytes.len());

                    if !req.checksum.is_empty() {
                        let mut hasher = Sha256::new();
                        hasher.update(&encrypted_bytes);
                        let result = hasher.finalize();
                        let computed_checksum = hex::encode(result);
                        
                        if computed_checksum != req.checksum {
                            let err_msg = format!("Checksum mismatch. Expected: {}, Got: {}", req.checksum, computed_checksum);
                            error!("{}", err_msg);
                            self.publish_result(&req.id, Vec::new(), Some(err_msg)).await?;
                            return Err(anyhow::anyhow!("Checksum mismatch"));
                        }
                        debug!("Checksum verified for task {}", req.id);
                    }

                    match self.decrypt_payload(&encrypted_bytes) {
                        Ok(decrypted) => decrypted,
                        Err(e) => {
                            error!("Decryption failed for task {}: {}", req.id, e);
                            self.publish_result(&req.id, Vec::new(), Some(e.to_string())).await?;
                            return Err(e);
                        }
                    }
                }
                Err(e) => {
                    error!("Failed to decode base64 file for task {}: {}", req.id, e);
                    self.publish_result(&req.id, Vec::new(), Some(e.to_string())).await?;
                    return Err(e.into());
                }
            }
        } else if !req.image_url.is_empty() {

            info!("Requesting binary from registry: {}", req.image_url);
            self.request_binary_from_registry(&req.image_url).await?;

            match self.wait_for_binary(&req.image_url).await {
                Ok(binary) => binary,
                Err(e) => {
                    error!("Failed to get binary for task {}: {}", req.id, e);
                    self.publish_result(&req.id, Vec::new(), Some(e.to_string())).await?;
                    return Err(e);
                }
            }
        } else {
            let err = anyhow::anyhow!("No wasm binary or image URL provided");
            error!("Validation error for task {}: {}", req.id, err);
            self.publish_result(&req.id, Vec::new(), Some(err.to_string())).await?;
            return Err(err);
        };

        let runtime = self.runtime.clone();
        let pubsub = self.pubsub.clone();
        let running_tasks = self.running_tasks.clone();
        let domain_id = self.config.domain_id.clone();
        let channel_id = self.config.channel_id.clone();
        let qos = self.config.qos();
        let proplet_id = self.proplet.lock().await.id;
        let task_id = req.id.clone();
        let task_name = req.name.clone();
        let env = req.env.unwrap_or_default();

        {
            let mut tasks = self.running_tasks.lock().await;
            tasks.insert(req.id.clone(), TaskState::Running);
        }

        tokio::spawn(async move {
            let ctx = RuntimeContext { proplet_id };

            info!("Executing task {} in spawned task", task_id);

            let config = StartConfig {
                id: task_id.clone(),
                function_name: task_name.clone(),
                daemon: req.daemon,
                wasm_binary,
                cli_args: req.cli_args,
                env,
                args: req.inputs,
            };

            let result = runtime.start_app(ctx, config).await;

            let (result_data, error) = match result {
                Ok(data) => {
                    let result_str = String::from_utf8_lossy(&data).to_string();
                    info!(
                        "Task {} completed successfully. Result: {}",
                        task_id, result_str
                    );
                    (data, None)
                }
                Err(e) => {
                    error!("Task {} failed: {}", task_id, e);
                    (Vec::new(), Some(e.to_string()))
                }
            };

            let result_msg = ResultMessage {
                task_id: task_id.clone(),
                proplet_id,
                result: result_data,
                error,
            };

            let topic = build_topic(&domain_id, &channel_id, "control/proplet/results");

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
            .or_insert_with(|| ChunkAssemblyState::new(chunk.total_chunks, chunk.checksum.clone()));

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

        if !chunk.checksum.is_empty() && state.checksum.is_empty() {
             state.checksum = chunk.checksum;
        } else if !chunk.checksum.is_empty() && state.checksum != chunk.checksum {
             warn!("Chunk checksum mismatch for '{}'", chunk.app_name);
             return Err(anyhow::anyhow!("Chunk checksum mismatch"));
        }

        state.chunks.insert(chunk.chunk_idx, chunk.data);
        debug!(
            "Stored chunk {} for app '{}' ({}/{} chunks received)",
            chunk.chunk_idx,
            chunk.app_name,
            state.chunks.len(),
            state.total_chunks
        );

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
        let polling_interval = tokio::time::Duration::from_secs(1);

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
                let encrypted_binary = state.assemble();
                let checksum = state.checksum.clone();

                info!(
                    "Assembled binary for app '{}', encrypted size: {} bytes",
                    app_name,
                    encrypted_binary.len()
                );

                assembly.remove(app_name);

                if !checksum.is_empty() {
                    let mut hasher = Sha256::new();
                    hasher.update(&encrypted_binary);
                    let result = hasher.finalize();
                    let computed_checksum = hex::encode(result);
                    
                    if computed_checksum != checksum {
                        error!("Checksum mismatch for app {}. Expected: {}, Got: {}", app_name, checksum, computed_checksum);
                        return Err(anyhow::anyhow!("Checksum mismatch during reassembly"));
                    }
                    debug!("Checksum verified for assembled app {}", app_name);
                }

                let decrypted_binary = self.decrypt_payload(&encrypted_binary)
                    .context("Failed to decrypt assembled chunks")?;

                return Ok(Some(decrypted_binary));
            }
        }

        Ok(None)
    }

    async fn publish_result(
        &self,
        task_id: &str,
        result: Vec<u8>,
        error: Option<String>,
    ) -> Result<()> {
        let proplet_id = self.proplet.lock().await.id;

        let result_msg = ResultMessage {
            task_id: task_id.to_string(),
            proplet_id,
            result,
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
