# attestation-test

A WASI P2 (component model) guest that exercises TEE **attestation** through the
[ELASTIC TEE HAL](https://github.com/elasticproject-eu/wasmhal) typed WIT
bindings.

It imports `elastic:hal/{platform,attestation,random}@0.1.0` and exports
`run-attestation: func() -> string`. Proplet runs it via the
`start_app_component_export` path (function_name = `run-attestation`) with
`PROPLET_HAL_ENABLED=true`.

## Build

Requires the `wasm32-wasip2` target (`rustup target add wasm32-wasip2`):

```bash
cargo build --target wasm32-wasip2 --release
# -> target/wasm32-wasip2/release/attestation_test.wasm
```

The output is a true component (not a core module).

## What it does

`run-attestation` returns a summary string:

1. `platform::get-platform-info` — platform type/version.
2. `random::get-secure-random(32)` — a fresh nonce used as attestation
   report-data (falls back to a fixed value if the host RNG is unavailable).
3. `attestation::attestation(report_data)` — requests an attestation report.

On TEE hardware (AMD SEV / Intel TDX) the report is real evidence; elsewhere the
host returns a stub.

## WIT

`wit/world.wit` defines the guest world; `wit/deps/hal.wit` is kept
byte-compatible with `proplet/wit/hal/hal.wit` so guest imports match the host
exports.
