use anyhow::{Context, Result};
use async_trait::async_trait;
use image_rs::layer_store::LayerStore;
use image_rs::meta_store::MetaStore;
use image_rs::pull::PullClient;
use oci_client::client::ClientConfig;
use oci_client::secrets::RegistryAuth;
use oci_client::Reference;
use oci_spec::image::ImageConfiguration;
use std::path::PathBuf;
use std::process::Command;
use std::sync::Arc;
use tokio::io::AsyncWriteExt;
use tokio::sync::RwLock;
use tracing::warn;

use crate::config::PropletConfig;
use crate::runtime::{Runtime, RuntimeContext, StartConfig};

#[cfg(feature = "tee")]
use attestation_agent::{AttestationAPIs, AttestationAgent};

pub struct TeeWasmRuntime {
    config: PropletConfig,
    #[cfg(feature = "tee")]
    attestation_agent: Option<AttestationAgent>,
}

impl TeeWasmRuntime {
    pub async fn new(config: &PropletConfig) -> Result<Self> {
        #[cfg(feature = "tee")]
        {
            let mut agent = AttestationAgent::new(config.aa_config_path.as_deref())
                .context("Failed to create attestation agent")?;
            agent.init().await?;

            if config.tee_enabled {
                Self::setup_keyprovider_config()
                    .context("Failed to setup keyprovider config when TEE is enabled")?;
            }

            return Ok(Self {
                config: config.clone(),
                attestation_agent: Some(agent),
            });
        }

        #[cfg(not(feature = "tee"))]
        Ok(Self {
            config: config.clone(),
            attestation_agent: None,
        })
    }

    #[cfg(feature = "tee")]
    fn setup_keyprovider_config() -> Result<()> {
        if let Ok(existing_path) = std::env::var("OCICRYPT_KEYPROVIDER_CONFIG") {
            if std::path::Path::new(&existing_path).exists() {
                return Ok(());
            }
        }

        let config_path = PathBuf::from("/tmp/proplet/ocicrypt_keyprovider.conf");
        let config_dir = config_path
            .parent()
            .expect("Config path should always have a parent directory");

        std::fs::create_dir_all(&config_dir)
            .context("Failed to create keyprovider config directory")?;

        let keyprovider_addr = "127.0.0.1:50011";

        let config_content = format!(
            r#"{{
  "key-providers": {{
    "attestation-agent": {{
      "grpc": "{}"
    }}
  }}
}}"#,
            keyprovider_addr
        );

        std::fs::write(&config_path, config_content)
            .context("Failed to write keyprovider config")?;

        std::env::set_var("OCICRYPT_KEYPROVIDER_CONFIG", config_path);

        Ok(())
    }

    #[cfg(feature = "tee")]
    async fn pull_and_decrypt_wasm(&self, oci_reference: &str) -> Result<PathBuf> {
        let image_ref = Reference::try_from(oci_reference.to_string())
            .context("Failed to parse image reference")?;

        let layer_store = LayerStore::new(PathBuf::from(&self.config.layer_store_path))
            .context("Failed to create layer store")?;

        let client_config = ClientConfig::default();

        let mut pull_client = PullClient::new(
            image_ref.clone(),
            layer_store,
            &RegistryAuth::Anonymous,
            self.config.pull_concurrent_limit,
            client_config,
        )?;

        let (manifest, _digest, config) = pull_client
            .pull_manifest()
            .await
            .context("Failed to pull manifest")?;

        let is_wasm_image = manifest.config.media_type.contains("wasm")
            || manifest
                .layers
                .iter()
                .any(|l| l.media_type.contains("wasm"));

        let is_encrypted = manifest
            .layers
            .iter()
            .any(|l| l.media_type.contains("encrypted"));

        if is_wasm_image && !is_encrypted {
            let wasm_layer = manifest
                .layers
                .first()
                .ok_or_else(|| anyhow::anyhow!("No layers found in WASM image"))?;

            let blob_stream = pull_client
                .client
                .pull_blob_stream(&image_ref, wasm_layer)
                .await
                .context("Failed to pull WASM blob")?;

            let wasm_filename = format!("{}.wasm", wasm_layer.digest.replace("sha256:", ""));
            let wasm_path = PathBuf::from(&self.config.layer_store_path).join(wasm_filename);

            let mut file = tokio::fs::File::create(&wasm_path)
                .await
                .context("Failed to create WASM file")?;

            use futures_util::StreamExt;
            let mut stream = blob_stream.stream;
            while let Some(chunk) = stream.next().await {
                let chunk = chunk.context("Failed to read blob chunk")?;
                file.write_all(&chunk)
                    .await
                    .context("Failed to write WASM chunk")?;
            }

            file.sync_all().await.context("Failed to sync WASM file")?;

            return Ok(wasm_path);
        }

        let image_config = ImageConfiguration::from_reader(config.as_bytes()).unwrap_or_else(|e| {
            warn!(
                "Failed to parse image config (may be minimal WASM config): {}",
                e
            );
            ImageConfiguration::default()
        });

        let diff_ids = image_config.rootfs().diff_ids();

        let diff_ids_vec: Vec<String> = if diff_ids.is_empty() && !manifest.layers.is_empty() {
            manifest
                .layers
                .iter()
                .map(|layer| {
                    if is_encrypted {
                        String::new()
                    } else {
                        layer.digest.clone()
                    }
                })
                .collect()
        } else {
            diff_ids.to_vec()
        };

        let decrypt_config: Option<String> = if is_encrypted {
            Some("provider:attestation-agent".to_string())
        } else {
            None
        };

        let layer_metas = pull_client
            .async_pull_layers(
                manifest.layers.clone(),
                &diff_ids_vec,
                &decrypt_config.as_deref(),
                Arc::new(RwLock::new(MetaStore::default())),
            )
            .await
            .context("Failed to pull and decrypt layers")?;

        let layer_store_path = layer_metas
            .first()
            .map(|m| PathBuf::from(&m.store_path))
            .ok_or_else(|| anyhow::anyhow!("No layers found in image"))?;

        let wasm_path = layer_store_path.join("module.wasm");

        if wasm_path.exists() && wasm_path.is_file() {
            return Ok(wasm_path);
        }

        if let Ok(entries) = std::fs::read_dir(&layer_store_path) {
            for entry in entries.flatten() {
                let path = entry.path();
                if path.is_file() {
                    if let Some(ext) = path.extension() {
                        if ext == "wasm" {
                            return Ok(path);
                        }
                    }
                }
            }
        }

        Err(anyhow::anyhow!(
            "No WASM file found in layer store: {:?}",
            layer_store_path
        ))
    }

    #[cfg(not(feature = "tee"))]
    async fn pull_and_decrypt_wasm(&self, _oci_reference: &str) -> Result<PathBuf> {
        Err(anyhow::anyhow!(
            "TEE support is not enabled. Please compile with 'tee' feature."
        ))
    }

    fn run_wasm(&self, wasm_path: &PathBuf, config: &StartConfig) -> Result<Vec<u8>> {
        let runtime_path = self
            .config
            .external_wasm_runtime
            .as_ref()
            .map(|s| s.as_str())
            .unwrap_or("wasmtime");

        let mut cmd = Command::new(runtime_path);

        cmd.arg("--dir").arg("/tmp");

        for arg in &config.cli_args {
            cmd.arg(arg);
        }

        cmd.arg(wasm_path);

        for arg in &config.args {
            cmd.arg(arg.to_string());
        }

        cmd.stdout(std::process::Stdio::piped());
        cmd.stderr(std::process::Stdio::piped());

        let output = cmd.output().context("Failed to execute WASM runtime")?;

        if !output.stderr.is_empty() {
            warn!("WASM stderr:\n{}", String::from_utf8_lossy(&output.stderr));
        }

        if !output.status.success() {
            return Err(anyhow::anyhow!(
                "WASM execution failed with exit code: {:?}",
                output.status.code()
            ));
        }

        Ok(output.stdout)
    }

    fn run_wasm_embedded(&self, wasm_binary: &[u8], start_config: &StartConfig) -> Result<Vec<u8>> {
        use wasmtime::*;

        let mut config = Config::new();
        config.wasm_component_model(false);
        config.async_support(false);

        let engine = Engine::new(&config)?;
        let linker = Linker::new(&engine);

        let module = Module::from_binary(&engine, wasm_binary)?;

        let mut store = Store::new(&engine, ());
        let instance = linker.instantiate(&mut store, &module)?;

        let func_name = if start_config.function_name.is_empty() {
            "_start"
        } else {
            &start_config.function_name
        };

        let func = instance
            .get_func(&mut store, func_name)
            .ok_or_else(|| anyhow::anyhow!("Function '{}' not found in WASM module", func_name))?;

        let results_types: Vec<_> = func.ty(&store).results().collect();
        let mut results: Vec<Val> = results_types
            .iter()
            .map(|ty| match ty {
                wasmtime::ValType::I32 => Val::I32(0),
                wasmtime::ValType::I64 => Val::I64(0),
                wasmtime::ValType::F32 => Val::F32(0),
                wasmtime::ValType::F64 => Val::F64(0),
                wasmtime::ValType::V128 => Val::V128(0u128.into()),
                wasmtime::ValType::Ref(_) => Val::null_any_ref(),
            })
            .collect();

        func.call(&mut store, &[], &mut results)?;

        let output = results
            .into_iter()
            .filter_map(|v| match v {
                Val::I32(i) => Some(i.to_le_bytes().to_vec()),
                Val::I64(i) => Some(i.to_le_bytes().to_vec()),
                _ => None,
            })
            .flatten()
            .collect::<Vec<_>>();

        Ok(output)
    }
}

#[async_trait]
impl Runtime for TeeWasmRuntime {
    async fn start_app(&self, _ctx: RuntimeContext, config: StartConfig) -> Result<Vec<u8>> {
        #[cfg(feature = "tee")]
        {
            if let Some(ref agent) = self.attestation_agent {
                let _evidence = agent
                    .get_evidence(b"wasm-runner")
                    .await
                    .context("Failed to get TEE evidence")?;
            }
        }

        if !config.wasm_binary.is_empty() {
            return Ok(self.run_wasm_embedded(&config.wasm_binary, &config)?);
        }

        if !config.cli_args.is_empty() {
            if let Some(oci_ref) = config.cli_args.first() {
                if oci_ref.contains("/") || oci_ref.contains(":") {
                    let wasm_path = self.pull_and_decrypt_wasm(oci_ref).await?;

                    let mut exec_config = config.clone();
                    exec_config.cli_args.remove(0);

                    return self.run_wasm(&wasm_path, &exec_config);
                }
            }
        }

        Err(anyhow::anyhow!("No WASM binary or OCI reference provided"))
    }

    async fn stop_app(&self, _id: String) -> Result<()> {
        Ok(())
    }

    async fn get_pid(&self, _id: &str) -> Result<Option<u32>> {
        Ok(None)
    }
}
