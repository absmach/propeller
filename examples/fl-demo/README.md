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

This demo supports three ways to set up SuperMQ:

#### Option 1: Use the included minimal SuperMQ setup (default, for quick demos)

The `compose-http.yaml` file includes a minimal SuperMQ setup with all necessary services:

- **SpiceDB**: Authorization service
- **Auth Service**: Authentication and authorization
- **Domains Service**: Domain management
- **Clients Service**: Client management
- **Channels Service**: Channel management
- **RabbitMQ**: Message broker
- **NATS**: Message streaming
- **MQTT Adapter**: MQTT protocol adapter (exposed on port 1883)

**Usage:**
```bash
cd examples/fl-demo
SMQ_RELEASE_TAG=v0.18.1 docker compose -f compose-http.yaml up -d
```

#### Option 2: Use the production SuperMQ setup (recommended for production)

Use the full production SuperMQ setup from `docker/compose.yaml` with the FL demo extension.

**Prerequisites:**

The base `docker/compose.yaml` file requires a `docker/.env` file with SuperMQ environment variables. This is a requirement of the base SuperMQ setup (the nginx service explicitly references it), not something added by the FL demo.

**If you already have `docker/.env`:**

If you already have a `docker/.env` file (e.g., from a previous SuperMQ setup), you can use it directly - no changes needed! Just run:

```bash
docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env up -d
```

**If you need to create `docker/.env`:**

If you don't have a `docker/.env` file yet, create one with SuperMQ environment variables. Here's a minimal example based on the defaults used in `compose-http.yaml`:

```bash
# SuperMQ Release Tag
SMQ_RELEASE_TAG=latest

# SpiceDB Configuration
SMQ_SPICEDB_PRE_SHARED_KEY=secret
SMQ_SPICEDB_DATASTORE_ENGINE=postgres
SMQ_SPICEDB_DB_USER=spicedb
SMQ_SPICEDB_DB_PASS=spicedb
SMQ_SPICEDB_DB_NAME=spicedb
SMQ_SPICEDB_DB_PORT=5432
SMQ_SPICEDB_HOST=spicedb
SMQ_SPICEDB_PORT=50051
SMQ_SPICEDB_SCHEMA_FILE=./spicedb/schema.zed

# Auth Service
SMQ_AUTH_DB_USER=auth
SMQ_AUTH_DB_PASS=auth
SMQ_AUTH_DB_NAME=auth
SMQ_AUTH_DB_PORT=5432
SMQ_AUTH_HTTP_PORT=9001
SMQ_AUTH_GRPC_PORT=7001
SMQ_AUTH_SECRET_KEY=your-secret-key-here
SMQ_AUTH_CACHE_URL=redis://supermq-auth-redis:6379/0

# Domains Service
SMQ_DOMAINS_DB_USER=domains
SMQ_DOMAINS_DB_PASS=domains
SMQ_DOMAINS_DB_NAME=domains
SMQ_DOMAINS_DB_PORT=5432
SMQ_DOMAINS_HTTP_PORT=9003
SMQ_DOMAINS_GRPC_PORT=7003
SMQ_DOMAINS_CACHE_URL=redis://supermq-domains-redis:6379/0

# Clients Service
SMQ_CLIENTS_DB_USER=clients
SMQ_CLIENTS_DB_PASS=clients
SMQ_CLIENTS_DB_NAME=clients
SMQ_CLIENTS_DB_PORT=5432
SMQ_CLIENTS_HTTP_PORT=9006
SMQ_CLIENTS_GRPC_PORT=7006
SMQ_CLIENTS_CACHE_URL=redis://supermq-clients-redis:6379/0

# Channels Service
SMQ_CHANNELS_DB_USER=channels
SMQ_CHANNELS_DB_PASS=channels
SMQ_CHANNELS_DB_NAME=channels
SMQ_CHANNELS_DB_PORT=5432
SMQ_CHANNELS_HTTP_PORT=9005
SMQ_CHANNELS_GRPC_PORT=7005
SMQ_CHANNELS_CACHE_URL=redis://supermq-channels-redis:6379/0

# RabbitMQ
SMQ_RABBITMQ_COOKIE=secret
SMQ_RABBITMQ_USER=guest
SMQ_RABBITMQ_PASS=guest
SMQ_RABBITMQ_VHOST=/
SMQ_RABBITMQ_PORT=5672
SMQ_RABBITMQ_HTTP_PORT=15672
SMQ_RABBITMQ_WS_PORT=15675

# NATS
SMQ_NATS_PORT=4222
SMQ_NATS_HTTP_PORT=8222
SMQ_NATS_JETSTREAM_KEY=u7wFoAPgXpDueXOFldBnXDh4xjnSOyEJ2Cb8Z5SZvGLzIZ3U4exWhhoIBZHzuNvh

# MQTT Adapter
SMQ_MQTT_ADAPTER_MQTT_PORT=1883
SMQ_MQTT_ADAPTER_WS_PORT=8080
SMQ_MQTT_ADAPTER_MQTT_TARGET_HOST=rabbitmq
SMQ_MQTT_ADAPTER_MQTT_TARGET_PORT=1883
SMQ_MQTT_ADAPTER_MQTT_TARGET_USERNAME=guest
SMQ_MQTT_ADAPTER_MQTT_TARGET_PASSWORD=guest
SMQ_MQTT_ADAPTER_WS_TARGET_HOST=rabbitmq
SMQ_MQTT_ADAPTER_WS_TARGET_PORT=15675

# Event Store
SMQ_ES_URL=nats://supermq-nats:4222
SMQ_MESSAGE_BROKER_URL=nats://supermq-nats:4222

# Service URLs (gRPC)
SMQ_AUTH_GRPC_URL=auth:7001
SMQ_DOMAINS_GRPC_URL=domains:7003
SMQ_CLIENTS_GRPC_URL=clients:7006
SMQ_CHANNELS_GRPC_URL=channels:7005

# Other required variables (set to empty or defaults as needed)
SMQ_ALLOW_UNVERIFIED_USER=true
SMQ_SEND_TELEMETRY=false
SMQ_JAEGER_URL=
SMQ_JAEGER_TRACE_RATIO=0.0
```

> **Note**: This is a minimal example. For production, you should:
> - Use strong, unique passwords and keys
> - Configure proper SSL/TLS certificates
> - Set up proper authentication and authorization
> - Refer to the SuperMQ documentation for the complete list of variables and production best practices

**Usage (from repository root):**
```bash
docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env up -d
```

**To build and start:**
```bash
docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env up -d --build
```

This approach:
- Uses the full production SuperMQ stack with all services (Users, Groups, Notifications, HTTP/WS/CoAP adapters, Nginx, etc.)
- Extends the base setup with FL-specific services
- Allows you to leverage production-grade SuperMQ features

> **Tip**: If you want to avoid creating a `.env` file, use Option 1 (`compose-http.yaml`) which includes defaults for all variables.

#### Option 3: Use external SuperMQ instance

- If you have SuperMQ running elsewhere, update the `MQTT_BROKER` addresses in the compose file
- Or set environment variables to point to your external SuperMQ instance
- Update `MANAGER_MQTT_ADDRESS` and `PROPLET_MQTT_ADDRESS` to point to your SuperMQ MQTT adapter

### Build Client Wasm

From the repository root:

```bash
cd examples/fl-demo/client-wasm
GOOS=wasip1 GOARCH=wasm go build -o fl-client.wasm fl-client.go
cd ../../..
```

### Generate Auth Keys

SuperMQ auth service requires an EdDSA key file. Generate it before starting services:

**For minimal setup (compose-http.yaml):**

From the `examples/fl-demo` directory:

```bash
./generate-auth-key.sh
```

This will create a key file at `keys/active.key` that will be mounted into the auth service.

**For production setup:**

The production SuperMQ setup uses the auth service from the base compose file. Ensure your `docker/.env` file includes the necessary auth configuration. If you need to generate keys, refer to the SuperMQ documentation or use the same script and mount the keys appropriately in your production setup.

### Start Services

**For quick demo (minimal SuperMQ setup):**

From the `examples/fl-demo` directory:

```bash
docker compose -f compose-http.yaml up -d
```

**For production setup (full SuperMQ stack):**

From the repository root:

```bash
docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env up -d
```

> **Note**: If you already have a `docker/.env` file, you can use it directly. If not, see the "SuperMQ Setup" section above for a minimal example `.env` file.

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
