use anyhow::{Context, Result};
use elastic_tee_hal::interfaces::HalProvider;
use std::sync::Arc;
use wasmtime::*;
use wasmtime_wasi::p1::WasiP1Ctx;

fn main() -> Result<()> {
    let wasm_path = std::env::args()
        .nth(1)
        .ok_or_else(|| anyhow::anyhow!("usage: wasm-hal-runner <file.wasm>"))?;

    let wasm_bytes =
        std::fs::read(&wasm_path).with_context(|| format!("failed to read {wasm_path}"))?;

    let mut cfg = Config::new();
    cfg.wasm_reference_types(true);
    cfg.wasm_bulk_memory(true);
    cfg.wasm_simd(true);

    let engine = Engine::new(&cfg)?;
    let module = Module::from_binary(&engine, &wasm_bytes)
        .map_err(|e| anyhow::anyhow!("failed to compile WASM module: {e}"))?;

    let mut wasi_builder = wasmtime_wasi::WasiCtxBuilder::new();
    wasi_builder.inherit_stdio();
    wasi_builder.arg(&wasm_path);
    let wasi: WasiP1Ctx = wasi_builder.build_p1();

    let mut store = Store::new(&engine, wasi);

    let mut linker: Linker<WasiP1Ctx> = Linker::new(&engine);
    wasmtime_wasi::p1::add_to_linker_sync(&mut linker, |ctx| ctx)
        .map_err(|e| anyhow::anyhow!("failed to add WASI to linker: {e}"))?;

    let provider = Arc::new(HalProvider::with_defaults());
    proplet::hal_linker::add_to_linker(&mut linker, provider)
        .context("failed to add HAL to linker")?;

    let instance = linker
        .instantiate(&mut store, &module)
        .map_err(|e| anyhow::anyhow!("failed to instantiate module: {e}"))?;

    let func = instance
        .get_typed_func::<(), ()>(&mut store, "_start")
        .map_err(|e| anyhow::anyhow!("module has no _start export: {e}"))?;

    func.call(&mut store, ())
        .map_err(|e| anyhow::anyhow!("WASM execution failed: {e}"))?;

    Ok(())
}
