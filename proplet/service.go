package proplet

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	pkgerrors "github.com/absmach/propeller/pkg/errors"
	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// ChunkPayload represents a single chunk of a Wasm binary.
type ChunkPayload struct {
	AppName     string `json:"appName"`
	ChunkIdx    int    `json:"chunkIdx"`
	TotalChunks int    `json:"totalChunks"`
	Data        []byte `json:"data"`
}

// Validate checks if the ChunkPayload has all required fields and values.
func (c *ChunkPayload) Validate() error {
	if c.AppName == "" {
		return fmt.Errorf("chunk validation: appName is required but missing: %w", pkgerrors.ErrChunkValidationFailed)
	}
	if c.ChunkIdx < 0 || c.TotalChunks <= 0 {
		return fmt.Errorf("chunk validation: invalid chunk index (%d) or totalChunks (%d): %w", c.ChunkIdx, c.TotalChunks, pkgerrors.ErrChunkValidationFailed)
	}
	if len(c.Data) == 0 {
		return fmt.Errorf("chunk validation: chunk data is empty: %w", pkgerrors.ErrChunkValidationFailed)
	}
	return nil
}

// PropletService handles the core functionality of the Proplet.
type PropletService struct {
	config      *Config
	mqttClient  mqtt.Client
	runtime     *WasmRuntime
	appChunks   map[string][]ChunkPayload
	chunksMutex sync.Mutex
}

// NewPropletService creates a new instance of the PropletService.
func NewPropletService(ctx context.Context, config *Config) (*PropletService, error) {
	mqttClient, err := NewMQTTClient(config)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize MQTT client: %w", err)
	}

	runtime := NewWasmRuntime(ctx)
	return &PropletService{
		config:      config,
		mqttClient:  mqttClient,
		runtime:     runtime,
		appChunks:   make(map[string][]ChunkPayload),
		chunksMutex: sync.Mutex{},
	}, nil
}

// Run starts the PropletService and listens for commands and Wasm artifacts.
func (p *PropletService) Run(ctx context.Context) error {
	// Publish discovery and set LWT
	PublishDiscovery(p.mqttClient, p.config)

	// Subscribe to control topic
	controlTopic := fmt.Sprintf("channels/%s/messages/control", p.config.ChannelID)
	if token := p.mqttClient.Subscribe(controlTopic, 0, func(client mqtt.Client, msg mqtt.Message) {
		p.handleCommand(ctx, msg)
	}); token.Wait() && token.Error() != nil {
		return fmt.Errorf("failed to subscribe to control topic: %w", token.Error())
	}

	// Subscribe to registry topic
	registryTopic := fmt.Sprintf("channels/%s/messages/registry/proplet", p.config.ChannelID)
	if token := p.mqttClient.Subscribe(registryTopic, 0, func(client mqtt.Client, msg mqtt.Message) {
		p.handleChunk(msg)
	}); token.Wait() && token.Error() != nil {
		return fmt.Errorf("failed to subscribe to registry topic: %w", token.Error())
	}

	fmt.Println("Proplet service is running...")
	<-ctx.Done()
	return nil
}

// handleCommand processes incoming JSON-RPC commands.
func (p *PropletService) handleCommand(ctx context.Context, msg mqtt.Message) {
	var req RPCRequest
	if err := json.Unmarshal(msg.Payload(), &req); err != nil {
		p.publishErrorResponse(0, fmt.Errorf("invalid JSON-RPC request: %w", pkgerrors.ErrInvalidData))
		return
	}

	switch req.Method {
	case "start":
		if len(req.Params) < 1 {
			p.publishErrorResponse(req.ID, fmt.Errorf("invalid parameters for 'start': %w", pkgerrors.ErrInvalidParams))
			return
		}
		appName, ok := req.Params[0].(string)
		if !ok || appName == "" {
			p.publishErrorResponse(req.ID, fmt.Errorf("invalid appName for 'start': %w", pkgerrors.ErrInvalidValue))
			return
		}
		go p.handleStart(ctx, req.ID, appName)
	case "stop":
		if len(req.Params) < 1 {
			p.publishErrorResponse(req.ID, fmt.Errorf("invalid parameters for 'stop': %w", pkgerrors.ErrInvalidParams))
			return
		}
		appName, ok := req.Params[0].(string)
		if !ok || appName == "" {
			p.publishErrorResponse(req.ID, fmt.Errorf("invalid appName for 'stop': %w", pkgerrors.ErrInvalidValue))
			return
		}
		go p.handleStop(ctx, req.ID, appName)
	default:
		p.publishErrorResponse(req.ID, fmt.Errorf("unknown method '%s': %w", req.Method, pkgerrors.ErrInvalidMethod))
	}
}

// handleStart processes the "start" command.
func (p *PropletService) handleStart(ctx context.Context, id int, appName string) {
	if appName == "" {
		p.publishErrorResponse(id, fmt.Errorf("start app: appName is required but missing: %w", pkgerrors.ErrMissingValue))
		return
	}

	p.chunksMutex.Lock()
	chunks, exists := p.appChunks[appName]
	p.chunksMutex.Unlock()

	if !exists || len(chunks) == 0 {
		err := p.requestArtifact(appName)
		if err != nil {
			p.publishErrorResponse(id, fmt.Errorf("failed to request artifact: %w", pkgerrors.ErrArtifactRequestFailed))
			return
		}

		if !p.waitForChunks(ctx, appName) {
			p.publishErrorResponse(id, fmt.Errorf("timeout waiting for chunks: %w", pkgerrors.ErrChunksTimeout))
			return
		}
	}

	p.chunksMutex.Lock()
	wasmBinary := reassembleChunks(p.appChunks[appName])
	p.chunksMutex.Unlock()

	function, err := p.runtime.StartApp(ctx, appName, wasmBinary, "main")
	if err != nil {
		p.publishErrorResponse(id, fmt.Errorf("failed to start app '%s': %w", appName, pkgerrors.ErrAppStartFailed))
		return
	}

	_, err = function.Call(ctx)
	if err != nil {
		p.publishErrorResponse(id, fmt.Errorf("failed to execute function in app '%s': %w", appName, err))
		return
	}

	p.publishSuccessResponse(id, fmt.Sprintf("App '%s' started successfully", appName))
}

// handleStop processes the "stop" command.
func (p *PropletService) handleStop(ctx context.Context, id int, appName string) {
	if appName == "" {
		p.publishErrorResponse(id, fmt.Errorf("stop app: appName is required but missing: %w", pkgerrors.ErrMissingValue))
		return
	}

	err := p.runtime.StopApp(ctx, appName)
	if err != nil {
		p.publishErrorResponse(id, fmt.Errorf("failed to stop app '%s': %w", appName, pkgerrors.ErrAppStopFailed))
		return
	}

	p.publishSuccessResponse(id, fmt.Sprintf("App '%s' stopped successfully", appName))
}

// handleChunk processes Wasm chunks received from the Registry Proxy.
func (p *PropletService) handleChunk(msg mqtt.Message) {
	var chunk ChunkPayload
	if err := json.Unmarshal(msg.Payload(), &chunk); err != nil {
		fmt.Printf("Failed to unmarshal chunk payload: %v\n", err)
		return
	}

	if err := chunk.Validate(); err != nil {
		fmt.Printf("Invalid chunk payload: %v\n", err)
		return
	}

	p.chunksMutex.Lock()
	defer p.chunksMutex.Unlock()
	p.appChunks[chunk.AppName] = append(p.appChunks[chunk.AppName], chunk)
	fmt.Printf("Received chunk %d/%d for app '%s'\n", chunk.ChunkIdx+1, chunk.TotalChunks, chunk.AppName)
}

// requestArtifact sends an artifact request to the Registry Proxy.
func (p *PropletService) requestArtifact(appName string) error {
	topic := fmt.Sprintf("channels/%s/messages/registry/manager", p.config.ChannelID)
	payload := fmt.Sprintf(`{"appName": "%s", "action": "fetch"}`, appName)
	token := p.mqttClient.Publish(topic, 0, false, payload)
	token.Wait()
	if token.Error() != nil {
		return fmt.Errorf("artifact request failed: %w", token.Error())
	}
	return nil
}

// waitForChunks waits for all chunks of a Wasm app to arrive or times out.
func (p *PropletService) waitForChunks(ctx context.Context, appName string) bool {
	chunkArrival := make(chan struct{})
	go func() {
		for {
			p.chunksMutex.Lock()
			chunks, exists := p.appChunks[appName]
			p.chunksMutex.Unlock()

			if exists && len(chunks) > 0 {
				close(chunkArrival)
				return
			}

			select {
			case <-ctx.Done():
				return
			default:
				time.Sleep(100 * time.Millisecond)
			}
		}
	}()

	select {
	case <-chunkArrival:
		return true
	case <-time.After(30 * time.Second):
		return false
	}
}

// reassembleChunks reconstructs the binary from received Wasm chunks.
func reassembleChunks(chunks []ChunkPayload) []byte {
	chunkMap := make(map[int][]byte)
	for _, chunk := range chunks {
		chunkMap[chunk.ChunkIdx] = chunk.Data
	}

	var wasmBinary []byte
	for i := 0; i < len(chunks); i++ {
		wasmBinary = append(wasmBinary, chunkMap[i]...)
	}
	return wasmBinary
}

// publishErrorResponse publishes an error response to the control topic.
func (p *PropletService) publishErrorResponse(id int, err error) {
	response := RPCResponse{Error: err.Error(), ID: id}
	payload, _ := json.Marshal(response)
	topic := fmt.Sprintf("channels/%s/messages/control/proplet", p.config.ChannelID)
	p.mqttClient.Publish(topic, 0, false, payload)
}

// publishSuccessResponse publishes a success response to the control topic.
func (p *PropletService) publishSuccessResponse(id int, result string) {
	response := RPCResponse{Result: result, ID: id}
	payload, _ := json.Marshal(response)
	topic := fmt.Sprintf("channels/%s/messages/control/proplet", p.config.ChannelID)
	p.mqttClient.Publish(topic, 0, false, payload)
}
