package proplet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/absmach/propeller/task"
	mqtt "github.com/eclipse/paho.mqtt.golang"
)

const (
	filePermissions = 0o644
	pollingInterval = 5 * time.Second
)

type PropletService struct {
	config        Config
	mqttClient    Client
	chunks        map[string][][]byte
	chunkMetadata map[string]*ChunkPayload
	chunksMutex   sync.Mutex
	runtime       Runtime
}

type ChunkPayload struct {
	AppName     string `json:"app_name"`
	ChunkIdx    int    `json:"chunk_idx"`
	TotalChunks int    `json:"total_chunks"`
	Data        []byte `json:"data"`
}

func NewService(cfg Config, mqttClient Client, logger *slog.Logger, runtime Runtime) (*PropletService, error) {
	return &PropletService{
		config:        cfg,
		mqttClient:    mqttClient,
		chunks:        make(map[string][][]byte),
		chunkMetadata: make(map[string]*ChunkPayload),
		runtime:       runtime,
	}, nil
}

func (p *PropletService) Run(ctx context.Context, logger *slog.Logger) error {
	if err := p.mqttClient.SubscribeToManagerTopics(
		func(client mqtt.Client, msg mqtt.Message) {
			p.handleStartCommand(ctx, client, msg, logger)
		},
		func(client mqtt.Client, msg mqtt.Message) {
			p.handleStopCommand(ctx, client, msg, logger)
		},
		func(client mqtt.Client, msg mqtt.Message) {
			p.registryUpdate(ctx, client, msg, logger)
		},
	); err != nil {
		return fmt.Errorf("failed to subscribe to Manager topics: %w", err)
	}

	if err := p.mqttClient.SubscribeToRegistryTopic(
		func(client mqtt.Client, msg mqtt.Message) {
			p.handleChunk(ctx, client, msg)
		},
	); err != nil {
		return fmt.Errorf("failed to subscribe to Registry topics: %w", err)
	}

	logger.Info("Proplet service is running.")
	<-ctx.Done()

	return nil
}

func (p *PropletService) handleStartCommand(ctx context.Context, _ mqtt.Client, msg mqtt.Message, logger *slog.Logger) {
	var payload task.Task
	if err := json.Unmarshal(msg.Payload(), &payload); err != nil {
		logger.Error("Invalid start command payload", slog.Any("error", err))

		return
	}
	req := startRequest{
		ID:           payload.ID,
		FunctionName: payload.Name,
		WasmFile:     payload.File,
		Params:       payload.Inputs,
	}
	if err := req.Validate(); err != nil {
		logger.Error("Invalid start command payload", slog.Any("error", err))

		return
	}

	logger.Info("Received start command", slog.String("app_name", req.FunctionName))

	if err := p.runtime.StartApp(ctx, req.WasmFile, req.ID, req.FunctionName, req.Params...); err != nil {
		logger.Error("Failed to start app", slog.String("app_name", req.FunctionName), slog.Any("error", err))

		return
	}

	if p.config.RegistryURL != "" {
		err := p.mqttClient.PublishFetchRequest(req.FunctionName)
		if err != nil {
			logger.Error("Failed to publish fetch request", slog.String("app_name", req.FunctionName), slog.Any("error", err))

			return
		}

		go func() {
			logger.Info("Waiting for chunks", slog.String("app_name", req.FunctionName))

			for {
				p.chunksMutex.Lock()
				metadata, exists := p.chunkMetadata[req.FunctionName]
				receivedChunks := len(p.chunks[req.FunctionName])
				p.chunksMutex.Unlock()

				if exists && receivedChunks == metadata.TotalChunks {
					logger.Info("All chunks received, deploying app", slog.String("app_name", req.FunctionName))
					go p.deployAndRunApp(ctx, req.FunctionName)

					break
				}

				time.Sleep(pollingInterval)
			}
		}()
	}
}

func (p *PropletService) handleStopCommand(ctx context.Context, _ mqtt.Client, msg mqtt.Message, logger *slog.Logger) {
	var req stopRequest
	if err := json.Unmarshal(msg.Payload(), &req); err != nil {
		logger.Error("Invalid stop command payload", slog.Any("error", err))

		return
	}
	if err := req.Validate(); err != nil {
		logger.Error("Invalid stop command payload", slog.Any("error", err))

		return
	}

	if err := p.runtime.StopApp(ctx, req.ID); err != nil {
		logger.Error("Failed to stop app", slog.Any("error", err))

		return
	}

	logger.Info("App stopped successfully")
}

func (p *PropletService) handleChunk(ctx context.Context, _ mqtt.Client, msg mqtt.Message) {
	var chunk ChunkPayload
	if err := json.Unmarshal(msg.Payload(), &chunk); err != nil {
		log.Printf("Failed to unmarshal chunk payload: %v", err)

		return
	}

	if err := chunk.Validate(); err != nil {
		log.Printf("Invalid chunk payload: %v\n", err)

		return
	}

	p.chunksMutex.Lock()
	defer p.chunksMutex.Unlock()

	if _, exists := p.chunkMetadata[chunk.AppName]; !exists {
		p.chunkMetadata[chunk.AppName] = &chunk
	}

	p.chunks[chunk.AppName] = append(p.chunks[chunk.AppName], chunk.Data)

	log.Printf("Received chunk %d/%d for app '%s'\n", chunk.ChunkIdx+1, chunk.TotalChunks, chunk.AppName)

	if len(p.chunks[chunk.AppName]) == p.chunkMetadata[chunk.AppName].TotalChunks {
		log.Printf("All chunks received for app '%s'. Deploying...\n", chunk.AppName)
		go p.deployAndRunApp(ctx, chunk.AppName)
	}
}

func (p *PropletService) deployAndRunApp(ctx context.Context, appName string) {
	log.Printf("Assembling chunks for app '%s'\n", appName)

	p.chunksMutex.Lock()
	chunks := p.chunks[appName]
	delete(p.chunks, appName)
	p.chunksMutex.Unlock()

	_ = ctx
	_ = assembleChunks(chunks)

	log.Printf("App '%s' started successfully\n", appName)
}

func assembleChunks(chunks [][]byte) []byte {
	var wasmBinary []byte
	for _, chunk := range chunks {
		wasmBinary = append(wasmBinary, chunk...)
	}

	return wasmBinary
}

func (c *ChunkPayload) Validate() error {
	if c.AppName == "" {
		return errors.New("chunk validation: app_name is required but missing")
	}
	if c.ChunkIdx < 0 || c.TotalChunks <= 0 {
		return fmt.Errorf("chunk validation: invalid chunk_idx (%d) or total_chunks (%d)", c.ChunkIdx, c.TotalChunks)
	}
	if len(c.Data) == 0 {
		return errors.New("chunk validation: data is empty")
	}

	return nil
}

func (p *PropletService) UpdateRegistry(ctx context.Context, registryURL, registryToken string) error {
	if registryURL == "" {
		return errors.New("registry URL cannot be empty")
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

	if err := os.WriteFile("proplet/config.json", configData, filePermissions); err != nil {
		return fmt.Errorf("failed to write updated config to file: %w", err)
	}

	log.Printf("App Registry updated and persisted: %s\n", registryURL)

	return nil
}

func (p *PropletService) registryUpdate(ctx context.Context, client mqtt.Client, msg mqtt.Message, logger *slog.Logger) {
	var payload struct {
		RegistryURL   string `json:"registry_url"`
		RegistryToken string `json:"registry_token"`
	}
	if err := json.Unmarshal(msg.Payload(), &payload); err != nil {
		logger.Error("Invalid registry update payload", slog.Any("error", err))

		return
	}

	ackTopic := fmt.Sprintf(RegistryAckTopicTemplate, p.config.ChannelID)
	if err := p.UpdateRegistry(ctx, payload.RegistryURL, payload.RegistryToken); err != nil {
		client.Publish(ackTopic, 0, false, fmt.Sprintf(RegistryFailurePayload, err.Error()))
		logger.Error("Failed to update registry configuration", slog.String("ack_topic", ackTopic), slog.String("registry_url", payload.RegistryURL), slog.Any("error", err))
	} else {
		client.Publish(ackTopic, 0, false, RegistrySuccessPayload)
		logger.Info("App Registry configuration updated successfully", slog.String("ack_topic", ackTopic), slog.String("registry_url", payload.RegistryURL))
	}
}
