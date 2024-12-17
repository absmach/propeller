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

	pkgmqtt "github.com/absmach/propeller/pkg/mqtt"
	"github.com/absmach/propeller/task"
)

const (
	filePermissions = 0o644
	pollingInterval = 5 * time.Second
)

var (
	RegistryAckTopicTemplate    = "channels/%s/messages/control/manager/registry"
	updateRegistryTopicTemplate = "channels/%s/messages/control/manager/update"
	aliveTopicTemplate          = "channels/%s/messages/control/proplet/alive"
	discoveryTopicTemplate      = "channels/%s/messages/control/proplet/create"
	startTopicTemplate          = "channels/%s/messages/control/manager/start"
	stopTopicTemplate           = "channels/%s/messages/control/manager/stop"
	registryResponseTopic       = "channels/%s/messages/registry/server"
	fetchRequestTopicTemplate   = "channels/%s/messages/registry/proplet"
)

type PropletService struct {
	config        Config
	pubsub        pkgmqtt.PubSub
	chunks        map[string][][]byte
	chunkMetadata map[string]*ChunkPayload
	chunksMutex   sync.Mutex
	runtime       Runtime
	logger        *slog.Logger
}

type ChunkPayload struct {
	AppName     string `json:"app_name"`
	ChunkIdx    int    `json:"chunk_idx"`
	TotalChunks int    `json:"total_chunks"`
	Data        []byte `json:"data"`
}

func NewService(ctx context.Context, cfg Config, pubsub pkgmqtt.PubSub, logger *slog.Logger, runtime Runtime) (*PropletService, error) {
	topic := fmt.Sprintf(discoveryTopicTemplate, cfg.ChannelID)
	payload := map[string]interface{}{
		"proplet_id":    cfg.ThingID,
		"mg_channel_id": cfg.ChannelID,
	}
	if err := pubsub.Publish(ctx, topic, payload); err != nil {
		return nil, errors.Join(errors.New("failed to publish discovery"), err)
	}

	p := &PropletService{
		config:        cfg,
		pubsub:        pubsub,
		chunks:        make(map[string][][]byte),
		chunkMetadata: make(map[string]*ChunkPayload),
		runtime:       runtime,
		logger:        logger,
	}

	go p.startLivelinessUpdates(ctx)

	return p, nil
}

func (p *PropletService) startLivelinessUpdates(ctx context.Context) {
	ticker := time.NewTicker(p.config.LivelinessInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			p.logger.Info("stopping liveliness updates")

			return
		case <-ticker.C:
			topic := fmt.Sprintf(aliveTopicTemplate, p.config.ChannelID)
			payload := map[string]interface{}{
				"status":        "alive",
				"proplet_id":    p.config.ThingID,
				"mg_channel_id": p.config.ChannelID,
			}

			if err := p.pubsub.Publish(ctx, topic, payload); err != nil {
				p.logger.Error("failed to publish liveliness message", slog.Any("error", err))
			}

			p.logger.Debug("Published liveliness message", slog.String("topic", topic))
		}
	}
}

func (p *PropletService) Run(ctx context.Context, logger *slog.Logger) error {
	topic := fmt.Sprintf(startTopicTemplate, p.config.ChannelID)
	if err := p.pubsub.Subscribe(ctx, topic, p.handleStartCommand(ctx)); err != nil {
		return fmt.Errorf("failed to subscribe to start topic: %w", err)
	}

	topic = fmt.Sprintf(stopTopicTemplate, p.config.ChannelID)
	if err := p.pubsub.Subscribe(ctx, topic, p.handleStopCommand(ctx)); err != nil {
		return fmt.Errorf("failed to subscribe to stop topic: %w", err)
	}

	topic = fmt.Sprintf(registryResponseTopic, p.config.ChannelID)
	if err := p.pubsub.Subscribe(ctx, topic, p.handleChunk(ctx)); err != nil {
		return fmt.Errorf("failed to subscribe to registry topics: %w", err)
	}

	topic = fmt.Sprintf(updateRegistryTopicTemplate, p.config.ChannelID)
	if err := p.pubsub.Subscribe(ctx, topic, p.registryUpdate(ctx)); err != nil {
		return fmt.Errorf("failed to subscribe to update registry topic: %w", err)
	}

	logger.Info("Proplet service is running.")
	<-ctx.Done()

	return nil
}

func (p *PropletService) handleStartCommand(ctx context.Context) func(topic string, msg map[string]interface{}) error {
	return func(topic string, msg map[string]interface{}) error {
		data, err := json.Marshal(msg)
		if err != nil {
			return err
		}

		var payload task.Task
		if err := json.Unmarshal(data, &payload); err != nil {
			return err
		}

		req := startRequest{
			ID:           payload.ID,
			FunctionName: payload.Name,
			WasmFile:     payload.File,
			Params:       payload.Inputs,
		}
		if err := req.Validate(); err != nil {
			return err
		}

		p.logger.Info("Received start command", slog.String("app_name", req.FunctionName))

		if err := p.runtime.StartApp(ctx, req.WasmFile, req.ID, req.FunctionName, req.Params...); err != nil {
			return err
		}

		if p.config.RegistryURL != "" {
			payload := map[string]interface{}{
				"app_name": req.FunctionName,
			}
			topic := fmt.Sprintf(fetchRequestTopicTemplate, p.config.ChannelID)
			if err := p.pubsub.Publish(ctx, topic, payload); err != nil {
				return err
			}

			go func() {
				p.logger.Info("Waiting for chunks", slog.String("app_name", req.FunctionName))

				for {
					p.chunksMutex.Lock()
					metadata, exists := p.chunkMetadata[req.FunctionName]
					receivedChunks := len(p.chunks[req.FunctionName])
					p.chunksMutex.Unlock()

					if exists && receivedChunks == metadata.TotalChunks {
						p.logger.Info("All chunks received, deploying app", slog.String("app_name", req.FunctionName))
						go p.deployAndRunApp(ctx, req.FunctionName)

						break
					}

					time.Sleep(pollingInterval)
				}
			}()
		}

		return nil
	}
}

func (p *PropletService) handleStopCommand(ctx context.Context) func(topic string, msg map[string]interface{}) error {
	return func(topic string, msg map[string]interface{}) error {
		data, err := json.Marshal(msg)
		if err != nil {
			return err
		}

		var req stopRequest
		if err := json.Unmarshal(data, &req); err != nil {
			return err
		}

		if err := req.Validate(); err != nil {
			return err
		}

		if err := p.runtime.StopApp(ctx, req.ID); err != nil {
			return err
		}

		return nil
	}
}

func (p *PropletService) handleChunk(ctx context.Context) func(topic string, msg map[string]interface{}) error {
	return func(topic string, msg map[string]interface{}) error {
		data, err := json.Marshal(msg)
		if err != nil {
			return err
		}

		var chunk ChunkPayload
		if err := json.Unmarshal(data, &chunk); err != nil {
			return err
		}

		if err := chunk.Validate(); err != nil {
			return err
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

		return nil
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

func (p *PropletService) registryUpdate(ctx context.Context) func(topic string, msg map[string]interface{}) error {
	return func(topic string, msg map[string]interface{}) error {
		data, err := json.Marshal(msg)
		if err != nil {
			return err
		}

		var payload struct {
			RegistryURL   string `json:"registry_url"`
			RegistryToken string `json:"registry_token"`
		}
		if err := json.Unmarshal(data, &payload); err != nil {
			return err
		}

		ackTopic := fmt.Sprintf(RegistryAckTopicTemplate, p.config.ChannelID)

		if err := p.UpdateRegistry(ctx, payload.RegistryURL, payload.RegistryToken); err != nil {
			if err := p.pubsub.Publish(ctx, ackTopic, map[string]interface{}{"status": "failure", "error": err.Error()}); err != nil {
				p.logger.Error("Failed to publish ack message", slog.String("ack_topic", ackTopic), slog.Any("error", err))
			}
		} else {
			if err := p.pubsub.Publish(ctx, ackTopic, map[string]interface{}{"status": "success"}); err != nil {
				p.logger.Error("Failed to publish ack message", slog.String("ack_topic", ackTopic), slog.Any("error", err))
			}
			p.logger.Info("App Registry configuration updated successfully", slog.String("ack_topic", ackTopic), slog.String("registry_url", payload.RegistryURL))
		}

		return nil
	}
}
