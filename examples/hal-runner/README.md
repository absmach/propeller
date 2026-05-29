# hal-runner

Standalone WASI P2 (component model) host that serves the [ELASTIC TEE HAL](https://github.com/elasticproject-eu/wasmhal)
to a WASM **component** and invokes one of its exported functions. The P2
counterpart of proplet's embedded `hal_component` integration, extracted into a
small CLI so HAL components can be run outside proplet — no MQTT, manager, or
TEE hardware required.

Uses `wasmtime::component::bindgen!` to generate typed host bindings and bridges
them to the ELASTIC TEE HAL default providers, so it works on any Linux host and
returns stubs for TEE-only features (attestation, platform-info) when no TEE is
present.

## Build

```bash
cargo build --release
```

## Run a HAL component

```bash
# hal-test exercises platform/crypto/clock/random (default export run-hal-test)
(cd ../hal-test && cargo build --target wasm32-wasip2 --release)
./target/release/hal-runner ../hal-test/target/wasm32-wasip2/release/hal_test.wasm

# attestation-test (custom export)
./target/release/hal-runner \
  ../attestation-test/target/wasm32-wasip2/release/attestation_test.wasm \
  --function run-attestation
```

### Options

| Option                   | Description                                                 | Default        |
| ------------------------ | ----------------------------------------------------------- | -------------- |
| `-f, --function <name>`  | Exported function to call                                   | `run-hal-test` |
| `-e, --env <KEY=VALUE>`  | Pass an environment variable to the component (repeatable)  |                |

The called export must take no arguments and return a single `string`; the
runner prints it to stdout.

## How it works

- Loads a `wasm32-wasip2` **component** via [wasmtime](https://github.com/bytecodealliance/wasmtime) 44.
- Builds a `component::Linker` with WASI P2 (`wasmtime_wasi::p2`) and the
  `elastic:hal@0.1.0` interfaces (`platform`, `attestation`, `crypto`, `clock`,
  `random`) generated from `wit/hal.wit` via `bindgen!`.
- Bridges the generated host traits to `HalProvider::with_defaults()` plus the
  `Default*Provider` implementations.
- Instantiates the component, calls the requested export, prints its result.

## Provided HAL interfaces

| Interface                 | Backing                                                         |
| ------------------------- | --------------------------------------------------------------- |
| `elastic:hal/platform`    | platform-info + capability discovery (stub on non-TEE)          |
| `elastic:hal/attestation` | real evidence on AMD SEV / Intel TDX, stub otherwise            |
| `elastic:hal/crypto`      | default provider (hash, encrypt, decrypt, sign, verify, keygen) |
| `elastic:hal/clock`       | default provider (system/monotonic time, resolution, sleep)     |
| `elastic:hal/random`      | default provider (CSRNG)                                        |

`wit/hal.wit` is kept byte-compatible with `proplet/wit/hal/hal.wit`.
