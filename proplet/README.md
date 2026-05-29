# Proplet

A Rust worker that executes WebAssembly workloads and communicates with a central Manager via MQTT.

For full documentation, see [propeller.absmach.eu/docs/proplet](https://propeller.absmach.eu/docs/proplet).

## Runtimes

- **Wasmtime** — in-process WebAssembly execution (default)
- **Host runtime** — delegates to an external binary via subprocess
- **TEE runtime** — decrypts and runs encrypted WASM inside a Trusted Execution Environment

## Build

```bash
# Standard build
cargo build --release
```

## Configure

| Variable                        | Description                                               | Default                |
| ------------------------------- | --------------------------------------------------------- | ---------------------- |
| `PROPLET_LOG_LEVEL`             | Log level (`debug`, `info`, `warn`, `error`)              | `info`                 |
| `PROPLET_INSTANCE_ID`           | Unique ID for this instance                               | Generated UUID         |
| `PROPLET_MQTT_ADDRESS`          | MQTT broker address                                       | `tcp://localhost:1883` |
| `PROPLET_MQTT_TIMEOUT`          | MQTT operation timeout (seconds)                          | `30`                   |
| `PROPLET_MQTT_QOS`              | MQTT Quality of Service level                             | `2`                    |
| `PROPLET_LIVELINESS_INTERVAL`   | Heartbeat interval in seconds                             | `10`                   |
| `PROPLET_DOMAIN_ID`             | Magistrala domain ID                                      |                        |
| `PROPLET_CHANNEL_ID`            | Magistrala channel ID                                     |                        |
| `PROPLET_CLIENT_ID`             | MQTT client ID                                            |                        |
| `PROPLET_CLIENT_KEY`            | MQTT client key                                           |                        |
| `PROPLET_EXTERNAL_WASM_RUNTIME` | Path to external Wasm runtime; uses Wasmtime if unset     | `""` (empty)           |
| `PROPLET_HAL_ENABLED`           | Expose the ELASTIC TEE HAL to workloads (see HAL section) | `true`                 |
| `PROPLET_KBS_URI`               | Key Broker Service URL (required for encrypted workloads) |                        |
| `PROPLET_AA_CONFIG_PATH`        | Path to the Attestation Agent config file                 |                        |
| `PROPLET_LAYER_STORE_PATH`      | OCI layer cache path                                      | `/tmp/proplet/layers`  |

## Run without TEE

### Embedded Wasmtime runtime (default)

```bash
export PROPLET_DOMAIN_ID="your_domain_id"
export PROPLET_CHANNEL_ID="your_channel_id"
export PROPLET_CLIENT_ID="your_client_id"
export PROPLET_CLIENT_KEY="your_client_key"
./target/release/proplet
```

### External host runtime

```bash
export PROPLET_DOMAIN_ID="your_domain_id"
export PROPLET_CHANNEL_ID="your_channel_id"
export PROPLET_CLIENT_ID="your_client_id"
export PROPLET_CLIENT_KEY="your_client_key"
export PROPLET_EXTERNAL_WASM_RUNTIME="/usr/bin/wasmtime"
./target/release/proplet
```

CLI arguments and inputs are passed through the task definition:

```json
{
  "name": "add",
  "cli_args": ["--invoke", "add"],
  "inputs": [10, 20]
}
```

## Hardware Abstraction Layer (HAL)

The embedded Wasmtime runtime can expose the [ELASTIC TEE HAL](https://github.com/elasticproject-eu/wasmhal)
(platform, attestation, crypto, clock, random, ...) to workloads. Enabled by
default; disable with `PROPLET_HAL_ENABLED=false`.

HAL is available to both module formats:

- **P1 core modules** (`wasm32-wasip1`) — raw C-ABI imports under the
  `elastic:tee-hal/*` namespaces, wired in `src/hal_linker.rs`. See the
  `hal-test` example and the standalone `hal-runner`.
- **P2 components** (`wasm32-wasip2`, component model) — typed WIT bindings
  generated from `wit/hal/hal.wit` (package `elastic:hal@0.1.0`) and wired in
  `src/hal_component.rs`. Guests `import` the HAL interfaces; the host provides
  them on the component linker. See the `hal-component-test` example.

P2 v1 covers the provider-backed interfaces (`platform`, `attestation`,
`crypto`, `clock`, `random`). The stub-only interfaces (`sockets`, `gpu`,
`resources`, `events`, `communication`, `storage`) and the async HTTP-proxy
path remain P1-only for now.

Both paths bridge to the same `elastic_tee_hal` providers, so they return real
values on TEE hardware (AMD SEV / Intel TDX) and safe defaults elsewhere.

## Run inside a TEE

Proplet auto-detects TEE hardware by checking device files at startup:

- **Intel TDX** — `/dev/tdx_guest`
- **AMD SEV/SNP** — `/dev/sev`
- **Intel SGX** — `/dev/sgx_enclave`

No flag is needed. When a TEE is found, proplet logs:

```bash
INFO TEE detected automatically: TDX (method: device_file, details: "/dev/tdx_guest exists")
```

When no TEE is found, it runs in standard mode:

```bash
INFO No TEE detected, running in standard mode
```

### Start proplet in TEE mode

```bash
export PROPLET_DOMAIN_ID="your_domain_id"
export PROPLET_CHANNEL_ID="your_channel_id"
export PROPLET_CLIENT_ID="your_client_id"
export PROPLET_CLIENT_KEY="your_client_key"
export PROPLET_MQTT_ADDRESS="your_mqtt_address"
export PROPLET_KBS_URI="http://10.0.2.2:8082"
export PROPLET_AA_CONFIG_PATH="/etc/default/proplet.toml"
./target/release/proplet
```

`PROPLET_AA_CONFIG_PATH` points to the Attestation Agent config:

```toml
[token_configs]
[token_configs.coco_kbs]
url = "http://10.0.2.2:8082"
```

### Encrypted task definition

```json
{
  "name": "add",
  "image_url": "docker.io/rodneydav/tee-wasm-addition:encrypted",
  "encrypted": true,
  "kbs_resource_path": "default/key/propeller-addition",
  "cli_args": ["--invoke", "add"],
  "inputs": [10, 20]
}
```

Do not include a `file` field for encrypted workloads.

For full TEE setup (KBS, image encryption, CVM provisioning), see the [Encrypted workloads guide](https://propeller.absmach.eu/docs/tee).
