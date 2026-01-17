#!/usr/bin/env python3
"""
Test script for HTTP-based federated learning (diagram-mode).
This script tests the HTTP flow matching the architecture diagrams.
"""
import json
import sys
import requests
import time
from pathlib import Path

ORCHESTRATOR_URL = "http://localhost:8083"
COORDINATOR_URL = "http://localhost:8080"
MODEL_REGISTRY_URL = "http://localhost:8081"

def main():
    print("=" * 60)
    print("FML HTTP/Diagram-Mode Demo Test")
    print("=" * 60)
    
    # Step 1: Configure experiment via Orchestrator
    print("\n1. Configuring experiment via Orchestrator...")
    experiment_config = {
        "experiment_id": "exp-http-demo",
        "round_id": f"r-{int(time.time())}",
        "model_ref": "fl/models/global_model_v0",
        "participants": ["proplet-1", "proplet-2", "proplet-3"],
        "hyperparams": {
            "epochs": 1,
            "lr": 0.01,
            "batch_size": 16
        },
        "k_of_n": 3,
        "timeout_s": 60
    }
    
    try:
        response = requests.post(
            f"{ORCHESTRATOR_URL}/experiments",
            json=experiment_config,
            headers={"Content-Type": "application/json"},
            timeout=10
        )
        
        if response.status_code == 201:
            print(f"  ✅ Experiment configured: {response.json()}")
            round_id = experiment_config["round_id"]
        else:
            print(f"  ⚠️  Orchestrator returned {response.status_code}: {response.text}")
            round_id = experiment_config["round_id"]
    except requests.exceptions.RequestException as e:
        print(f"  ⚠️  Orchestrator not available: {e}")
        print("  Continuing with direct coordinator access...")
        round_id = f"r-{int(time.time())}"
    
    # Step 2: Verify services are running
    print("\n2. Verifying services...")
    
    services = {
        "Coordinator": COORDINATOR_URL,
        "Model Registry": MODEL_REGISTRY_URL,
        "Orchestrator": ORCHESTRATOR_URL
    }
    
    for name, url in services.items():
        try:
            resp = requests.get(f"{url}/health", timeout=5)
            if resp.status_code == 200:
                print(f"  ✅ {name} is running")
            else:
                print(f"  ⚠️  {name} returned {resp.status_code}")
        except requests.exceptions.RequestException as e:
            print(f"  ❌ {name} is not accessible: {e}")
    
    # Step 3: Check initial model in registry
    print("\n3. Checking model registry...")
    try:
        resp = requests.get(f"{MODEL_REGISTRY_URL}/models/0", timeout=5)
        if resp.status_code == 200:
            model = resp.json()
            print(f"  ✅ Initial model v0 found: {model}")
        else:
            print(f"  ⚠️  Model v0 not found: {resp.status_code}")
    except requests.exceptions.RequestException as e:
        print(f"  ❌ Failed to check model registry: {e}")
    
    # Step 4: Simulate client task retrieval and updates
    print("\n4. Simulating client workflow...")
    print(f"   Round ID: {round_id}")
    
    participants = ["proplet-1", "proplet-2", "proplet-3"]
    
    for proplet_id in participants:
        print(f"\n   Client {proplet_id}:")
        
        # Get task
        try:
            task_url = f"{COORDINATOR_URL}/task?round_id={round_id}&proplet_id={proplet_id}"
            resp = requests.get(task_url, timeout=5)
            if resp.status_code == 200:
                task = resp.json()["task"]
                print(f"     ✅ Task received: model_ref={task['model_ref']}")
            else:
                print(f"     ❌ Failed to get task: {resp.status_code}")
                continue
        except requests.exceptions.RequestException as e:
            print(f"     ❌ Error getting task: {e}")
            continue
        
        # Simulate update
        update = {
            "round_id": round_id,
            "proplet_id": proplet_id,
            "base_model_uri": task["model_ref"],
            "num_samples": 512,
            "metrics": {
                "loss": 0.5 + len(proplet_id) * 0.1
            },
            "update": {
                "w": [0.1 * (hash(proplet_id) % 10), 0.05, 0.15],
                "b": 0.1
            }
        }
        
        # Send update
        try:
            resp = requests.post(
                f"{COORDINATOR_URL}/update",
                json=update,
                headers={"Content-Type": "application/json"},
                timeout=5
            )
            if resp.status_code == 200:
                print(f"     ✅ Update sent successfully")
            else:
                print(f"     ❌ Failed to send update: {resp.status_code}")
        except requests.exceptions.RequestException as e:
            print(f"     ❌ Error sending update: {e}")
    
    # Step 5: Wait for aggregation
    print("\n5. Waiting for aggregation...")
    time.sleep(3)
    
    # Step 6: Check round completion
    print("\n6. Checking round completion...")
    try:
        resp = requests.get(f"{COORDINATOR_URL}/rounds/{round_id}/complete", timeout=5)
        if resp.status_code == 200:
            status = resp.json()
            print(f"   ✅ Round status: {status}")
        else:
            print(f"   ⚠️  Round status check returned {resp.status_code}")
    except requests.exceptions.RequestException as e:
        print(f"   ❌ Error checking round status: {e}")
    
    # Step 7: Check for new model
    print("\n7. Checking for aggregated model...")
    try:
        resp = requests.get(f"{MODEL_REGISTRY_URL}/models/1", timeout=5)
        if resp.status_code == 200:
            model = resp.json()
            print(f"   ✅ Aggregated model v1 found: {model}")
        else:
            print(f"   ⚠️  Model v1 not found yet (may need more time): {resp.status_code}")
    except requests.exceptions.RequestException as e:
        print(f"   ❌ Error checking model: {e}")
    
    print("\n" + "=" * 60)
    print("Test complete!")
    print("=" * 60)
    print("\nMonitor services:")
    print(f"  Coordinator: docker compose -f compose-http.yaml logs -f coordinator-http")
    print(f"  Aggregator: docker compose -f compose-http.yaml logs -f aggregator")
    print(f"  Model Registry: docker compose -f compose-http.yaml logs -f model-registry")

if __name__ == "__main__":
    main()
