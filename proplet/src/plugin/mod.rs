pub mod registry;

use anyhow::Result;
use std::sync::Mutex;
use tracing::warn;
use wasmtime::component::{Component, Linker};
use wasmtime::{Engine, Store};
use wasmtime::component::ResourceTable;
use wasmtime_wasi::{WasiCtx, WasiCtxBuilder, WasiCtxView, WasiView};

wasmtime::component::bindgen!({
    path: "wit/proplet-plugin.wit",
    world: "proplet-plugin",
});

pub use exports::propeller::proplet_plugin::lifecycle::{
    AuthorizeResponse, EnrichResponse, TaskInfo, TaskResult,
};

struct PluginState {
    wasi: WasiCtx,
    table: ResourceTable,
}

impl WasiView for PluginState {
    fn ctx(&mut self) -> WasiCtxView<'_> {
        WasiCtxView {
            ctx: &mut self.wasi,
            table: &mut self.table,
        }
    }
}

struct Inner {
    instance: PropletPlugin,
    store: Store<PluginState>,
}

pub struct WasmPlugin {
    inner: Mutex<Inner>,
}

impl WasmPlugin {
    pub fn load(engine: &Engine, wasm_bytes: &[u8]) -> Result<Self> {
        let mut linker: Linker<PluginState> = Linker::new(engine);
        wasmtime_wasi::p2::add_to_linker_sync(&mut linker)?;

        let wasi = WasiCtxBuilder::new().build();
        let table = ResourceTable::new();
        let mut store = Store::new(engine, PluginState { wasi, table });

        let component = Component::from_binary(engine, wasm_bytes)?;
        let instance = PropletPlugin::instantiate(&mut store, &component, &linker)?;

        Ok(Self {
            inner: Mutex::new(Inner { instance, store }),
        })
    }

    pub fn authorize(&self, task: TaskInfo) -> Result<AuthorizeResponse> {
        let mut g = self.inner.lock().unwrap();
        let Inner { instance, store } = &mut *g;
        instance
            .propeller_proplet_plugin_lifecycle()
            .call_authorize(store, &task)
            .map_err(Into::into)
    }

    pub fn enrich(&self, task: TaskInfo) -> Result<EnrichResponse> {
        let mut g = self.inner.lock().unwrap();
        let Inner { instance, store } = &mut *g;
        instance
            .propeller_proplet_plugin_lifecycle()
            .call_enrich(store, &task)
            .map_err(Into::into)
    }

    pub fn on_task_start(&self, task: TaskInfo) {
        let mut g = self.inner.lock().unwrap();
        let Inner { instance, store } = &mut *g;
        if let Err(e) = instance
            .propeller_proplet_plugin_lifecycle()
            .call_on_task_start(store, &task)
        {
            warn!("Plugin on_task_start error: {}", e);
        }
    }

    pub fn on_task_complete(&self, result: TaskResult) {
        let mut g = self.inner.lock().unwrap();
        let Inner { instance, store } = &mut *g;
        if let Err(e) = instance
            .propeller_proplet_plugin_lifecycle()
            .call_on_task_complete(store, &result)
        {
            warn!("Plugin on_task_complete error: {}", e);
        }
    }
}
