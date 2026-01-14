//go:build wasm
// +build wasm

// FL Client Wasm Module
// This module implements a simple federated learning client that:
// 1. Reads ROUND_ID and MODEL_URI from environment
// 2. Performs toy local training
// 3. Returns JSON update in the new format
package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"time"
)

func main() {
	// Read environment variables (set by Manager from round start message)
	roundID := os.Getenv("ROUND_ID")
	modelURI := os.Getenv("MODEL_URI")
	hyperparamsJSON := os.Getenv("HYPERPARAMS")

	if roundID == "" {
		fmt.Fprintf(os.Stderr, "Missing ROUND_ID environment variable\n")
		os.Exit(1)
	}

	// Parse hyperparameters if provided
	var epochs int = 1
	var lr float64 = 0.01
	var batchSize int = 16

	if hyperparamsJSON != "" {
		var hyperparams map[string]interface{}
		if err := json.Unmarshal([]byte(hyperparamsJSON), &hyperparams); err == nil {
			if e, ok := hyperparams["epochs"].(float64); ok {
				epochs = int(e)
			}
			if l, ok := hyperparams["lr"].(float64); ok {
				lr = l
			}
			if b, ok := hyperparams["batch_size"].(float64); ok {
				batchSize = int(b)
			}
		}
	}

	// Initialize model (in a real implementation, we'd fetch from modelURI)
	// For this demo, we'll use a simple model structure
	model := map[string]interface{}{
		"w": []float64{0.0, 0.0, 0.0}, // weights
		"b": 0.0,                      // bias
	}

	// If modelURI is provided, in a real implementation we'd fetch it
	// For this demo, we'll simulate loading a model
	if modelURI != "" {
		// In a real implementation: fetch model from modelURI via HTTP
		// For now, we'll use a simple initialization
		// model = fetchModel(modelURI)
	}

	// Simulate local training
	// In a real implementation, this would train on local data
	rand.Seed(time.Now().UnixNano())

	weights := model["w"].([]float64)
	for epoch := 0; epoch < epochs; epoch++ {
		// Simulate training: add small random updates
		for i := range weights {
			// Simple gradient-like update
			gradient := (rand.Float64() - 0.5) * 0.1
			weights[i] += lr * gradient
		}
		// Update bias
		bias := model["b"].(float64)
		model["b"] = bias + lr*(rand.Float64()-0.5)*0.1
	}

	// Generate number of samples (simulated)
	numSamples := batchSize * epochs
	if numSamples == 0 {
		numSamples = 512 // default
	}

	// Create update in new format
	update := map[string]interface{}{
		"round_id":      roundID,
		"base_model_uri": modelURI,
		"num_samples":   numSamples,
		"metrics": map[string]interface{}{
			"loss": rand.Float64() * 0.5 + 0.5, // simulated loss
		},
		"update": model,
	}

	// Output JSON update
	updateJSON, err := json.Marshal(update)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to marshal update: %v\n", err)
		os.Exit(1)
	}

	fmt.Print(string(updateJSON))
}
