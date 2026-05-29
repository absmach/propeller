# hal-test

A WASI P2 (component model) guest that smoke-tests the [ELASTIC TEE HAL](https://github.com/elasticproject-eu/wasmhal)
through typed WIT bindings.

It imports `elastic:hal/{platform,crypto,clock,random}@0.1.0` and exports
`run-hal-test: func() -> string`. Proplet runs it via the
`start_app_component_export` path (function_name = `run-hal-test`) with
`PROPLET_HAL_ENABLED=true`; the standalone `hal-runner` can run it too.

## Build

Requires the `wasm32-wasip2` target (`rustup target add wasm32-wasip2`):

```bash
cargo build --target wasm32-wasip2 --release
# -> target/wasm32-wasip2/release/hal_test.wasm
```

The output is a true component (not a core module).

## What it does

`run-hal-test` exercises each interface and returns a summary string:

- `platform::get-platform-info` + `list-capabilities`
- `random::get-random-bytes(32)`
- `clock::get-system-time`
- `crypto::hash` — `sha256("hello")` (deterministic; asserted by proplet's e2e test)
- `crypto::generate-keypair`

Values are real on TEE hardware (AMD SEV / Intel TDX) and safe defaults
elsewhere.

## Run via hal-runner

```bash
(cd ../hal-runner && cargo build --release)
../hal-runner/target/release/hal-runner target/wasm32-wasip2/release/hal_test.wasm
```

## WIT

`wit/world.wit` defines the guest world; `wit/deps/hal.wit` is kept
byte-compatible with `proplet/wit/hal/hal.wit` so guest imports match the host
exports.
