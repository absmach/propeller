# Proplet

A Rust implementation of a Proplet worker component for Propeller orchestration system.

## Overview

Proplet is a lightweight worker service that executes WebAssembly workloads on edge devices, IoT nodes, or cloud instances. It communicates with a central Manager via MQTT and provides task lifecycle management.

## Features

- **Dual Runtime Support**:
  - **Wasmtime Runtime**: In-process WebAssembly execution using Wasmtime
  - **Host Runtime**: External WebAssembly runtime integration via subprocess execution
  - **TeeWasmRuntime (TEE)**: Encrypted WASM execution with TEE attestation and KBS integration

- **MQTT Communication**: Pub/sub messaging with central Manager for task orchestration

- **Liveliness Reporting**: Periodic heartbeats (every 10 seconds) to indicate worker status

- **Task Management**: Full lifecycle support (start, stop, remove tasks)

- **Chunk Assembly**: Handles large Wasm binary downloads split into chunks

- **Daemon Mode**: Supports both daemon and non-daemon task execution

- **Environment Variables & CLI Args**: Passes configuration to Wasm functions (via Host runtime)

## Architecture

The Rust implementation maintains feature parity with the Go version while leveraging Rust's performance and safety benefits:

- **Async/Await**: Built on Tokio for high-performance async I/O
- **Type Safety**: Strong typing throughout with comprehensive error handling
- **Memory Safety**: No garbage collection overhead, zero-cost abstractions
- **Modular Design**: Clean separation of concerns with runtime trait abstraction

## Building

```bash
# Build with all features
cargo build --release --features "tee,all-attesters"

# Build with specific TEE support
cargo build --release --features "tee,tdx-attester"

# Build without TEE support
cargo build --release
```

## Configuration

Configure via environment variables:

```bash
# Logging
export PROPLET_LOG_LEVEL=info

# Instance identification
export PROPLET_INSTANCE_ID=$(uuidgen)

# MQTT configuration
export PROPLET_MQTT_ADDRESS=tcp://localhost:1883
export PROPLET_MQTT_TIMEOUT=30
export PROPLET_MQTT_QOS=2

# SuperMQ credentials
export PROPLET_DOMAIN_ID=your-domain-id
export PROPLET_CHANNEL_ID=your-channel-id
export PROPLET_CLIENT_ID=your-client-id
export PROPLET_CLIENT_KEY=your-client-key

# Optional: External WebAssembly runtime
export PROPLET_EXTERNAL_WASM_RUNTIME=/usr/local/bin/wasmtime

# Optional: Kubernetes namespace
export PROPLET_MANAGER_K8S_NAMESPACE=default

# Liveliness interval (seconds)
export PROPLET_LIVELINESS_INTERVAL=10

# Optional: Enable monitoring
export PROPLET_ENABLE_MONITORING=true
```

## Running Encrypted WASM Workloads

### Port Migration Notice

The attestation-agent service now uses port **50010** (previously 50002) and coco_keyprovider uses port **50011**. Existing deployments expecting port 50002 should update their configurations to use the new ports.

### Prerequisites

1. **Attestation Agent** must be running as a gRPC keyprovider service:

```bash
# Start attestation-agent as keyprovider
attestation-agent \
  --aa-config /path/to/aa-config.toml \
  --attestation_sock /run/attestation-agent.sock
```

The attestation-agent will:

- Perform TEE attestation
- Fetch decryption keys from KBS
- Provide decryption service to OCI layer handler via gRPC

2. **KBS** must be running and configured with encryption keys

### TEE Configuration

Proplet automatically detects if it's running inside a Trusted Execution Environment (TEE) by checking for:

- Intel TDX: `/dev/tdx_guest`, `/sys/firmware/tdx_guest`, cpuinfo flags, kernel messages
- AMD SEV/SNP: `/dev/sev`, EFI variables, cpuinfo flags, kernel messages
- Intel SGX: `/dev/sgx_enclave`, `/dev/sgx/enclave`, cpuinfo flags

**Configuration:**

```bash
# KBS endpoint (required when TEE is detected)
export PROPLET_KBS_URI=http://10.0.2.2:8082

# Optional: Attestation agent config path
export PROPLET_AA_CONFIG_PATH=/path/to/aa-config.toml

# Optional: Layer store for OCI images (default: /tmp/proplet/layers)
export PROPLET_LAYER_STORE_PATH=/tmp/proplet/layers
```

When Proplet starts, it will automatically detect and log the TEE status:

```bash
# If TEE is detected:
INFO TEE detected automatically: TDX (method: device_file, details: "/dev/tdx_guest exists")

# If no TEE is detected:
INFO No TEE detected, running in standard mode
```

### Task Manifest for Encrypted Workloads

```json
{
  "name": "my-function",
  "image_url": "docker.io/user/wasm:encrypted",
  "encrypted": true,
  "kbs_resource_path": "default/key/my-app",
  "cli_args": ["--invoke", "function_name"],
  "inputs": [10, 20]
}
```

**Note**: For encrypted workloads, always use `image_url` and do not provide a `file` field. The `kbs_resource_path` field is required for encrypted workloads and should match the path where the encryption key is stored in KBS. If not provided, an empty string will be used which may cause runtime errors when KBS requires a non-empty path.

### Complete Setup

1. **Start KBS** (Key Broker Service):

```bash
cd /path/to/trustee
docker-compose up -d
```

2. **Upload encryption key to KBS**:

```bash
./target/release/kbs-client \
  --url http://localhost:8080 \
  config \
  --auth-private-key kbs/config/private.key \
  set-resource \
  --resource-file /path/to/encryption-key.pem \
  --path default/key/my-app
```

3. **Encrypt and push WASM image**:

```bash
# Generate encryption keys
openssl genrsa -out private_key.pem 2048
openssl rsa -in private_key.pem -pubout -out public_key.pem

# Encrypt WASM image
skopeo copy \
  --encryption-key type:jwe:method:pkcs1:pubkey:./public_key.pem \
  oci:localhost/wasm:latest \
  oci:localhost/wasm:encrypted

# Push to registry
skopeo copy oci:localhost/wasm:encrypted \
  docker://docker.io/user/wasm:encrypted
```

4. **Start attestation-agent as keyprovider**:

```bash
attestation-agent \
  --aa-config /path/to/aa-config.toml \
  --attestation_sock /run/attestation-agent.sock
```

5. **Build and run Proplet**:

```bash
cd /home/rodneyosodo/code/absmach/propeller/proplet
cargo build --release --features tee

PROPLET_KBS_URI=http://localhost:8080 \
./target/release/proplet
```

**Note**: TEE is auto-detected at runtime. The `PROPLET_TEE_ENABLED` environment variable is **not required** and has no effect. TEE detection is performed by checking for TEE-specific devices (e.g., `/dev/tdx_guest` for Intel TDX, `/dev/sev` for AMD SEV/SNP, `/dev/sgx_enclave` for Intel SGX).

## Running

```bash
# Using Wasmtime runtime (default)
./target/release/proplet

# Using external runtime
export PROPLET_EXTERNAL_WASM_RUNTIME=/path/to/wasmtime
./target/release/proplet
```

## MQTT Topics

Proplet uses the following MQTT topic structure:

```
m/{domainID}/c/{channelID}/control/proplet/alive
    └─ Liveliness heartbeat (every 10 seconds)

m/{domainID}/c/{channelID}/control/proplet/create
    └─ Discovery announcement on startup

m/{domainID}/c/{channelID}/control/manager/start
    └─ Task start commands from Manager

m/{domainID}/c/{channelID}/control/manager/stop
    └─ Task stop commands from Manager

m/{domainID}/c/{channelID}/registry/server
    └─ Incoming Wasm binary chunks from Registry

m/{domainID}/c/{channelID}/registry/proplet
    └─ Requests for Wasm binary chunks to Registry

m/{domainID}/c/{channelID}/control/proplet/results
    └─ Task execution results back to Manager
```

## Dependencies

Key Rust crates used:

- **tokio**: Async runtime
- **rumqttc**: MQTT client library
- **wasmtime**: WebAssembly runtime
- **serde**: Serialization/deserialization
- **anyhow**: Error handling
- **tracing**: Structured logging
- **uuid**: Unique identifiers

**TEE dependencies** (with `--features tee`):

- **attestation-agent**: TEE attestation and KBS integration
- **image-rs**: OCI image pulling with encryption support
- **kbs_protocol**: KBS client protocol
- **oci-client**: OCI registry client

## Differences from Go Implementation

1. **WASI Support**: The Wasmtime runtime currently uses a simplified setup without full WASI support. CLI args and environment variables are supported via Host runtime.

2. **Error Handling**: Uses Rust's `Result` type and `anyhow` for error propagation instead of Go's error handling.

3. **Concurrency**: Uses Tokio's async/await instead of Go's goroutines and channels.

4. **Memory Management**: No garbage collection; uses Rust's ownership system for memory safety.

5. **TEE Support**: Rust implementation includes encrypted workload support with TEE attestation, not available in Go version.

## Performance

Rust provides several performance benefits:

- Zero-cost abstractions
- No garbage collection pauses
- Efficient async I/O with Tokio
- Native performance for compute-intensive tasks

## Future Enhancements

- Full WASI support in Wasmtime runtime
- Metrics and observability improvements
- Additional runtime backends (e.g., Wasmer)
- Enhanced security features
- gRPC support as alternative to MQTT
- Improved resource management and scheduling

## License

Same as the main Propeller project.
