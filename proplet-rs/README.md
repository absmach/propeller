# Proplet-rs

A Rust implementation of the Proplet worker component for the Propeller orchestration system.

## Overview

Proplet-rs is a lightweight worker service that executes WebAssembly workloads on edge devices, IoT nodes, or cloud instances. It communicates with a central Manager via MQTT and provides task lifecycle management.

## Features

- **Dual Runtime Support**:
  - **Wasmtime Runtime**: In-process WebAssembly execution using Wasmtime
  - **Host Runtime**: External WebAssembly runtime integration via subprocess execution

- **MQTT Communication**: Pub/sub messaging with the central Manager for task orchestration
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
export PROPLET_EXTERNAL_WASM_RUNTIME=/path/to/external/runtime

# Optional: Kubernetes namespace
export PROPLET_MANAGER_K8S_NAMESPACE=default

# Liveliness interval (seconds)
export PROPLET_LIVELINESS_INTERVAL=10
```

## Running

```bash
# Using Wasmtime runtime (default)
./target/release/proplet-rs

# Using external runtime
export PROPLET_EXTERNAL_WASM_RUNTIME=/path/to/wasmtime
./target/release/proplet-rs
```

## MQTT Topics

Proplet-rs uses the following MQTT topic structure:

```
m/{domainID}/c/{channelID}/messages/control/proplet/alive
    └─ Liveliness heartbeat (every 10 seconds)

m/{domainID}/c/{channelID}/messages/control/proplet/create
    └─ Discovery announcement on startup

m/{domainID}/c/{channelID}/messages/control/manager/start
    └─ Task start commands from Manager

m/{domainID}/c/{channelID}/messages/control/manager/stop
    └─ Task stop commands from Manager

m/{domainID}/c/{channelID}/messages/registry/server
    └─ Incoming Wasm binary chunks from Registry

m/{domainID}/c/{channelID}/messages/registry/proplet
    └─ Requests for Wasm binary chunks to Registry

m/{domainID}/c/{channelID}/messages/control/proplet/results
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

## Differences from Go Implementation

1. **WASI Support**: The Wasmtime runtime currently uses a simplified setup without full WASI support. CLI args and environment variables are supported via the Host runtime.

2. **Error Handling**: Uses Rust's `Result` type and `anyhow` for error propagation instead of Go's error handling.

3. **Concurrency**: Uses Tokio's async/await instead of Go's goroutines and channels.

4. **Memory Management**: No garbage collection; uses Rust's ownership system for memory safety.

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

## License

Same as the main Propeller project.
