package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
)

func main() {
	modelsDir := "/tmp/fl-models"
	if dir := os.Getenv("MODELS_DIR"); dir != "" {
		modelsDir = dir
	}

	// Create models directory if it doesn't exist
	if err := os.MkdirAll(modelsDir, 0755); err != nil {
		log.Fatalf("Failed to create models directory: %v", err)
	}

	// Initialize with a default model if none exists
	defaultModelPath := filepath.Join(modelsDir, "global_model_v0.json")
	if _, err := os.Stat(defaultModelPath); os.IsNotExist(err) {
		defaultModel := `{
  "w": [0.0, 0.0, 0.0],
  "b": 0.0,
  "version": 0
}`
		if err := os.WriteFile(defaultModelPath, []byte(defaultModel), 0644); err != nil {
			log.Printf("Warning: Failed to create default model: %v", err)
		} else {
			log.Printf("Created default model at %s", defaultModelPath)
		}
	}

	// Serve models directory
	fs := http.FileServer(http.Dir(modelsDir))
	http.Handle("/models/", http.StripPrefix("/models/", fs))

	port := "8080"
	if p := os.Getenv("PORT"); p != "" {
		port = p
	}

	log.Printf("Model server starting on port %s, serving from %s", port, modelsDir)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
