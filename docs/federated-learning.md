# Federated Learning in Propeller

> **⚠️ ARCHITECTURAL CHANGE**: FL is **NOT a core Propeller feature**. This document describes the **sample FML application** that demonstrates how to build federated learning on top of Propeller's generic orchestration capabilities.

## Overview

Propeller is a **generic Wasm orchestrator** (orchestration + transport + execution). Federated Learning (FL) is implemented as a **sample FML application** with an external coordinator, demonstrating how to build FL workflows without coupling FL logic into the core Manager.

## Architecture

```
┌─────────────────┐
│ FML Coordinator │  ← External service (owns FL rounds, aggregation, model versioning)
└────────┬────────┘
         │ MQTT
         │
┌────────▼────────┐
│     Manager     │  ← Generic task launcher (workload-agnostic)
└────────┬────────┘
         │ MQTT
         │
   ┌─────┴─────┬─────────┬─────────┐
   │          │         │         │
┌──▼──┐  ┌───▼──┐  ┌───▼──┐  ┌───▼──┐
│ P1  │  │  P2  │  │  P3  │  │ ... │  ← Edge Proplets (execute Wasm FL client)
└─────┘  └──────┘  └──────┘  └─────┘
```

**Key Components:**

- **Manager**: Generic task launcher + message forwarder (no FL logic)
- **FML Coordinator** (sample app): Owns rounds, aggregation, model versioning
- **Model Server** (sample app): Lightweight MQTT-based model distribution
- **Proplets**: Execute Wasm FL client workloads
  - **Rust Proplet** (Wasmtime): Full FML support
  - **Embedded Proplet** (C/WAMR/Zephyr): Full FML support for microcontrollers
- **MQTT**: Communication channel

## Sample FML Application

See `examples/fl-demo/` for the complete sample implementation.

### Manager Responsibilities (Generic)

Manager is **workload-agnostic** and only:

1. **Round Start Listener**: Subscribes to `fl/rounds/start`, launches tasks for participants
2. **Update Forwarder**: Subscribes to `fl/rounds/+/updates/+`, forwards verbatim to `fml/updates`

Manager does **NOT**:
- Track FL rounds
- Inject FL-specific env vars
- Validate FL envelopes
- Aggregate updates
- Advance rounds

### FML Coordinator Responsibilities

The external coordinator (sample app) handles **all FL semantics**:

1. Subscribes to `fl/rounds/start` to initialize round state
2. Subscribes to `fml/updates` (forwarded by Manager)
3. Tracks round progress in memory
4. Aggregates updates using FedAvg (weighted by `num_samples`)
5. Writes aggregated models to file system
6. Publishes round completion to `fl/rounds/{round_id}/complete`

## Workflow

### 1. Round Start

External trigger (or coordinator) publishes to `fl/rounds/start`:

```json
{
  "round_id": "r-0001",
  "model_uri": "fl/models/global_model_v0",
  "task_wasm_image": "oci://example/fl-client-wasm:latest",
  "participants": ["proplet-1", "proplet-2", "proplet-3"],
  "hyperparams": {"epochs": 1, "lr": 0.01, "batch_size": 16},
  "k_of_n": 3,
  "timeout_s": 30
}
```

### 2. Manager Launches Tasks

Manager (generic handler):
- Receives round start message
- For each participant:
  - Creates a standard Propeller task
  - Pins task to specified proplet
  - Sets env vars: `ROUND_ID`, `MODEL_URI`, `HYPERPARAMS`
  - Launches task

**No FL-specific validation or state tracking.**

### 3. Proplets Execute Wasm Client

Wasm client (works on both Rust and embedded proplets):
- Reads `ROUND_ID`, `MODEL_URI` (MQTT topic), `HYPERPARAMS` from environment
- Subscribes to model from `MODEL_URI` MQTT topic (receives retained message)
- Performs local training
- Outputs JSON update

**Embedded Proplet Specific:**
- Automatically subscribes to model topic when `MODEL_URI` is provided
- Publishes updates directly to `fl/rounds/{round_id}/updates/{proplet_id}`
- Parses Wasm output as JSON update envelope

### 4. Proplets Publish Updates

Proplets publish to: `fl/rounds/{round_id}/updates/{proplet_id}`

```json
{
  "round_id": "r-0001",
  "proplet_id": "proplet-2",
  "base_model_uri": "fl/models/global_model_v0",
  "num_samples": 512,
  "metrics": {"loss": 0.73},
  "update": {"w": [0.12, -0.05, 1.01], "b": 0.33}
}
```

### 5. Manager Forwards Updates

Manager (generic forwarder):
- Receives update from `fl/rounds/+/updates/+`
- Forwards **verbatim** to `fml/updates`
- Adds optional timestamp metadata

**No inspection, validation, or aggregation.**

### 6. Coordinator Aggregates

Coordinator:
- Receives updates from `fml/updates`
- Tracks round state
- When `k_of_n` updates received (or timeout):
  - Aggregates using FedAvg (weighted by `num_samples`)
  - Writes new model: `global_model_v{N+1}.json`
  - Publishes completion: `fl/rounds/{round_id}/complete`

## Model Format

Simple JSON format (demo):

```json
{
  "w": [0.10, -0.03, 1.00],
  "b": 0.31,
  "version": 1
}
```

Distributed via MQTT topic `fl/models/global_model_v{N}` (lightweight, consistent with Propeller architecture)

## MQTT Topics

| Topic Pattern | Direction | Purpose |
|--------------|-----------|---------|
| `fl/rounds/start` | External → Manager | Start FL round (generic task launch) |
| `fl/rounds/{round_id}/updates/{proplet_id}` | Proplet → Manager | Send training updates |
| `fml/updates` | Manager → Coordinator | Forwarded updates (verbatim) |
| `fl/models/publish` | Coordinator → Model Server | Publish new aggregated model |
| `fl/models/global_model_v{N}` | Model Server → All | Model distribution (retained messages) |
| `fl/rounds/{round_id}/complete` | Coordinator → All | Round completion notification |

## Code Locations

- **Sample FML Coordinator**: `examples/fl-demo/coordinator/`
- **Sample Model Server**: `examples/fl-demo/model-server/`
- **Sample FL Client Wasm**: `examples/fl-demo/client-wasm/`
- **Manager Generic Handlers**: `manager/service.go::handleRoundStart()`, `handleUpdateForward()`
- **Docker Compose**: `examples/fl-demo/compose.yaml`

## Key Design Principles

1. **Manager is workload-agnostic**: No FL-specific logic in Manager
2. **Coordinator owns FL semantics**: All aggregation, round tracking, model versioning
3. **Lightweight MQTT-based distribution**: Models distributed via MQTT (consistent with Propeller architecture)
4. **Generic message forwarding**: Manager forwards updates verbatim

## Deprecated Components

The following are **deprecated** and kept only for backward compatibility:

- `task.FLSpec`: FL spec embedded in tasks
- `task.TaskKindFederated`: Federated task kind
- Manager FL aggregation functions (removed)
- Manager FL env injection (removed)
- Manager FL round management (removed)

See `examples/fl-demo/` for the new architecture.

## Running the Sample

See `examples/fl-demo/README.md` for complete instructions.

Quick start:

```bash
cd examples/fl-demo
docker compose up -d

# Trigger a round (using mosquitto_pub)
mosquitto_pub -h localhost -t "fl/rounds/start" -m '{
  "round_id": "r-0001",
  "model_uri": "fl/models/global_model_v0",
  "task_wasm_image": "oci://example/fl-client-wasm:latest",
  "participants": ["proplet-1", "proplet-2", "proplet-3"],
  "hyperparams": {"epochs": 1, "lr": 0.01, "batch_size": 16},
  "k_of_n": 3,
  "timeout_s": 30
}'
```

## Limitations (Sample Application)

- No secure aggregation
- No differential privacy
- No persistence (round state in memory)
- Simple model format (JSON)
- No large model support
- No embedded FL state in task specs

These are intentional limitations for the sample application. Production FL systems would implement these features in the external coordinator.
