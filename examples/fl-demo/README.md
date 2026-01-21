# Production FL Demo - Quick Start Guide

This guide covers running the production-grade Federated Learning (FL) demo application using the full SuperMQ stack.

## Prerequisites

- Docker and Docker Compose
- Python 3 (for provisioning script)
- `docker/.env` file with SuperMQ configuration

## Step 1: Prepare Environment

All commands in this guide should be run from the repository root (`/propeller`).

### Generate Auth Keys (if needed)

From the repository root:

```bash
(cd examples/fl-demo && ./generate-auth-key.sh)
```

### Build Client WASM

From the repository root:

```bash
(cd examples/fl-demo/client-wasm && GOOS=wasip1 GOARCH=wasm go build -o fl-client.wasm fl-client.go)
```

## Step 2: Build Images

**IMPORTANT**: Manager and proplet must be built from source as the pre-built images don't include FL endpoints.

From the repository root:

```bash
docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env build manager proplet proplet-2 proplet-3
```

This builds:

- `propeller-manager:local` - Manager with FL endpoints
- `propeller-proplet:local` - Proplet with FL endpoints (used by all proplet instances)

> **Note**: Building these images may take several minutes, especially the Rust-based proplet. Subsequent builds will be faster due to Docker layer caching.

## Step 3: Start Services

From the repository root:

```bash
docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env up -d
```

> **Alternative**: You can combine building and starting in one command:
>
> ```bash
> docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env up -d --build
> ```

This starts:

- Full SuperMQ production stack (Auth, Domains, Clients, Channels, RabbitMQ, NATS, MQTT Adapter, Nginx)
- FL-specific services (Model Registry, Aggregator, Local Data Store, Coordinator)
- Propeller services (Manager, Proplets)

## Step 4: Provision SuperMQ Resources

**IMPORTANT**: Before the manager and proplets can connect to MQTT, you must provision the necessary SuperMQ resources (domain, channel, and clients).

### Run Provisioning Script

> **Note**: Python dependencies are listed in `requirements.txt`. Install them with:
>
> ```bash
> pip install -r examples/fl-demo/requirements.txt
> ```

From the repository root:

```bash
(cd examples/fl-demo && python3 provision-smq.py)
```

The script will:

- Create a domain named "fl-demo"
- Create clients: manager, proplet-1, proplet-2, proplet-3, fl-coordinator
- Create a channel named "fl"
- Display the client IDs and keys

**Note**: If the domain already exists (route conflict), the script will use the existing domain.

The provisioning script also automatically updates `compose.yaml` with the new client credentials, domain ID, and channel ID. A backup of the original file is created as `compose.yaml.bak`.

If you need to manually update the compose file, edit:

- `MANAGER_CLIENT_ID` and `MANAGER_CLIENT_KEY`
- `PROPLET_CLIENT_ID` and `PROPLET_CLIENT_KEY` (for each proplet)
- `MANAGER_DOMAIN_ID` and `PROPLET_DOMAIN_ID`
- `MANAGER_CHANNEL_ID` and `PROPLET_CHANNEL_ID`

Or set them as environment variables in your `docker/.env` file.

## Step 5: Restart Services After Provisioning

Restart the manager and proplets to pick up the new credentials:

```bash
docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env restart manager proplet proplet-2 proplet-3
```

### Verify Services Are Running

```bash
# Check all containers
docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env ps

# Check manager health
curl http://localhost:7070/health

# Check manager MQTT connection
docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env logs manager | grep -i "connected\|mqtt"
```

## Step 6: Initialize Model Registry

Before starting a round, ensure the model registry has an initial model:

```bash
# Check if model v0 exists
curl http://localhost:8084/models/0

# If it doesn't exist, create it
curl -X POST http://localhost:8084/models \
  -H "Content-Type: application/json" \
  -d '{
    "version": 0,
    "model": {
      "w": [0.0, 0.0, 0.0],
      "b": 0.0
    }
  }'
```

## Step 7: Trigger a Federated Learning Round

### Option A: Using MQTT (via nginx)

Publish a round start message to the MQTT topic. **MQTT connections require authentication** using client credentials:

```bash
# Get client credentials from compose.yaml or provisioning output
# Use the manager client ID and key, or fl-coordinator client credentials

# Get credentials from compose.yaml (check MANAGER_CLIENT_ID and MANAGER_CLIENT_KEY)
# Or use fl-coordinator client credentials from provisioning output
mosquitto_pub -h localhost -p 1883 \
  -u "<CLIENT_ID>" \
  -P "<CLIENT_KEY>" \
  -t "fl/rounds/start" \
  -m '{
    "round_id": "r-0001",
    "model_uri": "fl/models/global_model_v0",
    "task_wasm_image": "oci://example/fl-client-wasm:latest",
    "participants": ["proplet-1", "proplet-2", "proplet-3"],
    "hyperparams": {"epochs": 1, "lr": 0.01, "batch_size": 16},
    "k_of_n": 3,
    "timeout_s": 30
  }'
```

> **Note**:
>
> - MQTT connections go through nginx. The port is configured via `SMQ_NGINX_MQTT_PORT` in your `docker/.env` file (default: 1883).
> - Use `-u` for client ID (username) and `-P` for client key (password).
> - Get the current client ID and key from `compose.yaml` (MANAGER_CLIENT_ID and MANAGER_CLIENT_KEY) or from the provisioning script output.

### Option B: Using HTTP API (Manager)

> **Note**: If you get a 404 error, ensure the manager was built from source (see Step 2). You can rebuild and restart:
>
> ```bash
> docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env build manager
> docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env up -d manager
> ```

```bash
curl -X POST http://localhost:7070/fl/experiments \
  -H "Content-Type: application/json" \
  -d '{
    "experiment_id": "exp-r-0001",
    "round_id": "r-0001",
    "model_ref": "fl/models/global_model_v0",
    "participants": ["proplet-1", "proplet-2", "proplet-3"],
    "hyperparams": {"epochs": 1, "lr": 0.01, "batch_size": 16},
    "k_of_n": 3,
    "timeout_s": 60,
    "task_wasm_image": "oci://example/fl-client-wasm:latest"
  }'
```

### Option C: Using Python Test Script (Recommended)

For a complete end-to-end test, use the Python script which handles the correct API format:

From the repository root:

```bash
(cd examples/fl-demo && python3 test-fl-http.py)
```

This script:

- Verifies all services are running
- Creates an initial model if needed
- Configures an experiment via the Manager API
- Simulates client training and updates
- Waits for aggregation
- Verifies the aggregated model

## Step 8: Monitor Round Progress

### View Logs

```bash
# Coordinator logs (shows aggregation progress)
docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env logs -f fl-demo-coordinator

# Manager logs (shows task distribution)
docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env logs -f propeller-manager

# Proplet logs (shows training execution)
docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env logs -f propeller-proplet
```

### Check Round Status

```bash
# Check if round completed
curl http://localhost:8080/rounds/r-0001/complete

# Check aggregated model
curl http://localhost:8084/models/1
```

## Troubleshooting

### Manager Not Connecting to MQTT

1. **Verify provisioning completed**: Check that the provisioning script ran successfully
2. **Check credentials**: Ensure client IDs and keys in `compose.yaml` match the provisioning output
3. **Restart services**: Restart manager and proplets after provisioning
4. **Check logs**: `docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env logs manager`

### Services Not Starting

1. **Check ports**: Ensure ports 1883, 7070, 8080, 8083, 8084, 8085 are not in use
2. **Check .env file**: Verify `docker/.env` exists and has required variables
3. **Check logs**: `docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env logs`

### Round Not Starting

1. **Verify model exists**: `curl http://localhost:8084/models/0`
2. **Check manager is running**: `curl http://localhost:7070/health`
3. **Check proplets are running**: `docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env ps | grep proplet`
4. **Check MQTT connectivity**: Verify nginx is exposing MQTT port 1883

## Quick Reference

### Service Ports

- **Manager**: 7070
- **Coordinator**: 8080
- **Model Registry**: 8084
- **Aggregator**: 8085
- **Local Data Store**: 8083
- **MQTT (via nginx)**: 1883

### Common Commands

```bash
# Start all services
docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env up -d

# Stop all services
docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env down

# View logs
docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env logs -f [service-name]

# Restart services
docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env restart [service-name]

# Check service status
docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env ps
```

### Health Check URLs

- Manager: `http://localhost:7070/health`
- Coordinator: `http://localhost:8080/health`
- Model Registry: `http://localhost:8084/health`
- Aggregator: `http://localhost:8085/health`
- Local Data Store: `http://localhost:8083/health`

## Architecture Overview

This demo shows how to build Federated Learning on top of Propeller's generic orchestration:

- **Manager**: Generic task launcher (no FL logic)
- **FML Coordinator**: External service that owns FL rounds, aggregation, and model versioning
- **Model Registry**: HTTP file server for model distribution
- **Aggregator**: Service that aggregates client updates using FedAvg
- **Local Data Store**: Service that provides datasets to clients
- **Client Wasm**: Sample FL training workload executed by proplets

The workflow:

1. Round start message triggers manager to launch tasks
2. Proplets execute Wasm client, perform local training
3. Updates are sent to coordinator
4. Coordinator aggregates updates when `k_of_n` reached
5. New model is stored and published for next round
