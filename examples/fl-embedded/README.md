# Federated ML Example for Embedded Proplet

This example demonstrates how to use Federated Machine Learning on the embedded proplet using the host functions provided by the embedded runtime.

## Overview

The embedded proplet provides three host functions that allow WASM modules to access FL environment variables:

1. `get_proplet_id()` - Returns the PROPLET_ID (Manager-known identity)
2. `get_model_data()` - Returns MODEL_DATA JSON string (global model fetched by proplet)
3. `get_dataset_data()` - Returns DATASET_DATA JSON string (local dataset fetched by proplet)

## Architecture

```
┌─────────────┐
│   Manager   │ Creates FL task with ROUND_ID env var
└──────┬──────┘       │ MQTT: m/{domain}/c/{channel}/control/manager/start
       │              ▼
       │   ┌─────────────────────────────────────────────────────────┐
       │   │  Embedded Proplet (mqtt_client.c + wasm_handler.c)     │
       │   ├─────────────────────────────────────────────────────────┤
       │   │  1. Receives start_task() with ROUND_ID                 │
       │   │  2. Sets PROPLET_ID from config.client_id               │
       │   │  3. Fetches model from Model Registry → MODEL_DATA     │
       │   │  4. Fetches dataset from Data Store → DATASET_DATA      │
       │   │  5. Executes WASM client with host functions registered │
       │   │  6. WASM client calls host functions to get data        │
       │   │  7. WASM client trains locally, outputs JSON update     │
       │   │  8. Proplet parses JSON, submits update via MQTT:      │
       │   │     fl/rounds/{round_id}/updates/{proplet_id}           │
       └───┴─────────────────────────────────────────────────────────┘
                    │
                    └──→ MQTT: fl/rounds/{round_id}/updates/{proplet_id}
                              ▼
                    ┌─────────────────┐
                    │   Coordinator   │ Aggregates updates
                    └─────────────────┘
```

## Building the Example

### Prerequisites

- TinyGo installed (for WASM compilation)
- Go 1.21+

### Build Steps

```bash
cd examples/fl-embedded

# Build WASM module using TinyGo
tinygo build -target=wasi -o fl-client.wasm fl-client.go
```

## Host Functions

The embedded proplet runtime provides these host functions:

### `get_proplet_id(ret_offset *i32, ret_len *i32) -> i32`

Returns the PROPLET_ID as a string in WASM linear memory.
- `ret_offset`: Pointer to store the memory offset of the string
- `ret_len`: Pointer to store the length of the string
- Returns: 1 on success, 0 on failure

### `get_model_data(ret_offset *i32, ret_len *i32) -> i32`

Returns the MODEL_DATA JSON string in WASM linear memory.
- `ret_offset`: Pointer to store the memory offset of the string
- `ret_len`: Pointer to store the length of the string
- Returns: 1 on success, 0 on failure

### `get_dataset_data(ret_offset *i32, ret_len *i32) -> i32`

Returns the DATASET_DATA JSON string in WASM linear memory.
- `ret_offset`: Pointer to store the memory offset of the string
- `ret_len`: Pointer to store the length of the string
- Returns: 1 on success, 0 on failure

### Compatibility Note

For compatibility with TinyGo and WASI, the embedded proplet can also set these values as environment variables:
- `PROPLET_ID` - Set from `config.client_id`
- `MODEL_DATA` - Set from fetched model JSON
- `DATASET_DATA` - Set from fetched dataset JSON

The example code uses host functions first, then falls back to `os.Getenv()` for compatibility.

## Usage

### 1. Deploy the WASM Module

The compiled `fl-client.wasm` should be made available to the Manager, either:
- Embedded as base64 in the task start command
- Available via the registry (image_url)

### 2. Create FL Task via Manager

The Manager creates a task with the following environment variables:

```json
{
  "id": "fl-task-1",
  "name": "fl-client",
  "file": "<base64-encoded-wasm>",
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

### 3. Embedded Proplet Workflow

1. **Task Detection**: Proplet detects FL task via `ROUND_ID` environment variable
2. **PROPLET_ID Setup**: Proplet sets `PROPLET_ID` from `config.client_id`
3. **Model Fetching**: Proplet fetches model from Model Registry via HTTP GET
   - URL: `{MODEL_REGISTRY_URL}/models/{version}`
   - Stores result in `g_current_task.model_data`
4. **Dataset Fetching**: Proplet fetches dataset from Local Data Store via HTTP GET
   - URL: `{DATA_STORE_URL}/datasets/{proplet_id}`
   - Stores result in `g_current_task.dataset_data`
5. **WASM Execution**: Proplet executes WASM module with host functions registered
6. **Host Function Calls**: WASM module calls host functions to get:
   - `PROPLET_ID` via `get_proplet_id()`
   - `MODEL_DATA` via `get_model_data()`
   - `DATASET_DATA` via `get_dataset_data()`
7. **Training**: WASM module performs local training and outputs JSON update
8. **Update Submission**: Proplet captures stdout, parses JSON, and publishes to:
   - MQTT topic: `fl/rounds/{round_id}/updates/{proplet_id}`

## Example Output

The WASM module outputs JSON to stdout:

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

## Differences from Rust Proplet

1. **Host Functions**: Embedded proplet uses host functions instead of environment variables
   - Rust: `os.Getenv("MODEL_DATA")`
   - Embedded: `get_model_data(&offset, &length)`

2. **Update Submission**: Embedded proplet uses MQTT directly
   - Rust: HTTP POST with MQTT fallback
   - Embedded: MQTT only (HTTP can be added later)

3. **Data Fetching**: Both use HTTP GET, but embedded has MQTT fallback for models
   - Model: HTTP GET → MQTT subscription fallback
   - Dataset: HTTP GET only

## Testing

To test the example:

1. Start the FL infrastructure (coordinator, model registry, data store)
2. Deploy the embedded proplet with the compiled WASM module
3. Create an FL task via the Manager
4. Monitor MQTT topic `fl/rounds/{round_id}/updates/{proplet_id}` for updates

## Troubleshooting

### Host Functions Not Available

If host functions are not found:
- Ensure the embedded proplet has been built with the latest `wasm_handler.c`
- Check that native symbols are registered during module instantiation
- Verify WAMR runtime is properly initialized

### Model/Dataset Not Fetched

If `MODEL_DATA` or `DATASET_DATA` are empty:
- Check HTTP connectivity to Model Registry and Data Store
- Verify URLs are correct in environment variables
- Check proplet logs for HTTP fetch errors
- Fallback to synthetic data will be used if fetch fails

### Update Not Published

If updates are not published:
- Check MQTT connection status
- Verify `ROUND_ID` is set correctly
- Check proplet logs for MQTT publish errors
- Ensure WASM module outputs valid JSON to stdout
