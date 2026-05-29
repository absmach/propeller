# hal-component-test

A WASI P2 (component model) guest that consumes the [ELASTIC TEE HAL](https://github.com/elasticproject-eu/wasmhal)
through typed WIT bindings. The P2 counterpart of the P1 `hal-test` example.

It imports the `elastic:hal@0.1.0` interfaces and exports a single
`run-hal-test: func() -> string`. Proplet runs it via the
`start_app_component_export` path, registering the HAL host bindings on the
component linker when `PROPLET_HAL_ENABLED=true`.

## Build

Requires the `wasm32-wasip2` target (`rustup target add wasm32-wasip2`):

```bash
cargo build --target wasm32-wasip2 --release
# -> target/wasm32-wasip2/release/hal_component_test.wasm
```

The output is a true component (not a core module), so no `wasm-tools
component new` step is needed.

## What it does

`run-hal-test` calls the imported HAL and returns a summary string:

- `platform::get-platform-info`
- `crypto::hash` — `sha256("hello")`
- `random::get-secure-random` — 16 bytes
- `clock::get-system-time`

## Run via proplet

Send a task with `function_name = "run-hal-test"` and this component as the
`file`. With `PROPLET_HAL_ENABLED=true` (default), the `results` field of the
returned message contains the summary, e.g.:

```
platform: type=None version=0.0.0
sha256(hello)=2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824
random16=...
time: 1716998400s 0ns
```

`platform` shows real values on TEE hardware (AMD SEV / Intel TDX) and defaults
elsewhere.

## WIT

`wit/world.wit` defines the guest world; `wit/deps/hal.wit` is kept
byte-compatible with `proplet/wit/hal/hal.wit` so guest imports match the host
exports.
