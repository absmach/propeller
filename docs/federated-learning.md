# Federated Learning in Propeller

This document describes how Federated Machine Learning (FL) works in the Propeller codebase.

## Overview

Propeller implements a **Federated Averaging (FedAvg)** algorithm for distributed machine learning across edge devices. The system enables training ML models without exposing raw data - only model updates (weights/gradients) are shared between edge proplets and the central manager.

## Architecture

```
┌─────────────┐
│   Manager   │  ← Central orchestrator (aggregates updates, manages rounds)
└──────┬──────┘
       │ MQTT
       │
   ┌───┴───┬─────────┬─────────┐
   │       │         │         │
┌──▼──┐ ┌──▼──┐  ┌──▼──┐  ┌──▼──┐
│ P1  │ │ P2  │  │ P3  │  │ ... │  ← Edge Proplets (train locally)
└─────┘ └─────┘ └─────┘ └─────┘
```

**Key Components:**
- **Manager**: Orchestrates FL rounds, aggregates updates, manages task lifecycle
- **Proplets**:
  - **Rust Proplet** (Wasmtime): Execute Wasm workloads, train locally, send updates
  - **Embedded Proplet** (C/WAMR/Zephyr): Same FL support for microcontrollers
- **Proxy**: Distributes model artifacts via OCI registry (chunked over MQTT)
- **MQTT**: Communication channel for all coordination

## FL Workflow

### 1. Task Creation

A federated learning task is created with the following configuration:

```go
task := Task{
    Name:     "fl-training-job",
    Kind:     TaskKindFederated,
    Mode:     ModeTrain,  // or ModeInfer
    ImageURL: "registry.example.com/fl-model:v1",
    FL: &FLSpec{
        JobID:          "job-uuid",
        RoundID:        1,              // Starts at round 1
        GlobalVersion:  "version-uuid",
        TotalRounds:    3,              // Train for 3 rounds
        ClientsPerRound: 2,            // Select 2 proplets per round
        MinParticipants: 2,            // Need at least 2 updates to aggregate
        RoundTimeoutSec: 300,          // 5 minute timeout per round
        UpdateFormat:   "json-f64",    // Format: json-f64 or custom
        ModelRef:       "registry.example.com/model:v1",
        LocalEpochs:    1,             // Local training epochs
        BatchSize:      32,
        LearningRate:   0.01,
    },
}
```

**Location**: `task/task.go`, `cli/fl.go`

### 2. Round Initialization

When a federated task is started:

1. **Manager** selects proplets using the scheduler (round-robin by default)
2. **Manager** creates round 1 tasks for each selected proplet
3. **Manager** injects FL environment variables into each task:
   - `FL_JOB_ID`: Unique job identifier
   - `FL_ROUND_ID`: Current round number (starts at 1)
   - `FL_GLOBAL_VERSION`: Version of the global model
   - `FL_GLOBAL_UPDATE_B64`: Base64-encoded global model weights (if available)
   - `FL_FORMAT`: Update format (e.g., "json-f64")
   - `FL_NUM_SAMPLES`: Number of training samples (for weighted averaging)

**Location**: `manager/service.go::StartTask()`, `injectFLEnv()`

### 3. Model Distribution

If the model is referenced via `ImageURL` or `ModelRef`:

1. **Proplet** requests the model from Proxy via MQTT topic:
   ```
   m/{domain}/c/{channel}/registry/proplet
   ```

2. **Proxy** fetches the Wasm module from OCI registry

3. **Proxy** chunks the binary (for large models) and streams via MQTT:
   ```
   m/{domain}/c/{channel}/registry/server
   ```

4. **Proplet** assembles chunks and stores the Wasm binary

**Location**: `proxy/service.go`, `proplet/src/service.rs::handle_chunk()`

### 4. Local Training on Proplets

Each proplet executes the Wasm workload:

1. **Proplet** receives start command via MQTT:
   ```
   m/{domain}/c/{channel}/control/manager/start
   ```

2. **Proplet** loads the Wasm module using Wasmtime runtime

3. **Proplet** sets environment variables (including FL_* vars)

4. **Proplet** calls the exported function (typically `train()` or `main()`)

5. **Wasm Module** (e.g., `examples/fl-train/fl-train.go`):
   - Reads `FL_GLOBAL_UPDATE_B64` to get current global model
   - Trains locally on private data
   - Produces model update (delta weights)
   - Outputs update as JSON or binary

6. **Proplet** captures the output and formats it as an `UpdateEnvelope`

**Location**: 
- Rust proplet: `proplet/src/service.rs::handle_start_command()`, `build_fl_update_envelope()`
- Embedded proplet: `embed-proplet/src/mqtt_client.c::handle_start_command()`, `publish_results_with_error()`

### 5. Update Publishing

The proplet publishes the update envelope:

```json
{
  "task_id": "task-uuid",
  "results": {
    "job_id": "job-uuid",
    "round_id": 1,
    "global_version": "version-uuid",
    "proplet_id": "proplet-1",
    "num_samples": 100,
    "update_b64": "base64-encoded-weights...",
    "format": "json-f64"
  }
}
```

**MQTT Topic**: `m/{domain}/c/{channel}/control/proplet/results`

**Location**: 
- Rust proplet: `proplet/src/service.rs::build_fl_update_envelope()`
- Embedded proplet: `embed-proplet/src/mqtt_client.c::publish_results_with_error()`

### 6. Update Collection & Validation

The Manager receives updates:

1. **Manager** subscribes to results topic: `m/{domain}/c/{channel}/control/proplet/results`

2. **Manager** validates the update envelope:
   - Checks `job_id` and `round_id` match the task
   - Verifies `proplet_id` matches the expected proplet
   - Ensures update format is valid

3. **Manager** marks the task as completed and stores the update

**Location**: `manager/service.go::updateResultsHandler()`, `parseAndValidateTrainResults()`

### 7. Aggregation Trigger

After each update is received:

1. **Manager** checks round progress: `roundProgress()`
   - Counts expected proplets (tasks created for this round)
   - Counts completed tasks (with valid updates)
   - Collects all update envelopes

2. **Manager** triggers aggregation when `completed >= expected`:
   - Calls `tryAggregateAndAdvance()`
   - Uses mutex to ensure aggregation happens only once per round

**Location**: `manager/service.go::tryAggregateAndAdvance()`, `roundProgress()`

### 8. FedAvg Aggregation

The Manager aggregates updates using **Federated Averaging**:

#### For `json-f64` format:

```go
// Weighted average: sum(update_i * num_samples_i) / total_samples
for each update:
    weights = decode_base64(update.update_b64)  // []float64
    weight = update.num_samples / total_samples
    aggregated[i] += weights[i] * weight

aggregated[i] = aggregated[i] / total_samples
```

**Example:**
- Proplet 1: `[1.0, 2.0, 3.0]` with 10 samples
- Proplet 2: `[2.0, 3.0, 4.0]` with 20 samples
- Total: 30 samples
- Aggregated: `[(1*10 + 2*20)/30, (2*10 + 3*20)/30, (3*10 + 4*20)/30]`
- Result: `[1.67, 2.67, 3.67]`

#### For other formats:

Updates are concatenated with a delimiter (for custom aggregation logic).

**Location**: `manager/service.go::aggregateJSONF64()`, `aggregateConcat()`

### 9. Round Progression

After aggregation completes:

1. **Manager** stores the aggregated envelope:
   ```
   Key: fl/{job_id}/{round_id}/aggregate
   Value: UpdateEnvelope (with aggregated weights)
   ```

2. **Manager** publishes aggregated model (optional notification):
   ```
   Topic: m/{domain}/c/{channel}/control/manager/fl/aggregated
   ```

3. **Manager** creates next round tasks:
   - Finds all tasks from current round
   - Creates new tasks with `round_id = current_round + 1`
   - Injects new global model via `FL_GLOBAL_UPDATE_B64`
   - Starts the new round tasks

4. **Process repeats** until `round_id > total_rounds`

**Location**: `manager/service.go::startNextRound()`, `buildNextRoundTask()`

### 10. Round Completion

When all rounds complete:

- Final aggregated model is stored
- All tasks are marked as completed
- FL job is finished

## Data Structures

### UpdateEnvelope

```go
type UpdateEnvelope struct {
    TaskID        string         // Task that produced this update
    JobID         string         // FL job identifier
    RoundID       uint64         // Round number
    GlobalVersion string         // Model version
    PropletID     string         // Which proplet produced this
    NumSamples    uint64         // Number of training samples (for weighting)
    UpdateB64     string         // Base64-encoded model update
    Format        string         // "json-f64", "f32-delta", etc.
    Metrics       map[string]any // Optional metrics
}
```

**Location**: `pkg/fl/types.go`

### FLSpec

```go
type FLSpec struct {
    JobID          string         // Unique job ID
    RoundID        uint64         // Current round
    GlobalVersion  string         // Model version
    MinParticipants uint64        // Min clients for aggregation
    RoundTimeoutSec uint64         // Round timeout
    ClientsPerRound uint64         // Clients per round
    TotalRounds    uint64         // Total rounds to run
    UpdateFormat   string         // Update format
    ModelRef       string         // Model artifact reference
    LocalEpochs    uint64         // Local training epochs
    BatchSize      uint64         // Batch size
    LearningRate   float64        // Learning rate
}
```

**Location**: `task/task.go`

## MQTT Topics

| Topic Pattern | Direction | Purpose |
|--------------|-----------|---------|
| `m/{domain}/c/{channel}/control/manager/start` | Manager → Proplet | Start task (includes FL spec) |
| `m/{domain}/c/{channel}/control/proplet/results` | Proplet → Manager | Send training results/updates |
| `m/{domain}/c/{channel}/control/manager/fl/aggregated` | Manager → All | Publish aggregated model (optional) |
| `m/{domain}/c/{channel}/registry/proplet` | Proplet → Proxy | Request model artifact |
| `m/{domain}/c/{channel}/registry/server` | Proxy → Proplet | Stream model chunks |

## Embedded Proplet Specific Notes

The embedded proplet (C/WAMR/Zephyr) implements the same FL protocol as the Rust proplet, with some implementation differences:

### Implementation Details

1. **FL Task Detection**: 
   - Parses `fl` object from start command JSON
   - Also detects FL tasks via `FL_JOB_ID` environment variable
   - Location: `embed-proplet/src/mqtt_client.c::handle_start_command()`

2. **FL Field Parsing**:
   - Parses all FL spec fields: `job_id`, `round_id`, `global_version`, `update_format`
   - Also parses hyperparameters: `min_participants`, `total_rounds`, `local_epochs`, `batch_size`, `learning_rate`
   - Environment variables override FL spec fields when present
   - Location: `embed-proplet/src/mqtt_client.c::handle_start_command()`

3. **Update Envelope Creation**:
   - Matches Rust proplet structure exactly
   - Includes all required fields: `task_id`, `job_id`, `round_id`, `global_version`, `proplet_id`, `num_samples`, `update_b64`, `format`, `metrics`
   - Publishes to same topic: `m/{domain}/c/{channel}/control/proplet/results`
   - Location: `embed-proplet/src/mqtt_client.c::publish_results_with_error()`

4. **Error Handling**:
   - All Wasm execution failures publish structured error envelopes
   - Error messages included in `error` field of result message
   - Validates update size limits (max: 1536 bytes before base64 encoding)
   - Location: `embed-proplet/src/wasm_handler.c::execute_wasm_module()`

5. **Validation**:
   - Validates FL task has required `job_id` field
   - Validates update payload size before encoding
   - Ensures null-terminated strings for all parsed fields
   - Location: `embed-proplet/src/mqtt_client.c::handle_start_command()`, `publish_results_with_error()`

### Memory Constraints

- **Update Size Limit**: 1536 bytes (before base64 encoding) - configurable via `MAX_UPDATE_B64_LEN`
- **Base64 Buffer**: 2048 bytes for encoded updates
- **Task Structure**: Fixed-size buffers for all FL fields (see `MAX_ID_LEN`, `MAX_NAME_LEN`, etc.)

### Testing

A host-build test is provided to verify envelope structure:
- Location: `embed-proplet/test_fl_envelope.c`
- Compile: `gcc -o test_fl_envelope test_fl_envelope.c -I../src -L. -lmqtt_client -lcjson -lm`
- Run: `./test_fl_envelope`

For full integration testing, use the Manager's FL integration test suite which exercises both Rust and embedded proplets.

### Parity with Rust Proplet

The embedded proplet maintains full parity with the Rust proplet:
- ✅ Same UpdateEnvelope structure
- ✅ Same MQTT topic naming
- ✅ Same validation rules
- ✅ Same error handling semantics
- ✅ Same FL environment variable support

## Key Algorithms

### FedAvg (Federated Averaging)

The aggregation algorithm:

```
1. Collect updates from N clients: {u₁, u₂, ..., uₙ}
2. Each update has num_samples: {n₁, n₂, ..., nₙ}
3. Total samples: N = Σnᵢ
4. Weighted average: w_agg = Σ(uᵢ * nᵢ) / N
```

**Implementation**: `manager/service.go::aggregateJSONF64()`

### Round Progress Tracking

The manager tracks:
- **Expected**: Number of proplets that should participate (tasks created)
- **Completed**: Number of proplets that sent updates
- **Updates**: Map of `proplet_id → UpdateEnvelope`

Aggregation triggers when `completed >= expected` (or timeout).

**Implementation**: `manager/service.go::roundProgress()`

## Privacy & Security

- **No Raw Data**: Only model updates (weights) are transmitted, never training data
- **Secure Communication**: MQTT over SuperMQ with authentication
- **Isolated Execution**: Wasm modules run in isolated runtime (Wasmtime)
- **Update Validation**: Manager validates job_id, round_id, proplet_id to prevent attacks

## Example Flow

```
1. User creates FL task via CLI:
   $ propeller-cli fl create my-fl-job --mode train --rounds 3 --clients-per-round 2

2. Manager creates round 1 tasks for 2 proplets

3. Proplet 1 & 2 receive start commands, fetch model, train locally

4. Proplet 1 sends update: [1.0, 2.0] with 10 samples
   Proplet 2 sends update: [2.0, 3.0] with 20 samples

5. Manager aggregates: [(1*10 + 2*20)/30, (2*10 + 3*20)/30] = [1.67, 2.67]

6. Manager creates round 2 tasks with aggregated model [1.67, 2.67]

7. Process repeats for rounds 2 and 3

8. Final aggregated model is the result
```

## Code Locations

- **Task Definition**: `task/task.go`
- **Manager Orchestration**: `manager/service.go`
- **Rust Proplet Execution**: `proplet/src/service.rs`
- **Embedded Proplet Execution**: `embed-proplet/src/mqtt_client.c`, `embed-proplet/src/wasm_handler.c`
- **Aggregation Logic**: `manager/service.go::aggregateJSONF64()`
- **Update Envelope**: `pkg/fl/types.go`
- **CLI Commands**: `cli/fl.go`
- **Example Wasm Module**: `examples/fl-train/fl-train.go`

## Testing

- **Unit Tests**: `manager/service_test.go` (FedAvg correctness)
- **Integration Tests**: `manager/fl_integration_test.go` (end-to-end workflow)
- **Embedded Proplet Test**: `embed-proplet/test_fl_envelope.c` (envelope structure validation)

Run tests: 
- Manager: `make test` or `go test -v ./manager`
- Embedded: `gcc -o test_fl_envelope embed-proplet/test_fl_envelope.c -Iembed-proplet/src -L. -lmqtt_client -lcjson -lm && ./test_fl_envelope`
