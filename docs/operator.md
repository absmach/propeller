# Propeller Kubernetes Operator

Propeller Kubernetes Operator provides Kubernetes-native orchestration for **WebAssembly (WASM) workloads** across the cloud-edge continuum. It extends Kubernetes with Custom Resource Definitions (CRDs) to manage WASM tasks, with optional support for federated learning workflows.

## Philosophy

Propeller is fundamentally a **general WASM orchestrator**. The operator reflects this by:
- **Primary Use Case**: Orchestrating any WASM workload (functions, services, data processing, etc.)
- **Specialized Workflow**: Federated learning is built on top of general WASM execution
- **Flexible Execution**: WASM tasks can run with or without FL context - FL features activate automatically when FL environment variables are present

### Key Features

- **Kubernetes-Native**: Uses CRDs and standard Kubernetes primitives (Jobs, ConfigMaps, Secrets)
- **General WASM Orchestration**: Primary use case - manages any WASM workload from simple functions to complex applications
- **Cloud-Edge Continuum**: Deploy WASM workloads from cloud servers to edge devices
- **Proplet Management**: Groups and schedules proplets (workers) for task execution
- **Federated Learning Support** (Optional): Specialized workflow for multi-round FL experiments with participant coordination
- **Model Artifact Management**: Tracks aggregated model checkpoints (for FL workloads)
- **Hybrid Deployment**: Can work alongside the standalone HTTP/MQTT manager

## Architecture

The operator follows a dual-manager architecture:

```
┌─────────────────────────────────────────────────────────┐
│              Core Orchestration Package                  │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  │
│  │  Scheduler   │  │ State Machine│  │ FL Coordinator│  │
│  └──────────────┘  └──────────────┘  └──────────────┘  │
└─────────────────────────────────────────────────────────┘
           │                        │
           ▼                        ▼
┌──────────────────────┐  ┌──────────────────────┐
│  Standalone Manager  │  │  Operator Manager    │
│  (HTTP + MQTT)       │  │  (K8s CRDs)          │
└──────────────────────┘  └──────────────────────┘
```

### Components

- **WasmTask Controller** (Primary): Manages general WASM workloads - the core orchestration capability
- **PropletGroup Controller**: Manages groups of proplets for scheduling
- **FederatedJob Controller** (Specialized): Manages FL experiments built on top of WASM tasks
- **TrainingRound Controller** (Specialized): Orchestrates individual FL training rounds
- **Core Orchestration**: Shared logic for scheduling, state management, and workload coordination

### Workload Execution Model

The operator follows the same model as the Rust proplet:

1. **General WASM Execution** (Default - Primary Use Case):
   - Any WASM binary/image can be executed
   - Standard task lifecycle: Pending → Running → Completed/Failed
   - Results published via standard result messages
   - No special configuration needed - just provide a WASM image/binary

2. **FL-Enhanced Execution** (Automatic - When FL context present):
   - **Same WASM execution** as above, but with FL-specific enhancements that activate automatically:
     - Automatic model fetching from registry (if `MODEL_URI` env var present)
     - Automatic dataset fetching from data store (if configured)
     - FL update publishing to coordinator (if `ROUND_ID` env var present)
   - **No special configuration needed** - FL features activate based on environment variables
   - The proplet detects FL context and automatically enables FL features
   - You can use `WasmTask` directly with FL env vars, or use `FederatedJob` for multi-round orchestration

**Key Principle**: FL is not a separate execution mode - it's the same WASM execution with automatic FL enhancements when FL environment variables are detected.

## Installation

### Prerequisites

- Kubernetes cluster (v1.20+)
- `kubectl` configured to access your cluster
- Helm 3.x (optional, for Helm installation)

### Option 1: Using Helm (Recommended)

1. **Add the Helm repository** (if using a chart repository):
   ```bash
   helm repo add propeller https://charts.propeller.io
   helm repo update
   ```

2. **Install the operator**:
   ```bash
   helm install propeller-operator ./config/helm \
     --namespace propeller-system \
     --create-namespace
   ```

3. **Verify installation**:
   ```bash
   kubectl get pods -n propeller-system
   kubectl get crds | grep propeller
   ```

### Option 2: Using Kustomize

1. **Apply RBAC and CRDs**:
   ```bash
   kubectl apply -f config/rbac/
   kubectl apply -f config/crd/bases/
   ```

2. **Deploy the operator**:
   ```bash
   kubectl apply -f config/manager/manager.yaml
   ```

3. **Verify installation**:
   ```bash
   kubectl get pods -n system
   ```

### Option 3: Manual Installation

1. **Create namespace**:
   ```bash
   kubectl create namespace propeller-system
   ```

2. **Apply RBAC**:
   ```bash
   kubectl apply -f config/rbac/service_account.yaml
   kubectl apply -f config/rbac/role.yaml
   kubectl apply -f config/rbac/role_binding.yaml
   kubectl apply -f config/rbac/leader_election_role.yaml
   kubectl apply -f config/rbac/leader_election_role_binding.yaml
   ```

3. **Apply CRDs**:
   ```bash
   kubectl apply -f config/crd/bases/
   ```

4. **Deploy operator**:
   ```bash
   kubectl apply -f config/manager/manager.yaml
   ```

### Configuration

The operator can be configured via environment variables or Helm values:

| Variable | Description | Default |
|----------|-------------|---------|
| `WATCH_NAMESPACE` | Namespace to watch (empty = all namespaces) | `""` |
| `--leader-elect` | Enable leader election | `false` |
| `--metrics-bind-address` | Metrics server address | `:8080` |
| `--health-probe-bind-address` | Health probe address | `:8081` |

## Custom Resource Definitions

### WasmTask (Primary CRD)

The `WasmTask` CRD is the primary resource for orchestrating WASM workloads. It represents any WebAssembly task that can be executed on proplets.

A `WasmTask` represents a general WebAssembly workload that can be executed on proplets.

#### Spec Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `imageUrl` | string | Yes* | OCI reference to the WASM image |
| `file` | []byte | No | WASM binary file (base64 encoded). If provided, takes precedence over imageUrl |
| `cliArgs` | []string | No | Command-line arguments for the WASM workload |
| `inputs` | []uint64 | No | Input data for the workload |
| `env` | map[string]string | No | Environment variables |
| `daemon` | bool | No | Run as a daemon (long-running) |
| `propletId` | string | No | Specific proplet to run on. If empty, scheduler selects |
| `propletGroupRef` | LocalObjectReference | No | Reference to PropletGroup for scheduling |
| `mode` | string | No | Execution mode: `infer`, `train` |
| `monitoringProfile` | MonitoringProfile | No | Monitoring configuration |
| `resources` | ResourceRequirements | No | Kubernetes resource requirements |
| `restartPolicy` | RestartPolicy | No | Pod restart policy |

*Either `imageUrl` or `file` must be provided.

#### Status Fields

| Field | Type | Description |
|-------|------|-------------|
| `phase` | string | Current phase: `Pending`, `Scheduled`, `Running`, `Completed`, `Failed` |
| `propletId` | string | Proplet where the task is running |
| `jobRef` | ObjectReference | Reference to the K8s Job executing this task |
| `startTime` | Time | When the task started |
| `finishTime` | Time | When the task finished |
| `results` | map[string]interface{} | Task execution results |
| `error` | string | Error message if task failed |
| `conditions` | []Condition | Kubernetes conditions |

#### Example: Simple WASM Task

```yaml
apiVersion: propeller.absmach.io/v1alpha1
kind: WasmTask
metadata:
  name: image-processor
  namespace: default
spec:
  imageUrl: "oci://registry.example.com/wasm/image-processor:v1.0.0"
  env:
    INPUT_PATH: "/data/input.jpg"
    OUTPUT_PATH: "/data/output.jpg"
  mode: "infer"
  resources:
    requests:
      memory: "128Mi"
      cpu: "100m"
    limits:
      memory: "256Mi"
      cpu: "500m"
```

#### Example: WASM Task with Binary File

```yaml
apiVersion: propeller.absmach.io/v1alpha1
kind: WasmTask
metadata:
  name: custom-wasm-task
spec:
  file: <base64-encoded-wasm-binary>
  cliArgs:
    - "--input"
    - "/data/input"
    - "--output"
    - "/data/output"
  env:
    DEBUG: "true"
```

#### Example: Daemon WASM Task

```yaml
apiVersion: propeller.absmach.io/v1alpha1
kind: WasmTask
metadata:
  name: wasm-service
spec:
  imageUrl: "oci://registry.example.com/wasm/api-server:v1.0.0"
  daemon: true
  env:
    PORT: "8080"
  restartPolicy: Always
  monitoringProfile:
    enabled: true
    interval: 30
    metrics:
      - "cpu"
      - "memory"
      - "requests"
```

#### Example: WASM Task with FL Context (Automatic FL Activation)

When you add FL environment variables to a WasmTask, the proplet automatically activates FL features:

```yaml
apiVersion: propeller.absmach.io/v1alpha1
kind: WasmTask
metadata:
  name: fl-training-task
spec:
  imageUrl: "oci://registry.example.com/wasm/fl-client:v1.0.0"
  env:
    ROUND_ID: "round-1"           # FL feature: Activates FL mode
    MODEL_URI: "oci://registry/models/mnist:v1"  # FL feature: Fetches model
    HYPERPARAMS: '{"learning_rate": 0.01, "batch_size": 32}'
    # Proplet automatically:
    # - Fetches model from registry
    # - Fetches dataset from data store
    # - Publishes FL updates to coordinator
```

**Note**: This is how FederatedJob works internally - it creates WasmTasks with FL environment variables. You can also create FL tasks directly using WasmTask.

### FederatedJob (Specialized Workflow)

A `FederatedJob` represents a specialized federated learning workflow built on top of general WASM task execution. It orchestrates multiple training rounds, where each round creates WASM tasks with FL-specific environment variables.

#### Spec Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `experimentId` | string | Yes | Unique identifier for the experiment |
| `modelRef` | string | Yes | OCI reference to the initial model |
| `taskWasmImage` | string | Yes | OCI reference to the WASM image for FL client tasks |
| `participants` | []ParticipantSpec | Yes | List of proplet IDs participating in the experiment |
| `hyperparams` | map[string]interface{} | No | Training hyperparameters |
| `kOfN` | int | Yes | Minimum participants required for aggregation |
| `timeoutSeconds` | int | Yes | Timeout for each round in seconds |
| `rounds.total` | int | Yes | Total number of training rounds |
| `rounds.strategy` | string | No | Round execution strategy (`sequential` or `parallel`) |
| `aggregator.algorithm` | string | Yes | Aggregation algorithm (e.g., `fedavg`) |
| `aggregator.config` | map[string]interface{} | No | Algorithm-specific configuration |

#### Status Fields

| Field | Type | Description |
|-------|------|-------------|
| `phase` | string | Current phase: `Pending`, `Running`, `Completed`, `Failed` |
| `currentRound` | int | Current round number |
| `completedRounds` | int | Number of completed rounds |
| `conditions` | []Condition | Kubernetes conditions |
| `participants` | []ParticipantStatus | Status of each participant |
| `aggregatedModelRef` | string | OCI reference to the latest aggregated model |

#### Example

```yaml
apiVersion: propeller.absmach.io/v1alpha1
kind: FederatedJob
metadata:
  name: fl-mnist-experiment
  namespace: default
spec:
  experimentId: "exp-001"
  modelRef: "oci://registry.example.com/models/mnist:v1"
  taskWasmImage: "oci://registry.example.com/fl-client:latest"
  participants:
    - propletId: "proplet-1"
      namespace: "default"
    - propletId: "proplet-2"
      namespace: "default"
    - propletId: "proplet-3"
      namespace: "default"
  hyperparams:
    learningRate: 0.01
    batchSize: 32
    epochs: 5
  kOfN: 2
  timeoutSeconds: 300
  rounds:
    total: 10
    strategy: "sequential"
  aggregator:
    algorithm: "fedavg"
    config: {}
```

### TrainingRound

A `TrainingRound` represents a single federated learning training round.

#### Spec Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `roundId` | string | Yes | Unique identifier for this round |
| `federatedJobRef` | LocalObjectReference | Yes | Reference to parent FederatedJob |
| `modelRef` | string | Yes | OCI reference to the model for this round |
| `taskWasmImage` | string | Yes | OCI reference to the WASM image |
| `participants` | []string | Yes | List of proplet IDs |
| `hyperparams` | map[string]interface{} | No | Round-specific hyperparameters |
| `kOfN` | int | Yes | Minimum participants for aggregation |
| `timeoutSeconds` | int | Yes | Round timeout in seconds |

#### Status Fields

| Field | Type | Description |
|-------|------|-------------|
| `phase` | string | Current phase: `Pending`, `Running`, `Aggregating`, `Completed`, `Failed` |
| `startTime` | Time | When the round started |
| `endTime` | Time | When the round ended |
| `participants` | []RoundParticipantStatus | Status of each participant |
| `updatesReceived` | int | Number of updates received |
| `updatesRequired` | int | Number of updates required (k-of-n) |
| `aggregatedModelRef` | string | OCI reference to aggregated model |
| `conditions` | []Condition | Kubernetes conditions |

#### Example

```yaml
apiVersion: propeller.absmach.io/v1alpha1
kind: TrainingRound
metadata:
  name: round-1-fl-mnist-experiment
  namespace: default
spec:
  roundId: "round-1"
  federatedJobRef:
    name: fl-mnist-experiment
  modelRef: "oci://registry.example.com/models/mnist:v1"
  taskWasmImage: "oci://registry.example.com/fl-client:latest"
  participants:
    - "proplet-1"
    - "proplet-2"
    - "proplet-3"
  hyperparams:
    learningRate: 0.01
  kOfN: 2
  timeoutSeconds: 300
```

### PropletGroup

A `PropletGroup` represents a group of proplets (workers) for scheduling.

#### Spec Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `selector.matchLabels` | map[string]string | No | Label selector for matching proplets |
| `scheduling.algorithm` | string | Yes | Scheduling algorithm: `round-robin`, `least-loaded`, `random` |
| `scheduling.maxTasksPerProplet` | int | No | Maximum tasks per proplet |

#### Status Fields

| Field | Type | Description |
|-------|------|-------------|
| `proplets` | []PropletInfo | List of proplets in the group |
| `totalProplets` | int | Total number of proplets |
| `availableProplets` | int | Number of available proplets |

#### Example

```yaml
apiVersion: propeller.absmach.io/v1alpha1
kind: PropletGroup
metadata:
  name: edge-devices
  namespace: default
spec:
  selector:
    matchLabels:
      proplet-type: "edge"
  scheduling:
    algorithm: "round-robin"
    maxTasksPerProplet: 1
```

### ModelCheckpoint

A `ModelCheckpoint` represents an aggregated model artifact.

#### Spec Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `federatedJobRef` | LocalObjectReference | Yes | Reference to parent FederatedJob |
| `roundRef` | LocalObjectReference | No | Reference to TrainingRound |
| `modelRef` | string | Yes | OCI reference to the model artifact |
| `algorithm` | string | Yes | Aggregation algorithm used |
| `metadata` | CheckpointMetadata | No | Additional metadata |

#### Status Fields

| Field | Type | Description |
|-------|------|-------------|
| `phase` | string | Current phase: `Uploading`, `Ready`, `Failed` |
| `uploadTime` | Time | When the checkpoint was uploaded |
| `sizeBytes` | int64 | Size of the checkpoint in bytes |

#### Example

```yaml
apiVersion: propeller.absmach.io/v1alpha1
kind: ModelCheckpoint
metadata:
  name: model-exp-001-round-5
  namespace: default
spec:
  federatedJobRef:
    name: fl-mnist-experiment
  roundRef:
    name: round-5-fl-mnist-experiment
  modelRef: "oci://registry.example.com/models/mnist:v5"
  algorithm: "fedavg"
  metadata:
    totalSamples: 10000
    numParticipants: 3
    metrics:
      accuracy: 0.95
      loss: 0.05
```

## Usage Examples

### General WASM Workloads (Primary Use Case)

The operator's primary purpose is orchestrating general WASM workloads. These examples show common patterns:

#### Simple WASM Function

```yaml
apiVersion: propeller.absmach.io/v1alpha1
kind: WasmTask
metadata:
  name: hello-wasm
spec:
  imageUrl: "oci://registry.example.com/wasm/hello:v1.0.0"
  env:
    MESSAGE: "Hello from Kubernetes!"
```

```bash
kubectl apply -f wasmtask.yaml
kubectl get wasmtask hello-wasm
kubectl logs job/hello-wasm-job
```

#### Data Processing Task

```yaml
apiVersion: propeller.absmach.io/v1alpha1
kind: WasmTask
metadata:
  name: data-processor
spec:
  imageUrl: "oci://registry.example.com/wasm/data-processor:v1.0.0"
  cliArgs:
    - "--input"
    - "/data/raw"
    - "--output"
    - "/data/processed"
  env:
    BATCH_SIZE: "1000"
    PARALLEL: "true"
  resources:
    requests:
      memory: "512Mi"
      cpu: "500m"
    limits:
      memory: "1Gi"
      cpu: "1000m"
```

#### Scheduled Task with PropletGroup

```yaml
apiVersion: propeller.absmach.io/v1alpha1
kind: WasmTask
metadata:
  name: edge-analytics
spec:
  imageUrl: "oci://registry.example.com/wasm/analytics:v1.0.0"
  propletGroupRef:
    name: edge-devices
  env:
    SENSOR_ID: "sensor-001"
    INTERVAL: "60"
```

### Federated Learning Workloads (Specialized Workflow)

Federated learning is a specialized workflow that uses general WASM tasks with FL-specific environment variables. The operator creates WASM tasks that automatically activate FL features when `ROUND_ID` and `MODEL_URI` are present.

#### Basic Federated Learning Experiment

1. **Create a FederatedJob**:

```yaml
apiVersion: propeller.absmach.io/v1alpha1
kind: FederatedJob
metadata:
  name: mnist-fl-experiment
spec:
  experimentId: "mnist-exp-001"
  modelRef: "oci://registry.example.com/models/mnist:initial"
  taskWasmImage: "oci://registry.example.com/fl-client:v1.0.0"
  participants:
    - propletId: "proplet-1"
    - propletId: "proplet-2"
    - propletId: "proplet-3"
  kOfN: 2
  timeoutSeconds: 600
  rounds:
    total: 20
    strategy: "sequential"
  aggregator:
    algorithm: "fedavg"
```

```bash
kubectl apply -f federatedjob.yaml
```

2. **Monitor the experiment**:

```bash
# Check FederatedJob status
kubectl get federatedjob mnist-fl-experiment -o yaml

# Watch TrainingRounds
kubectl get trainingrounds -l owner=federatedjob

# Check Jobs created for participants
kubectl get jobs -l round=round-1

# View logs from a participant job
kubectl logs job/round-1-mnist-fl-experiment-proplet-1
```

3. **Check round status**:

```bash
kubectl describe traininground round-1-mnist-fl-experiment
```

### Multi-Namespace Deployment

To deploy the operator in a specific namespace:

```bash
helm install propeller-operator ./config/helm \
  --namespace propeller-system \
  --create-namespace \
  --set watchNamespace="default"
```

### Custom Scheduling

Create a PropletGroup with custom scheduling:

```yaml
apiVersion: propeller.absmach.io/v1alpha1
kind: PropletGroup
metadata:
  name: gpu-proplets
spec:
  selector:
    matchLabels:
      accelerator: "gpu"
  scheduling:
    algorithm: "least-loaded"
    maxTasksPerProplet: 2
```

## Controller Behavior

### WasmTask Controller (Primary)

The WasmTask controller is the core orchestration component, managing general WASM workloads:

1. **Pending Phase**:
   - Validates the spec (ensures imageUrl or file is provided)
   - Selects a proplet (using PropletID, PropletGroupRef, or default scheduler)
   - Creates a ConfigMap with environment variables and configuration
   - Creates a Kubernetes Job to execute the WASM workload
   - Transitions to `Running`

2. **Running Phase**:
   - Watches the Job status
   - On job success: extracts results, transitions to `Completed`
   - On job failure: captures error, transitions to `Failed`
   - **Note**: If FL environment variables (ROUND_ID, MODEL_URI) are present, the proplet automatically activates FL features

3. **Completed/Failed Phase**:
   - Terminal states, no further reconciliation
   - Results (if any) are stored in status.results

**FL Integration**: When a WasmTask includes FL environment variables (`ROUND_ID`, `MODEL_URI`), the proplet automatically:
- Fetches models from the model registry
- Fetches datasets from the data store
- Publishes FL updates to the coordinator
- No special WasmTask configuration needed - FL features activate based on environment variables

### FederatedJob Controller (Specialized)

The FederatedJob controller is a specialized workflow built on top of WasmTask orchestration:

1. **Pending Phase**:
   - Validates the spec
   - Creates the first TrainingRound (which creates WasmTasks with FL env vars)
   - Transitions to `Running`

2. **Running Phase**:
   - Watches TrainingRound status
   - Each TrainingRound creates WasmTasks with FL environment variables:
     - `ROUND_ID`: Identifies the training round
     - `MODEL_URI`: Points to the model to train
     - `HYPERPARAMS`: Training hyperparameters (JSON)
   - On round completion:
     - If more rounds remain: creates next TrainingRound
     - If all rounds complete: transitions to `Completed`
   - On round failure: transitions to `Failed`

3. **Completed/Failed Phase**:
   - Terminal states, no further reconciliation

**Key Insight**: FederatedJob doesn't execute FL directly - it orchestrates WasmTasks that have FL context. The actual FL behavior happens in the proplet when it detects FL environment variables.

### TrainingRound Controller (Specialized)

The TrainingRound controller orchestrates individual FL training rounds by creating WasmTasks with FL context:

1. **Pending Phase**:
   - Creates WasmTasks (via K8s Jobs) for each participant proplet
   - Each WasmTask includes FL environment variables:
     - `ROUND_ID`: Current round identifier
     - `MODEL_URI`: Model reference for this round
     - `HYPERPARAMS`: Training hyperparameters
   - Creates ConfigMaps with task configuration
   - Transitions to `Running`

2. **Running Phase**:
   - Watches WasmTask/Job status for each participant
   - Proplets automatically detect FL context and activate FL features
   - Collects FL updates (k-of-n threshold)
   - On timeout: transitions to `Failed`
   - When k-of-n met: transitions to `Aggregating`

3. **Aggregating Phase**:
   - Performs model aggregation
   - Updates aggregated model reference
   - Transitions to `Completed`

4. **Completed/Failed Phase**:
   - Terminal states

**Key Insight**: TrainingRound creates standard WasmTasks. The FL behavior is automatic when proplets see FL environment variables.

### PropletGroup Controller

The PropletGroup controller manages groups of proplets for scheduling:

- Discovers proplets based on label selectors
- Tracks proplet health and availability
- Updates status with proplet information

## Understanding General WASM vs FL

### How They Work Together

The operator's design reflects Propeller's architecture:

1. **WasmTask is the Foundation**:
   - Executes any WASM workload
   - No FL-specific code in the operator
   - Standard Kubernetes Job execution

2. **FL is Environment-Based**:
   - When a WasmTask has `ROUND_ID` and `MODEL_URI` env vars, the proplet automatically:
     - Detects FL context
     - Fetches model from registry
     - Fetches dataset from data store
     - Publishes FL updates to coordinator
   - This happens in the proplet, not the operator

3. **FederatedJob is a Convenience**:
   - Orchestrates multiple rounds
   - Creates WasmTasks with FL env vars automatically
   - Tracks round completion and aggregation
   - You could achieve the same with WasmTask + manual round management

### When to Use What

- **Use WasmTask**: For any WASM workload (functions, services, data processing, single FL tasks)
- **Use FederatedJob**: For multi-round FL experiments with automatic round management
- **Use TrainingRound**: Usually created by FederatedJob, but can be used standalone for single-round FL

## Troubleshooting

### Operator Not Starting

**Symptoms**: Operator pod is not running or in CrashLoopBackOff

**Diagnosis**:
```bash
kubectl logs -n system deployment/propeller-operator
kubectl describe pod -n system -l control-plane=controller-manager
```

**Common Issues**:
- Missing RBAC permissions: Check ClusterRole and ClusterRoleBinding
- CRDs not installed: Verify CRDs are applied
- Image pull errors: Check image repository and credentials

### WasmTask Not Starting

**Symptoms**: WasmTask phase remains `Pending`

**Diagnosis**:
```bash
kubectl describe wasmtask <name>
kubectl get events --field-selector involvedObject.name=<name>
kubectl logs -n system deployment/propeller-operator
```

**Common Issues**:
- Invalid spec: Check validation errors in status conditions
- Missing image: Verify `imageUrl` is accessible or `file` is provided
- Proplet selection: Check if proplet exists (if `propletId` specified) or PropletGroup is valid
- Resource quotas: Check if namespace has sufficient resources

### FederatedJob Stuck in Pending

**Symptoms**: FederatedJob phase remains `Pending`

**Diagnosis**:
```bash
kubectl describe federatedjob <name>
kubectl get events --field-selector involvedObject.name=<name>
```

**Common Issues**:
- Invalid spec: Check validation errors in status conditions
- Missing participants: Ensure proplets exist and are accessible
- Resource quotas: Check if namespace has sufficient resources
- TrainingRound creation: Check if TrainingRound CRDs are being created

### TrainingRound Not Creating Jobs

**Symptoms**: TrainingRound created but no Jobs are created

**Diagnosis**:
```bash
kubectl describe traininground <name>
kubectl logs -n system deployment/propeller-operator
```

**Common Issues**:
- Insufficient permissions: Check if operator can create Jobs
- Invalid image reference: Verify `taskWasmImage` is accessible
- Namespace issues: Ensure operator can create resources in target namespace

### Jobs Failing

**Symptoms**: Participant Jobs are failing

**Diagnosis**:
```bash
kubectl describe job <job-name>
kubectl logs job/<job-name>
```

**Common Issues**:
- Image pull errors: Check image accessibility and credentials
- Resource limits: Verify pod resource requests/limits
- Configuration errors: Check ConfigMap data

### Aggregation Not Completing

**Symptoms**: TrainingRound stuck in `Aggregating` phase

**Diagnosis**:
```bash
kubectl get traininground <name> -o yaml
kubectl logs -n system deployment/propeller-operator | grep aggregation
kubectl get wasmtasks -l round=<round-id>
```

**Common Issues**:
- Aggregator service unavailable: Check aggregator service connectivity
- Model storage issues: Verify model registry access
- Timeout: Check if aggregation is taking longer than expected
- Insufficient updates: Verify WasmTasks completed successfully and published FL updates

### FL Features Not Activating

**Symptoms**: WasmTask with FL env vars doesn't show FL behavior

**Diagnosis**:
```bash
kubectl get wasmtask <name> -o yaml
kubectl logs job/<job-name>
# Check proplet logs for FL detection
```

**Common Issues**:
- Missing `ROUND_ID`: FL features require `ROUND_ID` env var
- Missing `MODEL_URI`: Model fetching requires `MODEL_URI` env var
- Proplet not detecting FL: Check proplet version and FL support
- Coordinator URL: Verify `COORDINATOR_URL` is set if using HTTP coordinator

## API Reference

### Status Conditions

All CRDs use Kubernetes standard conditions:

| Type | Status | Reason | Description |
|------|--------|--------|-------------|
| `Ready` | `True`/`False` | Various | Overall readiness |
| `RoundActive` | `True`/`False` | `Running`/`Waiting` | Round activity status |
| `AggregationComplete` | `True`/`False` | `Completed`/`Pending` | Aggregation status |
| `ParticipantsReady` | `True`/`False` | `AllReady`/`SomeMissing` | Participant readiness |

### Events

The operator emits Kubernetes events for important state changes:

```bash
kubectl get events --field-selector involvedObject.kind=FederatedJob
```

Common events:
- `FederatedJobCreated`: Job was created
- `RoundStarted`: A new round started
- `RoundCompleted`: A round completed
- `AggregationComplete`: Aggregation finished
- `JobFailed`: A participant job failed

### Metrics

The operator exposes Prometheus metrics on `:8080/metrics`:

- `propeller_federatedjobs_total`: Total number of FederatedJobs
- `propeller_trainingrounds_total`: Total number of TrainingRounds
- `propeller_rounds_active`: Number of active rounds
- `propeller_jobs_created_total`: Total Jobs created
- `propeller_reconcile_duration_seconds`: Reconciliation duration

## Best Practices

### General WASM Workloads

1. **Resource Management**:
   - Set appropriate resource limits for WASM tasks
   - Use ResourceQuotas to limit resource consumption
   - Consider using PriorityClasses for important workloads

2. **Image Management**:
   - Use OCI registries for WASM images
   - Version your WASM workloads
   - Consider image pull policies for edge deployments

3. **Environment Variables**:
   - Use ConfigMaps for non-sensitive configuration
   - Use Secrets for sensitive data (API keys, credentials)
   - Keep environment variable names consistent

4. **Proplet Selection**:
   - Use PropletGroups for dynamic scheduling
   - Specify PropletID for deterministic placement
   - Consider proplet capabilities (CPU, memory, GPU)

### Federated Learning Workloads

1. **FL Environment Variables**:
   - Always set `ROUND_ID` for FL tasks
   - Provide `MODEL_URI` for model fetching
   - Include `HYPERPARAMS` as JSON string
   - Proplets automatically detect and activate FL features

2. **Model Management**:
   - Use versioned model references
   - Store checkpoints in reliable storage
   - Implement model validation before aggregation

3. **Round Configuration**:
   - Set appropriate `kOfN` values (minimum participants)
   - Configure timeouts based on expected training duration
   - Monitor round completion rates

### Security

1. **ServiceAccounts**: Use ServiceAccounts with minimal permissions
2. **Secrets**: Store sensitive data in Secrets, not ConfigMaps
3. **Pod Security**: Enable Pod Security Standards
4. **Network Policies**: Restrict pod network access where possible

### Monitoring

1. **General WASM**: Monitor task completion rates, execution times, resource usage
2. **FL Workloads**: Set up alerts for failed rounds, track experiment progress
3. **Operator Metrics**: Monitor operator health and reconciliation performance

### Scaling

1. **PropletGroups**: Use for dynamic proplet discovery
2. **Namespace Scoping**: Consider namespace-scoped deployment for multi-tenancy
3. **Leader Election**: Use in high-availability setups

## Summary

The Propeller Kubernetes Operator is a **general WASM orchestrator** that:

1. **Primary Function**: Orchestrates any WASM workload via `WasmTask` CRD
2. **FL Support**: Provides specialized workflows (`FederatedJob`, `TrainingRound`) that create WasmTasks with FL context
3. **Automatic FL Detection**: Proplets automatically detect and activate FL features when FL environment variables are present
4. **Unified Execution**: FL is not a separate execution mode - it's the same WASM execution with automatic FL enhancements

This design matches the Rust proplet's architecture: general WASM execution with optional FL features that activate based on environment variables.

## Additional Resources

- [Core Orchestration Package](../pkg/orchestration/README.md)
- [Standalone Manager Documentation](./manager.md)
- [Architecture Overview](./architecture.md)
- [Examples](../examples/fl-demo/README.md)
- [Rust Proplet README](../proplet/README.md)