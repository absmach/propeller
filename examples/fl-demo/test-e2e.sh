#!/bin/bash
# Complete FL End-to-End Test Script
# Run from repository root

set -e  # Exit on error

echo "=== FL End-to-End Test ==="
echo ""

# Step 1: Verify prerequisites
echo "Step 1: Checking prerequisites..."
if [ ! -f "docker/.env" ]; then
    echo "ERROR: docker/.env file not found"
    exit 1
fi

# Step 2: Build and push WASM (if not already done)
echo "Step 2: Building WASM client..."
cd examples/fl-demo/client-wasm
if [ ! -f "fl-client.wasm" ]; then
    GOOS=wasip1 GOARCH=wasm go build -o fl-client.wasm fl-client.go
    echo "✓ WASM built successfully"
else
    echo "✓ WASM already built"
fi
cd ../../..

# Step 3: Check services are running
echo "Step 3: Checking services..."
docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env ps | grep -q "Up" || {
    echo "ERROR: Services not running. Start them first with:"
    echo "  docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env up -d"
    exit 1
}
echo "✓ Services are running"

# Step 4: Verify model registry has initial model
echo "Step 4: Checking model registry..."
if ! curl -s http://localhost:8084/models/0 > /dev/null; then
    echo "Creating initial model..."
    curl -X POST http://localhost:8084/models \
      -H "Content-Type: application/json" \
      -d '{"version": 0, "model": {"w": [0.0, 0.0, 0.0], "b": 0.0}}' > /dev/null
    echo "✓ Initial model created"
else
    echo "✓ Initial model exists"
fi

# Step 5: Export CLIENT_IDs
echo "Step 5: Exporting CLIENT_IDs..."
export PROPLET_CLIENT_ID=$(grep '^PROPLET_CLIENT_ID=' docker/.env | grep -v '=""' | tail -1 | cut -d '=' -f2 | tr -d '"')
export PROPLET_2_CLIENT_ID=$(grep '^PROPLET_2_CLIENT_ID=' docker/.env | cut -d '=' -f2 | tr -d '"')
export PROPLET_3_CLIENT_ID=$(grep '^PROPLET_3_CLIENT_ID=' docker/.env | cut -d '=' -f2 | tr -d '"')

if [ -z "$PROPLET_CLIENT_ID" ] || [ -z "$PROPLET_2_CLIENT_ID" ] || [ -z "$PROPLET_3_CLIENT_ID" ]; then
    echo "ERROR: CLIENT_IDs not found in docker/.env"
    exit 1
fi
echo "✓ CLIENT_IDs exported"

# Step 6: Get GitHub username from .env or prompt
GITHUB_USER=$(grep '^PROXY_REGISTRY_USERNAME=' docker/.env | cut -d '=' -f2 | tr -d '"' || echo "")
if [ -z "$GITHUB_USER" ]; then
    read -p "Enter your GitHub username (lowercase): " GITHUB_USER
fi

# Step 7: Configure experiment
echo "Step 6: Configuring experiment..."
ROUND_ID="r-$(date +%s)"
EXPERIMENT_ID="exp-$ROUND_ID"

RESPONSE=$(curl -s -X POST http://localhost:7070/fl/experiments \
  -H "Content-Type: application/json" \
  -d "{
    \"experiment_id\": \"$EXPERIMENT_ID\",
    \"round_id\": \"$ROUND_ID\",
    \"model_ref\": \"fl/models/global_model_v0\",
    \"participants\": [\"$PROPLET_CLIENT_ID\", \"$PROPLET_2_CLIENT_ID\", \"$PROPLET_3_CLIENT_ID\"],
    \"hyperparams\": {\"epochs\": 1, \"lr\": 0.01, \"batch_size\": 16},
    \"k_of_n\": 3,
    \"timeout_s\": 60,
    \"task_wasm_image\": \"ghcr.io/$GITHUB_USER/fl-client-wasm:latest\"
  }")

if echo "$RESPONSE" | grep -q "configured"; then
    echo "✓ Experiment configured: $ROUND_ID"
else
    echo "ERROR: Experiment configuration failed: $RESPONSE"
    exit 1
fi

# Step 8: Wait for round to complete
echo "Step 7: Waiting for round to complete (max 90 seconds)..."
TIMEOUT=90
ELAPSED=0
while [ $ELAPSED -lt $TIMEOUT ]; do
    if docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env logs coordinator-http 2>&1 | grep -q "Round complete.*$ROUND_ID"; then
        echo "✓ Round completed!"
        break
    fi
    sleep 2
    ELAPSED=$((ELAPSED + 2))
    echo -n "."
done
echo ""

if [ $ELAPSED -ge $TIMEOUT ]; then
    echo "WARNING: Round did not complete within timeout"
    echo "Check logs: docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env logs coordinator-http | grep '$ROUND_ID'"
    exit 1
fi

# Step 9: Verify results
echo "Step 8: Verifying results..."

# Check aggregated model exists
if curl -s http://localhost:8084/models/1 > /dev/null; then
    echo "✓ Aggregated model (version 1) exists"
    echo "Model: $(curl -s http://localhost:8084/models/1)"
else
    echo "ERROR: Aggregated model not found"
    exit 1
fi

# Check all proplets completed
COMPLETED=$(docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env logs proplet proplet-2 proplet-3 2>&1 | grep -c "Task.*completed successfully.*$ROUND_ID" || echo "0")
if [ "$COMPLETED" -ge 3 ]; then
    echo "✓ All 3 proplets completed training"
else
    echo "WARNING: Only $COMPLETED/3 proplets completed"
fi

echo ""
echo "=== Test Complete ==="
echo "Round ID: $ROUND_ID"
echo "Experiment ID: $EXPERIMENT_ID"
echo ""
echo "View aggregated model: curl http://localhost:8084/models/1"
echo "View round logs: docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env logs coordinator-http | grep '$ROUND_ID'"
