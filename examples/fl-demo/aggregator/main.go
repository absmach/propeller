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
		// Debug: log the first update structure
		updateJSON, _ := json.Marshal(req.Updates[0].Update)
		slog.Info("First update structure", "update", string(updateJSON))
		
		if w, ok := req.Updates[0].Update["w"].([]interface{}); ok {
			aggregatedW = make([]float64, len(w))
			for i := range w {
				aggregatedW[i] = 0
			}
			slog.Info("Initialized weights array", "length", len(aggregatedW))
		} else {
			slog.Warn("First update missing 'w' field or wrong type", "update_keys", getKeys(req.Updates[0].Update))
		}
	}

	for i, update := range req.Updates {
		if update.Update == nil {
			slog.Warn("Update is nil", "index", i)
			continue
		}

		weight := float64(update.NumSamples)
		totalSamples += update.NumSamples

		if w, ok := update.Update["w"].([]interface{}); ok {
			slog.Info("Processing weights", "update_index", i, "num_samples", update.NumSamples, "weights", w)
			for j, v := range w {
				if f, ok := v.(float64); ok {
					if j < len(aggregatedW) {
						aggregatedW[j] += f * weight
					}
				}
			}
		} else {
			slog.Warn("Update missing 'w' field", "update_index", i, "update_keys", getKeys(update.Update))
		}

		if b, ok := update.Update["b"].(float64); ok {
			slog.Info("Processing bias", "update_index", i, "bias", b)
			aggregatedB += b * weight
		} else {
			slog.Warn("Update missing 'b' field", "update_index", i)
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

	slog.Info("Aggregation complete", "total_samples", totalSamples, "aggregated_w", aggregatedW, "aggregated_b", aggregatedB)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(model)
}

func getKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
