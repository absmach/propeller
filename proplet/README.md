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
| `PROPLET_TENANT_ID`             | Propeller tenant ID                                       |                        |
| `PROPLET_CHANNEL_ID`            | Propeller channel ID                                      |                        |
| `PROPLET_ENTITY_ID`             | MQTT entity ID                                            |                        |
| `PROPLET_API_KEY`               | MQTT entity key                                           |                        |
| `PROPLET_EXTERNAL_WASM_RUNTIME` | Path to external Wasm runtime; uses Wasmtime if unset     | `""` (empty)           |
| `PROPLET_HAL_ENABLED`           | Expose the ELASTIC TEE HAL to workloads (see HAL section) | `true`                 |
| `PROPLET_KBS_URI`               | Key Broker Service URL (required for encrypted workloads) |                        |
| `PROPLET_AA_CONFIG_PATH`        | Path to the Attestation Agent config file                 |                        |
| `PROPLET_LAYER_STORE_PATH`      | OCI layer cache path                                      | `/tmp/proplet/layers`  |

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

P1 core modules (`wasm32-wasip1`) still execute, but receive WASI only — no
HAL.

The HAL bridges to the `elastic_tee_hal` providers, returning real values on
TEE hardware (AMD SEV / Intel TDX) and safe defaults elsewhere. v1 covers the
provider-backed interfaces (`platform`, `attestation`, `crypto`, `clock`,
`random`); the stub-only interfaces (`sockets`, `gpu`, `resources`, `events`,
`communication`, `storage`) and the async HTTP-proxy path are not yet wired.

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

## WASI security policy

A task can carry a per-task WASI sandbox policy through `extra_config.wasi_security`. The
value is a **TOML document sent as a JSON string**; the proplet parses it when the task
starts and uses it to build the task's `WasiCtx`.

```toml
# Example WASI security policy.

# WASI argv for the guest. Distinct from the task's `inputs` (function-call arguments) and `cli_args`.
arguments = ["--verbose"]

# Extra environment variables for the guest. Applied after the task's own env, so these have priority on conflict.
[env]
LOG_LEVEL = "debug"

[storage]
# Entries are `host::guest`; a single path uses the same value for both.
# When a policy is present these replace the proplet-global preopened_dirs, so the task can only reach what is listed here.
readonly = ["/srv/models::/models"]

# Mount a host directory into the guest. The guest can read and write to this.
mount = ["/var/lib/task::/data"]

[network]
# akin to: guest is allowed to use the getaddrinfo() in POSIX.
allow_ip_name_lookup = false

# `[tcp://|udp://]<ip>:<port>`.
# No scheme means both protocols, an unspecified IP (0.0.0.0) matches any host, and port 0 matches any port.
bind = ["tcp://0.0.0.0:8080"]
connect = ["tcp://10.0.0.5:5432"]
```

Via the CLI:

```bash
propeller-cli tasks create "my-task" --wasi-security policy.toml
```

Semantics:

- **Storage is exclusive.** When a task has a policy, the proplet-global `preopened_dirs`
  are ignored for that task and only the policy's `readonly`/`mount` entries are preopened.
  A policy entry that cannot be preopened fails the task rather than silently granting less.
- **Network is deny-by-default.** Wasmtime rejects every socket address unless a rule
  permits it, so without a policy a task has no `wasi:sockets` access at all, and the rules
  are grants rather than restrictions.
- **Core modules get no network.** WASI preview1 has no sockets, so `network` rules on a
  core (non-component) module are ignored with a warning. `env`, `arguments` and `storage`
  still apply.
- **`wasi:http` is not covered.** Outbound `wasi:http` requests do not go through the
  WASI socket address check, so a policy without a `network` section still permits HTTP
  egress.

An invalid policy is rejected as soon as possible; the proplet reports the
parse error back as a failed task result.
