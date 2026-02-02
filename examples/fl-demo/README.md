# Production FL Demo - Quick Start Guide

This guide covers running the production-grade Federated Learning (FL) demo application using the full SuperMQ stack.

> **IMPORTANT**: This document contains placeholders (e.g., `YOUR_GITHUB_USERNAME`, `ghp_xxxxx`, UUID placeholders) that you must replace with your own values. Please search for and update all placeholders before following the instructions.

## Prerequisites

- Docker and Docker Compose
- Python 3 (for provisioning script)
- `docker/.env` file with SuperMQ configuration
- GitHub account with Personal Access Token (PAT) for GHCR (if using GHCR for WASM storage)

## Quick Start Overview

> **Important**: Some steps are **one-time setup** and only need to be done once (unless you remove Docker volumes). These are marked as **[ONE-TIME]**. Steps without this marker should be repeated as needed.

For a complete end-to-end test, follow these steps in order:

1. **[Build and Push WASM Client](#step-1-build-and-push-wasm-client)** - Build the FL client WASM and push to GHCR
2. **[Configure GHCR Authentication [ONE-TIME]](#step-2-configure-ghcr-authentication-one-time)** - Set up authentication for private GHCR packages
3. **[Build and Start Services](#step-3-build-and-start-services)** - Build Docker images and start all services
4. **[Provision SuperMQ Resources [ONE-TIME]](#step-4-provision-supermq-resources-one-time)** - Create domain, channel, and clients (one-time setup)
5. **[Recreate Services After Provisioning](#step-5-recreate-services-after-provisioning)** - Recreate services to pick up new credentials
6. **[Initialize Model Registry](#step-6-initialize-model-registry)** - Create initial model (version 0)
7. **[Run FL Round](#step-7-trigger-a-federated-learning-round)** - Configure and start a federated learning round
8. **[Verify Round Execution](#step-8-verify-round-execution)** - Check that the round completed successfully
9. **[Monitor Progress](#step-9-monitor-round-progress)** - View logs and check results

For automated testing, use the [Complete Test Script](#complete-test-script) at the end of this document.

## Understanding CLIENT_IDs vs Instance IDs

**IMPORTANT**: When configuring FL experiments, you must use **SuperMQ CLIENT_IDs** (UUIDs), not instance IDs.

- **Instance IDs**: `"proplet-1"`, `"proplet-2"`, `"proplet-3"` - These are just labels for identification
- **CLIENT_IDs**: `"3fe95a65-74f1-4ede-bf20-ef565f04cecb"` - These are the actual SuperMQ client credentials that proplets use to register with the manager

Proplets register themselves using their CLIENT_ID (from `PROPLET_CLIENT_ID`, `PROPLET_2_CLIENT_ID`, `PROPLET_3_CLIENT_ID` in your `docker/.env` file). The manager tracks proplets by these CLIENT_IDs.

For example, your `docker/.env` file should contain:

```bash
PROPLET_CLIENT_ID=3fe95a65-74f1-4ede-bf20-ef565f04cecb      # For proplet-1
PROPLET_2_CLIENT_ID=1f074cd1-4e22-4e21-92ca-e35a21d3ce29    # For proplet-2
PROPLET_3_CLIENT_ID=0d89e6d7-6410-40b5-bcda-07b0217796b8   # For proplet-3
```

## Step 1: Build and Push WASM Client

**Repeat when**: Code changes or first time setup.

### Build WASM Binary

From the repository root:

```bash
cd examples/fl-demo/client-wasm
GOOS=wasip1 GOARCH=wasm go build -o fl-client.wasm fl-client.go
cd ../../..
```

### Push to GitHub Container Registry (GHCR) - Recommended

#### Option 1: Using Docker Login (Recommended)

```bash
# 1. Login to GHCR (you'll need a GitHub PAT with 'write:packages' permission)
#    Create PAT at: https://github.com/settings/tokens
#    Select 'write:packages' scope
echo $GITHUB_TOKEN | docker login ghcr.io -u YOUR_GITHUB_USERNAME --password-stdin

# 2. Push to GHCR using ORAS (from repository root)
docker run --rm \
  -v "$(pwd)/examples/fl-demo/client-wasm:/workspace" \
  -w /workspace \
  -v "$HOME/.docker/config.json:/root/.docker/config.json:ro" \
  ghcr.io/oras-project/oras:v1.3.0 \
  push ghcr.io/YOUR_GITHUB_USERNAME/fl-client-wasm:latest \
  fl-client.wasm:application/wasm
```

Replace `YOUR_GITHUB_USERNAME` with your GitHub username (lowercase). After pushing, you should see:

```text
Pushed [registry] ghcr.io/YOUR_GITHUB_USERNAME/fl-client-wasm:latest
ArtifactType: application/vnd.unknown.artifact.v1
Digest: sha256:...
```

#### Option 2: Using Environment Variables

```bash
# Set your GitHub token
export GITHUB_TOKEN=ghp_xxxxx

# Push using ORAS with environment variables
docker run --rm \
  -v $(pwd)/examples/fl-demo/client-wasm:/workspace \
  -w /workspace \
  -e ORAS_USER=YOUR_GITHUB_USERNAME \
  -e ORAS_PASSWORD=$GITHUB_TOKEN \
  ghcr.io/oras-project/oras:v1.3.0 \
  push ghcr.io/YOUR_GITHUB_USERNAME/fl-client-wasm:latest \
  fl-client.wasm:application/wasm
```

### Push to Local Registry (Alternative)

If you prefer using a local registry instead of GHCR:

```bash
# Push WASM to local registry using ORAS
docker run --rm \
  -v $(pwd)/examples/fl-demo/client-wasm:/workspace \
  -w /workspace \
  --network host \
  ghcr.io/oras-project/oras:latest \
  push localhost:5000/fl-client-wasm:latest \
  fl-client.wasm:application/wasm

# Verify it's there
docker run --rm \
  --network host \
  ghcr.io/oras-project/oras:latest \
  manifest fetch localhost:5000/fl-client-wasm:latest
```

**Important**: When configuring experiments, use `local-registry:5000/fl-client-wasm:latest` (the Docker service name) as your `task_wasm_image`, not `localhost:5000`. The proxy service runs inside Docker and needs to use the service name to reach the registry.

## Step 2: Configure GHCR Authentication [ONE-TIME]

**One-time setup**: This only needs to be done once, unless you change your GitHub credentials.

**IMPORTANT**: GHCR packages are **private by default**, so you **must** configure authentication. If you see `403 Forbidden` errors in proxy logs, authentication is missing or incorrect.

Add to your `docker/.env` file:

```bash
# GHCR Authentication for Proxy (REQUIRED for private packages)
PROXY_AUTHENTICATE=true
PROXY_REGISTRY_URL=ghcr.io
PROXY_REGISTRY_USERNAME=YOUR_GITHUB_USERNAME  # Replace with your GitHub username (lowercase, case-sensitive!)
PROXY_REGISTRY_PASSWORD=ghp_xxxxx  # Replace with your GitHub PAT token with read:packages permission
```

**To create a GitHub PAT:**

1. Go to: <https://github.com/settings/tokens>
2. Click "Generate new token (classic)"
3. Select `read:packages` and `write:packages` scopes
4. Copy the token and use it as `PROXY_REGISTRY_PASSWORD`

**Important Notes**:

- **Username + Password is required for GHCR** - use your GitHub username (lowercase) and a PAT token as the password
- Make sure your GitHub username is **lowercase** (GHCR is case-sensitive)
- Ensure your PAT has `read:packages` permission
- If you see `403 Forbidden` errors, verify:
  - `PROXY_AUTHENTICATE=true` is set
  - Username is lowercase and matches your GitHub username exactly
  - PAT token has `read:packages` permission
  - Repository name matches exactly (case-sensitive)

## Step 3: Build and Start Services

**Repeat when**: Services stopped, system restarted, or code changed.

### Build Images

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

### Start Services

```bash
# Start all services
docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env up -d

# Wait a few seconds for services to start, then verify
docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env ps
```

> **Alternative**: You can combine building and starting in one command:
>
> ```bash
> docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env up -d --build
> ```

This starts:

- Full SuperMQ production stack (Auth, Domains, Clients, Channels, RabbitMQ, NATS, MQTT Adapter, Nginx)
- FL-specific services (Model Registry, Aggregator, Local Data Store, Coordinator)
- Propeller services (Manager, Proplets, Proxy)

## Step 4: Provision SuperMQ Resources [ONE-TIME]

**One-time setup**: This creates persistent data in SuperMQ databases. Only needs to be repeated if you remove Docker volumes (e.g., `docker compose down -v`).

**IMPORTANT**: Before the manager and proplets can connect to MQTT, you must provision the necessary SuperMQ resources (domain, channel, and clients).

> **When to provision**:
>
> - First time setup
> - After `docker compose down -v` (removes volumes)
> - If you want fresh credentials
>
> **When to skip**:
>
> - Services already provisioned and running
> - Just restarting services
> - Running additional FL rounds

### Run Provisioning Script

> **Note**: Python dependencies are listed in `requirements.txt`. Install them with:
> ```bash
> pip install -r examples/fl-demo/requirements.txt
> ```

From the repository root:

```bash
(cd examples/fl-demo && python3 provision-smq.py)
```

The script will:

- Create a domain named "fl-demo"
- Create clients: manager, proplet-1, proplet-2, proplet-3, fl-coordinator, proxy
- Create a channel named "fl"
- Display the client IDs and keys
- Automatically update `docker/.env` with the new client credentials, domain ID, and channel ID (backup created as `docker/.env.bak`)

**Note**: If the domain already exists (route conflict), the script will use the existing domain.

The script updates the following environment variables in `docker/.env`:

- `MANAGER_CLIENT_ID` and `MANAGER_CLIENT_KEY`
- `PROPLET_CLIENT_ID` and `PROPLET_CLIENT_KEY` (for proplet-1)
- `PROPLET_2_CLIENT_ID` and `PROPLET_2_CLIENT_KEY` (for proplet-2)
- `PROPLET_3_CLIENT_ID` and `PROPLET_3_CLIENT_KEY` (for proplet-3)
- `COORDINATOR_CLIENT_ID` and `COORDINATOR_CLIENT_KEY`
- `PROXY_CLIENT_ID` and `PROXY_CLIENT_KEY`
- `MANAGER_DOMAIN_ID`, `PROPLET_DOMAIN_ID`, and `PROXY_DOMAIN_ID`
- `MANAGER_CHANNEL_ID`, `PROPLET_CHANNEL_ID`, and `PROXY_CHANNEL_ID`

**Important**: You must also manually set `PROXY_REGISTRY_URL` in `docker/.env`:

- For GHCR: `PROXY_REGISTRY_URL=ghcr.io`
- For local registry: `PROXY_REGISTRY_URL=http://local-registry:5000`

If the script cannot update the file automatically, you can manually add or update these variables in `docker/.env`.

## Step 5: Recreate Services After Provisioning

**IMPORTANT**: After provisioning (or if you change credentials), you must **recreate** (not just restart) the manager, coordinator, and proplets to pick up the new credentials. The provisioning script updates `docker/.env` with the new client IDs, keys, domain ID, and channel ID. Using `restart` won't pick up new environment variables - you need to recreate the containers.

**Repeat when**: After provisioning or credential changes.

From the repository root:

```bash
# Recreate services to pick up new credentials from .env file
docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env up -d --force-recreate manager coordinator-http proplet proplet-2 proplet-3 proxy

# Verify services are running
docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env ps manager coordinator-http proplet proplet-2 proplet-3 proxy

# Check manager health (wait a few seconds after recreation)
curl http://localhost:7070/health

# Check coordinator health
curl http://localhost:8086/health
```

> **Note**: If services don't start properly, or if containers exited, you need to recreate them:
> ```bash
> docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env up -d --force-recreate manager coordinator-http proplet proplet-2 proplet-3 proxy
> ```
>
> **Important**: After recreating, wait a few seconds for services to start, then verify they're running.

### Verify Services Are Running

```bash
# Check all containers
docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env ps

# Check manager MQTT connection
docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env logs manager | grep -i "connected\|mqtt\|subscribe"

# Verify proplet is using correct channel ID (should show new channel ID, not old one)
docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env logs proplet | grep -i "channel\|subscribe" | head -5
```

> **Note**: If the manager health check fails, check the logs:
> ```bash
> docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env logs manager
> ```
>
> Common issues:
>
> - Manager not connecting to MQTT: Verify credentials match provisioning output
> - Port 7070 not accessible: Ensure manager container is running and port is exposed
> - Coordinator connection refused: Ensure coordinator-http service is running
> - Proplet using old channel ID: Recreate proplet containers to pick up new channel ID from docker/.env

## Step 6: Initialize Model Registry

**Repeat when**: Model registry is empty or you want to reset to initial model.

Before starting a round, ensure the model registry has an initial model:

```bash
# Check if model v0 already exists
if curl -s http://localhost:8084/models/0 > /dev/null; then
    echo "Model v0 already exists, skipping initialization"
else
    # Create initial model (version 0)
    curl -X POST http://localhost:8084/models \
      -H "Content-Type: application/json" \
      -d '{
        "version": 0,
        "model": {
          "w": [0.0, 0.0, 0.0],
          "b": 0.0
        }
      }'
    echo "Initial model created"
fi

# Verify model exists
curl http://localhost:8084/models/0
# Expected: {"w":[0,0,0],"b":0}
```

## Step 7: Trigger a Federated Learning Round

**Repeat for**: Each FL round you want to run.

### Option A: Using HTTP API (Manager) - Recommended

> **Note**: If you get a 404 error, ensure the manager was built from source (see Step 3). You can rebuild and restart:
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

# Configure experiment (replace YOUR_GITHUB_USERNAME with your actual username)
# GHCR repository names are case-sensitive - use the EXACT same name you used when pushing
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
    \"task_wasm_image\": \"ghcr.io/YOUR_GITHUB_USERNAME/fl-client-wasm:latest\"
  }"

# Expected response:
# {"experiment_id":"exp-r-...","round_id":"r-...","status":"configured"}
```

### Option B: Using MQTT (via nginx)

Publish a round start message to the MQTT topic. **MQTT connections require authentication** using client credentials:

```bash
# Get client credentials from docker/.env or provisioning output
# Use the manager client ID and key, or fl-coordinator client credentials
mosquitto_pub -h localhost -p 1883 \
  -u "<CLIENT_ID>" \
  -P "<CLIENT_KEY>" \
  -t "fl/rounds/start" \
  -m '{
    "round_id": "r-0001",
    "model_uri": "fl/models/global_model_v0",
    "task_wasm_image": "ghcr.io/YOUR_GITHUB_USERNAME/fl-client-wasm:latest",
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
> - Get the current client ID and key from `docker/.env` (MANAGER_CLIENT_ID and MANAGER_CLIENT_KEY) or from the provisioning script output.

## Step 8: Verify Round Execution

**Repeat for**: Each round you run.

After configuring an experiment, verify it worked correctly:

```bash
# Set the round ID from the response above
ROUND_ID="r-1769015984"  # Replace with your actual round ID

# 1. Verify tasks were launched
docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env logs manager | grep "launched task"
# Should show 3 "launched task" messages with UUID proplet_ids

# 2. Verify WASM is being requested from GHCR
docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env logs proplet proplet-2 proplet-3 | grep -i "Requesting binary from registry"
# Should show: "Requesting binary from registry: ghcr.io/YOUR_GITHUB_USERNAME/fl-client-wasm:latest"

# 3. Verify proxy fetched and sent chunks
docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env logs proxy | grep -i "sent container chunk\|successfully sent all chunks"
# Should show chunk sending messages

# 4. Verify WASM execution
docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env logs proplet proplet-2 proplet-3 | grep -i "Executing task\|Starting Host runtime\|Task.*completed successfully"
# Should show execution and completion messages

# 5. Verify coordinator received updates
docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env logs coordinator-http | grep "$ROUND_ID"
# Should show:
# - "Received experiment configuration"
# - "Received update" (3 times)
# - "Round complete: received k_of_n updates"
# - "Aggregated model stored"

# 6. Verify aggregated model
curl http://localhost:8084/models/1
# Should return the aggregated model with updated weights
```

### Expected Results

After a successful round, you should see:

1. **Manager logs**: 3 "launched task" messages with UUID proplet_ids
2. **Proxy logs**: "successfully sent all chunks" for the WASM binary
3. **Proplet logs**: "Task ... completed successfully" with training results
4. **Coordinator logs**: "Round complete: received k_of_n updates" and "Aggregated model stored"
5. **Model Registry**: New model version 1 with aggregated weights

### Verifying Weight Updates (w)

The FL client implements logistic regression training that should update both weights `w` and bias `b`. To verify that weights are being updated correctly:

```bash
# 1. Check initial model (should have w=[0,0,0] and b=0)
curl http://localhost:8084/models/0
# Expected: {"w":[0,0,0],"b":0}

# 2. After running a round, check the aggregated model
curl http://localhost:8084/models/1
# Expected: {"w":[<non-zero>,<non-zero>,<non-zero>],"b":<non-zero>}
# The w values should NOT all be zero after training

# 3. Check proplet logs for debug output showing weight updates
docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env logs proplet proplet-2 proplet-3 | grep "DEBUG:"
# Should show:
# - "DEBUG: Initial model - w: [0.000000, 0.000000, 0.000000], b: 0.000000"
# - "DEBUG: First sample - x: [<values>], y: <0 or 1>"
# - "DEBUG: Final model after training - w: [<non-zero>,<non-zero>,<non-zero>], b: <non-zero>"

# 4. Verify the update payload contains non-zero weights
docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env logs proplet proplet-2 proplet-3 | grep "DEBUG: Update payload"
# The update payload should show w values that are not all zeros
```

**Troubleshooting**: If `w` remains `[0,0,0]` after training:

- Check proplet logs for "DEBUG:" messages to see if weights are being updated during training
- Verify that datasets contain valid `x` and `y` values (see "Verifying Step 5: Dataset Loading")
- Ensure the wasm client was rebuilt after code changes: `cd examples/fl-demo/client-wasm && GOOS=wasip1 GOARCH=wasm go build -o fl-client.wasm fl-client.go`

## Verifying Step 5: Dataset Loading from Local Data Store

**Purpose**: Verify that proplets successfully fetch datasets from Local Data Store (Step 5 in the sequence diagram) instead of falling back to synthetic data.

### Prerequisites

Before running an FL round, ensure that:

1. Local Data Store service is running
2. Participant UUIDs are set in `docker/.env` (PROPLET_CLIENT_ID, PROPLET_2_CLIENT_ID, PROPLET_3_CLIENT_ID)
3. Datasets have been auto-seeded on Local Data Store startup

### Verify Datasets Are Available

Check that datasets exist for each participant UUID:

```bash
# Extract participant UUIDs from docker/.env
export PROPLET_CLIENT_ID=$(grep '^PROPLET_CLIENT_ID=' docker/.env | grep -v '=""' | tail -1 | cut -d '=' -f2 | tr -d '"')
export PROPLET_2_CLIENT_ID=$(grep '^PROPLET_2_CLIENT_ID=' docker/.env | cut -d '=' -f2 | tr -d '"')
export PROPLET_3_CLIENT_ID=$(grep '^PROPLET_3_CLIENT_ID=' docker/.env | cut -d '=' -f2 | tr -d '"')

# Verify each dataset is accessible
echo "Checking dataset for proplet-1 (UUID: $PROPLET_CLIENT_ID)..."
curl -s http://localhost:8083/datasets/$PROPLET_CLIENT_ID | jq '.schema, .proplet_id, .size' || echo "ERROR: Dataset not found"

echo "Checking dataset for proplet-2 (UUID: $PROPLET_2_CLIENT_ID)..."
curl -s http://localhost:8083/datasets/$PROPLET_2_CLIENT_ID | jq '.schema, .proplet_id, .size' || echo "ERROR: Dataset not found"

echo "Checking dataset for proplet-3 (UUID: $PROPLET_3_CLIENT_ID)..."
curl -s http://localhost:8083/datasets/$PROPLET_3_CLIENT_ID | jq '.schema, .proplet_id, .size' || echo "ERROR: Dataset not found"
```

**Expected output**: Each curl should return a JSON object with:

- `"schema": "fl-demo-dataset-v1"`
- `"proplet_id": "<uuid>"`
- `"size": 64` (or similar number of samples)

### Verify Dataset Loading During FL Round

After starting an FL round, check proplet logs to confirm datasets are being fetched:

```bash
# Check proplet-1 logs for successful dataset fetch
docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env logs proplet | grep -i "dataset"

# Check all proplet logs
docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env logs proplet proplet-2 proplet-3 | grep -i "dataset"
```

**Success indicators** (should see these messages):

- `"Fetching dataset from Local Data Store: http://local-data-store:8083/datasets/<uuid>"`
- `"Successfully fetched dataset with <N> samples and passed to client"`
- `"Loaded dataset with <N> samples from Local Data Store"` (in WASM client logs)

**Failure indicators** (should NOT see these):

- `"HTTP 404 Not Found (will use synthetic data)"`
- `"Failed to fetch dataset from Local Data Store"`
- `"DATASET_DATA not available, using synthetic data"`

### Verify Local Data Store Logs

Check Local Data Store service logs to confirm datasets are being served:

```bash
# Check Local Data Store logs
docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env logs local-data-store | grep -i "dataset"
```

**Expected logs**:

- `"Dataset seeded"` (on startup, for each participant UUID)
- `"Dataset served"` (when a proplet requests a dataset)
- `"Dataset missing"` should NOT appear for participant UUIDs

### Troubleshooting

If datasets are missing:

1. **Check environment variables**: Ensure `PROPLET_CLIENT_ID`, `PROPLET_2_CLIENT_ID`, `PROPLET_3_CLIENT_ID` are set in `docker/.env` and passed to `local-data-store` service.

2. **Restart Local Data Store**: Datasets are auto-seeded on startup:

   ```bash
   docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env restart local-data-store
   docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env logs local-data-store | grep -i "seeded"
   ```

3. **Manually verify dataset files**: Check that dataset files exist in the volume:

   ```bash
   docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env exec local-data-store ls -la /data/datasets/
   ```

4. **Check dataset format**: Verify the dataset JSON structure:

   ```bash
   docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env exec local-data-store cat /data/datasets/$PROPLET_CLIENT_ID.json | jq '.schema, .proplet_id, .size'
   ```

### Dataset Format

The Local Data Store returns datasets in the following format:

```json
{
  "schema": "fl-demo-dataset-v1",
  "proplet_id": "<uuid>",
  "data": [
    {"x": [0.1, 0.2, 0.3], "y": 0},
    {"x": [0.4, 0.5, 0.6], "y": 1},
    ...
  ],
  "size": 64
}
```

This format is compatible with the WASM client which expects:

- `data`: Array of samples, each with `x` (feature vector) and `y` (label)
- `size`: Number of samples (optional, can be inferred from data length)

## Step 9: Monitor Round Progress

**Optional**: Use these commands to monitor round progress in real-time.

### View Logs

From the repository root:

```bash
# Coordinator logs (shows aggregation progress)
docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env logs -f coordinator-http

# Manager logs (shows task distribution)
docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env logs -f manager

# Proplet logs (shows training execution)
docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env logs -f proplet proplet-2 proplet-3

# Proxy logs (shows WASM fetching)
docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env logs -f proxy
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
>
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

## Complete Test Script

For automated testing, a complete script is available at `examples/fl-demo/test-e2e.sh`. Run it:

```bash
./examples/fl-demo/test-e2e.sh
```

> **Note**: The test script assumes provisioning (Step 4) has already been done. If you need to provision, run the provisioning step manually first, or the script will fail when checking for services.

The script:

- Verifies prerequisites
- Builds WASM if needed
- Checks services are running
- Creates initial model if needed
- Exports CLIENT_IDs
- Configures and runs an experiment
- Waits for round completion
- Verifies results

## Quick Reference: What to Repeat

**One-time setup** ([ONE-TIME] - do once, persists in Docker volumes):

- **Step 2**: Configure GHCR Authentication - Only needed once unless credentials change
- **Step 4**: Provision SuperMQ Resources - Only needed once unless you run `docker compose down -v` (removes volumes)

**Runtime steps** (repeat for each test/round):

- **Step 1**: Build and Push WASM Client - Only if code changed
- **Step 3**: Build and Start Services - Only if services stopped or code changed
- **Step 5**: Recreate Services After Provisioning - Only after provisioning or credential changes
- **Step 6**: Initialize Model Registry - Only if registry is empty
- **Step 7**: Run FL Round - Repeat for each round
- **Step 8**: Verify Round Execution - Repeat for each round
- **Step 9**: Monitor Progress - Optional, repeat as needed

**For subsequent rounds** (after initial setup):

1. Ensure services are running (Step 3)
2. Initialize model registry if needed (Step 6)
3. Run FL round (Step 7)
4. Verify execution (Step 8)

**When starting completely fresh** (volumes removed):

- You'll need to repeat all steps, including provisioning (Step 4)

## Troubleshooting

### Manager Not Connecting to MQTT

1. **Verify provisioning completed**: Check that the provisioning script ran successfully
2. **Check credentials**: Ensure client IDs and keys in `docker/.env` match the provisioning output
3. **Verify channel ID**: Ensure `MANAGER_CHANNEL_ID` and `PROPLET_CHANNEL_ID` in `docker/.env` match the new channel ID from provisioning
4. **Recreate services**: Recreate manager and proplets after provisioning to pick up new credentials:

   ```bash
   docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env up -d --force-recreate manager proplet proplet-2 proplet-3
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

4. **Verify coordinator MQTT credentials**: Ensure `COORDINATOR_CLIENT_ID` and `COORDINATOR_CLIENT_KEY` are set in `docker/.env` (updated by provisioning script)
5. **Restart coordinator if needed**:

   ```bash
   docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env restart coordinator-http
   ```

6. **Verify coordinator health** (external port is 8086):

   ```bash
   curl http://localhost:8086/health
   ```

### Proplet Using Old Channel ID

If proplet logs show the old channel ID in MQTT topics instead of the new one:

1. **Verify docker/.env has new channel ID**: Check that `PROPLET_CHANNEL_ID` in `docker/.env` matches provisioning output
2. **Recreate all proplet instances** to pick up new channel ID:

   ```bash
   docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env up -d --force-recreate proplet proplet-2 proplet-3
   ```

3. **Verify new channel ID is being used**: Check logs for the new channel ID in topic names:

   ```bash
   docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env logs proplet | grep -i "subscribe\|channel" | head -5
   ```

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

```json
{"level":"WARN","msg":"skipping participant: proplet not found","proplet_id":"proplet-1","error":"not found"}
```

**Root Cause**: The `participants` array in ConfigureExperiment is using instance IDs (`"proplet-1"`, `"proplet-2"`, `"proplet-3"`) instead of SuperMQ CLIENT_IDs (UUIDs).

**Solution**:

1. **Verify your `docker/.env` file has CLIENT_IDs**:

   ```bash
   grep -E '^(PROPLET_CLIENT_ID|PROPLET_2_CLIENT_ID|PROPLET_3_CLIENT_ID)=' docker/.env
   ```

   Should show UUIDs like:

   ```text
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

   ```json
   {"level":"INFO","msg":"launched task for FL round participant","proplet_id":"3fe95a65-74f1-4ede-bf20-ef565f04cecb",...}
   ```

   You should see 3 "launched task" messages with UUID proplet_ids, not warnings about "proplet not found".

**Why this happens**: Proplets register themselves with their SuperMQ CLIENT_ID (UUID), not their instance ID. The manager looks up proplets by the ID they registered with, so you must use CLIENT_IDs in the participants array.

### "Round timeout exceeded" with 0 updates - WASM Not Executing

If you see this in coordinator logs:

```text
WARN Round timeout exceeded round_id=r-1769011931 timeout_s=60 updates=0
```

And proplet logs show `"Requesting binary from registry: ghcr.io/..."` but no execution, the issue is that **the proxy service is not fetching and serving the WASM binary**.

**Root Cause**: The proxy service must:

1. Be running and connected to MQTT
2. Be subscribed to the `registry/proplet` topic (where proplets request binaries)
3. Have authentication configured for GHCR (**REQUIRED** - GHCR packages are private by default)
4. Fetch the binary from GHCR and chunk it
5. Publish chunks to `registry/server` topic (where proplets receive them)

**Common Issues**:

- **"Channel full, dropping container request"**: Fixed in latest code - channel buffer increased to handle concurrent requests
- **"403 Forbidden"**: Authentication not configured - see Solution #3 below
- **"failed to fetch container"**: Check authentication and repository name (case-sensitive)

**Diagnosis Steps**:

1. **Check if proxy service is running**:

   ```bash
   docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env ps proxy
   ```

   Should show `propeller-proxy` container running.

2. **Check proxy logs for errors**:

   ```bash
   docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env logs proxy --tail 50
   ```

   Look for:

   - Connection errors
   - "failed to fetch container" errors
   - Authentication errors
   - "Received container request" messages (good sign)

3. **Verify proxy is subscribed to registry topic**:

   ```bash
   docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env logs proxy | grep -i "subscribe\|registry"
   ```

   Should show subscription to `registry/proplet` topic.

4. **Check if proxy received binary requests**:

   ```bash
   docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env logs proxy | grep -i "container request\|app_name"
   ```

   Should show requests for `ghcr.io/YOUR_GITHUB_USERNAME/fl-client-wasm:latest`.

5. **Check if proplets are receiving chunks**:

   ```bash
   docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env logs proplet proplet-2 proplet-3 | grep -i "chunk\|assembled\|binary"
   ```

   Should show "Assembled binary" messages if chunks are being received.

**Solution**:

1. **Check for case sensitivity issues**:

   If you see errors like:

   ```text
   "failed to create repository for ghcr.io/YOUR_GITHUB_USERNAME/fl-client-wasm:latest: invalid reference: invalid repository \"YOUR_GITHUB_USERNAME/fl-client-wasm\""
   ```

   This means the repository name in your `task_wasm_image` doesn't match the exact name you used when pushing to GHCR. GHCR repository names are **case-sensitive**.

   **Fix**: Use the **exact same repository name** (including case) that you used when pushing:

   ```bash
   # Example: If you pushed to: ghcr.io/yourusername/fl-client-wasm:latest (lowercase)
   # Then use: ghcr.io/yourusername/fl-client-wasm:latest (same case)
   # NOT: ghcr.io/YourUsername/fl-client-wasm:latest (capitalized)
   ```

   > **Note**: Replace `YOUR_GITHUB_USERNAME` with your actual GitHub username (lowercase). Make sure the case matches exactly what you used when pushing to GHCR.

   To verify what you pushed, check your push command output or your GHCR repository page.

2. **Ensure proxy service is running**:

   ```bash
   docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env up -d proxy
   ```

3. **Configure GHCR authentication** (REQUIRED for private packages):

   See [Step 2: Configure GHCR Authentication](#step-2-configure-ghcr-authentication-one-time) for detailed instructions.

4. **Verify proxy has correct domain/channel IDs**:

   ```bash
   grep -E '^(PROXY_DOMAIN_ID|PROXY_CHANNEL_ID|PROXY_CLIENT_ID|PROXY_CLIENT_KEY)=' docker/.env
   ```

   These should match your manager/proplet domain and channel IDs.

5. **Restart proxy after configuration**:

   ```bash
   docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env stop proxy
   docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env up -d proxy
   ```

   **Note**: Use `up -d` instead of `restart` to ensure environment variables are reloaded.

6. **Verify proxy can access GHCR**:

   Check proxy logs for successful fetches:

   ```bash
   docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env logs proxy | grep -i "sent container chunk\|successfully sent all chunks"
   ```

**Note**: If your GHCR package is **public**, you may not need authentication. However, GHCR packages are private by default, so you'll likely need to configure authentication.

### Proxy Service Configuration Error

If you see `"failed to load TOML configuration: stat config.toml: no such file or directory"` in proxy logs, the proxy service is missing required environment variables.

**Root Cause**: The proxy service requires environment variables to be set. If they're not set, it tries to load from `config.toml` (which doesn't exist), causing it to crash loop.

**Solution**:

1. **Add proxy configuration to `docker/.env`**:

   ```bash
   # Proxy Service Configuration (for WASM binary fetching)
   # Use the same domain and channel as manager/proplet
   # IMPORTANT: Replace these placeholders with actual values from your provisioning output
   PROXY_DOMAIN_ID=<YOUR_DOMAIN_ID>  # Replace with your actual domain ID (UUID format)
   PROXY_CHANNEL_ID=<YOUR_CHANNEL_ID>  # Replace with your actual channel ID (UUID format)

   # Provision a client for proxy (or reuse manager client for testing)
   # Option 1: Use manager client credentials (for quick testing)
   # IMPORTANT: Replace these placeholders with actual values from your provisioning output
   PROXY_CLIENT_ID=<YOUR_PROXY_CLIENT_ID>  # Replace with your actual proxy client ID (UUID format)
   PROXY_CLIENT_KEY=<YOUR_PROXY_CLIENT_KEY>  # Replace with your actual proxy client key (UUID format)

   # Option 2: Provision a separate client for proxy (recommended for production)
   # Run: python3 examples/fl-demo/provision-smq.py
   # Then use the new client ID and key here

   # GHCR Configuration (for fetching WASM binaries)
   PROXY_REGISTRY_URL=ghcr.io
   PROXY_AUTHENTICATE=true
   PROXY_REGISTRY_USERNAME=YOUR_GITHUB_USERNAME  # Replace with your GitHub username (lowercase)
   PROXY_REGISTRY_PASSWORD=ghp_xxxxx  # Replace with your GitHub PAT token

   # Optional: Proxy settings
   PROXY_LOG_LEVEL=info
   PROXY_MQTT_ADDRESS=tcp://mqtt-adapter:1883
   PROXY_CHUNK_SIZE=512000
   ```

   > **Note**: Replace all placeholders (`<YOUR_DOMAIN_ID>`, `<YOUR_CHANNEL_ID>`, `<YOUR_PROXY_CLIENT_ID>`, `<YOUR_PROXY_CLIENT_KEY>`, `YOUR_GITHUB_USERNAME`, `ghp_xxxxx`) with your actual values. These values are typically generated by the provisioning script (Step 4) or can be obtained from your `docker/.env` file after provisioning.

2. **Restart proxy service**:

   ```bash
   docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env restart proxy
   ```

3. **Verify proxy is running**:

   ```bash
   docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env ps proxy
   docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env logs proxy --tail 20
   ```

   Should show successful connection, not TOML errors.

4. **Test proxy can fetch from GHCR**:

   After proplets request a binary, check proxy logs for:

   ```text
   Received container request app_name=ghcr.io/YOUR_GITHUB_USERNAME/fl-client-wasm:latest
   sent container chunk to MQTT stream
   ```

**Quick Fix**: For testing, you can reuse the manager's client credentials for the proxy. For production, provision a separate client for the proxy service.

## Quick Reference

### Service Ports

- **Manager**: 7070
- **Coordinator**: 8080 (internal), 8086 (external)
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

- Manager: <http://localhost:7070/health>
- Coordinator: <http://localhost:8086/health>
- Model Registry: <http://localhost:8084/health>
- Aggregator: <http://localhost:8085/health>
- Local Data Store: <http://localhost:8083/health>

## Architecture Overview

This demo shows how to build Federated Learning on top of Propeller's generic orchestration:

- **Manager**: Generic task launcher (no FL logic)
- **FML Coordinator**: External service that owns FL rounds, aggregation, and model versioning
- **Model Registry**: HTTP file server for model distribution
- **Aggregator**: Service that aggregates client updates using FedAvg
- **Local Data Store**: Service that provides datasets to clients
- **Client Wasm**: Sample FL training workload executed by proplets
- **Proxy**: Service that fetches WASM binaries from container registries and serves them to proplets

The workflow:

1. Round start message triggers manager to launch tasks
2. Proplets request WASM binary from proxy via MQTT
3. Proxy fetches binary from GHCR/local registry and chunks it
4. Proplets receive chunks, assemble binary, and execute Wasm client
5. Proplets perform local training and send updates to coordinator
6. Coordinator aggregates updates when `k_of_n` reached
7. New model is stored and published for next round
