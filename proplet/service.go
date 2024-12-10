package proplet

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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
	wasmBinary    []byte
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

// NewService initializes the Proplet service.
func NewService(ctx context.Context, config *Config, wasmBinary []byte, logger *slog.Logger) (*PropletService, error) {
	mqttClient, err := NewMQTTClient(config, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize MQTT client: %w", err)
	}

	runtime := NewWazeroRuntime(ctx)

	return &PropletService{
		config:        config,
		mqttClient:    mqttClient,
		runtime:       runtime,
		wasmBinary:    wasmBinary,
		chunks:        make(map[string][][]byte),
		chunkMetadata: make(map[string]*ChunkPayload),
	}, nil
}

// Run starts the PropletService and subscribes to relevant topics.
func (p *PropletService) Run(ctx context.Context, logger *slog.Logger) error {
	if err := SubscribeToManagerTopics(
		p.mqttClient,
		p.config,
		func(client mqtt.Client, msg mqtt.Message) {
			p.handleStartCommand(client, msg, logger)
		},
		func(client mqtt.Client, msg mqtt.Message) {
			p.handleStopCommand(client, msg, logger)
		},
		func(client mqtt.Client, msg mqtt.Message) {
			p.registryUpdate(client, msg, logger)
		},
		logger,
	); err != nil {
		return fmt.Errorf("failed to subscribe to Manager topics: %w", err)
	}

	if err := SubscribeToRegistryTopic(
		p.mqttClient,
		p.config.ChannelID,
		func(client mqtt.Client, msg mqtt.Message) {
			p.handleChunk(client, msg) // No logger needed for handleChunk
		},
		logger,
	); err != nil {
		return fmt.Errorf("failed to subscribe to Registry topics: %w", err)
	}

	logger.Info("Proplet service is running.")
	<-ctx.Done()
	return nil
}

func (p *PropletService) handleStartCommand(client mqtt.Client, msg mqtt.Message, logger *slog.Logger) {
	var req StartRequest
	if err := json.Unmarshal(msg.Payload(), &req); err != nil {
		logger.Error("Invalid start command payload", slog.Any("error", err))
		return
	}

	logger.Info("Received start command", slog.String("app_name", req.AppName))

	// If WASM binary is preloaded, deploy it
	if p.wasmBinary != nil {
		logger.Info("Using preloaded WASM binary", slog.String("app_name", req.AppName))
		function, err := p.runtime.StartApp(context.Background(), req.AppName, p.wasmBinary, "main")
		if err != nil {
			logger.Error("Failed to start app", slog.String("app_name", req.AppName), slog.Any("error", err))
			return
		}

		_, err = function.Call(context.Background())
		if err != nil {
			logger.Error("Error executing app", slog.String("app_name", req.AppName), slog.Any("error", err))
		} else {
			logger.Info("App started successfully", slog.String("app_name", req.AppName))
		}
		return
	}

	// Publish fetch request to the Registry Proxy only if Registry URL is available
	if p.config.RegistryURL != "" {
		err := PublishFetchRequest(p.mqttClient, p.config.ChannelID, req.AppName, logger)
		if err != nil {
			logger.Error("Failed to publish fetch request", slog.String("app_name", req.AppName), slog.Any("error", err))
			return
		}

		// Wait for chunks to be received and assembled
		go func() {
			logger.Info("Waiting for chunks", slog.String("app_name", req.AppName))

			// Poll for chunk completion
			for {
				p.chunksMutex.Lock()
				metadata, exists := p.chunkMetadata[req.AppName]
				receivedChunks := len(p.chunks[req.AppName])
				p.chunksMutex.Unlock()

				if exists && receivedChunks == metadata.TotalChunks {
					logger.Info("All chunks received, deploying app", slog.String("app_name", req.AppName))
					go p.deployAndRunApp(req.AppName)
					break
				}

				time.Sleep(5 * time.Second) // Avoid tight polling
			}
		}()
	} else {
		logger.Warn("Registry URL is empty, and no binary provided", slog.String("app_name", req.AppName))
	}
}

// handleStopCommand processes the stop command from the Manager.
func (p *PropletService) handleStopCommand(client mqtt.Client, msg mqtt.Message, logger *slog.Logger) {
	var req StopRequest
	if err := json.Unmarshal(msg.Payload(), &req); err != nil {
		logger.Error("Invalid stop command payload", slog.Any("error", err))
		return
	}

	logger.Info("Received stop command", slog.String("app_name", req.AppName))

	err := p.runtime.StopApp(context.Background(), req.AppName)
	if err != nil {
		logger.Error("Failed to stop app", slog.String("app_name", req.AppName), slog.Any("error", err))
		return
	}

	logger.Info("App stopped successfully", slog.String("app_name", req.AppName))
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
