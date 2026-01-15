# Testing Federated Learning Demo

This guide provides step-by-step instructions to test the federated learning demo application.

## Prerequisites

- Docker and Docker Compose installed
- Go 1.21+ installed (for building WASM client)
- Python 3 with `requests` library installed
- All services should be able to communicate on the `fl-demo` network

## Step 1: Build the WASM Client

The federated learning client runs as a WebAssembly module. Build it first:

```bash
cd /home/jeff-mboya/Documents/propeller/examples/fl-demo/client-wasm
GOOS=wasip1 GOARCH=wasm go build -o fl-client.wasm fl-client.go
cd ..
```

**Expected output:** A `fl-client.wasm` file should be created in the `client-wasm/` directory.

**Verify:**
```bash
ls -lh client-wasm/fl-client.wasm
```

## Step 2: Configure MQTT Broker (Optional but Recommended)

To prevent connection drops, update the MQTT configuration:

```bash
cat > mqtt/mosquitto.conf << 'EOF'
listener 1883
allow_anonymous true

# Connection persistence settings to prevent premature disconnects
persistence true
persistence_location /mosquitto/data/

# Keep connections alive longer
keepalive_interval 60
max_connections -1

# Client settings
max_inflight_messages 100
max_queued_messages 1000

# Timeouts
connection_messages true
retry_interval 20

listener 9001
protocol websockets
allow_anonymous true
EOF
```

## Step 3: Start All Services

Start all Docker containers:

```bash
cd /home/jeff-mboya/Documents/propeller/examples/fl-demo
docker compose up -d
```

**Expected output:**
```
[+] Running 8/8
 ✔ Container fl-demo-mqtt-1         Started
 ✔ Container fl-demo-manager-1      Started
 ✔ Container fl-demo-model-server-1 Started
 ✔ Container fl-demo-coordinator-1  Started
 ✔ Container fl-demo-proplet-1-1    Started
 ✔ Container fl-demo-proplet-2-1    Started
 ✔ Container fl-demo-proplet-3-1    Started
```

## Step 4: Verify Services Are Running

Check the status of all containers:

```bash
docker compose ps
```

**Expected output:** All services should show "Up" status. Proplets may show "health: starting" initially, which is normal.

**Verify manager is accessible:**
```bash
curl http://localhost:7070/health
```

**Expected response:** Should return a health status (may take a few seconds after startup) like below:

```bash
{"status":"pass","version":"v0.3.0","commit":"22541f09d2d2fdda32f94b0322b0f4b96b276e92","description":"manager service","build_time":"2026-01-12T05:58:08Z","instance_id":"7b14279e-b01f-4108-b257-2ecb86b76576"}

```

## Step 5: Check Service Logs (Optional)

Verify services are connecting properly:

```bash
# Check coordinator (should show MQTT connection)
docker compose logs coordinator | tail -10

# Check manager (should show HTTP server listening)
docker compose logs manager | tail -10

# Check a proplet (should show MQTT connection)
docker compose logs proplet-1 | tail -10
```

**Expected logs:**
- Coordinator: `FML Coordinator connected to MQTT broker`
- Manager: `manager service http server listening at localhost:7070`
- Proplet: Should show MQTT connection (may have initial connection attempts)

## Step 6: Install Python Dependencies (If Needed)

The test script requires the `requests` library:

```bash
pip3 install requests
```

Or if using a virtual environment:
```bash
python3 -m venv venv
source venv/bin/activate
pip install requests
```

## Step 7: Run the Federated Learning Test

Execute the test script to start a federated learning round:

```bash
cd /home/jeff-mboya/Documents/propeller/examples/fl-demo
python3 test-fl-local.py
```

**Expected output:**
```
Reading WASM file: client-wasm/fl-client.wasm
WASM file encoded: 4279132 characters

Creating tasks for round: r-1768464194
Participants: ['proplet-1', 'proplet-2', 'proplet-3']

Creating task for proplet-1...
  Task created: <task-id-1>
  Starting task...
  Task started successfully

Creating task for proplet-2...
  Task created: <task-id-2>
  Starting task...
  Task started successfully

Creating task for proplet-3...
  Task created: <task-id-3>
  Starting task...
  Task started successfully

✅ Successfully launched 3 tasks

Monitor progress:
  docker compose logs -f coordinator
  docker compose logs -f manager
  docker compose logs -f proplet-1
```

## Step 8: Monitor Federated Learning Progress

### Watch Coordinator (Aggregation)
The coordinator aggregates model updates from all proplets:

```bash
docker compose logs -f coordinator
```

**What to look for:**
- `Received FL update from proplet-1`
- `Received FL update from proplet-2`
- `Received FL update from proplet-3`
- `Aggregating updates for round r-...`
- `Published aggregated model: fl/models/global_model_v1`

### Watch Manager (Orchestration)
The manager orchestrates task distribution:

```bash
docker compose logs -f manager
```

**What to look for:**
- `launched task for FL round participant`
- Task creation and start messages

### Watch Proplet (Training Execution)
Watch a proplet executing the training:

```bash
docker compose logs -f proplet-1
```

**What to look for:**
- Task start messages
- WASM execution logs
- FL update publication messages

### Watch All Services Together
```bash
docker compose logs -f
```

Press `Ctrl+C` to stop watching.

## Step 9: Verify Results

### Check Aggregated Models
The model server stores aggregated models:

```bash
docker compose exec model-server ls -la /tmp/fl-models/
```

**Expected output:** Should show model files like `global_model_v0.json`, `global_model_v1.json`, etc.

### View Model Contents
```bash
docker compose exec model-server cat /tmp/fl-models/global_model_v1.json
```

**Expected output:** JSON containing aggregated model weights.

### Check Task Status via Manager API
```bash
curl http://localhost:7070/tasks
```

This returns a list of all tasks with their status.

## Step 10: Clean Up (Optional)

To stop all services:

```bash
docker compose down
```

To stop and remove volumes (clears all data):

```bash
docker compose down -v
```

## Troubleshooting

### Issue: Manager not accessible on port 7070

**Solution:**
1. Check if port is exposed in `compose.yaml`:
   ```bash
   grep -A 5 "manager:" compose.yaml | grep "ports"
   ```
2. Restart manager:
   ```bash
   docker compose restart manager
   ```
3. Wait a few seconds and try again:
   ```bash
   curl http://localhost:7070/health
   ```

### Issue: Proplets showing "unhealthy"

**Solution:**
The healthcheck has been disabled in the compose file. If you still see this:
1. Restart proplets:
   ```bash
   docker compose restart proplet-1 proplet-2 proplet-3
   ```
2. Check logs for actual errors:
   ```bash
   docker compose logs proplet-1
   ```

### Issue: MQTT connection errors

**Solution:**
1. Verify MQTT broker is running:
   ```bash
   docker compose ps mqtt
   ```
2. Check MQTT logs:
   ```bash
   docker compose logs mqtt
   ```
3. Verify network connectivity:
   ```bash
   docker compose exec proplet-1 getent hosts mqtt
   ```
   Should return: `172.x.x.x mqtt`

### Issue: "Connection refused" when running test script

**Solution:**
1. Wait for manager to fully start (may take 10-15 seconds)
2. Verify manager is running:
   ```bash
   docker compose ps manager
   ```
3. Check manager logs:
   ```bash
   docker compose logs manager | grep listening
   ```
4. Try the health endpoint:
   ```bash
   curl http://localhost:7070/health
   ```

### Issue: Tasks not starting

**Solution:**
1. Check if proplets are alive:
   ```bash
   curl http://localhost:7070/proplets
   ```
2. Verify proplets are connected to MQTT:
   ```bash
   docker compose logs proplet-1 | grep -i "connected\|mqtt"
   ```
3. Check manager logs for errors:
   ```bash
   docker compose logs manager | grep -i error
   ```

### Issue: No updates received by coordinator

**Solution:**
1. Verify coordinator is subscribed:
   ```bash
   docker compose logs coordinator | grep -i "subscribed"
   ```
2. Check if proplets are publishing updates:
   ```bash
   docker compose logs proplet-1 | grep -i "update\|fl"
   ```
3. Verify MQTT topic structure matches expectations

## Expected Workflow

1. **Task Creation**: Manager creates tasks for each proplet via HTTP API
2. **Task Distribution**: Manager publishes start commands to MQTT
3. **WASM Execution**: Each proplet receives and executes the WASM module
4. **Local Training**: WASM module performs local training
5. **Update Publication**: Proplets publish updates to `fl/rounds/{round_id}/updates/{proplet_id}`
6. **Update Forwarding**: Manager forwards updates to `fml/updates`
7. **Aggregation**: Coordinator aggregates when `k_of_n` updates are received
8. **Model Distribution**: New aggregated model published to `fl/models/global_model_v{N}`

## Testing Multiple Rounds

To test multiple FL rounds, simply run the test script again:

```bash
python3 test-fl-local.py
```

Each run creates a new round with a unique round ID (timestamp-based).

## Advanced: Manual Testing via MQTT

You can also trigger rounds manually via MQTT (requires OCI registry for WASM):

```bash
mosquitto_pub -h localhost -t "fl/rounds/start" -m '{
  "round_id": "r-manual-001",
  "model_uri": "fl/models/global_model_v0",
  "task_wasm_image": "oci://example/fl-client-wasm:latest",
  "participants": ["proplet-1", "proplet-2", "proplet-3"],
  "hyperparams": {"epochs": 1, "lr": 0.01, "batch_size": 16},
  "k_of_n": 3,
  "timeout_s": 30
}'
```

## Next Steps

- Modify `client-wasm/fl-client.go` to implement your own training logic
- Adjust hyperparameters in the test script
- Add more proplets by duplicating the proplet service in `compose.yaml`
- Integrate with your own model storage backend
