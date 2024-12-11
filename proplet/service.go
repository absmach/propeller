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

	pkgerrors "github.com/absmach/propeller/pkg/errors"
	propletapi "github.com/absmach/propeller/proplet/api"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/tetratelabs/wazero"
	wazeroapi "github.com/tetratelabs/wazero/api"
)

const (
	filePermissions = 0o644
	pollingInterval = 5 * time.Second
)

type PropletService struct {
	config        Config
	mqttClient    mqtt.Client
	runtime       *WazeroRuntime
	wasmBinary    []byte
	chunks        map[string][][]byte
	chunkMetadata map[string]*ChunkPayload
	chunksMutex   sync.Mutex
}
type ChunkPayload struct {
	AppName     string `json:"app_name"`
	ChunkIdx    int    `json:"chunk_idx"`
	TotalChunks int    `json:"total_chunks"`
	Data        []byte `json:"data"`
}

type WazeroRuntime struct {
	runtime wazero.Runtime
	modules map[string]wazeroapi.Module
	mutex   sync.Mutex
}

func NewWazeroRuntime(ctx context.Context) *WazeroRuntime {
	return &WazeroRuntime{
		runtime: wazero.NewRuntime(ctx),
		modules: make(map[string]wazeroapi.Module),
	}
}

func (w *WazeroRuntime) StartApp(ctx context.Context, appName string, wasmBinary []byte, functionName string) (wazeroapi.Function, error) {
	if appName == "" {
		return nil, fmt.Errorf("start app: appName is required but missing: %w", pkgerrors.ErrMissingValue)
	}
	if len(wasmBinary) == 0 {
		return nil, fmt.Errorf("start app: Wasm binary is empty: %w", pkgerrors.ErrInvalidValue)
	}
	if functionName == "" {
		return nil, fmt.Errorf("start app: functionName is required but missing: %w", pkgerrors.ErrMissingValue)
	}

	w.mutex.Lock()
	defer w.mutex.Unlock()

	if _, exists := w.modules[appName]; exists {
		return nil, fmt.Errorf("start app: app '%s' is already running: %w", appName, pkgerrors.ErrAppAlreadyRunning)
	}

	module, err := w.runtime.Instantiate(ctx, wasmBinary)
	if err != nil {
		return nil, fmt.Errorf("start app: failed to instantiate Wasm module for app '%s': %w", appName, pkgerrors.ErrModuleInstantiation)
	}

	function := module.ExportedFunction(functionName)
	if function == nil {
		_ = module.Close(ctx)

		return nil, fmt.Errorf("start app: function '%s' not found in Wasm module for app '%s': %w", functionName, appName, pkgerrors.ErrFunctionNotFound)
	}

	w.modules[appName] = module

	return function, nil
}

func (w *WazeroRuntime) StopApp(ctx context.Context, appName string) error {
	if appName == "" {
		return fmt.Errorf("stop app: appName is required but missing: %w", pkgerrors.ErrMissingValue)
	}

	w.mutex.Lock()
	defer w.mutex.Unlock()

	module, exists := w.modules[appName]
	if !exists {
		return fmt.Errorf("stop app: app '%s' is not running: %w", appName, pkgerrors.ErrAppNotRunning)
	}

	if err := module.Close(ctx); err != nil {
		return fmt.Errorf("stop app: failed to stop app '%s': %w", appName, pkgerrors.ErrModuleStopFailed)
	}

	delete(w.modules, appName)

	return nil
}

func NewService(ctx context.Context, cfg Config, wasmBinary []byte, logger *slog.Logger) (*PropletService, error) {
	mqttClient, err := NewMQTTClient(cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize MQTT client: %w", err)
	}

	runtime := NewWazeroRuntime(ctx)

	return &PropletService{
		config:        cfg,
		mqttClient:    mqttClient,
		runtime:       runtime,
		wasmBinary:    wasmBinary,
		chunks:        make(map[string][][]byte),
		chunkMetadata: make(map[string]*ChunkPayload),
	}, nil
}

func (p *PropletService) Run(ctx context.Context, logger *slog.Logger) error {
	if err := SubscribeToManagerTopics(
		p.mqttClient,
		p.config,
		func(client mqtt.Client, msg mqtt.Message) {
			p.handleStartCommand(ctx, client, msg, logger)
		},
		func(client mqtt.Client, msg mqtt.Message) {
			p.handleStopCommand(ctx, client, msg, logger)
		},
		func(client mqtt.Client, msg mqtt.Message) {
			p.registryUpdate(ctx, client, msg, logger)
		},
		logger,
	); err != nil {
		return fmt.Errorf("failed to subscribe to Manager topics: %w", err)
	}

	if err := SubscribeToRegistryTopic(
		p.mqttClient,
		p.config.ChannelID,
		func(client mqtt.Client, msg mqtt.Message) {
			p.handleChunk(ctx, client, msg)
		},
		logger,
	); err != nil {
		return fmt.Errorf("failed to subscribe to Registry topics: %w", err)
	}

	logger.Info("Proplet service is running.")
	<-ctx.Done()

	return nil
}

func (p *PropletService) handleStartCommand(ctx context.Context, _ mqtt.Client, msg mqtt.Message, logger *slog.Logger) {
	var req propletapi.StartRequest
	if err := json.Unmarshal(msg.Payload(), &req); err != nil {
		logger.Error("Invalid start command payload", slog.Any("error", err))

		return
	}

	logger.Info("Received start command", slog.String("app_name", req.AppName))

	if p.wasmBinary != nil {
		logger.Info("Using preloaded WASM binary", slog.String("app_name", req.AppName))
		function, err := p.runtime.StartApp(ctx, req.AppName, p.wasmBinary, "main")
		if err != nil {
			logger.Error("Failed to start app", slog.String("app_name", req.AppName), slog.Any("error", err))

			return
		}

		_, err = function.Call(ctx)
		if err != nil {
			logger.Error("Error executing app", slog.String("app_name", req.AppName), slog.Any("error", err))
		} else {
			logger.Info("App started successfully", slog.String("app_name", req.AppName))
		}

		return
	}

	if p.config.RegistryURL != "" {
		err := PublishFetchRequest(p.mqttClient, p.config.ChannelID, req.AppName, logger)
		if err != nil {
			logger.Error("Failed to publish fetch request", slog.String("app_name", req.AppName), slog.Any("error", err))

			return
		}

		go func() {
			logger.Info("Waiting for chunks", slog.String("app_name", req.AppName))

			for {
				p.chunksMutex.Lock()
				metadata, exists := p.chunkMetadata[req.AppName]
				receivedChunks := len(p.chunks[req.AppName])
				p.chunksMutex.Unlock()

				if exists && receivedChunks == metadata.TotalChunks {
					logger.Info("All chunks received, deploying app", slog.String("app_name", req.AppName))
					go p.deployAndRunApp(ctx, req.AppName)

					break
				}

				time.Sleep(pollingInterval)
			}
		}()
	} else {
		logger.Warn("Registry URL is empty, and no binary provided", slog.String("app_name", req.AppName))
	}
}

func (p *PropletService) handleStopCommand(ctx context.Context, _ mqtt.Client, msg mqtt.Message, logger *slog.Logger) {
	var req propletapi.StopRequest
	if err := json.Unmarshal(msg.Payload(), &req); err != nil {
		logger.Error("Invalid stop command payload", slog.Any("error", err))

		return
	}

	logger.Info("Received stop command", slog.String("app_name", req.AppName))

	err := p.runtime.StopApp(ctx, req.AppName)
	if err != nil {
		logger.Error("Failed to stop app", slog.String("app_name", req.AppName), slog.Any("error", err))

		return
	}

	logger.Info("App stopped successfully", slog.String("app_name", req.AppName))
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

	wasmBinary := assembleChunks(chunks)

	function, err := p.runtime.StartApp(ctx, appName, wasmBinary, "main")
	if err != nil {
		log.Printf("Failed to start app '%s': %v\n", appName, err)

		return
	}

	_, err = function.Call(ctx)
	if err != nil {
		log.Printf("Failed to execute app '%s': %v\n", appName, err)

		return
	}

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
		client.Publish(ackTopic, 0, false, fmt.Sprintf(RegistryFailurePayload, err))
		logger.Error("Failed to update registry configuration", slog.String("ack_topic", ackTopic), slog.String("registry_url", payload.RegistryURL), slog.Any("error", err))
	} else {
		client.Publish(ackTopic, 0, false, RegistrySuccessPayload)
		logger.Info("App Registry configuration updated successfully", slog.String("ack_topic", ackTopic), slog.String("registry_url", payload.RegistryURL))
	}
}
