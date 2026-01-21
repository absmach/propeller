# Sample Federated Learning Application

This is a **sample FML (Federated Machine Learning) application** demonstrating how to build federated learning on top of Propeller's generic orchestration capabilities.

## Architecture

Propeller remains **workload-agnostic**. This demo shows how to build FL as an external application:

- **Manager**: Generic task launcher (no FL logic)
- **FML Coordinator**: External service that owns FL rounds, aggregation, and model versioning
- **Model Server**: Simple HTTP file server for model distribution
- **Client Wasm**: Sample FL training workload

## Components

### 1. FML Coordinator

- Subscribes to `fml/updates` (forwarded by Manager)
- Tracks round state in memory
- Aggregates updates using FedAvg (weighted by `num_samples`)
- Writes aggregated models to `/tmp/fl-models/`
- Publishes round completion to `fl/rounds/{round_id}/complete`

### 2. Model Server

- Lightweight MQTT-based model distribution
- Publishes models to `fl/models/global_model_v{N}` topics
- Subscribes to `fl/models/publish` to receive new models from coordinator
- Stores models in `/tmp/fl-models/` for persistence

### 3. Client Wasm

- Reads `ROUND_ID`, `MODEL_URI` (MQTT topic), `HYPERPARAMS` from environment
- Performs toy local training
- Outputs JSON update in format:
- **Works with both Rust proplet (Wasmtime) and embedded proplet (WAMR/Zephyr)**

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
- **SuperMQ**: This demo uses SuperMQ instead of Mosquitto for MQTT communication

### SuperMQ Setup

This demo includes a minimal SuperMQ setup in `compose-http.yaml`. The SuperMQ services include:

- **SpiceDB**: Authorization service
- **Auth Service**: Authentication and authorization
- **Domains Service**: Domain management
- **Clients Service**: Client management
- **Channels Service**: Channel management
- **RabbitMQ**: Message broker
- **NATS**: Message streaming
- **MQTT Adapter**: MQTT protocol adapter (exposed on port 1883)

#### Option 1: Use the included SuperMQ setup (default)

- The compose file includes all necessary SuperMQ services
- Set `SMQ_RELEASE_TAG` environment variable to specify SuperMQ version (defaults to `latest`)
- Example: `SMQ_RELEASE_TAG=v0.18.1 docker compose -f compose-http.yaml up -d`

#### Option 2: Use external SuperMQ instance

- If you have SuperMQ running elsewhere, update the `MQTT_BROKER` addresses in the compose file
- Or set environment variables to point to your external SuperMQ instance
- Update `MANAGER_MQTT_ADDRESS` and `PROPLET_MQTT_ADDRESS` to point to your SuperMQ MQTT adapter

> **Note**: For production deployments, use the full SuperMQ setup from `docker/compose.yaml` or your SuperMQ repository.

### Build Client Wasm

From the repository root:

```bash
cd examples/fl-demo/client-wasm
GOOS=wasip1 GOARCH=wasm go build -o fl-client.wasm fl-client.go
cd ..
```

### Start Services

From the repository root:

```bash
cd examples/fl-demo
docker compose -f compose-http.yaml up -d
```

Or if you're already in the `examples/fl-demo` directory:

```bash
docker compose -f compose-http.yaml up -d
```

### Trigger a Round

Publish a round start message to MQTT:

```bash
# Using mosquitto_pub (if installed) - connects to SuperMQ MQTT adapter
mosquitto_pub -h localhost -p 1883 -t "fl/rounds/start" -m '{
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

- Coordinator logs: `docker compose -f compose-http.yaml logs -f coordinator`
- Manager logs: `docker compose -f compose-http.yaml logs -f manager`
- Check aggregated models: `docker compose -f compose-http.yaml exec model-server ls -la /tmp/fl-models/`

### Optional: Using Test Scripts

The demo includes Python test scripts for automated testing:

1. **Install Python dependencies** (from `examples/fl-demo` directory):

   ```bash
   pip install -r requirements.txt
   ```

2. **Run HTTP test script** (automates full workflow):

   ```bash
   python3 test-fl-http.py
   ```

3. **Run local test script** (uses local WASM file):

   ```bash
   python3 test-fl-local.py
   ```

Alternatively, you can use the Go demo script:

```bash
go run demo.go
```

## Limitations (Demo Only)

- No secure aggregation
- No differential privacy
- No persistence (round state in memory)
- Simple model format (JSON)
- No large model support
- No embedded FL state in task specs
