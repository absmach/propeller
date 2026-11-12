#!/usr/bin/env bash
# Copyright (c) Abstract Machines
# SPDX-License-Identifier: Apache-2.0
#
# Propeller end-to-end integration test.
#
# Brings the stack up the same way the getting-started guide does — base
# services, provisioning, Propeller — then deploys the documented examples and
# asserts each one ran to completion with the right result and that the
# service logs show the expected flow. Exits non-zero on the first failed
# assertion, after dumping container logs.
#
# Everything runs against images built from the working tree
# (`make ci-images` + `make start-propeller-ci`), not the published ghcr.io
# images, so a pull request is tested against its own code.
#
# Examples covered, and what each one is here to prove:
#   addition            TinyGo module over the multipart upload path
#   addition-wat        same result via the inline base64 upload path
#   greet-component     WASM Component Model, WAVE-encoded string argument
#   http-server         WASI P2 proxy component serving real inbound HTTP
#   http-client         WASI P2 component making outgoing HTTP requests
#   filesystem          WASI P1 preopened directory round-trip
#
# Usage:
#   tools/ci/integration-test.sh
#
# Environment:
#   PROPELLER_CI_SKIP_BUILD=1  reuse the existing propeller-ci/* images
#   CI_SKIP_EXTERNAL=1        skip http-client, which calls httpbin.org
#   CI_EXPECTED_ADDITION=30   expected sum for the addition examples
#   CI_HTTP_SERVER_PORT=8044  port the http-server workload must bind
#   CI_PROPLET_DIRS=/tmp      host dirs preopened for the filesystem example
#   CI_ATOM_IDENTIFIER        Atom login    (default: admin)
#   CI_ATOM_SECRET            Atom password (default: $ATOM_ADMIN_SECRET in docker/.env)
#   CI_KEEP_STACK=1           leave the containers running on exit

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO_ROOT"

MANAGER_URL="${CI_MANAGER_URL:-http://localhost:7070}"
ATOM_URL="${CI_ATOM_URL:-http://localhost:8080}"

# 10 + 20. Covers both the TinyGo module and the WAT/base64 variant.
EXPECTED_ADDITION="${CI_EXPECTED_ADDITION:-30}"
# Must match CI_HTTP_SERVER_PORT in docker/compose.propeller.ci.yaml, which is
# the port published from the proplet container.
HTTP_SERVER_PORT="${CI_HTTP_SERVER_PORT:-8044}"

# Task states (pkg/task.State): 0 Pending, 1 Scheduled, 2 Running, 3 Completed.
STATE_RUNNING=2
STATE_COMPLETED=3

log()   { printf '\n\033[1;34m==> %s\033[0m\n' "$*"; }
info()  { printf '    %s\n' "$*"; }
pass()  { printf '\033[1;32m    PASS\033[0m %s\n' "$*"; }

fail() {
    printf '\n\033[1;31m==> FAIL: %s\033[0m\n' "$*" >&2
    exit 1
}

# dump_diagnostics prints enough state to debug a failure from the CI log
# alone: container status, then the tail of every service's logs.
dump_diagnostics() {
    {
        echo "----- docker ps -a -----"
        docker ps -a --format '{{.Names}}\t{{.Image}}\t{{.Status}}' || true
        for container in propeller-atom propeller-fluxmq-node1 propeller-nginx \
            propeller-manager propeller-proplet propeller-proxy; do
            echo "----- $container -----"
            docker logs --tail 500 "$container" 2>&1 || true
        done
    } >&2
}

cleanup() {
    local status=$?
    if [ "$status" -ne 0 ]; then
        dump_diagnostics
    fi
    if [ "${CI_KEEP_STACK:-0}" = "1" ]; then
        info "CI_KEEP_STACK=1, leaving containers running"
    else
        log "Tearing down"
        make stop-propeller-ci >/dev/null 2>&1 || true
        make stop-base >/dev/null 2>&1 || true
    fi
    exit "$status"
}
trap cleanup EXIT

# wait_for_url polls a URL until it answers 2xx, up to a timeout in seconds.
# A real request is required; testing the string only checks it is non-empty.
wait_for_url() {
    local what="$1" url="$2" timeout="$3"
    local deadline=$((SECONDS + timeout))

    until curl -sSf -o /dev/null --max-time 2 "$url"; do
        if (( SECONDS >= deadline )); then
            echo "----- docker ps -a -----" >&2
            docker ps -a --format '{{.Names}}\t{{.Image}}\t{{.Status}}' >&2 || true
            fail "$what ($url) did not become ready within ${timeout}s"
        fi
        sleep 2
    done
    pass "$what ready"
}

# assert_eq compares an actual value against an expected one.
assert_eq() {
    local what="$1" expected="$2" actual="$3"
    if [ "$actual" != "$expected" ]; then
        fail "$what: expected '$expected', got '$actual'"
    fi
    pass "$what = $actual"
}

# assert_log_contains fails unless the container's logs contain the pattern.
# Logs are captured first to avoid a SIGPIPE failure under `set -o pipefail`.
assert_log_contains() {
    local container="$1" pattern="$2" what="$3"
    local logs
    logs="$(docker logs "$container" 2>&1 || true)"
    if ! grep -qF -- "$pattern" <<<"$logs"; then
        fail "$what (no '$pattern' in $container logs)"
    fi
    pass "$what"
}

# assert_log_clean fails if a container logged an ERROR or panicked. WARN is
# allowed: a stock stack logs some (unconfigured FL coordinator, no local data
# store) and those are not failures.
assert_log_clean() {
    local container="$1"
    local logs offending
    logs="$(docker logs "$container" 2>&1 || true)"
    # rumqttc logs "Unsolicited pubrec packet: N" at ERROR when a PUBREC
    # arrives for a packet id it has already resolved, which happens
    # intermittently while the proplet resumes an MQTT session. It is protocol
    # bookkeeping, not a fault, and it is logged from the client library rather
    # than from Propeller, so it is filtered out. Any other ERROR still fails.
    offending="$(grep -E '"level":"ERROR"|panic:|fatal runtime error' <<<"$logs" |
        grep -v 'Unsolicited pubrec packet' || true)"
    if [ -n "$offending" ]; then
        printf '%s\n' "$offending" | head -20 >&2
        fail "$container logged an error or panicked"
    fi
    pass "$container logged no errors"
}

json() { curl -sS --max-time 30 "$@"; }

# --- task helpers ----------------------------------------------------------
#
# Every example follows create -> upload -> start -> wait, with the details
# (upload mechanism, CLI args, daemon mode) varying. These helpers keep the
# per-example blocks to their interesting assertions.

# create_task <json-body> -> echoes the task id. The body is passed through
# verbatim so each example can match its documented create call exactly.
create_task() {
    local body="$1" id
    id="$(json -X POST "${MANAGER_URL}/tasks" \
        -H 'Content-Type: application/json' -d "$body" | jq -r '.id // empty')"
    [ -n "$id" ] || fail "could not create task from body: ${body}"
    printf '%s' "$id"
}

# upload_multipart <task-id> <wasm-file> — the /upload endpoint.
upload_multipart() {
    local id="$1" file="$2" response
    response="$(json -X PUT "${MANAGER_URL}/tasks/${id}/upload" -F "file=@${file}")"
    assert_eq "upload accepted for $id" "$id" "$(jq -r '.id // empty' <<<"$response")"
    [ "$(jq -r '.file // empty' <<<"$response")" != "" ] || fail "task ${id} has no file after upload"
}

# upload_base64 <task-id> <wasm-file> — the JSON body upload path used by the
# addition-wat example, which carries the module inline as base64.
upload_base64() {
    local id="$1" file="$2" encoded response
    encoded="$(base64 -w0 <"$file")"
    response="$(json -X PUT "${MANAGER_URL}/tasks/${id}" \
        -H 'Content-Type: application/json' \
        -d "$(jq -n --arg file "$encoded" '{file: $file}')")"
    assert_eq "base64 upload accepted for $id" "$id" "$(jq -r '.id // empty' <<<"$response")"
    [ "$(jq -r '.file // empty' <<<"$response")" != "" ] || fail "task ${id} has no file after base64 upload"
}

# start_task <task-id>
start_task() {
    local id="$1"
    assert_eq "task ${id} accepted for start" "true" \
        "$(json -X POST "${MANAGER_URL}/tasks/${id}/start" | jq -r '.started // false')"
}

# wait_task <task-id> [timeout] -> echoes the final task JSON once it reaches a
# terminal state. A failure or interrupted state is fatal.
wait_task() {
    local id="$1" timeout="${2:-120}"
    local deadline=$((SECONDS + timeout)) task state

    while true; do
        task="$(json "${MANAGER_URL}/tasks/${id}")"
        state="$(jq -r '.state' <<<"$task")"
        case "$state" in
            "$STATE_COMPLETED") printf '%s' "$task"; return 0 ;;
            # 4 Failed, 5 Skipped, 6 Interrupted
            4 | 5 | 6) jq . <<<"$task" >&2; fail "task ${id} ended in terminal state ${state}" ;;
        esac
        if (( SECONDS >= deadline )); then
            jq . <<<"$task" >&2
            fail "task ${id} did not complete within ${timeout}s (state=${state})"
        fi
        sleep 1
    done
}

# wait_task_running <task-id> [timeout] -> echoes the task JSON once it is
# Running. Daemon workloads stay in this state for as long as they run, so they
# never reach Completed on their own.
wait_task_running() {
    local id="$1" timeout="${2:-120}"
    local deadline=$((SECONDS + timeout)) task state

    while true; do
        task="$(json "${MANAGER_URL}/tasks/${id}")"
        state="$(jq -r '.state' <<<"$task")"
        case "$state" in
            "$STATE_RUNNING") printf '%s' "$task"; return 0 ;;
            4 | 5 | 6) jq . <<<"$task" >&2; fail "task ${id} ended in terminal state ${state}" ;;
        esac
        if (( SECONDS >= deadline )); then
            jq . <<<"$task" >&2
            fail "task ${id} did not start within ${timeout}s (state=${state})"
        fi
        sleep 1
    done
}

# assert_result <label> <expected> <task-json>
# The single-value examples print their result with a trailing newline, so
# compare with newlines stripped.
assert_result() {
    local label="$1" expected="$2" task="$3" actual
    actual="$(jq -r '.results // ""' <<<"$task" | tr -d '\r\n')"
    assert_eq "$label result" "$expected" "$actual"
}

# assert_result_contains <label> <substring> <task-json>
assert_result_contains() {
    local label="$1" needle="$2" task="$3"
    if ! jq -r '.results // ""' <<<"$task" | grep -qF -- "$needle"; then
        jq -r '.results // ""' <<<"$task" | head -20 >&2
        fail "$label: results do not contain '$needle'"
    fi
    pass "$label results contain '$needle'"
}

# assert_http <label> <expected-body> <url> [curl-args...]
assert_http() {
    local label="$1" expected="$2" url="$3"
    shift 3
    local body deadline=$((SECONDS + 30))
    # The proplet returns "started at port ..." before the workload is
    # accepting connections, so poll rather than assume.
    while true; do
        body="$(curl -sS -m 5 "$@" "$url" 2>/dev/null || true)"
        if [ "$body" = "$expected" ]; then
            pass "$label ($url) = $expected"
            return 0
        fi
        if (( SECONDS >= deadline )); then
            printf 'last response body: %s\n' "${body:-<empty>}" >&2
            fail "$label ($url): expected '$expected', got '${body:-<empty>}'"
        fi
        sleep 1
    done
}

# ---------------------------------------------------------------------------
# 1. Build the images under test
# ---------------------------------------------------------------------------

if [ "${PROPELLER_CI_SKIP_BUILD:-0}" != "1" ]; then
    log "Building images under test"
    make ci-images
fi

[ -x ./build/cli ] || fail "build/cli is missing — run without PROPELLER_CI_SKIP_BUILD=1"
[ -f build/addition.wasm ] || fail "build/addition.wasm is missing — run without PROPELLER_CI_SKIP_BUILD=1"

# ---------------------------------------------------------------------------
# 2. Start the base services (Atom, FluxMQ, Nginx) and wait for Atom
# ---------------------------------------------------------------------------

log "Starting base services"
make start-base
wait_for_url "atom" "${ATOM_URL}/health" 240

# ---------------------------------------------------------------------------
# 3. Provision Atom resources (tenant, entities, channel) -> config.toml
# ---------------------------------------------------------------------------

log "Provisioning"
# Admin credentials come from docker/.env, the single source of truth.
ATOM_SECRET="$(sed -n 's/^ATOM_ADMIN_SECRET=//p' docker/.env | tail -1)"
[ -n "$ATOM_SECRET" ] || fail "could not read ATOM_ADMIN_SECRET from docker/.env"

# Start clean: a stale config.toml from a previous local run would mask a
# failed provisioning.
CONFIG=config.toml
rm -f "$CONFIG" docker/config.toml

PROPELLER_ATOM_IDENTIFIER="${CI_ATOM_IDENTIFIER:-admin}" \
PROPELLER_ATOM_SECRET="${CI_ATOM_SECRET:-$ATOM_SECRET}" \
PROPELLER_TENANT_NAME="${CI_TENANT_NAME:-propeller-ci}" \
PROPELLER_PROPLET_COUNT=1 \
    ./build/cli provision

# The compose file bind-mounts ./docker/config.toml, so copy it there.
[ -f "$CONFIG" ] || fail "provision did not produce $CONFIG"

for section in manager proplet proxy; do
    grep -q "^\[${section}\]$" "$CONFIG" || fail "config.toml is missing the [$section] section"
done
# One non-empty api_key per service identity.
api_keys="$(grep -c '^api_key = "[^"]\+"' "$CONFIG" || true)"
assert_eq "config.toml api_key count" "3" "$api_keys"

cp "$CONFIG" docker/config.toml

# ---------------------------------------------------------------------------
# 4. Start Propeller and wait for the proplet to register
#
# PROPLET_HTTP_ENABLED lets workloads make outgoing requests and serve inbound
# HTTP; PROPLET_DIRS preopens host directories into WASI P1 guests.
# ---------------------------------------------------------------------------

log "Starting Propeller"
# shellcheck disable=SC2086
PROPLET_HTTP_ENABLED=true PROPLET_DIRS="${CI_PROPLET_DIRS:-/tmp}" \
    make start-propeller-ci
wait_for_url "manager" "${MANAGER_URL}/health" 180

log "Waiting for the proplet to register"
# The count defaults to 0 so a transient empty response reads as "not yet".
alive_proplets() {
    local count
    count="$(json "${MANAGER_URL}/proplets" 2>/dev/null |
        jq -r '[.proplets[]? | select(.alive == true)] | length' 2>/dev/null)" || count=""

    printf '%s' "${count:-0}"
}

wait_for_proplet() {
    local deadline=$((SECONDS + 180))
    until [ "$(alive_proplets)" -ge 1 ]; do
        if (( SECONDS >= deadline )); then
            fail "no proplet registered as alive within 180s"
        fi
        sleep 2
    done
    pass "proplet registered and alive"
}

wait_for_proplet

# ---------------------------------------------------------------------------
# 5. Deploy the examples
# ---------------------------------------------------------------------------

log "Example: addition (TinyGo module, multipart upload)"
addition_id="$(create_task '{"name":"add","inputs":[10,20]}')"
info "task id: $addition_id"
upload_multipart "$addition_id" build/addition.wasm
start_task "$addition_id"
addition_task="$(wait_task "$addition_id")"
assert_result "addition" "$EXPECTED_ADDITION" "$addition_task"
assert_eq "addition ran on a proplet" "true" \
    "$(jq -r '(.proplet_id // "") != ""' <<<"$addition_task")"

log "Example: addition-wat (WAT module, base64 upload)"
# Same example compiled from WebAssembly text, carried inline as base64 in the
# task body rather than multipart. Exercises the other upload path.
addition_wat_id="$(create_task '{"name":"add","inputs":[10,20]}')"
info "task id: $addition_wat_id"
upload_base64 "$addition_wat_id" build/addition-wat.wasm
start_task "$addition_wat_id"
addition_wat_task="$(wait_task "$addition_wat_id")"
assert_result "addition-wat" "$EXPECTED_ADDITION" "$addition_wat_task"

log "Example: greet-component (WASM Component Model)"
# The task name must match the WIT export (greet) and the argument is
# WAVE-encoded. wasmtime wants double quotes here: greet('World') is rejected
# with "invalid token", greet("World") returns the string.
greet_id="$(create_task '{"name":"greet","inputs":["\"World\""]}')"
info "task id: $greet_id"
upload_multipart "$greet_id" build/greet-component.wasm
start_task "$greet_id"
greet_task="$(wait_task "$greet_id")"
assert_result "greet-component" '"Hello, World!"' "$greet_task"

log "Example: http-server (WASI P2 proxy component, daemon)"
# -Scli is required: the component imports wasi:cli/environment, which the
# proplet's default -Shttp alone does not link. The port must be given as
# --addr; a --port argument is passed through to the component and ignored
# for binding, which would leave the workload on the default proxy port.
http_server_id="$(create_task "{\"name\":\"_start\",\"daemon\":true,\"cli_args\":[\"-Scli\",\"--addr\",\"0.0.0.0:${HTTP_SERVER_PORT}\"]}")"
info "task id: $http_server_id"
upload_multipart "$http_server_id" build/http-server.wasm
start_task "$http_server_id"
http_server_task="$(wait_task_running "$http_server_id")"
pass "http-server daemon started ($(jq -r '.results // ""' <<<"$http_server_task" | tr -d '\n'))"

# The proplet reports the port before the component is accepting connections.
assert_http "http-server /health" "ok" "http://localhost:${HTTP_SERVER_PORT}/health"
assert_http "http-server /" "Hello from proplet WASM HTTP server!" "http://localhost:${HTTP_SERVER_PORT}/"
assert_http "http-server /echo" "Hello from curl" "http://localhost:${HTTP_SERVER_PORT}/echo" -X POST -d 'Hello from curl'

if [ "${CI_SKIP_EXTERNAL:-0}" = "1" ]; then
    log "Example: http-client SKIPPED (CI_SKIP_EXTERNAL=1)"
else
    log "Example: http-client (WASI P2 outgoing HTTP)"
    # -Shttp is required or the component's wasi:http/types import is unlinked.
    # NOTE: this example calls httpbin.org, so it depends on a third-party
    # service being reachable. Set CI_SKIP_EXTERNAL=1 to drop it from the run.
    http_client_id="$(create_task '{"name":"_start","cli_args":["-Shttp"]}')"
    info "task id: $http_client_id"
    upload_multipart "$http_client_id" build/http-client.wasm
    start_task "$http_client_id"
    http_client_task="$(wait_task "$http_client_id" 180)"
    assert_result_contains "http-client" "httpbin.org/get" "$http_client_task"
    assert_result_contains "http-client" "Status: 200 OK" "$http_client_task"
fi

# ---------------------------------------------------------------------------
# 6. Check the logs from the external-runtime phase
#
# These run before the proplet restart below: `docker logs` only shows the
# current container instance, so the messages from the tasks above are gone
# once the proplet is recreated.
# ---------------------------------------------------------------------------

log "Checking service logs (external runtime phase)"
# The manager's "successfully created proplet" line is not asserted: the
# discovery message is published once at startup and may race the manager's
# subscription. The /proplets assertion above is the registration check.
assert_log_contains propeller-manager "MQTT connection established" "manager connected to MQTT"
assert_log_contains propeller-manager "Subscribe to MQTT topic completed successfully" "manager subscribed to control topics"
assert_log_contains propeller-manager "\"msg\":\"Starting task completed successfully\"" "manager started a task"
assert_log_contains propeller-proplet "Decoded wasm binary" "proplet received a wasm module"
assert_log_contains propeller-proplet "Starting Host runtime app" "proplet ran a task on the external runtime"
for id in "$addition_id" "$addition_wat_id" "$greet_id"; do
    assert_log_contains propeller-proplet "Successfully published result for task ${id}" \
        "proplet published the result for ${id}"
done
[ "${CI_SKIP_EXTERNAL:-0}" = "1" ] ||
    assert_log_contains propeller-proplet "Successfully published result for task ${http_client_id}" \
        "proplet published the result for ${http_client_id}"

for container in propeller-manager propeller-proplet propeller-proxy; do
    assert_log_clean "$container"
done

# ---------------------------------------------------------------------------
# 7. filesystem on the in-process Wasmtime runtime
#
# PROPLET_DIRS is only honoured by the in-process runtime, so restart the
# proplet with PROPLET_EXTERNAL_WASM_RUNTIME="" to select it.
# ---------------------------------------------------------------------------

log "Example: filesystem (WASI P1 preopened dirs, in-process runtime)"
log "Restarting the proplet on the in-process Wasmtime runtime"
# shellcheck disable=SC2086
PROPLET_EXTERNAL_WASM_RUNTIME="" PROPLET_HTTP_ENABLED=true PROPLET_DIRS="${CI_PROPLET_DIRS:-/tmp}" \
    make stop-propeller-ci
# shellcheck disable=SC2086
PROPLET_EXTERNAL_WASM_RUNTIME="" PROPLET_HTTP_ENABLED=true PROPLET_DIRS="${CI_PROPLET_DIRS:-/tmp}" \
    make start-propeller-ci
wait_for_url "manager" "${MANAGER_URL}/health" 180
wait_for_proplet

filesystem_id="$(create_task '{"name":"_start"}')"
info "task id: $filesystem_id"
upload_multipart "$filesystem_id" build/filesystem.wasm
start_task "$filesystem_id"
filesystem_task="$(wait_task "$filesystem_id")"
assert_result_contains "filesystem" "All filesystem operations succeeded" "$filesystem_task"
assert_result_contains "filesystem" "Round-trip verification OK" "$filesystem_task"

# ---------------------------------------------------------------------------
# 8. Check the logs from the in-process runtime phase
# ---------------------------------------------------------------------------

log "Checking service logs (in-process runtime phase)"
assert_log_contains propeller-proplet "Using Wasmtime runtime" "proplet selected the in-process runtime"
assert_log_contains propeller-proplet "Starting Wasmtime runtime app" "proplet ran the task on the in-process runtime"
assert_log_contains propeller-proplet "Successfully published result for task ${filesystem_id}" \
    "proplet published the filesystem result"

for container in propeller-manager propeller-proplet propeller-proxy; do
    assert_log_clean "$container"
done

log "Integration test passed"
