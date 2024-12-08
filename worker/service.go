package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// ChunkPayload represents a single chunk of a Wasm binary.
type ChunkPayload struct {
	AppName     string `json:"appName"`
	ChunkIdx    int    `json:"chunkIdx"`
	TotalChunks int    `json:"totalChunks"`
	Data        []byte `json:"data"`
}

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
		return nil, fmt.Errorf("failed to initialize MQTT client: %v", err)
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

// Run starts the PropletService and listens for commands and artifacts.
func (p *PropletService) Run(ctx context.Context) error {
	// Publish discovery and set LWT
	PublishDiscovery(p.mqttClient, p.config)

	// Subscribe to control topic for commands
	controlTopic := fmt.Sprintf("channels/%s/messages/control", p.config.ChannelID)
	if token := p.mqttClient.Subscribe(controlTopic, 0, func(client mqtt.Client, msg mqtt.Message) {
		p.handleCommand(ctx, msg)
	}); token.Wait() && token.Error() != nil {
		return fmt.Errorf("failed to subscribe to control topic: %v", token.Error())
	}

	// Subscribe to registry topic for receiving Wasm chunks
	registryTopic := fmt.Sprintf("channels/%s/messages/registry/proplet", p.config.ChannelID)
	if token := p.mqttClient.Subscribe(registryTopic, 0, func(client mqtt.Client, msg mqtt.Message) {
		p.handleChunk(msg)
	}); token.Wait() && token.Error() != nil {
		return fmt.Errorf("failed to subscribe to registry topic: %v", token.Error())
	}

	fmt.Println("Proplet service is running...")
	<-ctx.Done()
	return nil
}

// handleCommand processes incoming JSON-RPC commands.
func (p *PropletService) handleCommand(ctx context.Context, msg mqtt.Message) {
	var req RPCRequest
	if err := json.Unmarshal(msg.Payload(), &req); err != nil {
		p.publishErrorResponse(req.ID, "Invalid JSON-RPC request")
		return
	}

	switch req.Method {
	case "start":
		if len(req.Params) < 1 {
			p.publishErrorResponse(req.ID, "Invalid parameters for 'start'")
			return
		}
		appName := req.Params[0].(string)
		go p.handleStart(ctx, req.ID, appName)
	case "stop":
		if len(req.Params) < 1 {
			p.publishErrorResponse(req.ID, "Invalid parameters for 'stop'")
			return
		}
		appName := req.Params[0].(string)
		go p.handleStop(ctx, req.ID, appName)
	default:
		p.publishErrorResponse(req.ID, "Unknown method")
	}
}

// handleStart processes the "start" command.
func (p *PropletService) handleStart(ctx context.Context, id int, appName string) {
	p.chunksMutex.Lock()
	chunks, exists := p.appChunks[appName]
	p.chunksMutex.Unlock()

	if !exists || len(chunks) == 0 {
		err := p.requestArtifact(appName)
		if err != nil {
			p.publishErrorResponse(id, fmt.Sprintf("Failed to request artifact: %v", err))
			return
		}

		fmt.Printf("Waiting for Wasm chunks for app: %s\n", appName)
		if !p.waitForChunks(ctx, appName) {
			p.publishErrorResponse(id, fmt.Sprintf("Timed out waiting for chunks for app: %s", appName))
			return
		}
	}

	p.chunksMutex.Lock()
	wasmBinary := reassembleChunks(p.appChunks[appName])
	p.chunksMutex.Unlock()

	function, err := p.runtime.StartApp(ctx, appName, wasmBinary, "main")
	if err != nil {
		p.publishErrorResponse(id, fmt.Sprintf("Failed to deploy app: %v", err))
		return
	}

	_, err = function.Call(ctx)
	if err != nil {
		p.publishErrorResponse(id, fmt.Sprintf("Failed to run app: %v", err))
		return
	}

	p.publishSuccessResponse(id, fmt.Sprintf("App %s started successfully", appName))
}

// handleStop processes the "stop" command.
func (p *PropletService) handleStop(ctx context.Context, id int, appName string) {
	err := p.runtime.StopApp(ctx, appName)
	if err != nil {
		p.publishErrorResponse(id, fmt.Sprintf("Failed to stop app: %v", err))
		return
	}
	p.publishSuccessResponse(id, fmt.Sprintf("App %s stopped successfully", appName))
}

// handleChunk processes Wasm chunks received from the Registry Proxy.
func (p *PropletService) handleChunk(msg mqtt.Message) {
	var chunk ChunkPayload
	if err := json.Unmarshal(msg.Payload(), &chunk); err != nil {
		fmt.Printf("Failed to unmarshal chunk payload: %v\n", err)
		return
	}

	p.chunksMutex.Lock()
	defer p.chunksMutex.Unlock()
	p.appChunks[chunk.AppName] = append(p.appChunks[chunk.AppName], chunk)
	fmt.Printf("Received chunk %d/%d for app: %s\n", chunk.ChunkIdx+1, chunk.TotalChunks, chunk.AppName)
}

// requestArtifact sends an artifact request to the Registry Proxy.
func (p *PropletService) requestArtifact(appName string) error {
	topic := fmt.Sprintf("channels/%s/messages/registry/manager", p.config.ChannelID)
	payload := fmt.Sprintf(`{"appName": "%s", "action": "fetch"}`, appName)
	token := p.mqttClient.Publish(topic, 0, false, payload)
	token.Wait()
	return token.Error()
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
func (p *PropletService) publishErrorResponse(id int, errMsg string) {
	response := RPCResponse{Error: errMsg, ID: id}
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
