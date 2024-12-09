package proplet

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type PropletService struct {
	config      *Config
	mqttClient  mqtt.Client
	runtime     *WazeroRuntime
	chunks      map[string][][]byte
	chunksMutex sync.Mutex
}

// ChunkPayload represents a single chunk of a Wasm binary.
type ChunkPayload struct {
	AppName     string `json:"app_name"`
	ChunkIdx    int    `json:"chunk_idx"`
	TotalChunks int    `json:"total_chunks"`
	Data        []byte `json:"data"`
}

// NewPropletService initializes the Proplet service.
func NewPropletService(ctx context.Context, config *Config) (*PropletService, error) {
	mqttClient, err := NewMQTTClient(config)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize MQTT client: %w", err)
	}

	runtime := NewWazeroRuntime(ctx)
	return &PropletService{
		config:     config,
		mqttClient: mqttClient,
		runtime:    runtime,
		chunks:     make(map[string][][]byte),
	}, nil
}

// Run starts the PropletService and subscribes to relevant topics.
func (p *PropletService) Run(ctx context.Context) error {
	err := SubscribeToTopics(p.mqttClient, p.config, p.handleCommand, p.handleChunk)
	if err != nil {
		return fmt.Errorf("failed to subscribe to MQTT topics: %w", err)
	}

	fmt.Println("Proplet service is running.")
	<-ctx.Done()
	return nil
}

// handleCommand processes commands from the Manager.
func (p *PropletService) handleCommand(client mqtt.Client, msg mqtt.Message) {
	var req RPCRequest
	if err := json.Unmarshal(msg.Payload(), &req); err != nil {
		fmt.Printf("Invalid command payload: %v\n", err)
		return
	}

	switch req.Method {
	case "start":
		params, _ := req.ParseParams()
		startReq := params.(StartRequest)
		go p.handleStart(startReq)
	case "stop":
		params, _ := req.ParseParams()
		stopReq := params.(StopRequest)
		go p.handleStop(stopReq)
	default:
		fmt.Printf("Unknown command: %s\n", req.Method)
	}
}

func (p *PropletService) handleStart(req StartRequest) {
	fmt.Printf("Received start command for app '%s'\n", req.AppName)

	// Publish fetch request to the Registry Proxy
	err := PublishFetchRequest(p.mqttClient, p.config.ChannelID, req.AppName)
	if err != nil {
		fmt.Printf("Failed to publish fetch request: %v\n", err)
		return
	}
}

// handleChunk processes Wasm chunks from the Registry Proxy.
func (p *PropletService) handleChunk(client mqtt.Client, msg mqtt.Message) {
	var chunk ChunkPayload
	if err := json.Unmarshal(msg.Payload(), &chunk); err != nil {
		fmt.Printf("Failed to unmarshal chunk payload: %v\n", err)
		return
	}

	// Validate the chunk payload
	if err := chunk.Validate(); err != nil {
		fmt.Printf("Invalid chunk payload: %v\n", err)
		return
	}

	// Safely append the chunk data
	p.chunksMutex.Lock()
	p.chunks[chunk.AppName] = append(p.chunks[chunk.AppName], chunk.Data)
	p.chunksMutex.Unlock()

	fmt.Printf("Received chunk %d/%d for app '%s'\n", chunk.ChunkIdx+1, chunk.TotalChunks, chunk.AppName)

	// Check if all chunks are received
	p.chunksMutex.Lock()
	if len(p.chunks[chunk.AppName]) == chunk.TotalChunks {
		p.chunksMutex.Unlock()
		go p.deployAndRunApp(chunk.AppName)
	} else {
		p.chunksMutex.Unlock()
	}
}

// deployAndRunApp deploys and starts the Wasm app.
func (p *PropletService) deployAndRunApp(appName string) {
	fmt.Printf("Assembling chunks for app '%s'\n", appName)

	// Safely retrieve and delete chunks
	p.chunksMutex.Lock()
	chunks := p.chunks[appName]
	delete(p.chunks, appName) // Clean up after deployment
	p.chunksMutex.Unlock()

	// Assemble Wasm binary
	wasmBinary := assembleChunks(chunks)

	// Deploy and start the app
	function, err := p.runtime.StartApp(context.Background(), appName, wasmBinary, "main")
	if err != nil {
		fmt.Printf("Failed to start app '%s': %v\n", appName, err)
		return
	}

	_, err = function.Call(context.Background())
	if err != nil {
		fmt.Printf("Failed to execute app '%s': %v\n", appName, err)
		return
	}

	fmt.Printf("App '%s' started successfully\n", appName)
}

// assembleChunks assembles the Wasm binary from chunks.
func assembleChunks(chunks [][]byte) []byte {
	var wasmBinary []byte
	for _, chunk := range chunks {
		wasmBinary = append(wasmBinary, chunk...)
	}
	return wasmBinary
}

// handleStop stops the Wasm app.
func (p *PropletService) handleStop(req StopRequest) {
	err := p.runtime.StopApp(context.Background(), req.AppName)
	if err != nil {
		fmt.Printf("Failed to stop app '%s': %v\n", req.AppName, err)
		return
	}
	fmt.Printf("App '%s' stopped successfully.\n", req.AppName)
}

// handleRegistryChunks processes Wasm chunks from the Registry Proxy.
func (p *PropletService) handleChunks(_ mqtt.Client, msg mqtt.Message) {
	var chunk ChunkPayload
	if err := json.Unmarshal(msg.Payload(), &chunk); err != nil {
		fmt.Printf("Invalid chunk payload: %v\n", err)
		return
	}
	fmt.Printf("Received chunk for app '%s': %d/%d\n", chunk.AppName, chunk.ChunkIdx+1, chunk.TotalChunks)
}

// Validate checks if the ChunkPayload has all required fields and valid values.
func (c *ChunkPayload) Validate() error {
	if c.AppName == "" {
		return fmt.Errorf("chunk validation: app_name is required but missing")
	}
	if c.ChunkIdx < 0 || c.TotalChunks <= 0 {
		return fmt.Errorf("chunk validation: invalid chunk_idx (%d) or total_chunks (%d)", c.ChunkIdx, c.TotalChunks)
	}
	if len(c.Data) == 0 {
		return fmt.Errorf("chunk validation: data is empty")
	}
	return nil
}
