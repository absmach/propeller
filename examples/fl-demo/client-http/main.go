package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"time"
)

type Task struct {
	RoundID    string                 `json:"round_id"`
	ModelRef   string                 `json:"model_ref"`
	Config     map[string]interface{} `json:"config"`
	Hyperparams map[string]interface{} `json:"hyperparams,omitempty"`
}

type TaskResponse struct {
	Task Task `json:"task"`
}

type Update struct {
	RoundID      string                 `json:"round_id"`
	PropletID    string                 `json:"proplet_id"`
	BaseModelURI string                 `json:"base_model_uri"`
	NumSamples   int                    `json:"num_samples"`
	Metrics      map[string]interface{} `json:"metrics"`
	Update       map[string]interface{} `json:"update"`
}

var (
	coordinatorURL string
	modelRegistryURL string
	propletID     string
	httpClient    *http.Client
)

func main() {
	coordinatorURL = "http://coordinator-http:8080"
	if url := os.Getenv("COORDINATOR_URL"); url != "" {
		coordinatorURL = url
	}

	modelRegistryURL = "http://model-registry:8081"
	if url := os.Getenv("MODEL_REGISTRY_URL"); url != "" {
		modelRegistryURL = url
	}

	propletID = "proplet-1"
	if id := os.Getenv("PROPLET_ID"); id != "" {
		propletID = id
	}

	httpClient = &http.Client{
		Timeout: 30 * time.Second,
	}

	// Get task from coordinator
	roundID := os.Getenv("ROUND_ID")
	if roundID == "" {
		roundID = fmt.Sprintf("r-%d", time.Now().Unix())
	}

	slog.Info("Fetching task from coordinator", "round_id", roundID, "proplet_id", propletID)

	task, err := fetchTask(roundID, propletID)
	if err != nil {
		log.Fatalf("Failed to fetch task: %v", err)
	}

	slog.Info("Task received", "model_ref", task.ModelRef, "round_id", task.RoundID)

	// Fetch model from registry
	model, err := fetchModel(task.ModelRef)
	if err != nil {
		log.Fatalf("Failed to fetch model: %v", err)
	}

	slog.Info("Model fetched", "model_ref", task.ModelRef)

	// Perform local training (simplified)
	update := performTraining(model, task, propletID)

	// Send update to coordinator
	if err := sendUpdate(update); err != nil {
		log.Fatalf("Failed to send update: %v", err)
	}

	slog.Info("Update sent successfully", "round_id", roundID)
}

func fetchTask(roundID, propletID string) (*Task, error) {
	url := fmt.Sprintf("%s/task?round_id=%s&proplet_id=%s", coordinatorURL, roundID, propletID)
	
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to fetch task: %s", string(body))
	}

	var taskResp TaskResponse
	if err := json.NewDecoder(resp.Body).Decode(&taskResp); err != nil {
		return nil, err
	}

	return &taskResp.Task, nil
}

func fetchModel(modelRef string) (map[string]interface{}, error) {
	// Extract version from modelRef (e.g., "fl/models/global_model_v0" -> "0")
	var version string
	if _, err := fmt.Sscanf(modelRef, "fl/models/global_model_v%s", &version); err != nil {
		version = "0"
	}

	url := fmt.Sprintf("%s/models/%s", modelRegistryURL, version)
	
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to fetch model: %s", string(body))
	}

	var model map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&model); err != nil {
		return nil, err
	}

	return model, nil
}

func performTraining(model map[string]interface{}, task *Task, propletID string) Update {
	// Simplified training - in production, this would use actual ML framework
	// For demo, simulate training updates

	epochs := 1
	if e, ok := task.Hyperparams["epochs"].(float64); ok {
		epochs = int(e)
	}

	lr := 0.01
	if l, ok := task.Hyperparams["lr"].(float64); ok {
		lr = l
	}

	batchSize := 16
	if b, ok := task.Hyperparams["batch_size"].(float64); ok {
		batchSize = int(b)
	}

	// Simulate training updates
	weights := []float64{0.0, 0.0, 0.0}
	if w, ok := model["w"].([]interface{}); ok {
		weights = make([]float64, len(w))
		for i, v := range w {
			if f, ok := v.(float64); ok {
				weights[i] = f
			}
		}
	}

	bias := 0.0
	if b, ok := model["b"].(float64); ok {
		bias = b
	}

	// Simple gradient-like update
	for epoch := 0; epoch < epochs; epoch++ {
		for i := range weights {
			weights[i] += lr * 0.1 // Simplified gradient
		}
		bias += lr * 0.05
	}

	update := Update{
		RoundID:      task.RoundID,
		PropletID:    propletID,
		BaseModelURI: task.ModelRef,
		NumSamples:   batchSize * epochs,
		Metrics: map[string]interface{}{
			"loss": 0.5 + float64(epochs)*0.1,
		},
		Update: map[string]interface{}{
			"w": weights,
			"b": bias,
		},
	}

	return update
}

func sendUpdate(update Update) error {
	updateJSON, err := json.Marshal(update)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/update", coordinatorURL)
	resp, err := httpClient.Post(url, "application/json", bytes.NewBuffer(updateJSON))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to send update: %s", string(body))
	}

	return nil
}
