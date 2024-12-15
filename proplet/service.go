package proplet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/absmach/propeller/proplet/api"
	"github.com/tetratelabs/wazero"
	wazeroapi "github.com/tetratelabs/wazero/api"
)

const (
	filePermissions = 0o644
	pollingTick     = 500 * time.Millisecond
	timeout         = 30 * time.Second
)

type ChunkPayload struct {
	AppName     string `json:"app_name"`
	ChunkIdx    int    `json:"chunk_idx"`
	TotalChunks int    `json:"total_chunks"`
	Data        []byte `json:"data"`
}

func NewService(ctx context.Context, cfg Config, wasmFilePath string, logger *slog.Logger) (*PropletService, error) {
	mqttService, err := NewMQTTService(ctx, cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize MQTT client: %w", err)
	}

	return &PropletService{
		config:        cfg,
		mqttService:   mqttService,
		runtime:       NewWazeroRuntime(ctx),
		wasmFilePath:  wasmFilePath,
		chunks:        make(map[string][][]byte),
		chunkMetadata: make(map[string]*ChunkPayload),
	}, nil
}

func NewWazeroRuntime(ctx context.Context) *WazeroRuntime {
	return &WazeroRuntime{
		runtimes: make(map[string]wazero.Runtime),
		modules:  make(map[string]wazeroapi.Module),
	}
}

func (p *PropletService) handleStartCmd(ctx context.Context, _ string, msg map[string]interface{}, logger *slog.Logger) error {
	startReq, err := parseRPCCommand[api.StartRequest](msg)
	if err != nil {
		return err
	}

	logger.Info("Received start command", slog.String("app_name", startReq.AppName))

	if err := p.prepareWASMBinary(ctx, logger, startReq.AppName); err != nil {
		return err
	}

	if err := p.runWASMApp(ctx, startReq.AppName, "", startReq.Params, logger); err != nil {
		return err
	}

	return nil
}

func (p *PropletService) prepareWASMBinary(ctx context.Context, logger *slog.Logger, appName string) error {
	if p.wasmBinary != nil {
		logger.Info("Using preloaded WASM binary", slog.String("app_name", appName))

		return nil
	}

	if p.wasmFilePath != "" {
		if err := p.loadWASMFromFile(logger); err != nil {
			return fmt.Errorf("failed to load WASM file: %w", err)
		}

		return nil
	}

	if p.config.RegistryURL == "" {
		logger.Warn("Registry URL is empty, and no binary provided", slog.String("app_name", appName))

		return nil
	}

	if err := p.mqttService.PublishFetchRequest(ctx, appName); err != nil {
		return fmt.Errorf("failed to publish fetch request for app '%s': %w", appName, err)
	}

	logger.Info("Waiting for chunks", slog.String("app_name", appName))

	if err := p.waitForChunks(ctx, logger, appName); err != nil {
		return err
	}

	logger.Info("WASM binary assembled successfully", slog.String("app_name", appName))

	return nil
}

func (p *PropletService) loadWASMFromFile(logger *slog.Logger) error {
	wasmBytes, err := os.ReadFile(p.wasmFilePath)
	if err != nil {
		return err
	}
	p.wasmBinary = wasmBytes
	logger.Info("WASM file loaded successfully", slog.String("path", p.wasmFilePath))

	return nil
}

func (p *PropletService) waitForChunks(ctx context.Context, _ *slog.Logger, appName string) error {
	timeout := time.After(timeout)
	ticker := time.NewTicker(pollingTick)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("context canceled while waiting for chunks for app '%s'", appName)
		case <-timeout:
			return fmt.Errorf("timed out waiting for chunks for app '%s'", appName)
		case <-ticker.C:
			p.chunksMutex.Lock()
			assembled := p.wasmBinary != nil
			p.chunksMutex.Unlock()

			if assembled {
				return nil
			}
		}
	}
}

func (p *PropletService) runWASMApp(ctx context.Context, appName, functionName string, params []string, logger *slog.Logger) error {
	var err error
	if functionName == "" {
		logger.Info("Retrieving function name from WASM binary", slog.String("app_name", appName))
		functionName, err = p.runtime.GetWASMFunctionName(ctx, p.wasmBinary)
		if err != nil {
			logger.Error("Failed to retrieve function name from WASM binary", slog.String("app_name", appName), slog.Any("error", err))

			return nil
		}
		logger.Info("Retrieved function name successfully", slog.String("app_name", appName), slog.String("function_name", functionName))
	}

	logger.Info("Running WASM app", slog.String("app_name", appName), slog.String("function_name", functionName))

	function, err := p.runtime.StartApp(ctx, appName, p.wasmBinary, functionName)
	if err != nil {
		logger.Error("Failed to start WASM app", slog.String("app_name", appName), slog.String("function_name", functionName), slog.Any("error", err))

		return nil
	}

	var args []uint64
	if len(params) > 0 {
		args = make([]uint64, len(params))
		for i, param := range params {
			arg, err := strconv.ParseUint(param, 10, 64)
			if err != nil {
				logger.Error("Invalid argument for WASM app", slog.String("app_name", appName), slog.String("arg", param), slog.Any("error", err))

				return nil
			}
			args[i] = arg
		}
	}

	result, err := function.Call(ctx, args...)
	if err != nil {
		logger.Error("Failed to execute WASM function", slog.String("app_name", appName), slog.String("function_name", functionName), slog.Any("error", err))

		return nil
	}

	logger.Info("WASM app executed successfully", slog.String("app_name", appName), slog.String("function_name", functionName), slog.Any("result", result))

	return nil
}

func (p *PropletService) handleStopCmd(ctx context.Context, _ string, msg map[string]interface{}, logger *slog.Logger) error {
	stopReq, err := parseRPCCommand[api.StartRequest](msg)
	if err != nil {
		return err
	}

	logger.Info("Received stop command", slog.String("app_name", stopReq.AppName))

	err = p.runtime.StopApp(ctx, stopReq.AppName)
	if err != nil {
		return fmt.Errorf("failed to stop app '%s': %w", stopReq.AppName, err)
	}

	logger.Info("App stopped successfully", slog.String("app_name", stopReq.AppName))

	return nil
}

func (p *PropletService) handleAppChunks(ctx context.Context, _ string, msg map[string]interface{}, logger *slog.Logger) error {
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
		var wasmBinary []byte
		for _, chunk := range p.chunks[chunk.AppName] {
			wasmBinary = append(wasmBinary, chunk...)
		}
		p.wasmBinary = wasmBinary
		delete(p.chunks, chunk.AppName)

		log.Printf("Binary for app '%s' assembled successfully. Ready to deploy.\n", chunk.AppName)

		go func() {
			if err := p.prepareWASMBinary(ctx, logger, chunk.AppName); err != nil {
				logger.Error("Failed to prepare WASM binary", slog.String("app_name", chunk.AppName), slog.Any("error", err))
			}
		}()
	}

	return nil
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

func parseRPCCommand[T any](msg map[string]interface{}) (T, error) {
	data, err := json.Marshal(msg)
	if err != nil {
		return *new(T), fmt.Errorf("failed to serialize message payload: %w", err)
	}

	var rpcReq api.RPCRequest
	if err := json.Unmarshal(data, &rpcReq); err != nil {
		return *new(T), fmt.Errorf("invalid command payload: %w", err)
	}

	parsed, err := rpcReq.ParseParams()
	if err != nil {
		return *new(T), fmt.Errorf("failed to parse command parameters: %w", err)
	}

	cmdParams, ok := parsed.(T)
	if !ok {
		return *new(T), errors.New("unexpected request type for command")
	}

	return cmdParams, nil
}
