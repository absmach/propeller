package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/absmach/propeller/task"
	"github.com/absmach/propeller/worker"
	"github.com/google/uuid"
)

//go:embed add.wasm
var addWasm []byte

// PropletConfig holds the configuration for creating a Proplet.
type PropletConfig struct {
	BrokerURL string `json:"brokerURL"`
	Token     string `json:"token"`
	ChannelID string `json:"channelID"`
}

var (
	propletCounter int
	counterMutex   sync.Mutex
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	config, err := loadConfig("proplet-config.json")
	if err != nil {
		fmt.Println("Failed to load configuration:", err)
		return
	}

	t := task.Task{
		ID:    uuid.New().String(),
		Name:  "Addition",
		State: task.Pending,
		Function: task.Function{
			File:   addWasm,
			Name:   "add",
			Inputs: []uint64{5, 10},
		},
	}

	fmt.Printf("task: %s\n", t.Name)

	propletName := generatePropletName()
	proplet, err := worker.NewProplet(propletName, config.ChannelID, config.BrokerURL, config.Token)
	if err != nil {
		fmt.Println("Failed to create Proplet:", err)
		return
	}

	w := worker.NewWasmWorker("Wasm-Worker-1")

	w.StartTask(ctx, t)
	results, err := w.RunTask(ctx, t.ID, proplet)
	if err != nil {
		fmt.Println(err)
	}
	fmt.Printf("results: %v\n", results)
}

func generatePropletName() string {
	counterMutex.Lock()
	defer counterMutex.Unlock()
	propletCounter++
	return fmt.Sprintf("Proplet-%d", propletCounter)
}

func loadConfig(filepath string) (*PropletConfig, error) {
	file, err := os.Open(filepath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var config PropletConfig
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&config); err != nil {
		return nil, err
	}

	return &config, nil
}
