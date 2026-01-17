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
	"syscall"
	"time"

	"github.com/gorilla/mux"
)

type ExperimentConfig struct {
	ExperimentID string                 `json:"experiment_id"`
	RoundID      string                 `json:"round_id"`
	ModelRef     string                 `json:"model_ref"`
	Participants []string               `json:"participants"`
	Hyperparams  map[string]interface{} `json:"hyperparams"`
	KOfN         int                    `json:"k_of_n"`
	TimeoutS     int                    `json:"timeout_s"`
}

var coordinatorURL string

func main() {
	port := "8083"
	if p := os.Getenv("ORCHESTRATOR_PORT"); p != "" {
		port = p
	}

	coordinatorURL = "http://coordinator-http:8080"
	if url := os.Getenv("COORDINATOR_URL"); url != "" {
		coordinatorURL = url
	}

	r := mux.NewRouter()
	r.HandleFunc("/health", healthHandler).Methods("GET")
	r.HandleFunc("/experiments", createExperimentHandler).Methods("POST")
	r.HandleFunc("/experiments/{experiment_id}", getExperimentHandler).Methods("GET")

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: r,
	}

	go func() {
		slog.Info("Orchestrator HTTP server starting", "port", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start HTTP server: %v", err)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	slog.Info("Shutting down Orchestrator")
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func createExperimentHandler(w http.ResponseWriter, r *http.Request) {
	var config ExperimentConfig
	if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
		http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	// Generate round ID if not provided
	if config.RoundID == "" {
		config.RoundID = fmt.Sprintf("r-%d", time.Now().Unix())
	}

	// Set defaults
	if config.KOfN == 0 {
		config.KOfN = 3
	}
	if config.TimeoutS == 0 {
		config.TimeoutS = 60
	}
	if config.ModelRef == "" {
		config.ModelRef = "fl/models/global_model_v0"
	}

	slog.Info("Creating experiment", "experiment_id", config.ExperimentID, "round_id", config.RoundID)

	// Store experiment config (in production, use database)
	// For now, just log it

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(config)
}

func getExperimentHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	experimentID := vars["experiment_id"]

	// In production, retrieve from database
	// For now, return placeholder
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"experiment_id": experimentID,
		"status":        "active",
	})
}
