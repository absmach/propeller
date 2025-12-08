use anyhow::Result;
use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::time::SystemTime;
use uuid::Uuid;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Proplet {
    pub id: Uuid,
    pub name: String,
    pub task_count: usize,
    pub alive: bool,
    pub alive_history: Vec<SystemTime>,
}

impl Proplet {
    pub fn new(id: Uuid, name: String) -> Self {
        Self {
            id,
            name,
            task_count: 0,
            alive: false,
            alive_history: Vec::new(),
        }
    }

    pub fn set_alive(&mut self, alive: bool) {
        self.alive = alive;
        if alive {
            self.alive_history.push(SystemTime::now());
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct StartRequest {
    pub id: Uuid,
    #[serde(rename = "cliArgs")]
    pub cli_args: Vec<String>,
    #[serde(rename = "functionName")]
    pub function_name: String,
    #[serde(rename = "wasmFile")]
    pub wasm_file: Vec<u8>,
    #[serde(rename = "imageURL")]
    pub image_url: String,
    pub params: Vec<f64>,
    pub daemon: bool,
    pub env: HashMap<String, String>,
}

impl StartRequest {
    pub fn validate(&self) -> Result<()> {
        if self.function_name.is_empty() {
            return Err(anyhow::anyhow!("function name is required"));
        }
        if self.wasm_file.is_empty() && self.image_url.is_empty() {
            return Err(anyhow::anyhow!("either wasm_file or image_url must be provided"));
        }
        Ok(())
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct StopRequest {
    pub id: Uuid,
}

impl StopRequest {
    pub fn validate(&self) -> Result<()> {
        Ok(())
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub enum TaskState {
    Pending,
    Scheduled,
    Running,
    Completed,
    Failed,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Task {
    pub id: Uuid,
    pub name: String,
    pub state: TaskState,
    #[serde(rename = "imageURL")]
    pub image_url: String,
    pub file: Vec<u8>,
    #[serde(rename = "cliArgs")]
    pub cli_args: Vec<String>,
    pub inputs: Vec<f64>,
    pub env: HashMap<String, String>,
    pub daemon: bool,
    #[serde(rename = "propletID")]
    pub proplet_id: Uuid,
    pub results: Vec<u8>,
    pub error: String,
    pub created_at: SystemTime,
    pub updated_at: SystemTime,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ChunkMetadata {
    pub id: Uuid,
    pub total: usize,
    pub size: usize,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Chunk {
    pub id: Uuid,
    pub sequence: usize,
    pub data: Vec<u8>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct LivelinessMessage {
    pub proplet_id: Uuid,
    pub alive: bool,
    pub task_count: usize,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DiscoveryMessage {
    pub proplet_id: Uuid,
    pub name: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ResultMessage {
    pub task_id: Uuid,
    pub proplet_id: Uuid,
    pub result: Vec<u8>,
    pub error: Option<String>,
}
