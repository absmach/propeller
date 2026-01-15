#!/usr/bin/env python3
"""
Test script for federated learning using local WASM file.
This script creates tasks directly via the manager HTTP API with base64-encoded WASM.
"""
import json
import base64
import sys
import requests
import time
from pathlib import Path

MANAGER_URL = "http://localhost:7070"  # Default manager port
WASM_FILE = "client-wasm/fl-client.wasm"
ROUND_ID = f"r-{int(time.time())}"

def main():
    # Read and encode WASM file
    wasm_path = Path(WASM_FILE)
    if not wasm_path.exists():
        print(f"Error: WASM file not found: {WASM_FILE}")
        print(f"Please build it first: cd client-wasm && tinygo build -target wasi -o fl-client.wasm fl-client.go")
        sys.exit(1)
    
    print(f"Reading WASM file: {wasm_path}")
    with open(wasm_path, 'rb') as f:
        wasm_data = f.read()
    
    wasm_b64 = base64.b64encode(wasm_data).decode('utf-8')
    print(f"WASM file encoded: {len(wasm_b64)} characters")
    
    # Participants
    participants = ["proplet-1", "proplet-2", "proplet-3"]
    
    # Hyperparameters
    hyperparams = {
        "epochs": 1,
        "lr": 0.01,
        "batch_size": 16
    }
    
    print(f"\nCreating tasks for round: {ROUND_ID}")
    print(f"Participants: {participants}")
    
    # Create and start tasks for each participant
    task_ids = []
    for proplet_id in participants:
        task_name = f"fl-round-{ROUND_ID}-{proplet_id}"
        
        # Create task
        task_data = {
            "name": task_name,
            "file": wasm_b64,
            "env": {
                "ROUND_ID": ROUND_ID,
                "MODEL_URI": "fl/models/global_model_v0",
                "HYPERPARAMS": json.dumps(hyperparams)
            },
            "proplet_id": proplet_id
        }
        
        try:
            print(f"\nCreating task for {proplet_id}...")
            response = requests.post(
                f"{MANAGER_URL}/tasks",
                json=task_data,
                headers={"Content-Type": "application/json"},
                timeout=10
            )
            
            if response.status_code != 201:
                print(f"  Error creating task: {response.status_code} - {response.text}")
                continue
            
            task = response.json()
            task_id = task.get("id") or task.get("task", {}).get("id")
            
            if not task_id:
                print(f"  Error: No task ID in response: {task}")
                continue
            
            task_ids.append((proplet_id, task_id))
            print(f"  Task created: {task_id}")
            
            # Start the task
            print(f"  Starting task...")
            start_response = requests.post(
                f"{MANAGER_URL}/tasks/{task_id}/start",
                timeout=10
            )
            
            if start_response.status_code == 200:
                print(f"  Task started successfully")
            else:
                print(f"  Warning: Start returned {start_response.status_code}: {start_response.text}")
        
        except requests.exceptions.RequestException as e:
            print(f"  Error: {e}")
            continue
    
    if task_ids:
        print(f"\n✅ Successfully launched {len(task_ids)} tasks")
        print(f"\nMonitor progress:")
        print(f"  docker compose logs -f coordinator")
        print(f"  docker compose logs -f manager")
        print(f"  docker compose logs -f proplet-1")
        print(f"\nCheck aggregated models:")
        print(f"  docker compose exec model-server ls -la /tmp/fl-models/")
    else:
        print("\n❌ Failed to launch any tasks")
        sys.exit(1)

if __name__ == "__main__":
    main()
