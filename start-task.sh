#!/usr/bin/env bash
# deploy_task.sh — Create, upload, and start a BRT task
# Usage: ./deploy_task.sh [WASM_FILE] [HOST] [PORT]

set -euo pipefail

# ── Configuration ────────────────────────────────────────────────────────────
BASE_URL="${BASE_URL:-http://localhost:7070}"
TASK_ID="${TASK_ID:-brt-eucnc-front-1}"
TASK_NAME="${TASK_NAME:-start-server}"
SERVER_HOST="${1:-0.0.0.0}"
SERVER_PORT="${2:-8383}"
WASM_FILE="${3:-/home/dusan/propeller/build/out__260531__eucnc_v0.1.2/brt-eucnc-demo/brt_eucnc_front.wasm}"

# ── Helpers ───────────────────────────────────────────────────────────────────
log() { echo "[$(date '+%H:%M:%S')] $*"; }
die() {
    echo "ERROR: $*" >&2
    exit 1
}

require_cmd() { command -v "$1" &>/dev/null || die "'$1' is required but not found."; }
require_cmd curl
require_cmd jq

# ── Validate WASM file ────────────────────────────────────────────────────────
[[ -f "$WASM_FILE" ]] || die "WASM file not found: $WASM_FILE"

# ── Step 1 — Create task ──────────────────────────────────────────────────────
log "Creating task '$TASK_ID' ..."

CREATE_RESPONSE=$(curl --silent --show-error --fail \
    --location "$BASE_URL/tasks" \
    --header 'Content-Type: application/json' \
    --data "{
    \"id\": \"$TASK_ID\",
    \"name\": \"$TASK_NAME\",
    \"inputs\": [
      \"\\\"$SERVER_HOST\\\"\",
      \"$SERVER_PORT\"
    ],
    \"daemon\": true,
    \"env\": {}
  }") || die "Failed to create task."

log "Create response: $CREATE_RESPONSE"

# Extract the UUID assigned by the server
TASK_UUID=$(echo "$CREATE_RESPONSE" | jq -r '.id // empty')
[[ -n "$TASK_UUID" ]] || die "Could not extract task UUID from response: $CREATE_RESPONSE"

log "Task UUID: $TASK_UUID"

# ── Step 2 — Upload WASM ──────────────────────────────────────────────────────
log "Uploading WASM file: $WASM_FILE ..."

UPLOAD_RESPONSE=$(curl --silent --show-error --fail \
    --location --request PUT "$BASE_URL/tasks/$TASK_UUID/upload" \
    --form "file=@\"$WASM_FILE\"") || die "Failed to upload WASM file."

log "Upload response: $UPLOAD_RESPONSE"

# ── Step 3 — Start task ───────────────────────────────────────────────────────
log "Starting task $TASK_UUID ..."

START_RESPONSE=$(curl --silent --show-error --fail \
    --location --request POST "$BASE_URL/tasks/$TASK_UUID/start") || die "Failed to start task."

log "Start response: $START_RESPONSE"

log "Done. Task '$TASK_UUID' is running on $SERVER_HOST:$SERVER_PORT."
