package fl

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
)

type WasmAggregator struct {
	wasmPath string
	runtime  string
}

func NewWasmAggregator(wasmPath, runtime string) (*WasmAggregator, error) {
	if _, err := os.Stat(wasmPath); err != nil {
		return nil, fmt.Errorf("wasm aggregator file not found: %w", err)
	}

	if runtime == "" {
		runtime = "wasmtime"
	}

	return &WasmAggregator{
		wasmPath: wasmPath,
		runtime:  runtime,
	}, nil
}

func (w *WasmAggregator) Aggregate(updates []Update) (Model, error) {
	updatesJSON, err := json.Marshal(updates)
	if err != nil {
		return Model{}, fmt.Errorf("failed to marshal updates: %w", err)
	}

	cmd := exec.Command(w.runtime, "run", w.wasmPath, "--", string(updatesJSON))
	output, err := cmd.Output()
	if err != nil {
		return Model{}, fmt.Errorf("wasm aggregator execution failed: %w", err)
	}

	var model Model
	if err := json.Unmarshal(output, &model); err != nil {
		return Model{}, fmt.Errorf("failed to unmarshal aggregated model: %w", err)
	}

	return model, nil
}
