package main

import (
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

type AggregateRequest struct {
	Updates []Update `json:"updates"`
}

type Update struct {
	RoundID      string                 `json:"round_id"`
	PropletID    string                 `json:"proplet_id"`
	BaseModelURI string                 `json:"base_model_uri"`
	NumSamples   int                    `json:"num_samples"`
	Metrics      map[string]interface{} `json:"metrics"`
	Update       map[string]interface{} `json:"update"`
}

type AggregatedModel struct {
	W       []float64 `json:"w"`
	B       float64   `json:"b"`
	Version int       `json:"version,omitempty"`
}

func main() {
	port := "8082"
	if p := os.Getenv("AGGREGATOR_PORT"); p != "" {
		port = p
	}

	http.HandleFunc("/health", healthHandler)
	http.HandleFunc("/aggregate", aggregateHandler)

	slog.Info("Aggregator service starting", "port", port)

	srv := &http.Server{
		Addr: ":" + port,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start aggregator: %v", err)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	slog.Info("Shutting down Aggregator")
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func aggregateHandler(w http.ResponseWriter, r *http.Request) {
	var req AggregateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	if len(req.Updates) == 0 {
		http.Error(w, "No updates provided", http.StatusBadRequest)
		return
	}

	slog.Info("Aggregating updates", "num_updates", len(req.Updates))

	var aggregatedW []float64
	var aggregatedB float64
	var totalSamples int

	if len(req.Updates) > 0 && req.Updates[0].Update != nil {
		if w, ok := req.Updates[0].Update["w"].([]interface{}); ok {
			aggregatedW = make([]float64, len(w))
			for i := range w {
				aggregatedW[i] = 0
			}
		}
	}

	for _, update := range req.Updates {
		if update.Update == nil {
			continue
		}

		weight := float64(update.NumSamples)
		totalSamples += update.NumSamples

		if w, ok := update.Update["w"].([]interface{}); ok {
			for i, v := range w {
				if f, ok := v.(float64); ok {
					if i < len(aggregatedW) {
						aggregatedW[i] += f * weight
					}
				}
			}
		}

		if b, ok := update.Update["b"].(float64); ok {
			aggregatedB += b * weight
		}
	}

	if totalSamples > 0 {
		weightNorm := float64(totalSamples)
		for i := range aggregatedW {
			aggregatedW[i] /= weightNorm
		}
		aggregatedB /= weightNorm
	}

	model := AggregatedModel{
		W: aggregatedW,
		B: aggregatedB,
	}

	slog.Info("Aggregation complete", "total_samples", totalSamples)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(model)
}
