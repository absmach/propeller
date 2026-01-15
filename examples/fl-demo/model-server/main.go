package main

import (
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type Model struct {
	W       []float64 `json:"w"`
	B       float64   `json:"b"`
	Version int       `json:"version"`
}

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
		defaultModel := Model{
			W:       []float64{0.0, 0.0, 0.0},
			B:       0.0,
			Version: 0,
		}
		modelJSON, _ := json.MarshalIndent(defaultModel, "", "  ")
		if err := os.WriteFile(defaultModelPath, modelJSON, 0644); err != nil {
			log.Printf("Warning: Failed to create default model: %v", err)
		} else {
			log.Printf("Created default model at %s", defaultModelPath)
		}
	}

	// MQTT connection options
	broker := "tcp://localhost:1883"
	if b := os.Getenv("MQTT_BROKER"); b != "" {
		broker = b
	}

	opts := mqtt.NewClientOptions()
	opts.AddBroker(broker)
	opts.SetClientID("fl-model-server")
	opts.SetAutoReconnect(true)
	opts.SetConnectRetry(true)
	opts.SetConnectRetryInterval(5 * time.Second)

	client := mqtt.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		log.Fatalf("Failed to connect to MQTT: %v", token.Error())
	}
	defer client.Disconnect(250)

	slog.Info("Model server connected to MQTT broker", "broker", broker)

	// Publish default model if it exists (after client is connected)
	if data, err := os.ReadFile(defaultModelPath); err == nil {
		var defaultModel Model
		if err := json.Unmarshal(data, &defaultModel); err == nil {
			publishModel(client, defaultModel, 0)
		}
	}

	// Subscribe to model publish requests (for coordinator to publish new models)
	handlePublish := func(c mqtt.Client, msg mqtt.Message) {
		handleModelPublish(c, msg, client, modelsDir)
	}
	if token := client.Subscribe("fl/models/publish", 0, handlePublish); token.Wait() && token.Error() != nil {
		log.Fatalf("Failed to subscribe to fl/models/publish: %v", token.Error())
	}
	slog.Info("Subscribed to fl/models/publish")

	// Watch for new model files and publish them
	go watchAndPublishModels(client, modelsDir)

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	slog.Info("Shutting down model server")
}

func handleModelPublish(_ mqtt.Client, msg mqtt.Message, client mqtt.Client, modelsDir string) {
	var model Model
	if err := json.Unmarshal(msg.Payload(), &model); err != nil {
		slog.Error("Failed to parse model", "error", err)
		return
	}

	modelPath := filepath.Join(modelsDir, fmt.Sprintf("global_model_v%d.json", model.Version))
	modelJSON, err := json.MarshalIndent(model, "", "  ")
	if err != nil {
		slog.Error("Failed to marshal model", "error", err)
		return
	}

	if err := os.WriteFile(modelPath, modelJSON, 0644); err != nil {
		slog.Error("Failed to write model file", "error", err, "path", modelPath)
		return
	}

	slog.Info("Saved model from publish request", "version", model.Version, "path", modelPath)

	// Publish model to MQTT topic (retained message so clients can get it immediately)
	topic := fmt.Sprintf("fl/models/global_model_v%d", model.Version)
	modelJSONCompact, _ := json.Marshal(model)
	if token := client.Publish(topic, 0, true, modelJSONCompact); token.Wait() && token.Error() != nil {
		slog.Error("Failed to publish model", "error", token.Error(), "topic", topic)
		return
	}

	slog.Info("Published model", "version", model.Version, "topic", topic)
}

func publishModel(client mqtt.Client, model Model, version int) {
	modelJSON, err := json.Marshal(model)
	if err != nil {
		slog.Error("Failed to marshal model for publishing", "error", err)
		return
	}

	topic := fmt.Sprintf("fl/models/global_model_v%d", version)
	// Use retained message (QoS 0, retained=true) so clients can get it immediately when subscribing
	if token := client.Publish(topic, 0, true, modelJSON); token.Wait() && token.Error() != nil {
		slog.Error("Failed to publish model", "error", token.Error(), "topic", topic)
		return
	}

	slog.Info("Published model", "version", version, "topic", topic)
}

func watchAndPublishModels(client mqtt.Client, modelsDir string) {
	// Simple polling approach - check for new models every few seconds
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	lastVersion := -1

	for range ticker.C {
		// Find latest model version
		entries, err := os.ReadDir(modelsDir)
		if err != nil {
			continue
		}

		maxVersion := -1
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			var version int
			if _, err := fmt.Sscanf(entry.Name(), "global_model_v%d.json", &version); err == nil {
				if version > maxVersion {
					maxVersion = version
				}
			}
		}

		// If new model found, publish it
		if maxVersion > lastVersion {
			modelPath := filepath.Join(modelsDir, fmt.Sprintf("global_model_v%d.json", maxVersion))
			data, err := os.ReadFile(modelPath)
			if err != nil {
				continue
			}

			var model Model
			if err := json.Unmarshal(data, &model); err != nil {
				continue
			}

			modelJSON, _ := json.Marshal(model)
			topic := fmt.Sprintf("fl/models/global_model_v%d", maxVersion)
			// Use retained message so clients can get it immediately when subscribing
			if token := client.Publish(topic, 0, true, modelJSON); token.Wait() && token.Error() == nil {
				slog.Info("Published new model", "version", maxVersion, "topic", topic)
				lastVersion = maxVersion
			}
		}
	}
}
