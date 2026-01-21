package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const (
	ManagerURL       = "http://localhost:7070"
	CoordinatorURL   = "http://localhost:8080"
	ModelRegistryURL = "http://localhost:8081"
	AggregatorURL    = "http://localhost:8082"
)

type ExperimentConfig struct {
	ExperimentID  string                 `json:"experiment_id"`
	RoundID       string                 `json:"round_id"`
	ModelRef      string                 `json:"model_ref"`
	Participants  []string               `json:"participants"`
	Hyperparams   map[string]interface{} `json:"hyperparams"`
	KOfN          int                    `json:"k_of_n"`
	TimeoutS      int                    `json:"timeout_s"`
	TaskWasmImage string                 `json:"task_wasm_image,omitempty"`
}

type TaskResponse struct {
	Task struct {
		RoundID    string                 `json:"round_id"`
		ModelRef   string                 `json:"model_ref"`
		Config     map[string]interface{} `json:"config"`
		Hyperparams map[string]interface{} `json:"hyperparams"`
	} `json:"task"`
}

type Update struct {
	RoundID      string                 `json:"round_id"`
	PropletID    string                 `json:"proplet_id"`
	BaseModelURI string                 `json:"base_model_uri"`
	NumSamples   int                    `json:"num_samples"`
	Metrics      map[string]interface{} `json:"metrics"`
	Update       map[string]interface{} `json:"update"`
}

type RoundStatus struct {
	RoundID    string `json:"round_id"`
	Completed  bool   `json:"completed"`
	NumUpdates int    `json:"num_updates"`
}

func main() {
	fmt.Println("=" + repeat("=", 59))
	fmt.Println("Propeller HTTP FL Demo - 12-Step Workflow")
	fmt.Println("=" + repeat("=", 59))

	client := &http.Client{Timeout: 30 * time.Second}

	if err := verifyServices(client); err != nil {
		fmt.Printf("Service verification failed: %v\n", err)
		return
	}

	if err := ensureInitialModel(client); err != nil {
		fmt.Printf("Failed to ensure initial model: %v\n", err)
		return
	}

	roundID := fmt.Sprintf("r-%d", time.Now().Unix())
	
	// Get proplet CLIENT_IDs from environment variables (SuperMQ client IDs, not instance IDs)
	participants := getParticipants()
	fmt.Printf("\nUsing participants (CLIENT_IDs): %v\n", participants)

	fmt.Printf("\n[Step 1] Configure experiment (Manager -> Coordinator)\n")
	if err := configureExperiment(client, roundID, participants); err != nil {
		fmt.Printf("Failed to configure experiment: %v\n", err)
		return
	}

	fmt.Printf("\n[Step 2] Load initial model (Coordinator -> Model Registry)\n")
	fmt.Printf("Coordinator loads model automatically during experiment configuration\n")

	time.Sleep(2 * time.Second)

	fmt.Printf("\n[Steps 3-7] Client workflow\n")
	for _, propletID := range participants {
		fmt.Printf("\nClient %s:\n", propletID)

		fmt.Printf("  [Step 3] GET /task (Coordinator -> Client)\n")
		task, err := getTask(client, roundID, propletID)
		if err != nil {
			fmt.Printf("    Failed: %v\n", err)
			continue
		}
		fmt.Printf("    Task received: model_ref=%s\n", task.Task.ModelRef)

		fmt.Printf("  [Step 4] Fetch model (Client -> Model Registry)\n")
		model, err := fetchModel(client, task.Task.ModelRef)
		if err != nil {
			fmt.Printf("    Failed: %v\n", err)
			continue
		}
		fmt.Printf("    Model fetched: version extracted from %s\n", task.Task.ModelRef)

		fmt.Printf("  [Step 5] Load local dataset (Client -> Local Data Store)\n")
		fmt.Printf("    Dataset loaded (simulated)\n")

		fmt.Printf("  [Step 6] Local training (Client internal)\n")
		update := trainModel(model, propletID, roundID, task.Task.ModelRef)
		fmt.Printf("    Training complete\n")

		fmt.Printf("  [Step 7] POST /update (Client -> Coordinator)\n")
		if err := sendUpdate(client, update); err != nil {
			fmt.Printf("    Failed: %v\n", err)
			continue
		}
		fmt.Printf("    Update sent to Coordinator\n")
	}

	fmt.Printf("\n[Steps 8-11] Coordinator operations (internal)\n")
	fmt.Printf("  [Step 8] Validate & buffer updates\n")
	fmt.Printf("  [Step 9] Aggregate updates (Coordinator -> Aggregator)\n")
	fmt.Printf("  [Step 10] New global model (Aggregator -> Coordinator)\n")
	fmt.Printf("  [Step 11] Store model (Coordinator -> Model Registry)\n")
	fmt.Printf("  Waiting for aggregation...\n")
	time.Sleep(3 * time.Second)

	fmt.Printf("\n[Step 12] Next round available (Coordinator -> Client)\n")
	status, err := getRoundStatus(client, roundID)
	if err != nil {
		fmt.Printf("Failed to get round status: %v\n", err)
	} else {
		fmt.Printf("Round status: completed=%v, updates=%d\n", status.Completed, status.NumUpdates)
	}

	fmt.Printf("\n[Verification] Checking for aggregated model...\n")
	if err := verifyAggregatedModel(client); err != nil {
		fmt.Printf("Verification failed: %v\n", err)
	} else {
		fmt.Printf("Aggregated model v1 found in Model Registry\n")
	}

	fmt.Println("\n" + "=" + repeat("=", 59))
	fmt.Println("Demo complete - All 12 steps executed")
	fmt.Println("=" + repeat("=", 59))
}

func verifyServices(client *http.Client) error {
	services := map[string]string{
		"Propeller Manager": ManagerURL,
		"FL Coordinator":    CoordinatorURL,
		"Model Registry":    ModelRegistryURL,
		"Aggregator":        AggregatorURL,
	}

	for name, url := range services {
		resp, err := client.Get(url + "/health")
		if err != nil {
			return fmt.Errorf("%s not accessible: %w", name, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("%s returned status %d", name, resp.StatusCode)
		}
		fmt.Printf("Service verified: %s\n", name)
	}
	return nil
}

func ensureInitialModel(client *http.Client) error {
	resp, err := client.Get(ModelRegistryURL + "/models/0")
	if err == nil && resp.StatusCode == http.StatusOK {
		resp.Body.Close()
		return nil
	}

	initialModel := map[string]interface{}{
		"version": 0,
		"model": map[string]interface{}{
			"w": []float64{0.0, 0.0, 0.0},
			"b": 0.0,
		},
	}

	data, _ := json.Marshal(initialModel)
	resp, err = client.Post(ModelRegistryURL+"/models", "application/json", bytes.NewBuffer(data))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to create initial model: %d", resp.StatusCode)
	}

	fmt.Printf("Initial model v0 created\n")
	return nil
}

func configureExperiment(client *http.Client, roundID string, participants []string) error {
	config := ExperimentConfig{
		ExperimentID: fmt.Sprintf("exp-%s", roundID),
		RoundID:      roundID,
		ModelRef:     "fl/models/global_model_v0",
		Participants: participants,
		Hyperparams: map[string]interface{}{
			"epochs":      1,
			"lr":          0.01,
			"batch_size":  16,
		},
		KOfN:         3,
		TimeoutS:    60,
		TaskWasmImage: "oci://example/fl-client-wasm:latest",
	}

	data, _ := json.Marshal(config)
	resp, err := client.Post(ManagerURL+"/fl/experiments", "application/json", bytes.NewBuffer(data))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to configure experiment: %d - %s", resp.StatusCode, string(body))
	}

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	fmt.Printf("Experiment configured: %s\n", result["experiment_id"])
	return nil
}

func getTask(client *http.Client, roundID, propletID string) (*TaskResponse, error) {
	url := fmt.Sprintf("%s/task?round_id=%s&proplet_id=%s", CoordinatorURL, roundID, propletID)
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get task: %d", resp.StatusCode)
	}

	var taskResp TaskResponse
	if err := json.NewDecoder(resp.Body).Decode(&taskResp); err != nil {
		return nil, err
	}

	return &taskResp, nil
}

func fetchModel(client *http.Client, modelRef string) (map[string]interface{}, error) {
	version := extractVersionFromModelRef(modelRef)
	url := fmt.Sprintf("%s/models/%d", ModelRegistryURL, version)

	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return map[string]interface{}{
			"w": []float64{0.0, 0.0, 0.0},
			"b": 0.0,
		}, nil
	}

	var model map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&model)
	return model, nil
}

func trainModel(model map[string]interface{}, propletID, roundID, modelRef string) Update {
	w := model["w"].([]interface{})
	weights := make([]float64, len(w))
	for i, v := range w {
		if f, ok := v.(float64); ok {
			weights[i] = f
		}
	}

	bias := 0.0
	if b, ok := model["b"].(float64); ok {
		bias = b
	}

	hash := hashString(propletID)
	for i := range weights {
		weights[i] += float64(hash%10) * 0.01
	}
	bias += float64(hash%5) * 0.01

	return Update{
		RoundID:      roundID,
		PropletID:    propletID,
		BaseModelURI: modelRef,
		NumSamples:   512,
		Metrics: map[string]interface{}{
			"loss": 0.5 + float64(hash%10)*0.01,
		},
		Update: map[string]interface{}{
			"w": weights,
			"b": bias,
		},
	}
}

func sendUpdate(client *http.Client, update Update) error {
	data, _ := json.Marshal(update)
	resp, err := client.Post(CoordinatorURL+"/update", "application/json", bytes.NewBuffer(data))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to send update: %d", resp.StatusCode)
	}

	return nil
}

func getRoundStatus(client *http.Client, roundID string) (*RoundStatus, error) {
	url := fmt.Sprintf("%s/rounds/%s/complete", CoordinatorURL, roundID)
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get round status: %d", resp.StatusCode)
	}

	var status RoundStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, err
	}

	return &status, nil
}

func verifyAggregatedModel(client *http.Client) error {
	resp, err := client.Get(ModelRegistryURL + "/models/1")
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("model v1 not found: %d", resp.StatusCode)
	}

	var model map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&model)
	return nil
}

func extractVersionFromModelRef(modelRef string) int {
	for i := len(modelRef) - 1; i >= 0; i-- {
		if modelRef[i] >= '0' && modelRef[i] <= '9' {
			var versionStr string
			for j := i; j >= 0 && modelRef[j] >= '0' && modelRef[j] <= '9'; j-- {
				versionStr = string(modelRef[j]) + versionStr
			}
			if v, err := parseInt(versionStr); err == nil {
				return v
			}
		}
	}
	return 0
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

func hashString(s string) int {
	hash := 0
	for _, char := range s {
		hash = hash*31 + int(char)
	}
	if hash < 0 {
		hash = -hash
	}
	return hash
}

func repeat(s string, n int) string {
	result := ""
	for i := 0; i < n; i++ {
		result += s
	}
	return result
}

func getParticipants() []string {
	// Get CLIENT_IDs from environment variables (SuperMQ client IDs, not instance IDs)
	// Note: Environment variables should be exported from docker/.env before running
	proplet1ID := os.Getenv("PROPLET_CLIENT_ID")
	if proplet1ID == "" {
		fmt.Println("Warning: PROPLET_CLIENT_ID not set")
		fmt.Println("  Export it from docker/.env: export PROPLET_CLIENT_ID=$(grep '^PROPLET_CLIENT_ID=' docker/.env | tail -1 | cut -d '=' -f2)")
		fmt.Println("  Using fallback 'proplet-1' (this will likely fail)")
		proplet1ID = "proplet-1"
	}

	proplet2ID := os.Getenv("PROPLET_2_CLIENT_ID")
	if proplet2ID == "" {
		fmt.Println("Warning: PROPLET_2_CLIENT_ID not set")
		fmt.Println("  Export it from docker/.env: export PROPLET_2_CLIENT_ID=$(grep '^PROPLET_2_CLIENT_ID=' docker/.env | cut -d '=' -f2)")
		fmt.Println("  Using fallback 'proplet-2' (this will likely fail)")
		proplet2ID = "proplet-2"
	}

	proplet3ID := os.Getenv("PROPLET_3_CLIENT_ID")
	if proplet3ID == "" {
		fmt.Println("Warning: PROPLET_3_CLIENT_ID not set")
		fmt.Println("  Export it from docker/.env: export PROPLET_3_CLIENT_ID=$(grep '^PROPLET_3_CLIENT_ID=' docker/.env | cut -d '=' -f2)")
		fmt.Println("  Using fallback 'proplet-3' (this will likely fail)")
		proplet3ID = "proplet-3"
	}

	return []string{proplet1ID, proplet2ID, proplet3ID}
}
