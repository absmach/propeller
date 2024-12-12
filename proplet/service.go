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
	"github.com/tetratelabs/wazero"
	wazeroapi "github.com/tetratelabs/wazero/api"
)

const (
	filePermissions  = 0o644
	pollingInterval  = 5 * time.Second
	chunkWaitTimeout = 10 * time.Minute
)

type PropletService struct {
	config        Config
	mqttService   *MQTTService
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
	mqttService, err := NewMQTTService(ctx, cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize MQTT client: %w", err)
	}

	return &PropletService{
		config:        cfg,
		mqttService:   mqttService,
		runtime:       NewWazeroRuntime(ctx),
		wasmBinary:    wasmBinary,
		chunks:        make(map[string][][]byte),
		chunkMetadata: make(map[string]*ChunkPayload),
	}, nil
}

func NewWazeroRuntime(ctx context.Context) *WazeroRuntime {
	return &WazeroRuntime{
		runtime: wazero.NewRuntime(ctx),
		modules: make(map[string]wazeroapi.Module),
	}
}

func (p *PropletService) Run(ctx context.Context, logger *slog.Logger) error {
	if err := p.mqttService.SubscribeToManagerTopics(ctx,
		func(topic string, msg map[string]interface{}) error {
			return p.handleStartCommand(ctx, topic, msg, logger)
		},
		func(topic string, msg map[string]interface{}) error {
			return p.handleStopCommand(ctx, topic, msg, logger)
		},
		func(topic string, msg map[string]interface{}) error {
			return p.registryUpdate(ctx, topic, msg, logger)
		},
	); err != nil {
		return fmt.Errorf("failed to subscribe to Manager topics: %w", err)
	}

	if err := p.mqttService.SubscribeToRegistryTopic(ctx, func(topic string, msg map[string]interface{}) error {
		return p.handleChunk(ctx, topic, msg)
	}); err != nil {
		return fmt.Errorf("failed to subscribe to registry topic: %w", err)
	}

	logger.Info("Proplet service is running.")
	<-ctx.Done()

	return nil
}

func (p *PropletService) handleStartCommand(ctx context.Context, _ string, msg map[string]interface{}, logger *slog.Logger) error {
	var req propletapi.StartRequest
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to serialize message payload: %w", err)
	}
	if err := json.Unmarshal(data, &req); err != nil {
		return fmt.Errorf("invalid start command payload: %w", err)
	}

	logger.Info("Received start command", slog.String("app_name", req.AppName))

	if p.wasmBinary != nil {
		logger.Info("Using preloaded WASM binary", slog.String("app_name", req.AppName))
		function, err := p.runtime.StartApp(ctx, req.AppName, p.wasmBinary, "main")
		if err != nil {
			return fmt.Errorf("failed to start app '%s': %w", req.AppName, err)
		}

		_, err = function.Call(ctx)
		if err != nil {
			return fmt.Errorf("error executing app '%s': %w", req.AppName, err)
		}

		return nil
	}

	if p.config.RegistryURL == "" {
		logger.Warn("Registry URL is empty, and no binary provided", slog.String("app_name", req.AppName))

		return nil
	}

	if err := p.mqttService.PublishFetchRequest(ctx, req.AppName); err != nil {
		return fmt.Errorf("failed to publish fetch request for app '%s': %w", req.AppName, err)
	}

	logger.Info("Waiting for chunks", slog.String("app_name", req.AppName))
	timeout := time.After(chunkWaitTimeout)

	for {
		select {
		case <-timeout:
			return fmt.Errorf("timed out waiting for chunks for app '%s'", req.AppName)
		default:
			p.chunksMutex.Lock()
			metadata, exists := p.chunkMetadata[req.AppName]
			receivedChunks := len(p.chunks[req.AppName])
			p.chunksMutex.Unlock()

			if exists && receivedChunks == metadata.TotalChunks {
				go p.deployAndRunApp(ctx, req.AppName)

				return nil
			}

			time.Sleep(pollingInterval)
		}
	}
}

func (p *PropletService) handleStopCommand(ctx context.Context, _ string, msg map[string]interface{}, logger *slog.Logger) error {
	var req propletapi.StopRequest
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to serialize message payload: %w", err)
	}

	if err := json.Unmarshal(data, &req); err != nil {
		return fmt.Errorf("invalid stop command payload: %w", err)
	}

	logger.Info("Received stop command", slog.String("app_name", req.AppName))

	err = p.runtime.StopApp(ctx, req.AppName)
	if err != nil {
		return fmt.Errorf("failed to stop app '%s': %w", req.AppName, err)
	}

	logger.Info("App stopped successfully", slog.String("app_name", req.AppName))

	return nil
}

func (p *PropletService) handleChunk(ctx context.Context, _ string, msg map[string]interface{}) error {
	var chunk ChunkPayload
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to serialize chunk payload: %w", err)
	}

	if err := json.Unmarshal(data, &chunk); err != nil {
		return fmt.Errorf("failed to unmarshal chunk payload: %w", err)
	}

	if err := chunk.Validate(); err != nil {
		return fmt.Errorf("invalid chunk payload: %w", err)
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

func (p *PropletService) registryUpdate(ctx context.Context, _ string, msg map[string]interface{}, _ *slog.Logger) error {
	var payload struct {
		RegistryURL   string `json:"registry_url"`
		RegistryToken string `json:"registry_token"`
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to serialize registry update payload: %w", err)
	}

	if err := json.Unmarshal(data, &payload); err != nil {
		return fmt.Errorf("invalid registry update payload: %w", err)
	}

	ackTopic := fmt.Sprintf(RegistryUpdateResponseTopic, p.config.ChannelID)
	if err := p.UpdateRegistry(ctx, payload.RegistryURL, payload.RegistryToken); err != nil {
		if pubErr := p.mqttService.pubsub.Publish(ctx, ackTopic, fmt.Sprintf(RegistryFailurePayload, err)); pubErr != nil {
			return fmt.Errorf("failed to publish registry update failure acknowledgment on topic '%s': %w", ackTopic, pubErr)
		}

		return fmt.Errorf("failed to update registry configuration on topic '%s' with registry URL '%s': %w", ackTopic, payload.RegistryURL, err)
	}

	if pubErr := p.mqttService.pubsub.Publish(ctx, ackTopic, RegistrySuccessPayload); pubErr != nil {
		return fmt.Errorf("failed to publish registry update success acknowledgment on topic '%s': %w", ackTopic, pubErr)
	}

	return nil
}
