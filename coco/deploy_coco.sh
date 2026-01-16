#!/bin/bash
# SPDX-License-Identifier: Apache-2.0
# Helper script to deploy Proplet on Confidential Containers (CoCo)

set -e

SCRIPT_DIR=$(dirname "$(readlink -f "$0")")
ROOT_DIR=$(dirname "$SCRIPT_DIR")
K8S_DIR="$SCRIPT_DIR"

# Configuration
IMAGE_NAME="ghcr.io/absmach/propeller/proplet"
IMAGE_TAG="latest"
RUNTIME_CLASS=${RUNTIME_CLASS:-kata}

echo "=== Proplet CoCo Deployment ==="

# 1. Build the Proplet container image using Makefile
echo "Building Proplet container image..."
cd "$ROOT_DIR"
make docker_proplet

# 2. (Optional) Load into Kind if using Kind
if kind get clusters &> /dev/null; then
  echo "Detected Kind cluster, loading image..."
  kind load docker-image "${IMAGE_NAME}:${IMAGE_TAG}" || echo "Warning: Failed to load image into Kind, continuing..."
fi

# 3. Apply Kubernetes manifests
echo "Applying Kubernetes manifests..."
# Temporarily update runtimeClassName if overridden
if [ "$RUNTIME_CLASS" != "kata" ]; then
    echo "Updating runtimeClassName to $RUNTIME_CLASS..."
    sed -i "s/runtimeClassName: kata/runtimeClassName: $RUNTIME_CLASS/g" "$K8S_DIR/proplet.yaml"
fi

kubectl apply -f "$K8S_DIR/proplet-config.yaml"
kubectl apply -f "$K8S_DIR/proplet.yaml"

echo "=== Deployment Submitted ==="
echo "Check status:"
echo "  kubectl get pods -l app=proplet"
echo "  kubectl logs -l app=proplet"
