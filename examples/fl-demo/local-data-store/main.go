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
	"strings"
	"sync"
	"syscall"

	"github.com/gorilla/mux"
)

type Dataset struct {
	Schema   string                 `json:"schema"`
	PropletID string                `json:"proplet_id,omitempty"`
	ClientID string                 `json:"client_id,omitempty"` // Legacy field for backward compatibility
	Data     []map[string]interface{} `json:"data"`
	Size     int                    `json:"size"`
}

type DatasetStore struct {
	datasets map[string]*Dataset
	mu       sync.RWMutex
	dataDir  string
}

var store = &DatasetStore{
	datasets: make(map[string]*Dataset),
	dataDir:  "/data/datasets",
}

func main() {
	if dir := os.Getenv("DATA_DIR"); dir != "" {
		store.dataDir = dir
	}

	if err := os.MkdirAll(store.dataDir, 0755); err != nil {
		log.Fatalf("Failed to create data directory: %v", err)
	}

	// Load existing datasets from disk
	loadDatasetsFromDisk()

	// Seed datasets for participants
	seedDatasetsForParticipants()

	port := "8083"
	if p := os.Getenv("DATA_STORE_PORT"); p != "" {
		port = p
	}

	r := mux.NewRouter()
	r.HandleFunc("/health", healthHandler).Methods("GET")
	r.HandleFunc("/datasets", listDatasetsHandler).Methods("GET")
	// Support both proplet_id and client_id for backward compatibility
	r.HandleFunc("/datasets/{proplet_id}", getDatasetHandler).Methods("GET")
	r.HandleFunc("/datasets/{proplet_id}", postDatasetHandler).Methods("POST")
	r.HandleFunc("/datasets/{client_id}", getDatasetHandler).Methods("GET") // Legacy route
	r.HandleFunc("/datasets/{client_id}", postDatasetHandler).Methods("POST") // Legacy route

	slog.Info("Local Data Store HTTP server starting", "port", port, "data_dir", store.dataDir)

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
	propletID := vars["proplet_id"]
	if propletID == "" {
		// Fallback to client_id for backward compatibility
		propletID = vars["client_id"]
	}
	if propletID == "" {
		http.Error(w, "proplet_id or client_id is required", http.StatusBadRequest)
		return
	}

	store.mu.RLock()
	dataset, exists := store.datasets[propletID]
	store.mu.RUnlock()

	if !exists {
		datasetFile := filepath.Join(store.dataDir, fmt.Sprintf("%s.json", propletID))
		data, err := os.ReadFile(datasetFile)
		if err != nil {
			slog.Warn("Dataset missing", "proplet_id", propletID, "path", datasetFile)
			http.Error(w, "Dataset not found", http.StatusNotFound)
			return
		}

		var loadedDataset Dataset
		if err := json.Unmarshal(data, &loadedDataset); err != nil {
			slog.Error("Invalid dataset file", "proplet_id", propletID, "error", err)
			http.Error(w, "Invalid dataset file", http.StatusInternalServerError)
			return
		}

		store.mu.Lock()
		store.datasets[propletID] = &loadedDataset
		store.mu.Unlock()

		dataset = &loadedDataset
	}

	slog.Info("Dataset served", "proplet_id", propletID, "size", dataset.Size)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(dataset)
}

func postDatasetHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	propletID := vars["proplet_id"]
	if propletID == "" {
		// Fallback to client_id for backward compatibility
		propletID = vars["client_id"]
	}
	if propletID == "" {
		http.Error(w, "proplet_id or client_id is required", http.StatusBadRequest)
		return
	}

	var dataset Dataset
	if err := json.NewDecoder(r.Body).Decode(&dataset); err != nil {
		http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	// Ensure schema and proplet_id are set
	if dataset.Schema == "" {
		dataset.Schema = "fl-demo-dataset-v1"
	}
	dataset.PropletID = propletID
	dataset.ClientID = propletID // For backward compatibility
	dataset.Size = len(dataset.Data)

	store.mu.Lock()
	store.datasets[propletID] = &dataset
	store.mu.Unlock()

	// Save to file using atomic write
	if err := saveDatasetAtomic(propletID, &dataset); err != nil {
		http.Error(w, fmt.Sprintf("Failed to write dataset file: %v", err), http.StatusInternalServerError)
		return
	}

	slog.Info("Dataset stored", "proplet_id", propletID, "size", dataset.Size, "path", filepath.Join(store.dataDir, fmt.Sprintf("%s.json", propletID)))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"proplet_id": propletID,
		"size":       dataset.Size,
		"status":     "stored",
	})
}

// getParticipantUUIDs reads participant UUIDs from environment variables
func getParticipantUUIDs() []string {
	// First, try FL_DATASET_PARTICIPANTS (comma-separated list)
	if participants := os.Getenv("FL_DATASET_PARTICIPANTS"); participants != "" {
		uuids := strings.Split(participants, ",")
		var result []string
		for _, uuid := range uuids {
			uuid = strings.TrimSpace(uuid)
			if uuid != "" {
				result = append(result, uuid)
			}
		}
		if len(result) > 0 {
			return result
		}
	}

	// Fallback to individual PROPLET_*_CLIENT_ID env vars
	var uuids []string
	if id := os.Getenv("PROPLET_CLIENT_ID"); id != "" {
		uuids = append(uuids, id)
	}
	if id := os.Getenv("PROPLET_2_CLIENT_ID"); id != "" {
		uuids = append(uuids, id)
	}
	if id := os.Getenv("PROPLET_3_CLIENT_ID"); id != "" {
		uuids = append(uuids, id)
	}

	return uuids
}

// generateDataset creates a deterministic dataset for a given proplet ID
func generateDataset(propletID string, numSamples int) *Dataset {
	sampleData := make([]map[string]interface{}, numSamples)
	
	// Use proplet ID hash to generate deterministic but unique data per proplet
	hash := 0
	for _, c := range propletID {
		hash = hash*31 + int(c)
	}
	
	for i := 0; i < numSamples; i++ {
		// Deterministic data based on proplet ID and sample index
		seed := (hash + i) % 1000
		sampleData[i] = map[string]interface{}{
			"x": []float64{
				float64(seed%10) / 10.0,
				float64((seed*2)%10) / 10.0,
				float64((seed*3)%10) / 10.0,
			},
			"y": float64(seed % 2),
		}
	}

	return &Dataset{
		Schema:    "fl-demo-dataset-v1",
		PropletID: propletID,
		ClientID:  propletID, // For backward compatibility
		Data:      sampleData,
		Size:      len(sampleData),
	}
}

// saveDatasetAtomic saves a dataset using atomic write (write to temp file then rename)
func saveDatasetAtomic(propletID string, dataset *Dataset) error {
	datasetFile := filepath.Join(store.dataDir, fmt.Sprintf("%s.json", propletID))
	tempFile := datasetFile + ".tmp"

	datasetJSON, err := json.MarshalIndent(dataset, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal dataset: %w", err)
	}

	if err := os.WriteFile(tempFile, datasetJSON, 0644); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	if err := os.Rename(tempFile, datasetFile); err != nil {
		os.Remove(tempFile) // Clean up temp file on error
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	return nil
}

// loadDatasetsFromDisk loads existing datasets from the data directory
func loadDatasetsFromDisk() {
	files, err := filepath.Glob(filepath.Join(store.dataDir, "*.json"))
	if err != nil {
		slog.Warn("Failed to list dataset files", "error", err)
		return
	}

	loaded := 0
	for _, file := range files {
		// Skip temp files
		if strings.HasSuffix(file, ".tmp") {
			continue
		}

		data, err := os.ReadFile(file)
		if err != nil {
			slog.Warn("Failed to read dataset file", "file", file, "error", err)
			continue
		}

		var dataset Dataset
		if err := json.Unmarshal(data, &dataset); err != nil {
			slog.Warn("Failed to parse dataset file", "file", file, "error", err)
			continue
		}

		// Extract proplet ID from filename if not in dataset
		if dataset.PropletID == "" && dataset.ClientID == "" {
			base := filepath.Base(file)
			propletID := strings.TrimSuffix(base, ".json")
			dataset.PropletID = propletID
			dataset.ClientID = propletID
		} else if dataset.PropletID == "" {
			dataset.PropletID = dataset.ClientID
		} else if dataset.ClientID == "" {
			dataset.ClientID = dataset.PropletID
		}

		propletID := dataset.PropletID
		if propletID == "" {
			continue
		}

		store.mu.Lock()
		store.datasets[propletID] = &dataset
		store.mu.Unlock()
		loaded++
	}

	if loaded > 0 {
		slog.Info("Loaded datasets from disk", "count", loaded)
	}
}

// seedDatasetsForParticipants seeds datasets for all participant UUIDs
func seedDatasetsForParticipants() {
	participantUUIDs := getParticipantUUIDs()
	if len(participantUUIDs) == 0 {
		slog.Info("No participant UUIDs found in environment, skipping dataset seeding")
		return
	}

	seeded := 0
	for _, propletID := range participantUUIDs {
		// Check if dataset already exists
		store.mu.RLock()
		_, exists := store.datasets[propletID]
		store.mu.RUnlock()

		if exists {
			slog.Info("Dataset already exists, skipping", "proplet_id", propletID)
			continue
		}

		// Generate dataset (64 samples for fast training)
		dataset := generateDataset(propletID, 64)

		// Save to memory
		store.mu.Lock()
		store.datasets[propletID] = dataset
		store.mu.Unlock()

		// Save to disk using atomic write
		if err := saveDatasetAtomic(propletID, dataset); err != nil {
			slog.Error("Failed to save dataset", "proplet_id", propletID, "error", err)
			continue
		}

		datasetPath := filepath.Join(store.dataDir, fmt.Sprintf("%s.json", propletID))
		slog.Info("Dataset seeded", "proplet_id", propletID, "path", datasetPath, "num_samples", dataset.Size)
		seeded++
	}

	if seeded > 0 {
		slog.Info("Seeded datasets for participants", "count", seeded)
	}
}
