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

| Variable                        | Description                                                 | Default                |
| ------------------------------- | ----------------------------------------------------------- | ---------------------- |
| `PROPLET_LOG_LEVEL`             | Log level (`debug`, `info`, `warn`, `error`)                | `info`                 |
| `PROPLET_INSTANCE_ID`           | Unique ID for this instance                                 | Generated UUID         |
| `PROPLET_MQTT_ADDRESS`          | MQTT broker address                                         | `tcp://localhost:1883` |
| `PROPLET_MQTT_TIMEOUT`          | MQTT operation timeout (seconds)                            | `30`                   |
| `PROPLET_MQTT_QOS`              | MQTT Quality of Service level                               | `2`                    |
| `PROPLET_LIVELINESS_INTERVAL`   | Heartbeat interval in seconds                               | `10`                   |
| `PROPLET_TENANT_ID`             | Propeller tenant ID                                         |                        |
| `PROPLET_CHANNEL_ID`            | Propeller channel ID                                        |                        |
| `PROPLET_ENTITY_ID`             | MQTT entity ID                                              |                        |
| `PROPLET_API_KEY`               | MQTT entity key                                             |                        |
| `PROPLET_EXTERNAL_WASM_RUNTIME` | Path to external Wasm runtime; uses Wasmtime if unset       | `""` (empty)           |
| `PROPLET_HAL_ENABLED`           | Expose the ELASTIC TEE HAL to workloads (see HAL section)   | `true`                 |
| `PROPLET_TEE_ENABLED`           | Force the TEE runtime on/off, overriding auto-detection     | (unset)                |
| `PROPLET_KBS_URI`               | Key Broker Service URL (required for encrypted workloads)   |                        |
| `PROPLET_AA_CONFIG_PATH`        | Path to the Attestation Agent config file (`.toml`/`.json`) |                        |
| `PROPLET_LAYER_STORE_PATH`      | OCI layer cache path                                        | `/tmp/proplet/layers`  |

## Run without TEE

### Embedded Wasmtime runtime (default)

```bash
export PROPLET_TENANT_ID="your_tenant_id"
export PROPLET_CHANNEL_ID="your_channel_id"
export PROPLET_ENTITY_ID="your_entity_id"
export PROPLET_API_KEY="your_api_key"
./target/release/proplet
```

### External host runtime

```bash
export PROPLET_TENANT_ID="your_tenant_id"
export PROPLET_CHANNEL_ID="your_channel_id"
export PROPLET_ENTITY_ID="your_entity_id"
export PROPLET_API_KEY="your_api_key"
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

The embedded Wasmtime runtime exposes the [ELASTIC TEE HAL](https://github.com/elasticproject-eu/wasmhal)
(platform, attestation, crypto, clock, random) to workloads. Enabled by
default; disable with `PROPLET_HAL_ENABLED=false`.

**HAL is available to P2 components only** (`wasm32-wasip2`, component model).
Typed WIT bindings are generated from `wit/hal/hal.wit`
(package `elastic:hal@0.1.0`) and wired in `src/hal_component.rs`: guests
`import` the HAL interfaces and the host provides them on the component linker.
See the `hal-test` and `attestation-test` examples (and the standalone
`hal-runner` for running HAL components outside proplet).

The HAL bridges to the `elastic_tee_hal` providers, returning real values on
TEE hardware (AMD SEV / Intel TDX) and safe defaults elsewhere. v1 covers the
provider-backed interfaces (`platform`, `attestation`, `crypto`, `clock`,
`random`); the stub-only interfaces (`sockets`, `gpu`, `resources`, `events`,
`communication`, `storage`) and the async HTTP-proxy path are not yet wired.

On Azure AMD CVMs the paravisor exposes no `/dev/sev*` or TSM configfs, so both
detection and attestation go through the guest vTPM: the platform provider
reports `AmdSev` and `attestation` returns the HCL report, a TPM quote bound to
the caller's report data, and the VCEK certificate from IMDS. This requires a
`elastic-tee-hal` revision with that support — older revs report no TEE and
return placeholder measurements. Reading the vTPM needs root or the `tss` group.

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
export PROPLET_TENANT_ID="your_tenant_id"
export PROPLET_CHANNEL_ID="your_channel_id"
export PROPLET_ENTITY_ID="your_entity_id"
export PROPLET_API_KEY="your_api_key"
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
