# FML Demo - Sample Federated Learning Application

This is a **sample FML (Federated Machine Learning) application** demonstrating how to build federated learning on top of Propeller's generic orchestration capabilities.

## Architecture

Propeller remains **workload-agnostic**. This demo shows how to build FL as an external application:

- **Manager**: Generic task launcher (no FL logic)
- **FML Coordinator**: External service that owns FL rounds, aggregation, and model versioning
- **Model Server**: Simple HTTP file server for model distribution
- **Client Wasm**: Sample FL training workload

## Components

### 1. FML Coordinator (`coordinator/`)

- Subscribes to `fml/updates` (forwarded by Manager)
- Tracks round state in memory
- Aggregates updates using FedAvg (weighted by `num_samples`)
- Writes aggregated models to `/tmp/fl-models/`
- Publishes round completion to `fl/rounds/{round_id}/complete`

### 2. Model Server (`model-server/`)

- Lightweight MQTT-based model distribution
- Publishes models to `fl/models/global_model_v{N}` topics
- Subscribes to `fl/models/publish` to receive new models from coordinator
- Stores models in `/tmp/fl-models/` for persistence

### 3. Client Wasm (`client-wasm/`)

- Reads `ROUND_ID`, `MODEL_URI` (MQTT topic), `HYPERPARAMS` from environment
- Performs toy local training
- Outputs JSON update in format:
  ```json
  {
    "round_id": "r-0001",
    "base_model_uri": "http://...",
    "num_samples": 512,
    "metrics": {"loss": 0.73},
    "update": {"w": [0.12, -0.05, 1.01], "b": 0.33}
  }
  ```

## Workflow

1. **Round Start**: Coordinator (or external trigger) publishes to `fl/rounds/start`:
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

2. **Manager**: Listens to `fl/rounds/start`, launches tasks for each participant (workload-agnostic)

3. **Proplets**: Execute Wasm client, subscribe to model topic from `MODEL_URI`, publish updates to `fl/rounds/{round_id}/updates/{proplet_id}`

4. **Manager**: Forwards updates verbatim from `fl/rounds/+/updates/+` to `fml/updates`

5. **Coordinator**: Receives updates, aggregates when `k_of_n` reached, writes new model, publishes to `fl/models/publish`

6. **Model Server**: Receives model from coordinator, publishes to `fl/models/global_model_v{N}` topic

## Running the Demo

### Prerequisites

- Docker and Docker Compose
- Propeller Manager and Proplet images built (or use published images)

### Build Client Wasm

```bash
cd examples/fl-demo/client-wasm
GOOS=wasip1 GOARCH=wasm go build -o fl-client.wasm fl-client.go
```

### Start Services

```bash
cd examples/fl-demo
docker compose up -d
```

### Trigger a Round

Publish a round start message to MQTT:

```bash
# Using mosquitto_pub (if installed)
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

### Monitor Progress

- Coordinator logs: `docker compose logs -f coordinator`
- Manager logs: `docker compose logs -f manager`
- Check aggregated models: `docker compose exec model-server ls -la /tmp/fl-models/`

## Key Design Principles

1. **Manager is workload-agnostic**: No FL-specific logic in Manager
2. **Coordinator owns FL semantics**: All aggregation, round tracking, model versioning
3. **Lightweight MQTT-based distribution**: Models distributed via MQTT topics (consistent with Propeller architecture)
4. **Generic message forwarding**: Manager forwards updates verbatim

## Limitations (Demo Only)

- No secure aggregation
- No differential privacy
- No persistence (round state in memory)
- Simple model format (JSON)
- No large model support
- No embedded FL state in task specs

## Next Steps

- Add persistence for round state
- Implement secure aggregation
- Add model versioning and rollback
- Support multiple concurrent rounds
- Add monitoring and metrics
