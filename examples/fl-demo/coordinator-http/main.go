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
	"sync"
	"syscall"
	"time"

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
	RoundID    string                 `json:"round_id"`
	ModelRef   string                 `json:"model_ref"`
	Config     map[string]interface{} `json:"config"`
	Hyperparams map[string]interface{} `json:"hyperparams,omitempty"`
}

type TaskResponse struct {
	Task Task `json:"task"`
}

var (
	rounds       = make(map[string]*RoundState)
	roundsMu     sync.RWMutex
	modelVersion = 0
	modelMu      sync.Mutex
	httpClient   *http.Client
	modelRegistryURL string
	aggregatorURL   string
)

func main() {
	// Configuration
	port := "8080"
	if p := os.Getenv("COORDINATOR_PORT"); p != "" {
		port = p
	}

	modelRegistryURL = "http://model-registry:8081"
	if url := os.Getenv("MODEL_REGISTRY_URL"); url != "" {
		modelRegistryURL = url
	}

	aggregatorURL = "http://aggregator:8082"
	if url := os.Getenv("AGGREGATOR_URL"); url != "" {
		aggregatorURL = url
	}

	httpClient = &http.Client{
		Timeout: 30 * time.Second,
	}

	// HTTP Router
	r := mux.NewRouter()
	r.HandleFunc("/health", healthHandler).Methods("GET")
	r.HandleFunc("/task", getTaskHandler).Methods("GET")
	r.HandleFunc("/update", postUpdateHandler).Methods("POST")
	r.HandleFunc("/update_cbor", postUpdateCBORHandler).Methods("POST")
	r.HandleFunc("/rounds/{round_id}/complete", getRoundCompleteHandler).Methods("GET")

	// Start HTTP server
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

	// Start round timeout checker
	go checkRoundTimeouts()

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	slog.Info("Shutting down FML Coordinator")
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func getTaskHandler(w http.ResponseWriter, r *http.Request) {
	// Extract query parameters
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
		// Initialize round if it doesn't exist
		roundsMu.Lock()
		round = &RoundState{
			RoundID:   roundID,
			ModelURI:  "fl/models/global_model_v0",
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

	// Get current model version
	modelMu.Lock()
	currentVersion := modelVersion
	modelMu.Unlock()

	modelRef := fmt.Sprintf("fl/models/global_model_v%d", currentVersion)

	// Create task response
	task := Task{
		RoundID:  roundID,
		ModelRef: modelRef,
		Config: map[string]interface{}{
			"proplet_id": propletID,
		},
		Hyperparams: map[string]interface{}{
			"epochs":      1,
			"lr":          0.01,
			"batch_size":  16,
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

	update.ReceivedAt = time.Now().UTC().Format(time.RFC3339)
	processUpdate(update)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "accepted"})
}

func postUpdateCBORHandler(w http.ResponseWriter, r *http.Request) {
	// For now, decode CBOR to JSON and process
	// In production, use proper CBOR library
	http.Error(w, "CBOR support coming soon", http.StatusNotImplemented)
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

func processUpdate(update Update) {
	roundID := update.RoundID
	if roundID == "" {
		slog.Warn("Update missing round_id, ignoring")
		return
	}

	roundsMu.Lock()
	round, exists := rounds[roundID]

	if !exists {
		slog.Info("Received update for unknown round, lazy initializing", "round_id", roundID)
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

	// Check if we have enough updates
	if len(round.Updates) >= round.KOfN {
		slog.Info("Round complete: received k_of_n updates", "round_id", roundID, "updates", len(round.Updates))
		round.Completed = true
		go aggregateAndAdvance(round)
	}
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

	// Call aggregator service
	aggregatorReq := map[string]interface{}{
		"updates": updates,
	}

	reqBody, err := json.Marshal(aggregatorReq)
	if err != nil {
		slog.Error("Failed to marshal aggregator request", "error", err)
		return
	}

	resp, err := httpClient.Post(aggregatorURL+"/aggregate", "application/json", 
		bytes.NewBuffer(reqBody))
	if err != nil {
		slog.Error("Failed to call aggregator", "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Error("Aggregator returned error", "status", resp.StatusCode)
		return
	}

	var aggregatedModel map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&aggregatedModel); err != nil {
		slog.Error("Failed to decode aggregated model", "error", err)
		return
	}

	// Increment model version
	modelMu.Lock()
	modelVersion++
	newVersion := modelVersion
	modelMu.Unlock()

	// Store model in registry
	modelData := map[string]interface{}{
		"version": newVersion,
		"model":   aggregatedModel,
	}

	storeReq, err := json.Marshal(modelData)
	if err != nil {
		slog.Error("Failed to marshal model data", "error", err)
		return
	}

	storeResp, err := httpClient.Post(modelRegistryURL+"/models", "application/json",
		bytes.NewBuffer(storeReq))
	if err != nil {
		slog.Error("Failed to store model in registry", "error", err)
		return
	}
	defer storeResp.Body.Close()

	if storeResp.StatusCode != http.StatusCreated {
		slog.Error("Model registry returned error", "status", storeResp.StatusCode)
		return
	}

	slog.Info("Aggregated model stored", "round_id", round.RoundID, "version", newVersion)
}

func checkRoundTimeouts() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		roundsMu.RLock()
		for roundID, round := range rounds {
			round.mu.Lock()
			if !round.Completed {
				elapsed := now.Sub(round.StartTime)
				if elapsed >= time.Duration(round.TimeoutS)*time.Second {
					slog.Warn("Round timeout exceeded", "round_id", roundID, "timeout_s", round.TimeoutS, "updates", len(round.Updates))
					round.Completed = true
					if len(round.Updates) > 0 {
						go aggregateAndAdvance(round)
					}
				}
			}
			round.mu.Unlock()
		}
		roundsMu.RUnlock()
	}
}
