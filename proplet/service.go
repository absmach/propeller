package proplet

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type PropletService struct {
	config        *Config
	mqttClient    mqtt.Client
	runtime       *WazeroRuntime
	chunks        map[string][][]byte
	chunkMetadata map[string]*ChunkPayload
	chunksMutex   sync.Mutex
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
		config:        config,
		mqttClient:    mqttClient,
		runtime:       runtime,
		chunks:        make(map[string][][]byte),
		chunkMetadata: make(map[string]*ChunkPayload),
	}, nil
}

// Run starts the PropletService and subscribes to relevant topics.
func (p *PropletService) Run(ctx context.Context) error {
	if err := SubscribeToManagerTopics(
		p.mqttClient,
		p.config,
		p.handleStartCommand,
		p.handleStopCommand,
		p.registryUpdate,
	); err != nil {
		return fmt.Errorf("failed to subscribe to Manager topics: %w", err)
	}

	if err := SubscribeToRegistryTopic(
		p.mqttClient,
		p.config.ChannelID,
		p.handleChunk,
	); err != nil {
		return fmt.Errorf("failed to subscribe to Registry topics: %w", err)
	}

	fmt.Println("Proplet service is running.")
	<-ctx.Done()
	return nil
}

func (p *PropletService) handleStartCommand(client mqtt.Client, msg mqtt.Message) {
	var req StartRequest
	if err := json.Unmarshal(msg.Payload(), &req); err != nil {
		fmt.Printf("Invalid start command payload: %v\n", err)
		return
	}

	fmt.Printf("Received start command for app '%s'\n", req.AppName)

	// Publish fetch request to the Registry Proxy
	err := PublishFetchRequest(p.mqttClient, p.config.ChannelID, req.AppName)
	if err != nil {
		fmt.Printf("Failed to publish fetch request: %v\n", err)
		return
	}

	// Wait for chunks to be received and assembled
	go func() {
		fmt.Printf("Waiting for chunks for app '%s'...\n", req.AppName)

		// Poll for chunk completion
		for {
			p.chunksMutex.Lock()
			metadata, exists := p.chunkMetadata[req.AppName]
			receivedChunks := len(p.chunks[req.AppName])
			p.chunksMutex.Unlock()

			if exists && receivedChunks == metadata.TotalChunks {
				fmt.Printf("All chunks received for app '%s'. Deploying...\n", req.AppName)
				go p.deployAndRunApp(req.AppName)
				break
			}

			time.Sleep(1 * time.Second) // Avoid tight polling
		}
	}()
}

// handleStopCommand processes the stop command from the Manager.
func (p *PropletService) handleStopCommand(client mqtt.Client, msg mqtt.Message) {
	var req StopRequest
	if err := json.Unmarshal(msg.Payload(), &req); err != nil {
		fmt.Printf("Invalid stop command payload: %v\n", err)
		return
	}

	fmt.Printf("Received stop command for app '%s'\n", req.AppName)

	err := p.runtime.StopApp(context.Background(), req.AppName)
	if err != nil {
		fmt.Printf("Failed to stop app '%s': %v\n", req.AppName, err)
		return
	}

	fmt.Printf("App '%s' stopped successfully.\n", req.AppName)
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

	// Safely append the chunk data and store metadata
	p.chunksMutex.Lock()
	defer p.chunksMutex.Unlock()

	// Store metadata if this is the first chunk
	if _, exists := p.chunkMetadata[chunk.AppName]; !exists {
		p.chunkMetadata[chunk.AppName] = &chunk
	}

	// Append chunk data
	p.chunks[chunk.AppName] = append(p.chunks[chunk.AppName], chunk.Data)

	fmt.Printf("Received chunk %d/%d for app '%s'\n", chunk.ChunkIdx+1, chunk.TotalChunks, chunk.AppName)

	// Check if all chunks are received
	if len(p.chunks[chunk.AppName]) == p.chunkMetadata[chunk.AppName].TotalChunks {
		fmt.Printf("All chunks received for app '%s'. Deploying...\n", chunk.AppName)
		go p.deployAndRunApp(chunk.AppName)
	}
}

// deployAndRunApp assembles, deploys, and starts the Wasm app.
func (p *PropletService) deployAndRunApp(appName string) {
	fmt.Printf("Assembling chunks for app '%s'\n", appName)

	// Safely retrieve and delete chunks
	p.chunksMutex.Lock()
	chunks := p.chunks[appName]
	delete(p.chunks, appName)
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

func (p *PropletService) UpdateRegistry(ctx context.Context, registryURL, registryToken string) error {
	if registryURL == "" {
		return fmt.Errorf("registry URL cannot be empty")
	}
	if _, err := url.ParseRequestURI(registryURL); err != nil {
		return fmt.Errorf("invalid registry URL '%s': %w", registryURL, err)
	}

	p.config.RegistryURL = registryURL
	p.config.RegistryToken = registryToken

	configData, err := json.MarshalIndent(p.config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize updated config: %w", err)
	}
	if err := os.WriteFile("proplet/config.json", configData, 0644); err != nil {
		return fmt.Errorf("failed to write updated config to file: %w", err)
	}

	fmt.Printf("App Registry updated and persisted: %s\n", registryURL)
	return nil
}
