//go:build wasm
// +build wasm

package main

import (
	"encoding/json"
	"fmt"
	"math"
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
		} else {
			fmt.Fprintf(os.Stderr, "Failed to parse DATASET_DATA: %v\n", err)
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

	// Extract and convert weights from JSON ([]interface{} -> []float64)
	var weights []float64
	if wInterface, ok := model["w"]; ok {
		if wSlice, ok := wInterface.([]interface{}); ok {
			weights = make([]float64, len(wSlice))
			for i, v := range wSlice {
				if f, ok := v.(float64); ok {
					weights[i] = f
				}
			}
		} else if wSlice, ok := wInterface.([]float64); ok {
			weights = make([]float64, len(wSlice))
			copy(weights, wSlice)
		} else {
			weights = []float64{0.0, 0.0, 0.0}
		}
	} else {
		weights = []float64{0.0, 0.0, 0.0}
	}

	// Extract bias
	var bias float64
	if bInterface, ok := model["b"]; ok {
		if b, ok := bInterface.(float64); ok {
			bias = b
		}
	}

	// Debug: Log initial model state
	if len(dataset) > 0 {
		firstSample := dataset[0]
		if x, ok := firstSample["x"].([]interface{}); ok && len(x) >= 3 {
			if y, ok := firstSample["y"].(float64); ok {
				fmt.Fprintf(os.Stderr, "DEBUG: Initial model - w: [%.6f, %.6f, %.6f], b: %.6f\n",
					weights[0], weights[1], weights[2], bias)
				fmt.Fprintf(os.Stderr, "DEBUG: First sample - x: [%.3f, %.3f, %.3f], y: %.0f\n",
					x[0].(float64), x[1].(float64), x[2].(float64), y)
			}
		}
	}

	rand.Seed(time.Now().UnixNano())

	// Logistic regression training with SGD
	for epoch := 0; epoch < epochs; epoch++ {
		// Shuffle dataset
		for i := len(dataset) - 1; i > 0; i-- {
			j := rand.Intn(i + 1)
			dataset[i], dataset[j] = dataset[j], dataset[i]
		}

		// Process in batches (or all at once if batch_size is large)
		for batchStart := 0; batchStart < len(dataset); batchStart += batchSize {
			batchEnd := batchStart + batchSize
			if batchEnd > len(dataset) {
				batchEnd = len(dataset)
			}

			// Process each sample in the batch
			for i := batchStart; i < batchEnd; i++ {
				sample := dataset[i]
				xInterface, xOk := sample["x"].([]interface{})
				yInterface, yOk := sample["y"].(float64)

				if !xOk || !yOk {
					continue
				}

				// Convert x to []float64
				x := make([]float64, len(xInterface))
				for j, v := range xInterface {
					if f, ok := v.(float64); ok {
						x[j] = f
					}
				}

				// Compute prediction: z = w·x + b
				z := bias
				for j := 0; j < len(weights) && j < len(x); j++ {
					z += weights[j] * x[j]
				}

				// Sigmoid: p = 1/(1+exp(-z))
				// Clamp z to prevent overflow
				if z > 20 {
					z = 20
				} else if z < -20 {
					z = -20
				}
				p := 1.0 / (1.0 + math.Exp(-z))

				// Error: err = p - y
				err := p - yInterface

				// Update weights: w[i] -= lr * err * x[i]
				for j := 0; j < len(weights) && j < len(x); j++ {
					weights[j] -= lr * err * x[j]
				}

				// Update bias: b -= lr * err
				bias -= lr * err
			}
		}
	}

	// Update model map with trained weights and bias
	weightsSlice := make([]float64, len(weights))
	copy(weightsSlice, weights)
	model["w"] = weightsSlice
	model["b"] = bias

	// Debug: Log final model state
	fmt.Fprintf(os.Stderr, "DEBUG: Final model after training - w: [%.6f, %.6f, %.6f], b: %.6f\n",
		weights[0], weights[1], weights[2], bias)

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

	// Debug: Log the update payload being sent
	fmt.Fprintf(os.Stderr, "DEBUG: Update payload (first 500 chars): %s\n",
		func() string {
			s := string(updateJSON)
			if len(s) > 500 {
				return s[:500] + "..."
			}
			return s
		}())

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
