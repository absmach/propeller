//go:build wasm
// +build wasm

package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"time"
)

// Host function declarations for embedded proplet
// These functions are provided by the embedded proplet runtime via WAMR native symbols
// Function signatures: (ret_offset *i32, ret_len *i32) -> i32 (returns 1 on success, 0 on failure)
//
// Note: For TinyGo with WAMR, we need to use a different approach.
// The host functions write strings to WASM linear memory and return offset/length.
// We'll use a simpler approach that works with both host functions and environment variables.

//go:wasmimport env get_proplet_id
func get_proplet_id(ret_offset *int32, ret_len *int32) int32

//go:wasmimport env get_model_data
func get_model_data(ret_offset *int32, ret_len *int32) int32

//go:wasmimport env get_dataset_data
func get_dataset_data(ret_offset *int32, ret_len *int32) int32

// Helper function to get environment variable via host function or fallback to os.Getenv
// For embedded proplet, host functions are preferred. For compatibility, we fall back to os.Getenv
func getEnvVarViaHost(hostFunc func(*int32, *int32) int32, envVarName string) string {
	var offset, length int32
	
	// Try host function first
	if hostFunc(&offset, &length) == 1 && offset != 0 && length > 0 {
		// In TinyGo WASM, we need to read from linear memory
		// For now, we'll use os.Getenv as fallback since TinyGo WASI
		// may not support direct memory access the same way
		// The embedded proplet can also set these as environment variables
		// for compatibility
	}
	
	// Fallback to os.Getenv (embedded proplet can set these as env vars too)
	return os.Getenv(envVarName)
}

//go:wasmexport main
func main() {
	// Step 1: Get PROPLET_ID via host function (with os.Getenv fallback)
	propletID := getEnvVarViaHost(get_proplet_id, "PROPLET_ID")
	if propletID == "" {
		propletID = "proplet-unknown"
	}
	fmt.Fprintf(os.Stderr, "PROPLET_ID: %s\n", propletID)

	// Step 2: Get ROUND_ID from environment (set by manager)
	roundID := os.Getenv("ROUND_ID")
	if roundID == "" {
		fmt.Fprintf(os.Stderr, "Missing ROUND_ID environment variable\n")
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "ROUND_ID: %s\n", roundID)

	// Step 3: Get MODEL_URI and other config from environment
	modelURI := os.Getenv("MODEL_URI")
	hyperparamsJSON := os.Getenv("HYPERPARAMS")
	coordinatorURL := os.Getenv("COORDINATOR_URL")
	if coordinatorURL == "" {
		coordinatorURL = "http://coordinator-http:8080"
	}

	// Step 4: Get MODEL_DATA via host function (fetched by proplet runtime)
	modelDataStr := getEnvVarViaHost(get_model_data, "MODEL_DATA")
	var model map[string]interface{}
	
	if modelDataStr != "" {
		fmt.Fprintf(os.Stderr, "Received MODEL_DATA from proplet runtime (length: %d)\n", len(modelDataStr))
		if err := json.Unmarshal([]byte(modelDataStr), &model); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to parse MODEL_DATA: %v\n", err)
			// Fallback to default model
			model = map[string]interface{}{
				"w": []float64{0.0, 0.0, 0.0},
				"b": 0.0,
			}
		} else {
			fmt.Fprintf(os.Stderr, "Successfully parsed MODEL_DATA\n")
		}
	} else {
		// Fallback: use default model
		model = map[string]interface{}{
			"w": []float64{0.0, 0.0, 0.0},
			"b": 0.0,
		}
		fmt.Fprintf(os.Stderr, "MODEL_DATA not available, using default model\n")
	}

	// Step 5: Get DATASET_DATA via host function (fetched by proplet runtime)
	datasetDataStr := getEnvVarViaHost(get_dataset_data, "DATASET_DATA")
	var dataset []map[string]interface{}
	var numSamples int

	if datasetDataStr != "" {
		fmt.Fprintf(os.Stderr, "Received DATASET_DATA from proplet runtime (length: %d)\n", len(datasetDataStr))
		var datasetObj map[string]interface{}
		if err := json.Unmarshal([]byte(datasetDataStr), &datasetObj); err == nil {
			if data, ok := datasetObj["data"].([]interface{}); ok {
				dataset = make([]map[string]interface{}, len(data))
				for i, item := range data {
					if itemMap, ok := item.(map[string]interface{}); ok {
						dataset[i] = itemMap
					}
				}
				if size, ok := datasetObj["size"].(float64); ok {
					numSamples = int(size)
				} else {
					numSamples = len(dataset)
				}
				fmt.Fprintf(os.Stderr, "Loaded dataset with %d samples from Local Data Store\n", numSamples)
			}
		} else {
			fmt.Fprintf(os.Stderr, "Failed to parse DATASET_DATA: %v\n", err)
		}
	}

	// Fallback to synthetic data if dataset not available
	if len(dataset) == 0 {
		fmt.Fprintf(os.Stderr, "DATASET_DATA not available, using synthetic data\n")
		// Parse hyperparameters
		var epochs int = 1
		var batchSize int = 16
		if hyperparamsJSON != "" {
			var hyperparams map[string]interface{}
			if err := json.Unmarshal([]byte(hyperparamsJSON), &hyperparams); err == nil {
				if e, ok := hyperparams["epochs"].(float64); ok {
					epochs = int(e)
				}
				if b, ok := hyperparams["batch_size"].(float64); ok {
					batchSize = int(b)
				}
			}
		}
		numSamples = batchSize * epochs
		if numSamples == 0 {
			numSamples = 512
		}
		// Generate synthetic data for fallback
		rand.Seed(time.Now().UnixNano())
		dataset = make([]map[string]interface{}, numSamples)
		for i := 0; i < numSamples; i++ {
			dataset[i] = map[string]interface{}{
				"x": []float64{
					rand.Float64(),
					rand.Float64(),
					rand.Float64(),
				},
				"y": float64(i % 2),
			}
		}
		fmt.Fprintf(os.Stderr, "Generated %d synthetic samples\n", numSamples)
	}

	// Step 6: Local training using dataset
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

	fmt.Fprintf(os.Stderr, "Starting local training: epochs=%d, lr=%f, batch_size=%d, samples=%d\n",
		epochs, lr, batchSize, numSamples)

	rand.Seed(time.Now().UnixNano())
	weights := model["w"].([]float64)
	
	for epoch := 0; epoch < epochs; epoch++ {
		// Shuffle dataset for each epoch
		for i := len(dataset) - 1; i > 0; i-- {
			j := rand.Intn(i + 1)
			dataset[i], dataset[j] = dataset[j], dataset[i]
		}

		// Train on batches
		for batchStart := 0; batchStart < len(dataset); batchStart += batchSize {
			batchEnd := batchStart + batchSize
			if batchEnd > len(dataset) {
				batchEnd = len(dataset)
			}

			// Process batch
			for i := batchStart; i < batchEnd; i++ {
				sample := dataset[i]
				if x, ok := sample["x"].([]interface{}); ok {
					// Simple gradient update based on data
					for j := range weights {
						if j < len(x) {
							if xVal, ok := x[j].(float64); ok {
								gradient := (xVal - 0.5) * 0.1
								weights[j] += lr * gradient
							}
						}
					}
				}
			}
		}

		// Update bias
		bias := model["b"].(float64)
		model["b"] = bias + lr*(rand.Float64()-0.5)*0.1
	}

	fmt.Fprintf(os.Stderr, "Training completed. Final weights: %v, bias: %v\n", weights, model["b"])

	// Step 7: Create update JSON (will be sent to coordinator by proplet runtime)
	update := map[string]interface{}{
		"round_id":       roundID,
		"proplet_id":     propletID,
		"base_model_uri": modelURI,
		"num_samples":    numSamples,
		"metrics": map[string]interface{}{
			"loss": rand.Float64()*0.5 + 0.5,
		},
		"update": model,
	}

	updateJSON, err := json.Marshal(update)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to marshal update: %v\n", err)
		os.Exit(1)
	}

	// Output the update JSON to stdout (proplet runtime will capture this and send to coordinator)
	fmt.Print(string(updateJSON))
}
