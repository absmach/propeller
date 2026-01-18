# Example Usage: FL on Embedded Proplet

This document shows how to use the FL embedded example with the embedded proplet.

## Quick Start

### 1. Build the WASM Module

```bash
cd examples/fl-embedded
make build
```

This creates `fl-client.wasm` that can be deployed to embedded proplets.

### 2. Prepare the Task Configuration

Create a task JSON file for the Manager:

```json
{
  "id": "fl-task-embedded-1",
  "name": "fl-client-embedded",
  "file": "<base64-encoded-fl-client.wasm>",
  "env": {
    "ROUND_ID": "round-1",
    "MODEL_URI": "fl/models/global_model_v0",
    "COORDINATOR_URL": "http://coordinator-http:8080",
    "MODEL_REGISTRY_URL": "http://model-registry:8081",
    "DATA_STORE_URL": "http://local-data-store:8083",
    "HYPERPARAMS": "{\"epochs\":1,\"lr\":0.01,\"batch_size\":16}"
  }
}
```

### 3. Base64 Encode the WASM Module

```bash
# Encode the WASM file
base64 -i fl-client.wasm -o fl-client.wasm.b64

# Or on macOS
base64 -i fl-client.wasm > fl-client.wasm.b64
```

### 4. Create Task via Manager API

```bash
# Replace <base64-wasm> with the content of fl-client.wasm.b64
curl -X POST http://manager:8080/tasks \
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

### 5. Start the Task

```bash
curl -X POST http://manager:8080/tasks/fl-task-embedded-1/start
```

## Expected Workflow

1. **Manager** creates task and publishes to MQTT topic: `m/{domain}/c/{channel}/control/manager/start`

2. **Embedded Proplet** receives start command:
   - Detects FL task via `ROUND_ID` environment variable
   - Sets `PROPLET_ID` from `config.client_id`
   - Fetches model from Model Registry: `GET {MODEL_REGISTRY_URL}/models/{version}`
   - Fetches dataset from Data Store: `GET {DATA_STORE_URL}/datasets/{proplet_id}`
   - Stores data in `g_current_task.model_data` and `g_current_task.dataset_data`

3. **WASM Module Execution**:
   - Proplet executes `fl-client.wasm`
   - WASM module calls host functions:
     - `get_proplet_id()` → gets PROPLET_ID
     - `get_model_data()` → gets MODEL_DATA JSON
     - `get_dataset_data()` → gets DATASET_DATA JSON
   - WASM module performs local training
   - WASM module outputs JSON update to stdout

4. **Update Submission**:
   - Proplet captures stdout (JSON update)
   - Proplet parses JSON
   - Proplet publishes to MQTT: `fl/rounds/{round_id}/updates/{proplet_id}`

5. **Coordinator** receives update and aggregates

## Monitoring

### Check Proplet Logs

The embedded proplet logs will show:
```
[INFO] FML task detected: ROUND_ID=round-1
[INFO] Fetching model from registry: http://model-registry:8081/models/0
[INFO] Successfully fetched model v0 via HTTP and stored in MODEL_DATA
[INFO] Fetching dataset for proplet_id=proplet-1 from: http://local-data-store:8083/datasets/proplet-1
[INFO] Successfully fetched dataset via HTTP and stored in DATASET_DATA
[INFO] WASM execution results published to MQTT topic
[INFO] Published FML update to fl/rounds/round-1/updates/proplet-1
```

### Monitor MQTT Topic

Subscribe to the FL updates topic:
```bash
mosquitto_sub -h <mqtt-broker> -t "fl/rounds/round-1/updates/+"
```

You should see JSON updates like:
```json
{
  "round_id": "round-1",
  "proplet_id": "proplet-1",
  "base_model_uri": "fl/models/global_model_v0",
  "num_samples": 512,
  "metrics": {
    "loss": 0.75
  },
  "update": {
    "w": [0.1, 0.2, 0.3],
    "b": 0.05
  }
}
```

## Troubleshooting

### Host Functions Not Working

If host functions return 0 (failure):
- Check that the embedded proplet was built with the latest `wasm_handler.c`
- Verify native symbols are registered: `wasm_runtime_register_natives()`
- The example falls back to `os.Getenv()` for compatibility

### Model/Dataset Not Available

If `MODEL_DATA` or `DATASET_DATA` are empty:
- Check HTTP connectivity from embedded proplet to services
- Verify URLs in environment variables
- Check proplet logs for HTTP errors
- The example will use synthetic data as fallback

### Update Not Published

If updates don't appear on MQTT:
- Check MQTT connection status
- Verify `ROUND_ID` is set correctly
- Check that WASM module outputs valid JSON to stdout
- Review proplet logs for MQTT publish errors
