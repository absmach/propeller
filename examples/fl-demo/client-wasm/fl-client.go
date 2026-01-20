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

//go:wasmexport run
func run() {
	main()
}

func main() {
	roundID := os.Getenv("ROUND_ID")
	modelURI := os.Getenv("MODEL_URI")
	hyperparamsJSON := os.Getenv("HYPERPARAMS")
	coordinatorURL := os.Getenv("COORDINATOR_URL")
	modelRegistryURL := os.Getenv("MODEL_REGISTRY_URL")
	propletID := os.Getenv("PROPLET_ID")

	if roundID == "" {
		fmt.Fprintf(os.Stderr, "Missing ROUND_ID environment variable\n")
		os.Exit(1)
	}

	if coordinatorURL == "" {
		coordinatorURL = "http://coordinator-http:8080"
	}
	if modelRegistryURL == "" {
		modelRegistryURL = "http://model-registry:8081"
	}
	if propletID == "" {
		propletID = "proplet-unknown"
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

	taskRequest := map[string]interface{}{
		"action": "get_task",
		"url":    fmt.Sprintf("%s/task?round_id=%s&proplet_id=%s", coordinatorURL, roundID, propletID),
	}
	taskRequestJSON, _ := json.Marshal(taskRequest)
	fmt.Fprintf(os.Stderr, "TASK_REQUEST:%s\n", string(taskRequestJSON))

	_ = extractModelVersion(modelURI)

	modelDataStr := os.Getenv("MODEL_DATA")
	var model map[string]interface{}
	
	if modelDataStr != "" {
		if err := json.Unmarshal([]byte(modelDataStr), &model); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to parse MODEL_DATA: %v\n", err)
			model = map[string]interface{}{
				"w": []float64{0.0, 0.0, 0.0},
				"b": 0.0,
			}
		}
	} else {
		model = map[string]interface{}{
			"w": []float64{0.0, 0.0, 0.0},
			"b": 0.0,
		}
		fmt.Fprintf(os.Stderr, "MODEL_DATA not available, using default model. Model should be fetched by proplet runtime.\n")
	}

	datasetDataStr := os.Getenv("DATASET_DATA")
	var dataset []map[string]interface{}
	var numSamples int

	if datasetDataStr != "" {
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
		}
	}

	if len(dataset) == 0 {
		fmt.Fprintf(os.Stderr, "DATASET_DATA not available, using synthetic data. Dataset should be fetched by proplet runtime.\n")
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
	}

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

	updateRequest := map[string]interface{}{
		"action": "post_update",
		"url":    fmt.Sprintf("%s/update", coordinatorURL),
		"data":   update,
	}
	updateRequestJSON, _ := json.Marshal(updateRequest)
	fmt.Fprintf(os.Stderr, "UPDATE_REQUEST:%s\n", string(updateRequestJSON))

	updateJSON, err := json.Marshal(update)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to marshal update: %v\n", err)
		os.Exit(1)
	}

	fmt.Print(string(updateJSON))
}

func extractModelVersion(modelRef string) int {
	version := 0
	for i := len(modelRef) - 1; i >= 0; i-- {
		if modelRef[i] >= '0' && modelRef[i] <= '9' {
			var versionStr string
			for j := i; j >= 0 && modelRef[j] >= '0' && modelRef[j] <= '9'; j-- {
				versionStr = string(modelRef[j]) + versionStr
			}
			if v, err := parseInt(versionStr); err == nil {
				version = v
				break
			}
		}
	}
	return version
}

func parseInt(s string) (int, error) {
	result := 0
	for _, char := range s {
		if char >= '0' && char <= '9' {
			result = result*10 + int(char-'0')
		} else {
			return 0, fmt.Errorf("invalid character")
		}
	}
	return result, nil
}
