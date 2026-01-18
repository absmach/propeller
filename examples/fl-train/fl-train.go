//go:build wasm
// +build wasm

// Build tags are required to ensure this file only compiles when building for WASM target.
// Without these tags, the file would be included in regular Go builds and fail to compile
// due to missing WASM-specific functionality. These tags are used by the Go compiler
// when building with: GOOS=wasip1 GOARCH=wasm go build -o example.wasm
package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
)

// Simple FL training example
// This Wasm module implements a basic federated learning training workload
// It reads FL environment variables and produces a simple model update

func main() {
	// Read FL environment variables
	jobID := os.Getenv("FL_JOB_ID")
	roundIDStr := os.Getenv("FL_ROUND_ID")
	globalVersion := os.Getenv("FL_GLOBAL_VERSION")
	globalUpdateB64 := os.Getenv("FL_GLOBAL_UPDATE_B64")
	numSamplesStr := os.Getenv("FL_NUM_SAMPLES")
	updateFormat := os.Getenv("FL_FORMAT")

	if jobID == "" || roundIDStr == "" {
		fmt.Fprintf(os.Stderr, "Missing required FL environment variables\n")
		os.Exit(1)
	}

	roundID, err := strconv.ParseUint(roundIDStr, 10, 64)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid FL_ROUND_ID: %v\n", err)
		os.Exit(1)
	}

	numSamples := uint64(1)
	if numSamplesStr != "" {
		if n, err := strconv.ParseUint(numSamplesStr, 10, 64); err == nil {
			numSamples = n
		}
	}

	// Initialize model from global update if available
	var model []float32
	if globalUpdateB64 != "" {
		updateBytes, err := base64.StdEncoding.DecodeString(globalUpdateB64)
		if err == nil {
			if updateFormat == "json-f64" {
				var weights []float64
				if err := json.Unmarshal(updateBytes, &weights); err == nil {
					model = make([]float32, len(weights))
					for i, w := range weights {
						model[i] = float32(w)
					}
				}
			}
		}
	}

	// If no model provided, initialize with zeros (simple example)
	if model == nil {
		model = make([]float32, 10) // Simple 10-parameter model
	}

	// Simulate local training: add small random updates
	// In a real implementation, this would train on local data
	for i := range model {
		// Simple update: add a small value based on round and index
		// This is a placeholder - real training would use actual gradients
		update := float32(0.01) * float32(roundID) * float32(i+1) / float32(len(model))
		model[i] += update
	}

	// Convert model to update format
	var output []byte
	if updateFormat == "json-f64" {
		weights := make([]float64, len(model))
		for i, w := range model {
			weights[i] = float64(w)
		}
		output, _ = json.Marshal(weights)
	} else {
		// Default: simple text format
		output = []byte(fmt.Sprintf("round_%d_samples_%d", roundID, numSamples))
	}

	// Output the update (will be captured by proplet and sent to manager)
	fmt.Print(string(output))
}
