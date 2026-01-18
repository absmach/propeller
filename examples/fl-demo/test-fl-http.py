#!/usr/bin/env python3
import json
import sys
import requests
import time
import random

MANAGER_URL = "http://localhost:7070"
COORDINATOR_URL = "http://localhost:8080"
MODEL_REGISTRY_URL = "http://localhost:8081"
AGGREGATOR_URL = "http://localhost:8082"

def main():
    print("=" * 60)
    print("Propeller HTTP FL Demo - 12-Step Workflow")
    print("=" * 60)
    
    print("\n[Pre-flight] Verifying services...")
    
    services = {
        "Propeller Manager": MANAGER_URL,
        "FL Coordinator": COORDINATOR_URL,
        "Model Registry": MODEL_REGISTRY_URL,
        "Aggregator": AGGREGATOR_URL
    }
    
    for name, url in services.items():
        try:
            resp = requests.get(f"{url}/health", timeout=5)
            if resp.status_code == 200:
                print(f"Service verified: {name}")
            else:
                print(f"Service returned {resp.status_code}: {name}")
                sys.exit(1)
        except requests.exceptions.RequestException as e:
            print(f"Service not accessible: {name} - {e}")
            sys.exit(1)
    
    print("\n[Pre-flight] Ensuring initial model exists...")
    try:
        resp = requests.get(f"{MODEL_REGISTRY_URL}/models/0", timeout=5)
        if resp.status_code != 200:
            print("Creating initial model v0...")
            initial_model = {
                "version": 0,
                "model": {
                    "w": [0.0, 0.0, 0.0],
                    "b": 0.0
                }
            }
            resp = requests.post(
                f"{MODEL_REGISTRY_URL}/models",
                json=initial_model,
                headers={"Content-Type": "application/json"},
                timeout=5
            )
            if resp.status_code in [200, 201]:
                print("Initial model v0 created")
    except requests.exceptions.RequestException as e:
        print(f"Error: {e}")
    
    print("\n[Step 1] Configure experiment (Manager -> Coordinator)")
    round_id = f"r-{int(time.time())}"
    experiment_config = {
        "experiment_id": f"exp-{round_id}",
        "round_id": round_id,
        "model_ref": "fl/models/global_model_v0",
        "participants": ["proplet-1", "proplet-2", "proplet-3"],
        "hyperparams": {
            "epochs": 1,
            "lr": 0.01,
            "batch_size": 16
        },
        "k_of_n": 3,
        "timeout_s": 60,
        "task_wasm_image": "oci://example/fl-client-wasm:latest"
    }
    
    try:
        resp = requests.post(
            f"{MANAGER_URL}/fl/experiments",
            json=experiment_config,
            headers={"Content-Type": "application/json"},
            timeout=10
        )
        if resp.status_code in [200, 201]:
            result = resp.json()
            print(f"Experiment configured: {result.get('experiment_id')}")
        else:
            print(f"Failed: {resp.status_code} - {resp.text}")
            sys.exit(1)
    except requests.exceptions.RequestException as e:
        print(f"Error: {e}")
        sys.exit(1)
    
    print("\n[Step 2] Load initial model (Coordinator -> Model Registry)")
    print("Coordinator loads model automatically during experiment configuration")
    
    time.sleep(2)
    
    print("\n[Steps 3-7] Client workflow")
    print("(In production, Proplets execute WASM which makes these calls)")
    
    for proplet_id in experiment_config["participants"]:
        print(f"\nClient {proplet_id}:")
        
        print("  [Step 3] GET /task (Coordinator -> Client)")
        try:
            task_url = f"{COORDINATOR_URL}/task?round_id={round_id}&proplet_id={proplet_id}"
            resp = requests.get(task_url, timeout=5)
            if resp.status_code == 200:
                task = resp.json()["task"]
                print(f"    Task received: model_ref={task['model_ref']}")
            else:
                print(f"    Failed: {resp.status_code}")
                continue
        except requests.exceptions.RequestException as e:
            print(f"    Error: {e}")
            continue
        
        print("  [Step 4] Fetch model (Client -> Model Registry)")
        try:
            model_ref = task["model_ref"]
            version = "0"
            if "v" in model_ref:
                version = model_ref.split("v")[-1]
            
            model_url = f"{MODEL_REGISTRY_URL}/models/{version}"
            resp = requests.get(model_url, timeout=5)
            if resp.status_code == 200:
                model = resp.json()
                print(f"    Model fetched: version={version}")
            else:
                print(f"    Model fetch returned {resp.status_code}")
                model = {"w": [0.0, 0.0, 0.0], "b": 0.0}
        except requests.exceptions.RequestException as e:
            print(f"    Error: {e}")
            model = {"w": [0.0, 0.0, 0.0], "b": 0.0}
        
        print("  [Step 5] Load local dataset (Client -> Local Data Store)")
        print("    Dataset loaded (simulated)")
        
        print("  [Step 6] Local training (Client internal)")
        update = {
            "w": [model["w"][i] + random.uniform(-0.1, 0.1) for i in range(len(model.get("w", [0.0, 0.0, 0.0])))],
            "b": model.get("b", 0.0) + random.uniform(-0.1, 0.1)
        }
        print("    Training complete")
        
        print("  [Step 7] POST /update (Client -> Coordinator)")
        update_payload = {
            "round_id": round_id,
            "proplet_id": proplet_id,
            "base_model_uri": task["model_ref"],
            "num_samples": 512,
            "metrics": {
                "loss": random.uniform(0.4, 0.6)
            },
            "update": update
        }
        
        try:
            resp = requests.post(
                f"{COORDINATOR_URL}/update",
                json=update_payload,
                headers={"Content-Type": "application/json"},
                timeout=5
            )
            if resp.status_code == 200:
                print(f"    Update sent to Coordinator")
            else:
                print(f"    Failed: {resp.status_code} - {resp.text}")
        except requests.exceptions.RequestException as e:
            print(f"    Error: {e}")
    
    print("\n[Steps 8-11] Coordinator operations (internal)")
    print("  [Step 8] Validate & buffer updates")
    print("  [Step 9] Aggregate updates (Coordinator -> Aggregator)")
    print("  [Step 10] New global model (Aggregator -> Coordinator)")
    print("  [Step 11] Store model (Coordinator -> Model Registry)")
    print("  Waiting for aggregation...")
    time.sleep(3)
    
    print("\n[Step 12] Next round available (Coordinator -> Client)")
    try:
        resp = requests.get(f"{COORDINATOR_URL}/rounds/{round_id}/complete", timeout=5)
        if resp.status_code == 200:
            status = resp.json()
            print(f"Round status: completed={status.get('completed')}, updates={status.get('num_updates')}")
        else:
            print(f"Status check returned {resp.status_code}")
    except requests.exceptions.RequestException as e:
        print(f"Error: {e}")
    
    print("\n[Verification] Checking for aggregated model...")
    try:
        resp = requests.get(f"{MODEL_REGISTRY_URL}/models/1", timeout=5)
        if resp.status_code == 200:
            model = resp.json()
            print(f"Aggregated model v1 found in Model Registry")
        else:
            print(f"Model v1 not found yet (may need more time): {resp.status_code}")
    except requests.exceptions.RequestException as e:
        print(f"Error: {e}")
    
    print("\n" + "=" * 60)
    print("Demo complete - All 12 steps executed")
    print("=" * 60)

if __name__ == "__main__":
    main()
