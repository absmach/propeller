# Proplet on Confidential Containers (CoCo)

This directory contains resources for deploying Proplet on a Kubernetes cluster enabled with Confidential Containers (Kata Containers).

## Prerequisites

*   A Kubernetes cluster with [Confidential Containers](https://confidentialcontainers.org/) (Kata Containers) installed.
*   `kubectl` configured to access the cluster.
*   `docker` for building images (or another OCI builder).
*   A default StorageClass for handling ephemeral storage (optional but recommended).

## Cluster Setup (Quick Start)

To set up a local testing environment with Kind and Confidential Containers:

1.  **Create a Kind Cluster**:
    ```bash
    kind create cluster --name coco-test --config - <<EOF
    kind: Cluster
    apiVersion: kind.x-k8s.io/v1alpha4
    nodes:
    - role: control-plane
    - role: worker
    EOF
    ```

2.  **Install the CoCo Operator**:
    Deploy the Confidential Containers Operator to install Kata Containers and required components.

    ```bash
    kubectl apply -k github.com/confidential-containers/operator/config/release?ref=v0.8.0
    ```

    *Note: Check the [CoCo Operator releases](https://github.com/confidential-containers/operator/releases) for the latest version.*

3.  **Wait for Installation**:
    Wait for the `cc-runtime` runtime class to become available:

    ```bash
    kubectl get runtimeclass
    # Should show 'kata', 'kata-qemu', or 'kata-fc'
    ```

    Ensure all operator pods are running:
    ```bash
    kubectl get pods -n confidential-containers-system
    ```

## Deployment

The deployment setup consists of:
*   `proplet.yaml`: The main Deployment manifest. Checks for `runtimeClassName: kata` (default).
*   `proplet-config.yaml`: Configuration map containing `config.toml` (SuperMQ config) and environment variables.
*   `deploy_coco.sh`: A helper script to build and deploy.

### 1. Configuration

1.  **Edit `proplet-config.yaml`**:
    *   Set your `domain_id`, `client_id`, `client_key`, and `channel_id`.
    *   These values configure Proplet to connect to the SuperMQ message broker.

2.  **Edit `proplet.yaml`**:
    *   Update `PROPLET_MQTT_ADDRESS` if your MQTT broker is not running locally (default `tcp://localhost:1883`).
    *   Update `PROPLET_INSTANCE_ID` to a unique name for this instance.
    *   Ensure `runtimeClassName` matches your cluster's CoCo runtime class (e.g., `kata-qemu`, `kata-fc`, or just `kata`).

### 2. Deploy

Use the helper script to build and deploy:

```bash
./deploy_coco.sh
```

Or manually:

```bash
# 1. Build image
docker build -f ../docker/Dockerfile.proplet -t proplet:latest ..

# 2. Apply manifests
kubectl apply -f proplet-config.yaml
kubectl apply -f proplet.yaml
```

## Attestation Agent

In a CoCo environment, the Attestation Agent (AA) typically runs as a guest component inside the VM.
Proplet is configured to communicate with the AA on `localhost:50002` (standard CoCo port).

To attest the environment, ensure:
1.  Your Kubernetes cluster is properly configured for remote attestation (KBS/KBC setup).
2.  The Attestation Agent is active in the Guest VM.

## Troubleshooting

**Pod stuck in `ContainerCreating`**:
*   Check if Kata runtime is available: `kubectl get runtimeclasses`
*   Check Kubelet logs for QEMU/Kata startup errors.

**Proplet fails to connect**:
*   Check logs: `kubectl logs -l app=proplet`
*   Verify network connectivity to the MQTT broker.
