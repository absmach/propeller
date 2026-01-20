package main

import (
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/gorilla/mux"
)

type Dataset struct {
	ClientID string                 `json:"client_id"`
	Data    []map[string]interface{} `json:"data"`
	Size    int                     `json:"size"`
}

type DatasetStore struct {
	datasets map[string]*Dataset
	mu       sync.RWMutex
	dataDir  string
}

var store = &DatasetStore{
	datasets: make(map[string]*Dataset),
	dataDir:  "/tmp/fl-datasets",
}

func main() {
	if dir := os.Getenv("DATA_DIR"); dir != "" {
		store.dataDir = dir
	}

	if err := os.MkdirAll(store.dataDir, 0755); err != nil {
		log.Fatalf("Failed to create data directory: %v", err)
	}

	initializeSampleDatasets()

	port := "8083"
	if p := os.Getenv("DATA_STORE_PORT"); p != "" {
		port = p
	}

	r := mux.NewRouter()
	r.HandleFunc("/health", healthHandler).Methods("GET")
	r.HandleFunc("/datasets", listDatasetsHandler).Methods("GET")
	r.HandleFunc("/datasets/{client_id}", getDatasetHandler).Methods("GET")
	r.HandleFunc("/datasets/{client_id}", postDatasetHandler).Methods("POST")

	slog.Info("Local Data Store HTTP server starting", "port", port)

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: r,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start HTTP server: %v", err)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	slog.Info("Shutting down Local Data Store")
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func listDatasetsHandler(w http.ResponseWriter, r *http.Request) {
	store.mu.RLock()
	clientIDs := make([]string, 0, len(store.datasets))
	for clientID := range store.datasets {
		clientIDs = append(clientIDs, clientID)
	}
	store.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"clients": clientIDs,
		"count":   len(clientIDs),
	})
}

func getDatasetHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	clientID := vars["client_id"]
	if clientID == "" {
		http.Error(w, "client_id is required", http.StatusBadRequest)
		return
	}

	store.mu.RLock()
	dataset, exists := store.datasets[clientID]
	store.mu.RUnlock()

	if !exists {
		datasetFile := filepath.Join(store.dataDir, fmt.Sprintf("dataset_%s.json", clientID))
		data, err := os.ReadFile(datasetFile)
		if err != nil {
			http.Error(w, "Dataset not found", http.StatusNotFound)
			return
		}

		var loadedDataset Dataset
		if err := json.Unmarshal(data, &loadedDataset); err != nil {
			http.Error(w, "Invalid dataset file", http.StatusInternalServerError)
			return
		}

		store.mu.Lock()
		store.datasets[clientID] = &loadedDataset
		store.mu.Unlock()

		dataset = &loadedDataset
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(dataset)
}

func postDatasetHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	clientID := vars["client_id"]
	if clientID == "" {
		http.Error(w, "client_id is required", http.StatusBadRequest)
		return
	}

	var dataset Dataset
	if err := json.NewDecoder(r.Body).Decode(&dataset); err != nil {
		http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	dataset.ClientID = clientID
	dataset.Size = len(dataset.Data)

	store.mu.Lock()
	store.datasets[clientID] = &dataset
	store.mu.Unlock()

	// Save to file
	datasetFile := filepath.Join(store.dataDir, fmt.Sprintf("dataset_%s.json", clientID))
	datasetJSON, err := json.MarshalIndent(dataset, "", "  ")
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to marshal dataset: %v", err), http.StatusInternalServerError)
		return
	}

	if err := os.WriteFile(datasetFile, datasetJSON, 0644); err != nil {
		http.Error(w, fmt.Sprintf("Failed to write dataset file: %v", err), http.StatusInternalServerError)
		return
	}

	slog.Info("Dataset stored", "client_id", clientID, "size", dataset.Size)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"client_id": clientID,
		"size":      dataset.Size,
		"status":    "stored",
	})
}

func initializeSampleDatasets() {
	sampleClients := []string{"proplet-1", "proplet-2", "proplet-3"}

	for _, clientID := range sampleClients {
		sampleData := make([]map[string]interface{}, 512)
		for i := 0; i < 512; i++ {
			sampleData[i] = map[string]interface{}{
				"x": []float64{
					float64(i%10) / 10.0,
					float64((i*2)%10) / 10.0,
					float64((i*3)%10) / 10.0,
				},
				"y": float64(i%2),
			}
		}

		dataset := &Dataset{
			ClientID: clientID,
			Data:     sampleData,
			Size:     len(sampleData),
		}

		store.mu.Lock()
		store.datasets[clientID] = dataset
		store.mu.Unlock()

		// Save to file
		datasetFile := filepath.Join(store.dataDir, fmt.Sprintf("dataset_%s.json", clientID))
		datasetJSON, _ := json.MarshalIndent(dataset, "", "  ")
		os.WriteFile(datasetFile, datasetJSON, 0644)
	}

	slog.Info("Initialized sample datasets", "count", len(sampleClients))
}
