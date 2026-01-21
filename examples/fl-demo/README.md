# Production FL Demo - Quick Start Guide

This guide covers running the production-grade Federated Learning (FL) demo application using the full SuperMQ stack.

## Prerequisites

- Docker and Docker Compose
- Python 3 (for provisioning script)
- `docker/.env` file with SuperMQ configuration

## Step 1: Prepare Environment

All commands in this guide should be run from the repository root (`/propeller`).

### Understanding CLIENT_IDs vs Instance IDs

**IMPORTANT**: When configuring FL experiments, you must use **SuperMQ CLIENT_IDs** (UUIDs), not instance IDs.

- **Instance IDs**: `"proplet-1"`, `"proplet-2"`, `"proplet-3"` - These are just labels for identification
- **CLIENT_IDs**: `"3fe95a65-74f1-4ede-bf20-ef565f04cecb"` - These are the actual SuperMQ client credentials that proplets use to register with the manager

Proplets register themselves using their CLIENT_ID (from `PROPLET_CLIENT_ID`, `PROPLET_2_CLIENT_ID`, `PROPLET_3_CLIENT_ID` in your `docker/.env` file). The manager tracks proplets by these CLIENT_IDs, so the `participants` array in ConfigureExperiment must use CLIENT_IDs, not instance IDs.

Your `docker/.env` file should contain:
```bash
PROPLET_CLIENT_ID=3fe95a65-74f1-4ede-bf20-ef565f04cecb      # For proplet-1
PROPLET_2_CLIENT_ID=1f074cd1-4e22-4e21-92ca-e35a21d3ce29    # For proplet-2
PROPLET_3_CLIENT_ID=0d89e6d7-6410-40b5-bcda-07b0217796b8   # For proplet-3
```

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
docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env build manager proplet proplet-2 proplet-3 coordinator-http
```

This builds:

- `propeller-manager:local` - Manager with FL endpoints
- `propeller-proplet:local` - Proplet with FL endpoints (used by all proplet instances)
- `supermq-coordinator-http` - Coordinator with MQTT authentication support

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

**IMPORTANT**: After provisioning, you must restart the manager, coordinator, and proplets to pick up the new credentials. The provisioning script updates `compose.yaml` with the new client IDs, keys, and channel ID.

From the repository root:

```bash
docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env restart manager coordinator-http proplet proplet-2 proplet-3
```

> **Note**: If services don't restart properly, or if the manager container exited, you need to recreate them:
>
> ```bash
> docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env up -d manager coordinator-http proplet proplet-2 proplet-3
> ```
>
> **Important**: After recreating, wait a few seconds for services to start, then verify they're running:
>
> ```bash
> docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env ps manager coordinator-http proplet proplet-2 proplet-3
> ```

### Verify Services Are Running

From the repository root:

```bash
# Check all containers
docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env ps

# Check manager health (wait a few seconds after restart)
curl http://localhost:7070/health

# Check manager MQTT connection
docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env logs manager | grep -i "connected\|mqtt\|subscribe"

# Check coordinator is running and accessible
docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env ps coordinator-http
curl http://localhost:8086/health

# Verify proplet is using correct channel ID (should show new channel ID, not old one)
docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env logs proplet | grep -i "channel\|subscribe" | head -5
```

> **Note**: If the manager health check fails, check the logs:
>
> ```bash
> docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env logs manager
> ```
>
> Common issues:
>
> - Manager not connecting to MQTT: Verify credentials match provisioning output
> - Port 7070 not accessible: Ensure manager container is running and port is exposed
> - Coordinator connection refused: Ensure coordinator-http service is running (`docker compose ps coordinator-http`)
> - Proplet using old channel ID: Restart proplet containers to pick up new channel ID from compose.yaml

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
    "participants": ["<PROPLET_CLIENT_ID>", "<PROPLET_2_CLIENT_ID>", "<PROPLET_3_CLIENT_ID>"],
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
> - **IMPORTANT**: The `participants` array must use SuperMQ CLIENT_IDs (UUIDs), not instance IDs. Get these from your `docker/.env` file:
>   - `PROPLET_CLIENT_ID` (for proplet-1)
>   - `PROPLET_2_CLIENT_ID` (for proplet-2)
>   - `PROPLET_3_CLIENT_ID` (for proplet-3)

### Option B: Using HTTP API (Manager)

> **Note**: If you get a 404 error, ensure the manager was built from source (see Step 2). You can rebuild and restart:
>
> ```bash
> docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env build manager
> docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env up -d manager
> ```

```bash
# IMPORTANT: Export CLIENT_IDs from docker/.env (SuperMQ client IDs, NOT instance IDs)
# The participants array must use UUIDs, not "proplet-1", "proplet-2", "proplet-3"
export PROPLET_CLIENT_ID=$(grep '^PROPLET_CLIENT_ID=' docker/.env | grep -v '=""' | tail -1 | cut -d '=' -f2 | tr -d '"')
export PROPLET_2_CLIENT_ID=$(grep '^PROPLET_2_CLIENT_ID=' docker/.env | cut -d '=' -f2 | tr -d '"')
export PROPLET_3_CLIENT_ID=$(grep '^PROPLET_3_CLIENT_ID=' docker/.env | cut -d '=' -f2 | tr -d '"')

# Verify they're set correctly (should show UUIDs, not "proplet-1", etc.)
echo "PROPLET_CLIENT_ID=$PROPLET_CLIENT_ID"
echo "PROPLET_2_CLIENT_ID=$PROPLET_2_CLIENT_ID"
echo "PROPLET_3_CLIENT_ID=$PROPLET_3_CLIENT_ID"

# Configure experiment with CLIENT_IDs
curl -X POST http://localhost:7070/fl/experiments \
  -H "Content-Type: application/json" \
  -d "{
    \"experiment_id\": \"exp-r-$(date +%s)\",
    \"round_id\": \"r-$(date +%s)\",
    \"model_ref\": \"fl/models/global_model_v0\",
    \"participants\": [\"$PROPLET_CLIENT_ID\", \"$PROPLET_2_CLIENT_ID\", \"$PROPLET_3_CLIENT_ID\"],
    \"hyperparams\": {\"epochs\": 1, \"lr\": 0.01, \"batch_size\": 16},
    \"k_of_n\": 3,
    \"timeout_s\": 60,
    \"task_wasm_image\": \"oci://example/fl-client-wasm:latest\"
  }"
```

> **CRITICAL**: The `participants` array **MUST** use SuperMQ CLIENT_IDs (UUIDs from your `docker/.env` file), **NOT** instance IDs like `"proplet-1"`, `"proplet-2"`, `"proplet-3"`.
>
> - **Correct**: `"participants": ["3fe95a65-74f1-4ede-bf20-ef565f04cecb", "1f074cd1-4e22-4e21-92ca-e35a21d3ce29", "0d89e6d7-6410-40b5-bcda-07b0217796b8"]`
> - **Wrong**: `"participants": ["proplet-1", "proplet-2", "proplet-3"]`
>
> If you use instance IDs, you'll see errors like `"skipping participant: proplet not found"` in the manager logs. The test scripts (`test-fl-http.py`, `demo.go`) automatically read CLIENT_IDs from `docker/.env` if environment variables aren't set.

### Option C: Using Python Test Script (Recommended)

For a complete end-to-end test, use the Python script which handles the correct API format:

From the repository root:

```bash
# Option 1: Export from docker/.env first (recommended)
export PROPLET_CLIENT_ID=$(grep '^PROPLET_CLIENT_ID=' docker/.env | grep -v '=""' | tail -1 | cut -d '=' -f2 | tr -d '"')
export PROPLET_2_CLIENT_ID=$(grep '^PROPLET_2_CLIENT_ID=' docker/.env | cut -d '=' -f2 | tr -d '"')
export PROPLET_3_CLIENT_ID=$(grep '^PROPLET_3_CLIENT_ID=' docker/.env | cut -d '=' -f2 | tr -d '"')
(cd examples/fl-demo && python3 test-fl-http.py)

# Option 2: Script will auto-read from docker/.env if env vars not set
(cd examples/fl-demo && python3 test-fl-http.py)
```

> **Note**: The script automatically reads `PROPLET_CLIENT_ID`, `PROPLET_2_CLIENT_ID`, and `PROPLET_3_CLIENT_ID` from environment variables or directly from `docker/.env` if not set. These must be SuperMQ CLIENT_IDs (UUIDs), not instance IDs.

This script:

- Verifies all services are running
- Creates an initial model if needed
- Configures an experiment via the Manager API
- Simulates client training and updates
- Waits for aggregation
- Verifies the aggregated model

### Verifying ConfigureExperiment Success

After configuring an experiment, verify it worked correctly:

```bash
# Check manager logs - should see "launched task" messages with UUID proplet_ids
docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env logs manager | grep "launched task"

# Expected output (with UUIDs, not "proplet-1"):
# {"level":"INFO","msg":"launched task for FL round participant","proplet_id":"3fe95a65-74f1-4ede-bf20-ef565f04cecb",...}
# {"level":"INFO","msg":"launched task for FL round participant","proplet_id":"1f074cd1-4e22-4e21-92ca-e35a21d3ce29",...}
# {"level":"INFO","msg":"launched task for FL round participant","proplet_id":"0d89e6d7-6410-40b5-bcda-07b0217796b8",...}

# If you see warnings like "skipping participant: proplet not found" with "proplet-1", 
# it means you used instance IDs instead of CLIENT_IDs - see Troubleshooting section.
```

## Step 8: Monitor Round Progress

### View Logs

From the repository root:

```bash
# Coordinator logs (shows aggregation progress)
docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env logs -f coordinator-http

# Manager logs (shows task distribution)
docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env logs -f manager

# Proplet logs (shows training execution)
docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env logs -f proplet

# All proplet instances
docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env logs -f proplet proplet-2 proplet-3
```

### Filter Logs by Round ID

To filter logs for a specific round (e.g., `r-1769006783`):

```bash
# Set the round ID
ROUND_ID="r-1769006783"

# Filter proplet logs by round ID
docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env logs proplet proplet-2 proplet-3 | grep "$ROUND_ID"

# Filter manager logs by round ID
docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env logs manager | grep "$ROUND_ID"

# Filter coordinator logs by round ID
docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env logs coordinator-http | grep "$ROUND_ID"

# Filter all FL-related services by round ID
docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env logs manager coordinator-http proplet proplet-2 proplet-3 | grep "$ROUND_ID"

# Follow logs filtered by round ID (all proplets)
docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env logs -f proplet proplet-2 proplet-3 | grep --line-buffered "$ROUND_ID"
```

> **Note**: 
> - Use service names (e.g., `manager`, `proplet`, `coordinator-http`), not container names, when using `docker compose logs`.
> - The `--line-buffered` flag with `grep` ensures real-time output when following logs with `-f`.
> - Round IDs are typically in format `r-<timestamp>` (e.g., `r-1769006783`).

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
3. **Verify channel ID**: Ensure `MANAGER_CHANNEL_ID` and `PROPLET_CHANNEL_ID` match the new channel ID from provisioning
4. **Restart services**: Restart manager and proplets after provisioning:
   ```bash
   docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env restart manager proplet proplet-2 proplet-3
   ```
5. **Check logs**:
   ```bash
   docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env logs manager
   ```
6. **Verify MQTT adapter is running**:
   ```bash
   docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env ps mqtt-adapter
   ```

### Coordinator Connection Refused

If you see `"connection refused"` when manager tries to connect to coordinator:

1. **Rebuild coordinator** (if you just updated the code):
   ```bash
   docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env build coordinator-http
   docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env up -d coordinator-http
   ```
2. **Check if coordinator is running**:
   ```bash
   docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env ps coordinator-http
   ```
3. **Check coordinator logs**:
   ```bash
   docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env logs coordinator-http --tail 50
   ```
4. **Verify coordinator MQTT credentials**: Ensure `COORDINATOR_CLIENT_ID` and `COORDINATOR_CLIENT_KEY` are set in `compose.yaml` (updated by provisioning script)
5. **Restart coordinator if needed**:
   ```bash
   docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env restart coordinator-http
   ```
6. **Verify coordinator health** (external port is 8086):
   ```bash
   curl http://localhost:8086/health
   ```

### Proplet Using Old Channel ID

If proplet logs show the old channel ID (`f8abb6ef-0cb9-4171-91d8-6d899ae5fe9f`) in MQTT topics instead of the new one:

1. **Verify compose.yaml has new channel ID**: Check that `PROPLET_CHANNEL_ID` matches provisioning output (should be `20e82209-7913-434c-8966-ebae03759997`)
2. **Restart all proplet instances** to pick up new channel ID:
   ```bash
   docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env restart proplet proplet-2 proplet-3
   ```
3. **Verify new channel ID is being used**: Check logs for the new channel ID in topic names:
   ```bash
   docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env logs proplet | grep -i "subscribe\|channel" | head -5
   ```
   The topic should contain the new channel ID: `m/.../c/20e82209-7913-434c-8966-ebae03759997/...`

### Manager Health Endpoint Not Responding

1. **Check if manager is running**:
   ```bash
   docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env ps manager
   ```
2. **Check manager logs for errors**:
   ```bash
   docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env logs manager --tail 50
   ```
3. **Verify port 7070 is exposed**: Check that the manager service has ports configured in `compose.yaml`
4. **Restart manager**:
   ```bash
   docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env restart manager
   ```

### Services Not Starting

1. **Check ports**: Ensure ports 1883, 7070, 8080, 8083, 8084, 8085 are not in use
2. **Check .env file**: Verify `docker/.env` exists and has required variables
3. **Check logs**: `docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env logs`

### Round Not Starting

1. **Verify model exists**: `curl http://localhost:8084/models/0`
2. **Check manager is running**: `curl http://localhost:7070/health`
3. **Check proplets are running**: `docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env ps | grep proplet`
4. **Check MQTT connectivity**: Verify nginx is exposing MQTT port 1883

### "Skipping participant: proplet not found" Error

If you see this error in manager logs:
```
{"level":"WARN","msg":"skipping participant: proplet not found","proplet_id":"proplet-1","error":"not found"}
```

**Root Cause**: The `participants` array in ConfigureExperiment is using instance IDs (`"proplet-1"`, `"proplet-2"`, `"proplet-3"`) instead of SuperMQ CLIENT_IDs (UUIDs).

**Solution**:

1. **Verify your `docker/.env` file has CLIENT_IDs**:
   ```bash
   grep -E '^(PROPLET_CLIENT_ID|PROPLET_2_CLIENT_ID|PROPLET_3_CLIENT_ID)=' docker/.env
   ```
   Should show UUIDs like:
   ```
   PROPLET_CLIENT_ID=3fe95a65-74f1-4ede-bf20-ef565f04cecb
   PROPLET_2_CLIENT_ID=1f074cd1-4e22-4e21-92ca-e35a21d3ce29
   PROPLET_3_CLIENT_ID=0d89e6d7-6410-40b5-bcda-07b0217796b8
   ```

2. **Export CLIENT_IDs before calling ConfigureExperiment**:
   ```bash
   export PROPLET_CLIENT_ID=$(grep '^PROPLET_CLIENT_ID=' docker/.env | grep -v '=""' | tail -1 | cut -d '=' -f2 | tr -d '"')
   export PROPLET_2_CLIENT_ID=$(grep '^PROPLET_2_CLIENT_ID=' docker/.env | cut -d '=' -f2 | tr -d '"')
   export PROPLET_3_CLIENT_ID=$(grep '^PROPLET_3_CLIENT_ID=' docker/.env | cut -d '=' -f2 | tr -d '"')
   ```

3. **Verify success**: Check manager logs for:
   ```
   {"level":"INFO","msg":"launched task for FL round participant","proplet_id":"3fe95a65-74f1-4ede-bf20-ef565f04cecb",...}
   ```
   You should see 3 "launched task" messages with UUID proplet_ids, not warnings about "proplet not found".

**Why this happens**: Proplets register themselves with their SuperMQ CLIENT_ID (UUID), not their instance ID. The manager looks up proplets by the ID they registered with, so you must use CLIENT_IDs in the participants array.

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
