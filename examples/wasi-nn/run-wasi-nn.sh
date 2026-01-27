#!/bin/bash
# =============================================================================
# Run wasi-nn Image Classification Example on Propeller
# =============================================================================
#
# This script creates and runs a wasi-nn task with OpenVINO backend.
#
# Prerequisites:
#   1. Propeller services running (docker compose up -d)
#   2. propeller-cli provisioned (propeller-cli provision)
#   3. WASM file built (see README.md)
#   4. Fixture directory with model files mounted in proplet
#
# Usage:
#   ./run-wasi-nn.sh /path/to/wasi-nn-example.wasm
#
# Or set environment variables:
#   WASM_FILE=/path/to/wasi-nn-example.wasm ./run-wasi-nn.sh
#   MANAGER_URL=http://localhost:7070 ./run-wasi-nn.sh
#
# =============================================================================

set -e

# Configuration
MANAGER_URL="${MANAGER_URL:-http://localhost:7070}"
WASM_FILE="${1:-${WASM_FILE:-}}"
PROPELLER_DIR="${PROPELLER_DIR:-$(cd "$(dirname "$0")/../.." && pwd)}"
CLI="${PROPELLER_DIR}/build/cli"

echo "=============================================="
echo "  Propeller wasi-nn Example Runner"
echo "=============================================="
echo ""
echo "Manager URL:    $MANAGER_URL"
echo "Propeller Dir:  $PROPELLER_DIR"
echo "CLI:            $CLI"
echo ""

# Check CLI exists
if [ ! -f "$CLI" ]; then
    echo "Error: CLI not found at $CLI"
    echo ""
    echo "Build it with:"
    echo "  cd $PROPELLER_DIR"
    echo "  go build -o build/cli ./cmd/cli"
    exit 1
fi

# Check WASM file
if [ -z "$WASM_FILE" ]; then
    # Try to find it in common locations
    COMMON_PATHS=(
        "$PROPELLER_DIR/../wasmtime/crates/wasi-nn/examples/classification-example/target/wasm32-wasip1/release/wasi-nn-example.wasm"
        "$HOME/UV/wasmtime/crates/wasi-nn/examples/classification-example/target/wasm32-wasip1/release/wasi-nn-example.wasm"
        "./wasi-nn-example.wasm"
    )
    
    for path in "${COMMON_PATHS[@]}"; do
        if [ -f "$path" ]; then
            WASM_FILE="$path"
            break
        fi
    done
fi

if [ -z "$WASM_FILE" ] || [ ! -f "$WASM_FILE" ]; then
    echo "Error: WASM file not found"
    echo ""
    echo "Please provide the path to wasi-nn-example.wasm:"
    echo "  $0 /path/to/wasi-nn-example.wasm"
    echo ""
    echo "To build it:"
    echo "  cd <wasmtime-repo>/crates/wasi-nn/examples/classification-example"
    echo "  rustup target add wasm32-wasip1"
    echo "  cargo build --target wasm32-wasip1 --release"
    exit 1
fi

echo "WASM File:      $WASM_FILE"
echo ""

# Step 1: Create task
echo "Step 1: Creating wasi-nn task..."
RESPONSE=$("$CLI" tasks create "wasi-nn-example-$(date +%s)" \
    --cli-args="-S,nn,--dir=/home/proplet/fixture::fixture" 2>&1)

# Parse task ID
TASK_ID=$(echo "$RESPONSE" | grep -o '"id"[[:space:]]*:[[:space:]]*"[^"]*"' | head -1 | sed 's/"id"[[:space:]]*:[[:space:]]*"\([^"]*\)"/\1/')

if [ -z "$TASK_ID" ]; then
    echo "Error: Failed to create task"
    echo "Response: $RESPONSE"
    exit 1
fi

echo "Created task: $TASK_ID"
echo ""

# Step 2: Upload WASM file
echo "Step 2: Uploading WASM file..."
UPLOAD_RESPONSE=$(curl -s -X PUT "${MANAGER_URL}/tasks/${TASK_ID}/upload" \
    -F "file=@${WASM_FILE}")

echo "Upload complete"
echo ""

# Step 3: Start task
echo "Step 3: Starting task..."
"$CLI" tasks start "$TASK_ID"
echo ""

# Step 4: Wait and show results
echo "Step 4: Waiting for results..."
sleep 3

echo ""
echo "=============================================="
echo "  Task Results"
echo "=============================================="
echo ""
echo "Task ID: $TASK_ID"
echo ""

# Get task status
echo "Task Status:"
curl -s "${MANAGER_URL}/tasks/${TASK_ID}" | python3 -m json.tool 2>/dev/null || \
    curl -s "${MANAGER_URL}/tasks/${TASK_ID}"
echo ""

echo ""
echo "Proplet Logs (last 20 lines):"
echo "----------------------------------------------"
cd "$PROPELLER_DIR"
docker compose -f docker/compose.yaml logs proplet --tail 20 2>/dev/null | grep -v "^propeller-proplet" || \
    docker compose -f docker/compose.yaml --env-file docker/.env logs proplet --tail 20

echo ""
echo "=============================================="
echo "  Done!"
echo "=============================================="
echo ""
echo "To see more logs:"
echo "  docker compose -f docker/compose.yaml logs proplet --tail 50"
echo ""
echo "To view task details:"
echo "  curl ${MANAGER_URL}/tasks/${TASK_ID} | jq"
