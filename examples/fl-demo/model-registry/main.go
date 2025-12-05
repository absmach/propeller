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
	"strconv"
	"sync"
	"syscall"

	"github.com/gorilla/mux"
)

type Model struct {
	Version int                    `json:"version"`
	Model   map[string]interface{} `json:"model"`
}

type ModelStore struct {
	models map[int]Model
	mu     sync.RWMutex
	modelsDir string
}

var store = &ModelStore{
	models: make(map[int]Model),
	modelsDir: "/tmp/fl-models",
}

func main() {
	if dir := os.Getenv("MODELS_DIR"); dir != "" {
		store.modelsDir = dir
	}

	if err := os.MkdirAll(store.modelsDir, 0755); err != nil {
		log.Fatalf("Failed to create models directory: %v", err)
	}

	defaultModelPath := filepath.Join(store.modelsDir, "global_model_v0.json")
	if _, err := os.Stat(defaultModelPath); os.IsNotExist(err) {
		defaultModel := map[string]interface{}{
			"w":       []float64{0.0, 0.0, 0.0},
			"b":       0.0,
			"version": 0,
		}
		modelJSON, _ := json.MarshalIndent(defaultModel, "", "  ")
		if err := os.WriteFile(defaultModelPath, modelJSON, 0644); err == nil {
			store.mu.Lock()
			store.models[0] = Model{Version: 0, Model: defaultModel}
			store.mu.Unlock()
			slog.Info("Created default model", "path", defaultModelPath)
		}
	}

	port := "8081"
	if p := os.Getenv("REGISTRY_PORT"); p != "" {
		port = p
	}

	r := mux.NewRouter()
	r.HandleFunc("/health", healthHandler).Methods("GET")
	r.HandleFunc("/models", postModelHandler).Methods("POST")
	r.HandleFunc("/models/{version}", getModelHandler).Methods("GET")
	r.HandleFunc("/models", listModelsHandler).Methods("GET")

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: r,
	}

	go func() {
		slog.Info("Model Registry HTTP server starting", "port", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start HTTP server: %v", err)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	slog.Info("Shutting down Model Registry")
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func postModelHandler(w http.ResponseWriter, r *http.Request) {
	var modelData Model
	if err := json.NewDecoder(r.Body).Decode(&modelData); err != nil {
		http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	store.mu.Lock()
	store.models[modelData.Version] = modelData
	store.mu.Unlock()

	// Save to file
	modelFile := filepath.Join(store.modelsDir, fmt.Sprintf("global_model_v%d.json", modelData.Version))
	modelJSON, err := json.MarshalIndent(modelData.Model, "", "  ")
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to marshal model: %v", err), http.StatusInternalServerError)
		return
	}

	if err := os.WriteFile(modelFile, modelJSON, 0644); err != nil {
		http.Error(w, fmt.Sprintf("Failed to write model file: %v", err), http.StatusInternalServerError)
		return
	}

	slog.Info("Model stored", "version", modelData.Version, "file", modelFile)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"version": modelData.Version,
		"status":  "stored",
	})
}

func getModelHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	versionStr := vars["version"]

	version, err := strconv.Atoi(versionStr)
	if err != nil {
		http.Error(w, "Invalid version", http.StatusBadRequest)
		return
	}

	store.mu.RLock()
	model, exists := store.models[version]
	store.mu.RUnlock()

	if !exists {
		modelFile := filepath.Join(store.modelsDir, fmt.Sprintf("global_model_v%d.json", version))
		data, err := os.ReadFile(modelFile)
		if err != nil {
			http.Error(w, "Model not found", http.StatusNotFound)
			return
		}

		var modelData map[string]interface{}
		if err := json.Unmarshal(data, &modelData); err != nil {
			http.Error(w, "Invalid model file", http.StatusInternalServerError)
			return
		}

		model = Model{Version: version, Model: modelData}
		store.mu.Lock()
		store.models[version] = model
		store.mu.Unlock()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(model.Model)
}

func listModelsHandler(w http.ResponseWriter, r *http.Request) {
	store.mu.RLock()
	versions := make([]int, 0, len(store.models))
	for v := range store.models {
		versions = append(versions, v)
	}
	store.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"versions": versions,
	})
}
