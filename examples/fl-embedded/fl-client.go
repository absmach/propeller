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

//go:wasmimport env get_proplet_id
func get_proplet_id(ret_offset *int32, ret_len *int32) int32

//go:wasmimport env get_model_data
func get_model_data(ret_offset *int32, ret_len *int32) int32

//go:wasmimport env get_dataset_data
func get_dataset_data(ret_offset *int32, ret_len *int32) int32

func getEnvVarViaHost(hostFunc func(*int32, *int32) int32, envVarName string) string {
	var offset, length int32

	if hostFunc(&offset, &length) == 1 && offset != 0 && length > 0 {
	}

	return os.Getenv(envVarName)
}

//go:wasmexport main
func main() {
	propletID := getEnvVarViaHost(get_proplet_id, "PROPLET_ID")
	if propletID == "" {
		propletID = "proplet-unknown"
	}
	fmt.Fprintf(os.Stderr, "PROPLET_ID: %s\n", propletID)

	roundID := os.Getenv("ROUND_ID")
	if roundID == "" {
		fmt.Fprintf(os.Stderr, "Missing ROUND_ID environment variable\n")
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "ROUND_ID: %s\n", roundID)

	modelURI := os.Getenv("MODEL_URI")
	hyperparamsJSON := os.Getenv("HYPERPARAMS")
	coordinatorURL := os.Getenv("COORDINATOR_URL")
	if coordinatorURL == "" {
		coordinatorURL = "http://coordinator-http:8080"
	}

	modelDataStr := getEnvVarViaHost(get_model_data, "MODEL_DATA")
	var model map[string]interface{}

	if modelDataStr != "" {
		fmt.Fprintf(os.Stderr, "Received MODEL_DATA from proplet runtime (length: %d)\n", len(modelDataStr))
		if err := json.Unmarshal([]byte(modelDataStr), &model); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to parse MODEL_DATA: %v\n", err)
			model = map[string]interface{}{
				"w": []float64{0.0, 0.0, 0.0},
				"b": 0.0,
			}
		} else {
			fmt.Fprintf(os.Stderr, "Successfully parsed MODEL_DATA\n")
		}
	} else {
		model = map[string]interface{}{
			"w": []float64{0.0, 0.0, 0.0},
			"b": 0.0,
		}
		fmt.Fprintf(os.Stderr, "MODEL_DATA not available, using default model\n")
	}

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

	if len(dataset) == 0 {
		fmt.Fprintf(os.Stderr, "DATASET_DATA not available, using synthetic data\n")
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
		for i := len(dataset) - 1; i > 0; i-- {
			j := rand.Intn(i + 1)
			dataset[i], dataset[j] = dataset[j], dataset[i]
		}

		for batchStart := 0; batchStart < len(dataset); batchStart += batchSize {
			batchEnd := batchStart + batchSize
			if batchEnd > len(dataset) {
				batchEnd = len(dataset)
			}

			for i := batchStart; i < batchEnd; i++ {
				sample := dataset[i]
				if x, ok := sample["x"].([]interface{}); ok {
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

		bias := model["b"].(float64)
		model["b"] = bias + lr*(rand.Float64()-0.5)*0.1
	}

	fmt.Fprintf(os.Stderr, "Training completed. Final weights: %v, bias: %v\n", weights, model["b"])

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

	fmt.Print(string(updateJSON))
}
