package proplet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"log/slog"
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
	RegistryAckTopicTemplate  = "channels/%s/messages/control/manager/registry"
	aliveTopicTemplate        = "channels/%s/messages/control/proplet/alive"
	discoveryTopicTemplate    = "channels/%s/messages/control/proplet/create"
	startTopicTemplate        = "channels/%s/messages/control/manager/start"
	stopTopicTemplate         = "channels/%s/messages/control/manager/stop"
	registryResponseTopic     = "channels/%s/messages/registry/server"
	fetchRequestTopicTemplate = "channels/%s/messages/registry/proplet"
)

type PropletService struct {
	channelID          string
	thingID            string
	thingKey           string
	livelinessInterval time.Duration
	pubsub             pkgmqtt.PubSub
	chunks             map[string][][]byte
	chunkMetadata      map[string]*ChunkPayload
	chunksMutex        sync.Mutex
	runtime            Runtime
	logger             *slog.Logger
}

type ChunkPayload struct {
	AppName     string `json:"app_name"`
	ChunkIdx    int    `json:"chunk_idx"`
	TotalChunks int    `json:"total_chunks"`
	Data        []byte `json:"data"`
}

func NewService(ctx context.Context, channelID, thingID, thingKey string, livelinessInterval time.Duration, pubsub pkgmqtt.PubSub, logger *slog.Logger, runtime Runtime) (*PropletService, error) {
	topic := fmt.Sprintf(discoveryTopicTemplate, channelID)
	payload := map[string]interface{}{
		"proplet_id":    thingID,
		"mg_channel_id": channelID,
	}
	if err := pubsub.Publish(ctx, topic, payload); err != nil {
		return nil, errors.Join(errors.New("failed to publish discovery"), err)
	}

	p := &PropletService{
		channelID:          channelID,
		thingID:            thingID,
		thingKey:           thingKey,
		livelinessInterval: livelinessInterval,
		pubsub:             pubsub,
		chunks:             make(map[string][][]byte),
		chunkMetadata:      make(map[string]*ChunkPayload),
		runtime:            runtime,
		logger:             logger,
	}

	go p.startLivelinessUpdates(ctx)

	return p, nil
}

func (p *PropletService) startLivelinessUpdates(ctx context.Context) {
	ticker := time.NewTicker(p.livelinessInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			p.logger.Info("stopping liveliness updates")

			return
		case <-ticker.C:
			topic := fmt.Sprintf(aliveTopicTemplate, p.channelID)
			payload := map[string]interface{}{
				"status":        "alive",
				"proplet_id":    p.thingID,
				"mg_channel_id": p.channelID,
			}

			if err := p.pubsub.Publish(ctx, topic, payload); err != nil {
				p.logger.Error("failed to publish liveliness message", slog.Any("error", err))
			}

			p.logger.Debug("Published liveliness message", slog.String("topic", topic))
		}
	}
}

func (p *PropletService) Run(ctx context.Context, logger *slog.Logger) error {
	topic := fmt.Sprintf(startTopicTemplate, p.channelID)
	if err := p.pubsub.Subscribe(ctx, topic, p.handleStartCommand(ctx)); err != nil {
		return fmt.Errorf("failed to subscribe to start topic: %w", err)
	}

	topic = fmt.Sprintf(stopTopicTemplate, p.channelID)
	if err := p.pubsub.Subscribe(ctx, topic, p.handleStopCommand(ctx)); err != nil {
		return fmt.Errorf("failed to subscribe to stop topic: %w", err)
	}

	topic = fmt.Sprintf(registryResponseTopic, p.channelID)
	if err := p.pubsub.Subscribe(ctx, topic, p.handleChunk(ctx)); err != nil {
		return fmt.Errorf("failed to subscribe to registry topics: %w", err)
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
			imageURL:     payload.ImageURL,
			Params:       payload.Inputs,
		}
		if err := req.Validate(); err != nil {
			return err
		}

		p.logger.Info("Received start command", slog.String("app_name", req.FunctionName))

		if req.WasmFile != nil {
			if err := p.runtime.StartApp(ctx, req.WasmFile, req.ID, req.FunctionName, req.Params...); err != nil {
				return err
			}

			return nil
		}

		pl := map[string]interface{}{
			"app_name": req.imageURL,
		}
		tp := fmt.Sprintf(fetchRequestTopicTemplate, p.channelID)
		if err := p.pubsub.Publish(ctx, tp, pl); err != nil {
			return err
		}

		go func() {
			p.logger.Info("Waiting for chunks", slog.String("app_name", req.imageURL.String()))

			for {
				p.chunksMutex.Lock()
				urlStr := req.imageURL.String()
				metadata, exists := p.chunkMetadata[urlStr]
				receivedChunks := len(p.chunks[urlStr])
				p.chunksMutex.Unlock()

				if exists && receivedChunks == metadata.TotalChunks {
					p.logger.Info("All chunks received, deploying app", slog.String("app_name", urlStr))
					wasmBinary := assembleChunks(p.chunks[urlStr])
					if err := p.runtime.StartApp(ctx, wasmBinary, req.ID, req.FunctionName, req.Params...); err != nil {
						p.logger.Error("Failed to start app", slog.String("app_name", urlStr), slog.Any("error", err))
					}

					break
				}

				time.Sleep(pollingInterval)
			}
		}()

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

func (p *PropletService) handleChunk(_ context.Context) func(topic string, msg map[string]interface{}) error {
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

		return nil
	}
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
