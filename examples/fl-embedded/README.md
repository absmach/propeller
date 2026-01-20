# Federated Learning for Embedded Proplet

This example demonstrates federated machine learning on embedded proplets using host functions provided by the embedded runtime.

## Overview

The embedded proplet provides host functions that allow WASM modules to access FL environment variables:

- `get_proplet_id()` - Returns the PROPLET_ID (Manager-known identity)
- `get_model_data()` - Returns MODEL_DATA JSON string (global model fetched by proplet)
- `get_dataset_data()` - Returns DATASET_DATA JSON string (local dataset fetched by proplet)

## Prerequisites

- TinyGo installed (for WASM compilation)
- Go 1.21+
- SuperMQ infrastructure running (see main `docker/compose.yaml`)

## Building

```bash
cd examples/fl-embedded
make build
```

This creates `fl-client.wasm` that can be deployed to embedded proplets.

## Running the Demo

### 1. Build the WASM Module

```bash
make build
```

### 2. Base64 Encode the WASM Module

```bash
base64 -i fl-client.wasm -o fl-client.wasm.b64
```

### 3. Create Task via Manager API

```bash
curl -X POST http://localhost:7070/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "id": "fl-task-embedded-1",
    "name": "fl-client-embedded",
    "file": "<base64-wasm>",
    "env": {
      "ROUND_ID": "round-1",
      "MODEL_URI": "fl/models/global_model_v0",
      "COORDINATOR_URL": "http://coordinator-http:8080",
      "MODEL_REGISTRY_URL": "http://model-registry:8081",
      "DATA_STORE_URL": "http://local-data-store:8083",
      "HYPERPARAMS": "{\"epochs\":1,\"lr\":0.01,\"batch_size\":16}"
    }
  }'
```

### 4. Start the Task

```bash
curl -X POST http://localhost:7070/tasks/fl-task-embedded-1/start
```

## Workflow

1. **Manager** creates task and publishes to SuperMQ MQTT topic: `m/{domain}/c/{channel}/control/manager/start`

2. **Embedded Proplet** receives start command:
   - Detects FL task via `ROUND_ID` environment variable
   - Sets `PROPLET_ID` from `config.client_id`
   - Fetches model from Model Registry via HTTP GET
   - Fetches dataset from Data Store via HTTP GET
   - Stores data for WASM module access

3. **WASM Module Execution**:
   - Proplet executes `fl-client.wasm`
   - WASM module calls host functions to get PROPLET_ID, MODEL_DATA, and DATASET_DATA
   - WASM module performs local training
   - WASM module outputs JSON update to stdout

4. **Update Submission**:
   - Proplet captures stdout (JSON update)
   - Proplet publishes to SuperMQ MQTT: `fl/rounds/{round_id}/updates/{proplet_id}`

5. **Coordinator** receives update and aggregates

## Monitoring

### Check Proplet Logs

The embedded proplet logs will show:

```bash
[INFO] FML task detected: ROUND_ID=round-1
[INFO] Fetching model from registry: http://model-registry:8081/models/0
[INFO] Successfully fetched model v0 via HTTP
[INFO] Published FML update to fl/rounds/round-1/updates/proplet-1
```

### Monitor MQTT Topic

Subscribe to the FL updates topic (connects to SuperMQ MQTT adapter):

```bash
mosquitto_sub -h localhost -p 1883 -t "fl/rounds/round-1/updates/+"
```

## Differences from Rust Proplet

1. **Host Functions**: Embedded proplet uses host functions instead of environment variables
2. **Update Submission**: Embedded proplet uses MQTT directly (via SuperMQ)
3. **Data Fetching**: Both use HTTP GET, but embedded has MQTT fallback for models
