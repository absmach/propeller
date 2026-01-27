# Kubernetes Operator Implementation Summary

## Overview

This document summarizes the implementation of missing features for the Kubernetes operator to achieve functional equivalence with the standalone manager for both general WASM workloads and Federated Learning (FL) workflows.

## Implemented Features

### 1. FL Aggregation Algorithms ✅

**File**: `manager/operator/controllers/aggregation.go`

- **`AggregateFLUpdates()`**: Main aggregation function that routes to format-specific aggregators
- **`aggregateJSONF64()`**: Weighted average aggregation for JSON-encoded float64 vectors (FedAvg algorithm)
- **`aggregateConcat()`**: Concatenation-based aggregation for other formats

**Features**:
- Supports multiple aggregation formats (`json-f64`, `concat`, etc.)
- Weighted averaging based on number of samples per participant
- Dimension validation and error handling
- Generates aggregated update envelopes with metadata

### 2. Result Collection from Kubernetes Jobs ✅

**File**: `manager/operator/controllers/result_extractor.go`

- **`ExtractResultFromJob()`**: Extracts results from completed Jobs
  - First checks Job annotations (`propeller.absmach.io/result`)
  - Falls back to Pod termination messages
  - Returns structured result data

- **`ExtractFLUpdateFromResult()`**: Extracts FL update envelopes from task results
  - Parses nested result structures
  - Validates required fields (job_id, round_id, proplet_id)
  - Handles multiple result formats

- **`UpdateJobWithResult()`**: Helper to annotate Jobs with result data

**Integration**: Updated `WasmTask` controller to extract and store results in status

### 3. FL Update Collection and Validation ✅

**File**: `manager/operator/controllers/traininground_controller.go`

**Enhanced `handleRunning()` method**:
- Collects FL update envelopes from completed WasmTasks
- Validates updates match the current round
- Stores collected updates in annotations for aggregation phase
- Tracks update count vs k-of-n requirement
- Handles timeout scenarios

**Features**:
- Round ID validation
- Proplet ID matching
- Graceful handling of tasks without FL updates
- Stores updates in annotations for aggregation

### 4. Round Progress Tracking ✅

**File**: `manager/operator/controllers/traininground_controller.go`

- Tracks `UpdatesReceived` vs `UpdatesRequired` (k-of-n)
- Monitors participant status (Pending, Running, Completed, Failed)
- Timeout detection and handling
- Transition to Aggregating phase when k-of-n met

### 5. Functional PropletGroup Controller ✅

**File**: `manager/operator/controllers/propletgroup_controller.go`

**Proplet Discovery**:
- Discovers proplets as Pods with matching labels
- Falls back to Node discovery if no Pods found
- Tracks proplet liveness and availability
- Counts tasks per proplet

**Scheduling Algorithms**:
- **`SelectPropletFromGroup()`**: Implements multiple scheduling algorithms:
  - `round-robin`: Distributes tasks evenly
  - `least-loaded`: Selects proplet with fewest tasks
  - `random`: Random selection
  - Default: First available proplet

**Status Updates**:
- Updates `TotalProplets` and `AvailableProplets`
- Maintains `PropletInfo` list with liveness and task counts
- Reconciles every 30 seconds

### 6. Proper Proplet Selection (Replaced Hardcoded) ✅

**File**: `manager/operator/controllers/wasmtask_controller.go`

**Enhanced `handlePending()` method**:
- Uses `PropletGroup` if specified in task spec
- Applies scheduling algorithm from PropletGroup
- Falls back to default only if group unavailable
- Logs selection decisions

**File**: `manager/operator/controllers/scheduler.go`

- New `Scheduler` type for centralized proplet selection
- Integrates with PropletGroup controller
- Handles explicit proplet IDs, PropletGroup references, and defaults

### 7. FL Environment Variable Injection for Subsequent Rounds ✅

**File**: `manager/operator/controllers/traininground_controller.go`

**Enhanced environment variable setup**:
- Injects `ROUND_ID`, `MODEL_URI`, `PROPLET_ID`
- Adds `FL_GLOBAL_VERSION`, `FL_GLOBAL_UPDATE_B64`, `FL_GLOBAL_UPDATE_FORMAT` from previous round's aggregation
- Adds `FL_JOB_ID` from parent FederatedJob
- Passes hyperparameters as JSON

**File**: `manager/operator/controllers/federatedjob_controller.go`

- Passes aggregated update annotations to next TrainingRound
- Updates aggregated model reference in FederatedJob status
- Ensures model continuity across rounds

## Architecture Decisions

### Result Storage

Results are stored in multiple places for reliability:
1. **Job Annotations**: `propeller.absmach.io/result` - Primary storage
2. **Pod Termination Messages**: Fallback if annotations not available
3. **WasmTask Status**: `status.results` - Exposed via CRD

### FL Update Flow

1. **Collection**: TrainingRound controller collects updates from completed WasmTasks
2. **Storage**: Updates stored in TrainingRound annotations (`propeller.absmach.io/collected-updates`)
3. **Aggregation**: Aggregated update stored in annotations (`propeller.absmach.io/aggregated-update`)
4. **Propagation**: Aggregated update passed to next TrainingRound via annotations

### Proplet Discovery

Proplets can be discovered as:
1. **Pods**: With labels matching PropletGroup selector (preferred)
2. **Nodes**: With labels matching PropletGroup selector (fallback)

Liveness is determined by:
- **Pods**: `Pod.Phase == Running`
- **Nodes**: `NodeReady` condition status

## Usage Examples

### General WASM Task

```yaml
apiVersion: propeller.absmach.io/v1alpha1
kind: WasmTask
metadata:
  name: my-wasm-task
spec:
  imageUrl: "oci://registry.example.com/wasm/my-task:v1.0.0"
  propletGroupRef:
    name: edge-devices
  env:
    INPUT_PATH: "/data/input"
```

### Federated Learning Job

```yaml
apiVersion: propeller.absmach.io/v1alpha1
kind: FederatedJob
metadata:
  name: fl-experiment
spec:
  experimentId: "exp-001"
  modelRef: "oci://registry.example.com/models/initial:v1"
  taskWasmImage: "oci://registry.example.com/fl-client:v1.0.0"
  participants:
    - propletId: "proplet-1"
    - propletId: "proplet-2"
  kOfN: 2
  timeoutSeconds: 600
  rounds:
    total: 10
  aggregator:
    algorithm: "fedavg"
```

### PropletGroup

```yaml
apiVersion: propeller.absmach.io/v1alpha1
kind: PropletGroup
metadata:
  name: edge-devices
spec:
  selector:
    matchLabels:
      proplet-type: "edge"
  scheduling:
    algorithm: "least-loaded"
    maxTasksPerProplet: 5
```

## Result Format Expectations

### For General WASM Tasks

Jobs should output results in one of these formats:

**Option 1: Job Annotation**
```yaml
annotations:
  propeller.absmach.io/result: '{"output": "result data", "metrics": {...}}'
```

**Option 2: Pod Termination Message**
```yaml
# In pod spec terminationMessagePolicy: FallbackToLogsOnError
# Output JSON to stdout/stderr
```

### For FL Tasks

Results should include an FL update envelope:

```json
{
  "update_envelope": {
    "job_id": "exp-001",
    "round_id": 1,
    "proplet_id": "proplet-1",
    "num_samples": 1000,
    "update_b64": "base64-encoded-update",
    "format": "json-f64",
    "metrics": {
      "loss": 0.5,
      "accuracy": 0.9
    }
  }
}
```

Or nested in results:

```json
{
  "results": {
    "update_envelope": {
      "job_id": "exp-001",
      "round_id": 1,
      ...
    }
  }
}
```

## Limitations and Future Work

### Current Limitations

1. **Model Registry Integration**: Aggregated models are referenced but not actually uploaded to a registry
2. **Metrics Collection**: Infrastructure exists but no automatic collection from Jobs
3. **Proplet Health**: Basic liveness tracking, but no detailed health checks
4. **Result Extraction**: Relies on Job annotations or pod termination messages

### Future Enhancements

1. **Model Registry Upload**: Implement actual OCI registry upload for aggregated models
2. **Metrics API**: Expose metrics collection endpoints similar to standalone manager
3. **Advanced Scheduling**: Implement more sophisticated scheduling algorithms
4. **Proplet Health Monitoring**: Add detailed health checks and automatic recovery
5. **Result Streaming**: Support streaming results from long-running tasks

## Testing Recommendations

1. **Unit Tests**: Test aggregation algorithms with various input formats
2. **Integration Tests**: Test full FL round lifecycle
3. **E2E Tests**: Test with actual Kubernetes clusters and proplet deployments
4. **Result Format Tests**: Verify result extraction from different formats

## Migration Notes

For users migrating from standalone manager:

1. **Proplet Deployment**: Proplets should be deployed as Pods or DaemonSets with appropriate labels
2. **Result Format**: Ensure Jobs output results in expected format (annotations or termination messages)
3. **PropletGroup Setup**: Create PropletGroups to enable scheduling
4. **FL Updates**: Ensure FL tasks output update envelopes in the expected format
