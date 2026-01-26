package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/gorilla/mux"
)

type RoundState struct {
	RoundID   string
	ModelURI  string
	KOfN      int
	TimeoutS  int
	StartTime time.Time
	Updates   []Update
	Completed bool
	mu        sync.Mutex
}

type Update struct {
	RoundID      string                 `json:"round_id"`
	PropletID    string                 `json:"proplet_id"`
	BaseModelURI string                 `json:"base_model_uri"`
	NumSamples   int                    `json:"num_samples"`
	Metrics      map[string]interface{} `json:"metrics"`
	Update       map[string]interface{} `json:"update"`
	ReceivedAt   string                 `json:"received_at,omitempty"`
}

type Task struct {
	RoundID     string                 `json:"round_id"`
	ModelRef    string                 `json:"model_ref"`
	Config      map[string]interface{} `json:"config"`
	Hyperparams map[string]interface{} `json:"hyperparams,omitempty"`
}

type TaskResponse struct {
	Task Task `json:"task"`
}

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

var (
	rounds           = make(map[string]*RoundState)
	roundsMu         sync.RWMutex
	modelVersion     = 0
	modelMu          sync.Mutex
	httpClient       *http.Client
	modelRegistryURL string
	aggregatorURL    string
	mqttClient       mqtt.Client
	mqttEnabled      bool
)

func main() {
	port := "8080"
	if p := os.Getenv("COORDINATOR_PORT"); p != "" {
		port = p
	}

	// Use environment variables - no hardcoded defaults for deployment flexibility
	// For Docker Compose, set these in compose.yaml or .env file
	// For Kubernetes, use ConfigMaps or environment variables
	// For bare metal, set system environment variables
	modelRegistryURL = os.Getenv("MODEL_REGISTRY_URL")
	if modelRegistryURL == "" {
		log.Fatal("MODEL_REGISTRY_URL environment variable is required")
	}

	aggregatorURL = os.Getenv("AGGREGATOR_URL")
	if aggregatorURL == "" {
		log.Fatal("AGGREGATOR_URL environment variable is required")
	}

	httpClient = &http.Client{
		Timeout: 30 * time.Second,
	}

	mqttBroker := os.Getenv("MQTT_BROKER")
	if mqttBroker == "" {
		mqttBroker = "tcp://mqtt:1883"
	}
	mqttClientID := os.Getenv("MQTT_CLIENT_ID")
	if mqttClientID == "" {
		mqttClientID = "fl-coordinator"
	}
	mqttUsername := os.Getenv("MQTT_USERNAME")
	mqttPassword := os.Getenv("MQTT_PASSWORD")

	if mqttBroker != "" {
		opts := mqtt.NewClientOptions()
		opts.AddBroker(mqttBroker)
		opts.SetClientID(mqttClientID)
		if mqttUsername != "" {
			opts.SetUsername(mqttUsername)
		}
		if mqttPassword != "" {
			opts.SetPassword(mqttPassword)
		}
		opts.SetAutoReconnect(true)
		opts.SetConnectRetry(true)
		opts.SetConnectRetryInterval(5 * time.Second)

		mqttClient = mqtt.NewClient(opts)
		if token := mqttClient.Connect(); token.Wait() && token.Error() != nil {
			slog.Warn("Failed to connect to MQTT broker, push notifications disabled", "error", token.Error())
			mqttEnabled = false
		} else {
			mqttEnabled = true
			slog.Info("Connected to MQTT broker for push notifications", "broker", mqttBroker)
		}
	} else {
		mqttEnabled = false
		slog.Info("MQTT broker not configured, push notifications disabled")
	}

	r := mux.NewRouter()
	r.HandleFunc("/health", healthHandler).Methods("GET")
	r.HandleFunc("/experiments", postExperimentHandler).Methods("POST")
	r.HandleFunc("/task", getTaskHandler).Methods("GET")
	r.HandleFunc("/update", postUpdateHandler).Methods("POST")
	r.HandleFunc("/update_cbor", postUpdateCBORHandler).Methods("POST")
	r.HandleFunc("/rounds/{round_id}/complete", getRoundCompleteHandler).Methods("GET")
	r.HandleFunc("/rounds/next", getNextRoundHandler).Methods("GET")

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: r,
	}

	go func() {
		slog.Info("FML Coordinator HTTP server starting", "port", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start HTTP server: %v", err)
		}
	}()

	go checkRoundTimeouts()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	slog.Info("Shutting down FML Coordinator")
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func postExperimentHandler(w http.ResponseWriter, r *http.Request) {
	var config ExperimentConfig
	if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
		http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	if config.RoundID == "" {
		http.Error(w, "round_id is required", http.StatusBadRequest)
		return
	}

	if config.KOfN == 0 {
		config.KOfN = 3
	}
	if config.TimeoutS == 0 {
		config.TimeoutS = 60
	}
	if config.ModelRef == "" {
		config.ModelRef = "fl/models/global_model_v0"
	}

	slog.Info("Received experiment configuration",
		"experiment_id", config.ExperimentID,
		"round_id", config.RoundID,
		"model_ref", config.ModelRef,
		"k_of_n", config.KOfN)

	modelVersion := extractModelVersion(config.ModelRef)

	modelURL := fmt.Sprintf("%s/models/%d", modelRegistryURL, modelVersion)
	resp, err := httpClient.Get(modelURL)
	if err != nil {
		slog.Warn("Failed to load initial model from registry", "error", err, "version", modelVersion)
	} else {
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			var model map[string]interface{}
			if err := json.NewDecoder(resp.Body).Decode(&model); err == nil {
				slog.Info("Loaded initial model from registry", "version", modelVersion)
			}
		}
	}

	roundsMu.Lock()
	round := &RoundState{
		RoundID:   config.RoundID,
		ModelURI:  config.ModelRef,
		KOfN:      config.KOfN,
		TimeoutS:  config.TimeoutS,
		StartTime: time.Now(),
		Updates:   make([]Update, 0),
		Completed: false,
	}
	rounds[config.RoundID] = round
	roundsMu.Unlock()

	slog.Info("Experiment configured and round initialized",
		"round_id", config.RoundID,
		"model_version", modelVersion)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"experiment_id": config.ExperimentID,
		"round_id":      config.RoundID,
		"status":        "configured",
		"model_version": modelVersion,
	})
}

func extractModelVersion(modelRef string) int {
	version := 0
	for i := len(modelRef) - 1; i >= 0; i-- {
		if modelRef[i] >= '0' && modelRef[i] <= '9' {
			var versionStr string
			for j := i; j >= 0 && modelRef[j] >= '0' && modelRef[j] <= '9'; j-- {
				versionStr = string(modelRef[j]) + versionStr
			}
			if v, err := strconv.Atoi(versionStr); err == nil {
				version = v
				break
			}
		}
	}
	return version
}

func getTaskHandler(w http.ResponseWriter, r *http.Request) {
	roundID := r.URL.Query().Get("round_id")
	propletID := r.URL.Query().Get("proplet_id")

	if roundID == "" {
		http.Error(w, "round_id is required", http.StatusBadRequest)
		return
	}

	roundsMu.RLock()
	round, exists := rounds[roundID]
	roundsMu.RUnlock()

	if !exists {
		roundsMu.Lock()
		modelMu.Lock()
		currentVersion := modelVersion
		modelMu.Unlock()

		round = &RoundState{
			RoundID:   roundID,
			ModelURI:  fmt.Sprintf("fl/models/global_model_v%d", currentVersion),
			KOfN:      3,
			TimeoutS:  60,
			StartTime: time.Now(),
			Updates:   make([]Update, 0),
			Completed: false,
		}
		rounds[roundID] = round
		roundsMu.Unlock()
		slog.Info("Initialized round from task request", "round_id", roundID)
	}

	modelRef := round.ModelURI
	if modelRef == "" {
		modelMu.Lock()
		currentVersion := modelVersion
		modelMu.Unlock()
		modelRef = fmt.Sprintf("fl/models/global_model_v%d", currentVersion)
	}

	task := Task{
		RoundID:  roundID,
		ModelRef: modelRef,
		Config: map[string]interface{}{
			"proplet_id": propletID,
		},
		Hyperparams: map[string]interface{}{
			"epochs":     1,
			"lr":         0.01,
			"batch_size": 16,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(TaskResponse{Task: task})
}

func postUpdateHandler(w http.ResponseWriter, r *http.Request) {
	var update Update
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	// Validate required fields
	if update.RoundID == "" {
		http.Error(w, "round_id is required", http.StatusBadRequest)
		return
	}
	if update.PropletID == "" {
		http.Error(w, "proplet_id is required", http.StatusBadRequest)
		return
	}
	if len(update.Update) == 0 {
		http.Error(w, "update data is required and cannot be empty", http.StatusBadRequest)
		return
	}

	update.ReceivedAt = time.Now().UTC().Format(time.RFC3339)
	processUpdate(update)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "accepted"})
}

func postUpdateCBORHandler(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "CBOR support not implemented", http.StatusNotImplemented)
}

func getRoundCompleteHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	roundID := vars["round_id"]

	roundsMu.RLock()
	round, exists := rounds[roundID]
	roundsMu.RUnlock()

	if !exists {
		http.Error(w, "Round not found", http.StatusNotFound)
		return
	}

	round.mu.Lock()
	completed := round.Completed
	numUpdates := len(round.Updates)
	round.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"round_id":    roundID,
		"completed":   completed,
		"num_updates": numUpdates,
	})
}

func getNextRoundHandler(w http.ResponseWriter, r *http.Request) {
	roundsMu.RLock()
	defer roundsMu.RUnlock()

	var latestRound *RoundState
	var latestRoundID string
	for roundID, round := range rounds {
		round.mu.Lock()
		if round.Completed && (latestRound == nil || round.StartTime.After(latestRound.StartTime)) {
			latestRound = round
			latestRoundID = roundID
		}
		round.mu.Unlock()
	}

	if latestRound == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"next_round_available": false,
			"message":              "No completed rounds found",
		})
		return
	}

	modelMu.Lock()
	currentVersion := modelVersion
	modelMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"next_round_available": true,
		"last_completed_round": latestRoundID,
		"new_model_version":    currentVersion,
		"model_uri":            fmt.Sprintf("fl/models/global_model_v%d", currentVersion),
		"status":               "ready",
	})
}

func processUpdate(update Update) {
	roundID := update.RoundID
	if roundID == "" {
		slog.Warn("Update missing round_id, ignoring")
		return
	}

	roundsMu.Lock()
	round, exists := rounds[roundID]

	if !exists {
		modelURI := update.BaseModelURI
		if modelURI == "" {
			modelURI = "fl/models/global_model_v0"
		}

		round = &RoundState{
			RoundID:   roundID,
			ModelURI:  modelURI,
			KOfN:      3,
			TimeoutS:  60,
			StartTime: time.Now(),
			Updates:   make([]Update, 0),
			Completed: false,
		}
		rounds[roundID] = round
	}

	roundsMu.Unlock()

	round.mu.Lock()
	defer round.mu.Unlock()

	if round.Completed {
		slog.Warn("Received update for completed round, ignoring", "round_id", roundID)
		return
	}

	round.Updates = append(round.Updates, update)
	slog.Info("Received update", "round_id", roundID, "proplet_id", update.PropletID, "total_updates", len(round.Updates), "k_of_n", round.KOfN)

	if len(round.Updates) >= round.KOfN {
		slog.Info("Round complete: received k_of_n updates", "round_id", roundID, "updates", len(round.Updates))
		round.Completed = true
		go aggregateAndAdvance(round)
	}
}

// retryWithBackoff performs an HTTP request with exponential backoff retry
func retryWithBackoff(maxRetries int, initialDelay time.Duration, operation func() error) error {
	delay := initialDelay
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			slog.Info("Retrying operation", "attempt", attempt+1, "max_retries", maxRetries, "delay_ms", delay.Milliseconds())
			time.Sleep(delay)
			delay = time.Duration(float64(delay) * 1.5) // Exponential backoff: 1.5x multiplier
		}

		err := operation()
		if err == nil {
			if attempt > 0 {
				slog.Info("Operation succeeded after retry", "attempt", attempt+1)
			}
			return nil
		}

		lastErr = err
		slog.Warn("Operation failed, will retry", "attempt", attempt+1, "error", err)
	}

	return fmt.Errorf("operation failed after %d attempts: %w", maxRetries, lastErr)
}

func aggregateAndAdvance(round *RoundState) {
	round.mu.Lock()
	updates := make([]Update, len(round.Updates))
	copy(updates, round.Updates)
	round.mu.Unlock()

	if len(updates) == 0 {
		slog.Error("No updates to aggregate", "round_id", round.RoundID)
		return
	}

	slog.Info("Calling aggregator service", "round_id", round.RoundID, "num_updates", len(updates))

	aggregatorReq := map[string]interface{}{
		"updates": updates,
	}

	reqBody, err := json.Marshal(aggregatorReq)
	if err != nil {
		slog.Error("Failed to marshal aggregator request", "error", err)
		return
	}

	var aggregatedModel map[string]interface{}

	// Retry aggregator call with exponential backoff
	err = retryWithBackoff(3, 1*time.Second, func() error {
		resp, err := httpClient.Post(aggregatorURL+"/aggregate", "application/json",
			bytes.NewBuffer(reqBody))
		if err != nil {
			return fmt.Errorf("failed to call aggregator: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("aggregator returned error status: %d", resp.StatusCode)
		}

		if err := json.NewDecoder(resp.Body).Decode(&aggregatedModel); err != nil {
			return fmt.Errorf("failed to decode aggregated model: %w", err)
		}

		return nil
	})

	if err != nil {
		slog.Error("Failed to aggregate updates after retries", "round_id", round.RoundID, "error", err)
		return
	}

	modelMu.Lock()
	modelVersion++
	newVersion := modelVersion
	modelMu.Unlock()

	modelData := map[string]interface{}{
		"version": newVersion,
		"model":   aggregatedModel,
	}

	storeReq, err := json.Marshal(modelData)
	if err != nil {
		slog.Error("Failed to marshal model data", "error", err)
		return
	}

	// Retry model registry call with exponential backoff
	err = retryWithBackoff(3, 1*time.Second, func() error {
		storeResp, err := httpClient.Post(modelRegistryURL+"/models", "application/json",
			bytes.NewBuffer(storeReq))
		if err != nil {
			return fmt.Errorf("failed to store model in registry: %w", err)
		}
		defer storeResp.Body.Close()

		if storeResp.StatusCode != http.StatusCreated {
			return fmt.Errorf("model registry returned error status: %d", storeResp.StatusCode)
		}

		return nil
	})

	if err != nil {
		slog.Error("Failed to store model in registry after retries", "round_id", round.RoundID, "version", newVersion, "error", err)
		return
	}

	slog.Info("Aggregated model stored", "round_id", round.RoundID, "version", newVersion)

	nextRoundNotification := map[string]interface{}{
		"round_id":             round.RoundID,
		"new_model_version":    newVersion,
		"model_uri":            fmt.Sprintf("fl/models/global_model_v%d", newVersion),
		"status":               "complete",
		"next_round_available": true,
		"timestamp":            time.Now().UTC().Format(time.RFC3339),
	}

	notificationJSON, err := json.Marshal(nextRoundNotification)
	if err != nil {
		slog.Error("Failed to marshal notification", "error", err)
	} else {
		if mqttEnabled && mqttClient != nil && mqttClient.IsConnected() {
			topic := "fl/rounds/next"
			token := mqttClient.Publish(topic, 1, false, notificationJSON)
			if token.Wait() && token.Error() != nil {
				slog.Error("Failed to publish round completion notification", "error", token.Error())
			} else {
				slog.Info("Published round completion notification to MQTT",
					"topic", topic,
					"round_id", round.RoundID,
					"new_model_version", newVersion)
			}
		} else {
			slog.Info("Round complete, next round available (MQTT not available, clients should poll)",
				"round_id", round.RoundID,
				"new_model_version", newVersion)
		}
	}
}

func checkRoundTimeouts() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		// Collect rounds that need timeout checking while holding read lock
		roundsMu.RLock()
		roundsToCheck := make([]*RoundState, 0, len(rounds))
		for _, round := range rounds {
			roundsToCheck = append(roundsToCheck, round)
		}
		roundsMu.RUnlock()

		// Check timeouts without holding the map lock
		for _, round := range roundsToCheck {
			round.mu.Lock()
			if !round.Completed {
				elapsed := now.Sub(round.StartTime)
				if elapsed >= time.Duration(round.TimeoutS)*time.Second {
					slog.Warn("Round timeout exceeded", "round_id", round.RoundID, "timeout_s", round.TimeoutS, "updates", len(round.Updates))
					round.Completed = true
					if len(round.Updates) > 0 {
						go aggregateAndAdvance(round)
					}
				}
			}
			round.mu.Unlock()
		}
	}
}
