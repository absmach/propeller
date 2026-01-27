# Propeller Kubernetes Operator

The Propeller Kubernetes Operator provides Kubernetes-native orchestration for **WebAssembly (WASM) workloads** across the cloud-edge continuum.

## Philosophy

Propeller is fundamentally a **general WASM orchestrator**. The operator reflects this:
- **Primary Use Case**: Orchestrating any WASM workload (functions, services, data processing, etc.)
- **Specialized Workflow**: Federated learning is built on top of general WASM execution
- **Automatic FL Detection**: FL features activate automatically when FL environment variables are present

## Architecture

The operator implements a dual-manager strategy:
- **Standalone Manager** (existing): HTTP + MQTT orchestration
- **Operator Manager** (new): Kubernetes CRD-based orchestration

Both managers share the same core orchestration logic from `pkg/orchestration/`.

## Custom Resource Definitions

### WasmTask (Primary)
Represents a general WASM workload - the core orchestration capability. Any WASM binary/image can be executed. FL features activate automatically when `ROUND_ID` and `MODEL_URI` environment variables are present.

### PropletGroup
Represents a group of proplets (workers) for scheduling.

### FederatedJob (Specialized)
Represents a federated learning workflow built on top of WasmTask execution. Orchestrates multiple training rounds by creating WasmTasks with FL-specific environment variables.

### TrainingRound (Specialized)
Represents a single FL training round. Creates WasmTasks with FL context (`ROUND_ID`, `MODEL_URI`, `HYPERPARAMS`).

### ModelCheckpoint
Represents an aggregated model artifact (for FL workloads).

## Building

The operator requires Kubernetes dependencies. To add them:

```bash
go get sigs.k8s.io/controller-runtime@latest
go get k8s.io/apimachinery@latest
go get k8s.io/api@latest
go mod tidy
```

## Generating CRDs

To generate CRD manifests from the Go types:

```bash
# Install controller-gen if not already installed
go install sigs.k8s.io/controller-tools/cmd/controller-gen@latest

# Generate CRDs
controller-gen crd paths="./api/..." output:crd:dir=./config/crd/bases
```

## Deployment

### Using Helm

```bash
helm install propeller-operator ./config/helm
```

### Using Kustomize

```bash
kubectl apply -k config/default
```

## Usage

### General WASM Task (Primary)

Create a WasmTask for any WASM workload:

```yaml
apiVersion: propeller.absmach.io/v1alpha1
kind: WasmTask
metadata:
  name: my-wasm-task
spec:
  imageUrl: "oci://registry.example.com/wasm/my-app:v1.0.0"
  env:
    KEY: "value"
  resources:
    requests:
      memory: "128Mi"
      cpu: "100m"
```

### Federated Learning (Specialized)

Create a FederatedJob for FL workflows:

```yaml
apiVersion: propeller.absmach.io/v1alpha1
kind: FederatedJob
metadata:
  name: fl-mnist-experiment
spec:
  experimentId: "exp-001"
  modelRef: "oci://registry/model:v1"
  taskWasmImage: "oci://registry/fl-client:latest"
  participants:
    - propletId: "proplet-1"
  kOfN: 1
  timeoutSeconds: 300
  rounds:
    total: 10
  aggregator:
    algorithm: "fedavg"
```

**Note**: The FederatedJob creates WasmTasks with FL environment variables. The proplet automatically detects FL context and activates FL features.

## Development

The operator uses controller-runtime and follows standard Kubernetes operator patterns.
