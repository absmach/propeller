package main

import (
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
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
	ForwardedAt  string                 `json:"forwarded_at,omitempty"`
}

type Model struct {
	W       []float64 `json:"w"`
	B       float64   `json:"b"`
	Version int       `json:"version"`
}

var (
	rounds       = make(map[string]*RoundState)
	roundsMu     sync.RWMutex
	modelVersion = 0
	modelMu      sync.Mutex
	mqttClient   mqtt.Client
)

func main() {
	// MQTT connection options
	broker := "tcp://localhost:1883"
	if b := os.Getenv("MQTT_BROKER"); b != "" {
		broker = b
	}

	opts := mqtt.NewClientOptions()
	opts.AddBroker(broker)
	opts.SetClientID("fml-coordinator")
	opts.SetAutoReconnect(true)
	opts.SetConnectRetry(true)
	opts.SetConnectRetryInterval(5 * time.Second)

	client := mqtt.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		log.Fatalf("Failed to connect to MQTT: %v", token.Error())
	}
	defer client.Disconnect(250)

	mqttClient = client
	slog.Info("FML Coordinator connected to MQTT broker")

	// Subscribe to FML updates
	if token := client.Subscribe("fml/updates", 0, handleUpdate); token.Wait() && token.Error() != nil {
		log.Fatalf("Failed to subscribe to fml/updates: %v", token.Error())
	}
	slog.Info("Subscribed to fml/updates")

	// Subscribe to round start messages (to initialize round state)
	if token := client.Subscribe("fl/rounds/start", 0, handleRoundStart); token.Wait() && token.Error() != nil {
		log.Fatalf("Failed to subscribe to fl/rounds/start: %v", token.Error())
	}
	slog.Info("Subscribed to fl/rounds/start")

	// Start round timeout checker
	go checkRoundTimeouts()

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	slog.Info("Shutting down FML Coordinator")
}

func handleRoundStart(client mqtt.Client, msg mqtt.Message) {
	var roundStart map[string]interface{}
	if err := json.Unmarshal(msg.Payload(), &roundStart); err != nil {
		slog.Error("Failed to parse round start message", "error", err)
		return
	}

	roundID, ok := roundStart["round_id"].(string)
	if !ok || roundID == "" {
		slog.Warn("Round start message missing round_id, ignoring")
		return
	}

	kOfN := 3
	if k, ok := roundStart["k_of_n"].(float64); ok {
		kOfN = int(k)
	}

	timeoutS := 30
	if t, ok := roundStart["timeout_s"].(float64); ok {
		timeoutS = int(t)
	}

	modelURI, _ := roundStart["model_uri"].(string)

	roundsMu.Lock()
	defer roundsMu.Unlock()

	if _, exists := rounds[roundID]; exists {
		slog.Warn("Round already exists, ignoring start message", "round_id", roundID)
		return
	}

	rounds[roundID] = &RoundState{
		RoundID:   roundID,
		ModelURI:  modelURI,
		KOfN:      kOfN,
		TimeoutS:  timeoutS,
		StartTime: time.Now(),
		Updates:   make([]Update, 0),
		Completed: false,
	}

	slog.Info("Initialized round state", "round_id", roundID, "k_of_n", kOfN, "timeout_s", timeoutS)
}

func handleUpdate(client mqtt.Client, msg mqtt.Message) {
	var update Update
	if err := json.Unmarshal(msg.Payload(), &update); err != nil {
		slog.Error("Failed to parse update", "error", err)
		return
	}

	roundID := update.RoundID
	if roundID == "" {
		slog.Warn("Update missing round_id, ignoring")
		return
	}

	roundsMu.RLock()
	round, exists := rounds[roundID]
	roundsMu.RUnlock()

	if !exists {
		slog.Warn("Received update for unknown round, ignoring", "round_id", roundID)
		return
	}

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

	slog.Info("Aggregating updates", "round_id", round.RoundID, "num_updates", len(updates))

	// Perform FedAvg-like weighted aggregation
	// For this demo, we assume updates have structure: {"w": [float64...], "b": float64}
	var aggregatedW []float64
	var aggregatedB float64
	var totalSamples int

	// Initialize from first update
	if len(updates) > 0 && updates[0].Update != nil {
		if w, ok := updates[0].Update["w"].([]interface{}); ok {
			aggregatedW = make([]float64, len(w))
			for i, v := range w {
				if f, ok := v.(float64); ok {
					aggregatedW[i] = 0
				}
			}
		}
	}

	// Weighted average
	for _, update := range updates {
		if update.Update == nil {
			continue
		}

		weight := float64(update.NumSamples)
		totalSamples += update.NumSamples

		// Aggregate weights
		if w, ok := update.Update["w"].([]interface{}); ok {
			for i, v := range w {
				if f, ok := v.(float64); ok {
					if i < len(aggregatedW) {
						aggregatedW[i] += f * weight
					}
				}
			}
		}

		// Aggregate bias
		if b, ok := update.Update["b"].(float64); ok {
			aggregatedB += b * weight
		}
	}

	// Normalize by total samples
	if totalSamples > 0 {
		weightNorm := float64(totalSamples)
		for i := range aggregatedW {
			aggregatedW[i] /= weightNorm
		}
		aggregatedB /= weightNorm
	}

	// Increment model version
	modelMu.Lock()
	modelVersion++
	newVersion := modelVersion
	modelMu.Unlock()

	// Create new global model
	newModel := Model{
		W:       aggregatedW,
		B:       aggregatedB,
		Version: newVersion,
	}

	// Save model to file
	modelsDir := "/tmp/fl-models"
	if dir := os.Getenv("MODELS_DIR"); dir != "" {
		modelsDir = dir
	}

	modelFile := fmt.Sprintf("%s/global_model_v%d.json", modelsDir, newVersion)
	if err := os.MkdirAll(modelsDir, 0755); err != nil {
		slog.Error("Failed to create models directory", "error", err)
		return
	}

	modelJSON, err := json.MarshalIndent(newModel, "", "  ")
	if err != nil {
		slog.Error("Failed to marshal model", "error", err)
		return
	}

	if err := os.WriteFile(modelFile, modelJSON, 0644); err != nil {
		slog.Error("Failed to write model file", "error", err, "file", modelFile)
		return
	}

	slog.Info("Aggregated model saved", "round_id", round.RoundID, "version", newVersion, "file", modelFile)

	// Publish model to MQTT (model server will pick it up and republish)
	if mqttClient != nil {
		if token := mqttClient.Publish("fl/models/publish", 0, false, modelJSON); token.Wait() && token.Error() != nil {
			slog.Warn("Failed to publish model to model server", "error", token.Error())
		} else {
			slog.Info("Published model to model server", "version", newVersion)
		}
	}

	// Publish round completion
	completionMsg := map[string]interface{}{
		"round_id":      round.RoundID,
		"model_version": newVersion,
		"model_topic":   fmt.Sprintf("fl/models/global_model_v%d", newVersion),
		"num_updates":   len(updates),
		"total_samples": totalSamples,
		"completed_at":  time.Now().UTC().Format(time.RFC3339),
	}

	completionJSON, _ := json.Marshal(completionMsg)
	topic := fmt.Sprintf("fl/rounds/%s/complete", round.RoundID)

	if mqttClient != nil {
		if token := mqttClient.Publish(topic, 0, false, completionJSON); token.Wait() && token.Error() != nil {
			slog.Error("Failed to publish round completion", "round_id", round.RoundID, "error", token.Error())
		} else {
			slog.Info("Published round completion", "round_id", round.RoundID, "topic", topic)
		}
	}
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
