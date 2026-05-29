# hal-runner

Standalone wasmtime-based runner for WASM modules compiled against `elastic:tee-hal/*` imports. Uses the [ELASTIC TEE HAL](https://github.com/elasticproject-eu/wasmhal) default providers to serve all 11 HAL interfaces — no propeller or TEE hardware required.

> This runner targets **P1 core modules** (`wasm32-wasip1`) with raw C-ABI imports. For **P2 component-model** guests that consume HAL via typed WIT bindings, see the `hal-component-test` example, which runs directly through proplet's embedded runtime.

## Quick start

```bash
# Build the runner
cargo build --release

# Run a WASM module (e.g. hal-test, attestation-test)
./target/release/hal-runner path/to/module.wasm

# Specify a custom entry point
./target/release/hal-runner path/to/module.wasm --function run_export

# Pass environment variables (repeatable)
./target/release/hal-runner path/to/module.wasm -e FOO=bar -e DB_URL=postgres://localhost
```

For example:

```bash
# Build a WASM test
cd ../hal-test && cargo build --release
cd ../hal-runner
./target/release/hal-runner ../hal-test/target/wasm32-wasip1/release/hal-test.wasm
```

## CLI options

| Flag                    | Description                                   |
| ----------------------- | --------------------------------------------- |
| `-f`, `--function`      | Entry point function name (default: `_start`) |
| `-e`, `--env KEY=VALUE` | Pass an environment variable (can repeat)     |

## How it works

- Loads `wasm32-wasip1` core modules via [wasmtime](https://github.com/bytecodealliance/wasmtime)
- Adds WASI (`wasmtime_wasi::p1`) and all 11 `elastic:tee-hal/*` import modules to the linker
- Uses `HalProvider::with_defaults()` — works on any Linux host, returns stubs for TEE-only features (attestation, platform-info) when no TEE is present
- Entry-point fallback: `_start` → `main` → `run` → `run_export`
- Environment variables passed via `-e` are set through `WasiCtxBuilder::env()`

## Provided HAL interfaces

| Module                          | Status                                                          |
| ------------------------------- | --------------------------------------------------------------- |
| `elastic:tee-hal/platform`      | stub on non-TEE, real on AMD SEV / Intel TDX                    |
| `elastic:tee-hal/capabilities`  | default provider                                                |
| `elastic:tee-hal/crypto`        | default provider (hash, encrypt, decrypt, sign, verify, keygen) |
| `elastic:tee-hal/storage`       | in-memory container store                                       |
| `elastic:tee-hal/clock`         | default provider (system/monotonic time, sleep)                 |
| `elastic:tee-hal/random`        | default provider (CSRNG, hardware entropy detection)            |
| `elastic:tee-hal/sockets`       | stubs (returns -1)                                              |
| `elastic:tee-hal/gpu`           | stubs (returns -1)                                              |
| `elastic:tee-hal/resources`     | simple pass-through allocator                                   |
| `elastic:tee-hal/events`        | stub event system                                               |
| `elastic:tee-hal/communication` | stub inter-workload messaging                                   |
