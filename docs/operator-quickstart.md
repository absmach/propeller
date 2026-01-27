# Propeller Kubernetes Operator - Quick Start Guide

## Prerequisites

Before installing the operator, you need a Kubernetes cluster. This guide shows how to set up a local cluster using k3d (lightweight Kubernetes distribution).

### Install k3d

```bash
# macOS
brew install k3d

# Linux (using install script)
curl -s https://raw.githubusercontent.com/k3d-io/k3d/main/install.sh | bash

# Or download from: https://github.com/k3d-io/k3d/releases
```

### Install kubectl

```bash
# macOS
brew install kubectl

# Linux
curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl"
chmod +x kubectl
sudo mv kubectl /usr/local/bin/

# Or follow: https://kubernetes.io/docs/tasks/tools/
```

## Setting Up Local Kubernetes Cluster with k3d

### Create a k3d Cluster

```bash
# Create a new k3d cluster named "propeller"
k3d cluster create propeller \
  --port "8080:80@loadbalancer" \
  --port "8443:443@loadbalancer" \
  --wait

# Verify cluster is running
k3d cluster list
```

### Configure kubectl Context

```bash
# k3d automatically configures kubectl context
# Verify current context
kubectl config current-context

# Should show: k3d-propeller

# List all contexts
kubectl config get-contexts

# Switch context if needed
kubectl config use-context k3d-propeller
```

### Verify Cluster Access

```bash
# Check cluster connection
kubectl cluster-info

# List nodes
kubectl get nodes

# Should show one or more nodes in Ready state
```

### Optional: Create a Namespace

```bash
# Create namespace for the operator (optional, will be created during installation)
kubectl create namespace propeller-system
```

### Cleanup (When Done)

```bash
# Delete the k3d cluster
k3d cluster delete propeller

# Or delete all k3d clusters
k3d cluster delete --all
```

## Building and Loading the Operator Image

Before installing the operator, you need to build the operator image and load it into your k3d cluster.

### Install Dependencies

First, ensure all Go dependencies are up to date:

```bash
# From project root
go mod tidy

# If you get missing dependencies errors, install them:
go get sigs.k8s.io/controller-runtime@latest
go get k8s.io/apimachinery@latest
go get k8s.io/api@latest
go mod tidy
```

### Generate CRD Code

The CRD types need generated methods (DeepCopy, etc.). Generate them using `controller-gen`:

```bash
# Install controller-gen if not already installed
go install sigs.k8s.io/controller-tools/cmd/controller-gen@latest

# Remove old generated files if they exist
rm -f api/v1alpha1/zz_generated.deepcopy.go

# Generate deep copy methods and other required code
controller-gen object paths="./api/..."

# Generate CRD manifests (required for installation)
controller-gen crd paths="./api/..." output:crd:dir=./config/crd/bases
```

**Note**: The CRD types use `apiextensionsv1.JSON` for arbitrary JSON fields (instead of `map[string]interface{}`) to be compatible with `controller-gen`. This allows flexible JSON data while being Kubernetes-compatible.

### Build the Operator Binary

The operator is located in `manager/operator/`. Build it manually:

```bash
# Ensure build directory exists
mkdir -p build

# Build the operator binary
cd manager/operator
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ../../build/operator main.go
cd ../..

# Verify the binary was created
ls -lh build/operator
```

### Build Docker Image

Create a Dockerfile for the operator. Create `manager/operator/Dockerfile`:

```dockerfile
FROM golang:1.25.5-alpine3.22 AS builder
WORKDIR /workspace
COPY go.mod go.sum ./
RUN go mod download
COPY manager/operator ./manager/operator
COPY api ./api
COPY pkg ./pkg
WORKDIR /workspace/manager/operator
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -o manager main.go

FROM scratch
LABEL org.opencontainers.image.source=https://github.com/absmach/propeller
LABEL org.opencontainers.image.licenses=Apache-2.0
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /workspace/manager/operator/manager /manager
ENTRYPOINT ["/manager"]
```

Then build the image:

```bash
# From project root
docker build -t ghcr.io/absmach/propeller/operator:latest -f manager/operator/Dockerfile .
```

**Alternative**: If you prefer to use the pre-built binary approach:

```bash
# After building the binary (see above)
docker build \
  --build-arg SVC=operator \
  --tag=ghcr.io/absmach/propeller/operator:latest \
  -f docker/Dockerfile.dev ./build
```

### Load Image into k3d

```bash
# Load the image into your k3d cluster
k3d image import ghcr.io/absmach/propeller/operator:latest -c propeller

# Verify the image is available
k3d image list propeller
```

### Alternative: Use Local Image Name

If you prefer to use a simpler image name:

```bash
# Build with a local name
docker build \
  --build-arg SVC=operator \
  --build-arg GOARCH=amd64 \
  --tag=propeller-operator:local \
  -f docker/Dockerfile .

# Import into k3d
k3d image import propeller-operator:local -c propeller

# Install with custom image
helm install propeller-operator ./config/helm \
  --namespace propeller-system \
  --create-namespace \
  --set image.repository=propeller-operator \
  --set image.tag=local
```

## Quick Installation

### Install CRDs First

```bash
# Install CRDs
kubectl apply -f config/crd/bases/

# Verify CRDs are installed
kubectl get crds | grep propeller
```

### Install Operator with Helm

```bash
# Using Helm
helm install propeller-operator ./config/helm \
  --namespace propeller-system \
  --create-namespace

# Verify
kubectl get pods -n propeller-system
kubectl get crds | grep propeller
```

### Check Operator Status

```bash
# Check if operator pod is running
kubectl get pods -n propeller-system

# View operator logs
kubectl logs -n propeller-system -l app.kubernetes.io/name=propeller-operator -f

# Check operator deployment
kubectl get deployment -n propeller-system propeller-operator
```

## Minimal Examples

### General WASM Task (Primary Use Case)

The operator's main purpose is orchestrating general WASM workloads. Create a file `wasmtask.yaml`:

```yaml
apiVersion: propeller.absmach.io/v1alpha1
kind: WasmTask
metadata:
  name: my-wasm-task
spec:
  imageUrl: "oci://registry.example.com/wasm/my-app:v1.0.0"
  env:
    KEY: "value"
```

Apply it:

```bash
kubectl apply -f wasmtask.yaml
kubectl get wasmtask my-wasm-task
```

### Federated Learning Experiment (Specialized Workflow)

FL is a specialized workflow built on general WASM tasks. Create a file `federatedjob.yaml`:

```yaml
apiVersion: propeller.absmach.io/v1alpha1
kind: FederatedJob
metadata:
  name: my-fl-experiment
spec:
  experimentId: "exp-001"
  modelRef: "oci://registry.example.com/model:v1"
  taskWasmImage: "oci://registry.example.com/fl-client:latest"
  participants:
    - propletId: "proplet-1"
  kOfN: 1
  timeoutSeconds: 300
  rounds:
    total: 5
  aggregator:
    algorithm: "fedavg"
```

Apply it:

```bash
kubectl apply -f federatedjob.yaml
```

## Common Commands

```bash
# List WASM Tasks
kubectl get wasmtasks

# List FederatedJobs
kubectl get federatedjobs

# Get detailed status
kubectl get wasmtask <name> -o yaml
kubectl get federatedjob <name> -o yaml

# Watch resources
kubectl get wasmtasks -w
kubectl get trainingrounds -w

# Check Jobs
kubectl get jobs -l owner=wasmtask
kubectl get jobs -l owner=traininground

# View logs
kubectl logs job/<job-name>

# Delete resources
kubectl delete wasmtask <name>
kubectl delete federatedjob <name>
```

## Status Check

```bash
# Check WASM task phase
kubectl get wasmtask <name> -o jsonpath='{.status.phase}'

# Check proplet assignment
kubectl get wasmtask <name> -o jsonpath='{.status.propletId}'

# Check experiment phase
kubectl get federatedjob <name> -o jsonpath='{.status.phase}'

# Check current round
kubectl get federatedjob <name> -o jsonpath='{.status.currentRound}'

# Check round status
kubectl get traininground <name> -o jsonpath='{.status.phase}'

# Check updates received
kubectl get traininground <name> -o jsonpath='{.status.updatesReceived}/{.status.updatesRequired}'
```

## Troubleshooting Quick Fixes

```bash
# Check operator logs
kubectl logs -n propeller-system deployment/propeller-operator

# Check events
kubectl get events --sort-by='.lastTimestamp'

# Describe resources
kubectl describe federatedjob <name>
kubectl describe traininground <name>

# Check RBAC
kubectl auth can-i create jobs --as=system:serviceaccount:propeller-system:propeller-operator
```

For detailed documentation, see [operator.md](./operator.md).
