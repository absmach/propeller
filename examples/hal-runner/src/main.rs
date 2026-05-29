//! Standalone WASI P2 (component-model) host that serves the ELASTIC TEE HAL to
//! a WASM component and invokes one of its exported functions.
//!
//! The P2 counterpart of proplet's `hal_component` integration, extracted into a
//! small CLI so HAL components (e.g. `hal-test`, `attestation-test`) can be run
//! outside proplet. Uses `wasmtime::component::bindgen!` to generate typed host
//! bindings and bridges them to the `elastic_tee_hal` default providers — works
//! on any Linux host, returning stubs for TEE-only features when no TEE is
//! present.

use anyhow::{Context, Result};
use elastic_tee_hal::interfaces::{
    CapabilitiesInterface, ClockInterface, CryptoInterface, HalProvider, RandomInterface,
};
use elastic_tee_hal::providers::{
    DefaultCapabilitiesProvider, DefaultClockProvider, DefaultCryptoProvider, DefaultRandomProvider,
};
use std::path::PathBuf;
use wasmtime::component::{Component, Linker, ResourceTable, Val};
use wasmtime::{Config, Engine, Store};
use wasmtime_wasi::{WasiCtx, WasiCtxBuilder, WasiCtxView, WasiView};

wasmtime::component::bindgen!({
    world: "hal-imports",
    path: "wit",
});

use elastic::hal::{attestation, clock, crypto, platform, random};

struct HostState {
    wasi: WasiCtx,
    table: ResourceTable,
    provider: HalProvider,
}

impl WasiView for HostState {
    fn ctx(&mut self) -> WasiCtxView<'_> {
        WasiCtxView {
            ctx: &mut self.wasi,
            table: &mut self.table,
        }
    }
}

fn hash_algo_str(algo: crypto::HashAlgorithm) -> &'static str {
    match algo {
        crypto::HashAlgorithm::Sha256 => "SHA-256",
        crypto::HashAlgorithm::Sha512 => "SHA-512",
        crypto::HashAlgorithm::Blake3 => "BLAKE3",
    }
}

fn cipher_algo_str(algo: crypto::CipherAlgorithm) -> &'static str {
    match algo {
        crypto::CipherAlgorithm::Aes256Gcm => "AES-256-GCM",
        crypto::CipherAlgorithm::Chacha20Poly1305 => "ChaCha20-Poly1305",
    }
}

impl platform::Host for HostState {
    fn get_platform_info(&mut self) -> platform::PlatformInfo {
        match self
            .provider
            .platform
            .as_ref()
            .and_then(|p| p.platform_info().ok())
        {
            Some((platform_type, version, _attest)) => platform::PlatformInfo {
                platform_type,
                version,
            },
            None => platform::PlatformInfo {
                platform_type: "None".to_string(),
                version: "0.0.0".to_string(),
            },
        }
    }

    fn list_capabilities(&mut self) -> Vec<platform::CapabilityInfo> {
        match DefaultCapabilitiesProvider::default().list_capabilities() {
            Ok(list) => list
                .into_iter()
                .map(
                    |(feature_name, supported, version)| platform::CapabilityInfo {
                        feature_name,
                        supported,
                        version,
                    },
                )
                .collect(),
            Err(_) => Vec::new(),
        }
    }

    fn has_capability(&mut self, feature_name: String) -> bool {
        DefaultCapabilitiesProvider::default()
            .has_capability(&feature_name)
            .unwrap_or(false)
    }
}

impl attestation::Host for HostState {
    fn attestation(&mut self, report_data: Vec<u8>) -> Result<Vec<u8>, String> {
        match self.provider.platform.as_ref() {
            Some(p) => p.attestation(&report_data),
            None => Ok(b"{}".to_vec()),
        }
    }
}

impl crypto::Host for HostState {
    fn hash(&mut self, data: Vec<u8>, algorithm: crypto::HashAlgorithm) -> Result<Vec<u8>, String> {
        DefaultCryptoProvider::default().hash(&data, hash_algo_str(algorithm))
    }

    fn encrypt(
        &mut self,
        data: Vec<u8>,
        key: Vec<u8>,
        algorithm: crypto::CipherAlgorithm,
    ) -> Result<Vec<u8>, String> {
        DefaultCryptoProvider::default().encrypt(&data, &key, cipher_algo_str(algorithm))
    }

    fn decrypt(
        &mut self,
        data: Vec<u8>,
        key: Vec<u8>,
        algorithm: crypto::CipherAlgorithm,
    ) -> Result<Vec<u8>, String> {
        DefaultCryptoProvider::default().decrypt(&data, &key, cipher_algo_str(algorithm))
    }

    fn generate_keypair(&mut self) -> Result<crypto::KeyPair, String> {
        DefaultCryptoProvider::default()
            .generate_keypair()
            .map(|(public_key, private_key)| crypto::KeyPair {
                public_key,
                private_key,
            })
    }

    fn sign(&mut self, data: Vec<u8>, private_key: Vec<u8>) -> Result<Vec<u8>, String> {
        DefaultCryptoProvider::default().sign(&data, &private_key)
    }

    fn verify(
        &mut self,
        data: Vec<u8>,
        signature: Vec<u8>,
        public_key: Vec<u8>,
    ) -> Result<bool, String> {
        DefaultCryptoProvider::default().verify(&data, &signature, &public_key)
    }
}

impl clock::Host for HostState {
    fn get_system_time(&mut self) -> Result<clock::SystemTime, String> {
        DefaultClockProvider::default()
            .system_time()
            .map(|(seconds, nanoseconds)| clock::SystemTime {
                seconds,
                nanoseconds,
            })
    }

    fn get_monotonic_time(&mut self) -> Result<clock::MonotonicTime, String> {
        DefaultClockProvider::default().monotonic_time().map(
            |(elapsed_seconds, elapsed_nanoseconds)| clock::MonotonicTime {
                elapsed_seconds,
                elapsed_nanoseconds,
            },
        )
    }

    fn resolution(&mut self) -> Result<u64, String> {
        DefaultClockProvider::default().resolution()
    }

    fn sleep(&mut self, duration_ns: u64) -> Result<(), String> {
        DefaultClockProvider::default().sleep(duration_ns)
    }
}

impl random::Host for HostState {
    fn get_random_bytes(&mut self, length: u32) -> Result<Vec<u8>, String> {
        DefaultRandomProvider::default().get_random_bytes(length)
    }

    fn get_secure_random(&mut self, length: u32) -> Result<Vec<u8>, String> {
        DefaultRandomProvider::default().get_secure_random(length)
    }
}

struct Cli {
    wasm_file: PathBuf,
    function: String,
    envs: Vec<(String, String)>,
}

fn parse_cli() -> Result<Cli> {
    let args: Vec<String> = std::env::args().collect();
    if args.len() < 2 {
        eprintln!("Usage: {} <component.wasm> [options]", args[0]);
        eprintln!();
        eprintln!("Runs a WASI P2 component that imports the elastic:hal interfaces,");
        eprintln!("serving them with the ELASTIC TEE HAL default providers.");
        eprintln!();
        eprintln!("Options:");
        eprintln!("  -f, --function <name>   Exported function to call (default: run-hal-test)");
        eprintln!(
            "  -e, --env <KEY=VALUE>   Pass environment variable to the component (repeatable)"
        );
        eprintln!();
        eprintln!("Examples:");
        eprintln!(
            "  {} ../hal-test/target/wasm32-wasip2/release/hal_test.wasm",
            args[0]
        );
        eprintln!(
            "  {} ../attestation-test/target/wasm32-wasip2/release/attestation_test.wasm -f run-attestation",
            args[0]
        );
        std::process::exit(1);
    }

    let wasm_file = PathBuf::from(&args[1]);
    let mut function = "run-hal-test".to_string();
    let mut envs: Vec<(String, String)> = Vec::new();

    let mut i = 2;
    while i < args.len() {
        match args[i].as_str() {
            "--function" | "-f" => {
                i += 1;
                function = args
                    .get(i)
                    .cloned()
                    .context("--function requires a name argument")?;
            }
            "--env" | "-e" => {
                i += 1;
                let kv = args
                    .get(i)
                    .cloned()
                    .context("--env requires a KEY=VALUE argument")?;
                let (key, value) = kv
                    .split_once('=')
                    .with_context(|| format!("--env '{kv}' is not in KEY=VALUE format"))?;
                envs.push((key.to_string(), value.to_string()));
            }
            _ => return Err(anyhow::anyhow!("Unknown argument: {}", args[i])),
        }
        i += 1;
    }

    Ok(Cli {
        wasm_file,
        function,
        envs,
    })
}

fn main() -> Result<()> {
    tracing_subscriber::fmt()
        .with_env_filter(
            tracing_subscriber::EnvFilter::try_from_default_env().unwrap_or_else(|_| "info".into()),
        )
        .init();

    let cli = parse_cli()?;

    let wasm_bytes = std::fs::read(&cli.wasm_file)
        .with_context(|| format!("Failed to read component: {}", cli.wasm_file.display()))?;

    let mut config = Config::new();
    config.wasm_component_model(true);
    let engine = Engine::new(&config)
        .map_err(|e| anyhow::anyhow!("Failed to create wasmtime Engine: {e}"))?;

    let component = Component::from_binary(&engine, &wasm_bytes)
        .map_err(|e| anyhow::anyhow!("Failed to compile WASM component: {e}"))?;

    let mut linker: Linker<HostState> = Linker::new(&engine);
    wasmtime_wasi::p2::add_to_linker_sync(&mut linker)
        .map_err(|e| anyhow::anyhow!("Failed to add WASI P2 to linker: {e}"))?;
    HalImports::add_to_linker::<_, wasmtime::component::HasSelf<_>>(&mut linker, |s| s)
        .map_err(|e| anyhow::anyhow!("Failed to add ELASTIC TEE HAL to linker: {e}"))?;

    let mut wasi_builder = WasiCtxBuilder::new();
    wasi_builder.inherit_stdio();
    for (key, value) in &cli.envs {
        wasi_builder.env(key, value);
    }
    let state = HostState {
        wasi: wasi_builder.build(),
        table: ResourceTable::new(),
        provider: HalProvider::with_defaults(),
    };
    let mut store = Store::new(&engine, state);

    let instance = linker
        .instantiate(&mut store, &component)
        .map_err(|e| anyhow::anyhow!("Failed to instantiate component: {e}"))?;

    let func = instance
        .get_func(&mut store, &cli.function)
        .with_context(|| format!("Export '{}' not found in component", cli.function))?;

    // The HAL example exports each return a single string summary.
    let mut results = vec![Val::Bool(false)];
    func.call(&mut store, &[], &mut results)
        .map_err(|e| anyhow::anyhow!("Failed to call export '{}': {e}", cli.function))?;

    if let Some(Val::String(s)) = results.first() {
        print!("{s}");
    } else if let Some(v) = results.first() {
        println!("{v:?}");
    }

    Ok(())
}
