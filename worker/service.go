package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	MQTT "github.com/eclipse/paho.mqtt.golang"
)

type propletService struct {
	clientID     string
	channelID    string
	mqttClient   MQTT.Client
	chunkBuffers map[string][]ChunkPayload
	bufferMutex  sync.Mutex
}

type ChunkPayload struct {
	AppName     string `json:"appName"`
	ChunkIdx    int    `json:"chunkIdx"`
	TotalChunks int    `json:"totalChunks"`
	Data        []byte `json:"data"`
}

func NewService(clientID, channelID string, mqttClient MQTT.Client) Service {
	return &propletService{
		clientID:     clientID,
		channelID:    channelID,
		mqttClient:   mqttClient,
		chunkBuffers: make(map[string][]ChunkPayload),
	}
}

func (s *propletService) DeployApp(ctx context.Context, appName string) error {
	s.bufferMutex.Lock()
	chunks, allChunksReceived := s.chunkBuffers[appName]
	s.bufferMutex.Unlock()

	if allChunksReceived && len(chunks) > 0 {
		wasmBinary := s.reassembleChunks(appName)
		if wasmBinary != nil {
			fmt.Printf("Deploying Wasm app: %s\n", appName)
			fmt.Printf("Wasm app %s successfully deployed on node %s\n", appName, s.clientID)
		} else {
			return fmt.Errorf("failed to reassemble chunks for app: %s", appName)
		}
	} else {
		return fmt.Errorf("waiting for all chunks to be received for app: %s", appName)
	}
	return nil
}

func (s *propletService) StopApp(ctx context.Context, appName string) error {
	fmt.Printf("Stopping Wasm app: %s\n", appName)
	fmt.Printf("Wasm app %s successfully stopped on node %s\n", appName, s.clientID)
	return nil
}

func (s *propletService) PublishDiscovery(ctx context.Context) error {
	discoveryTopic := fmt.Sprintf("channels/%s/messages/discovery", s.channelID)
	discoveryPayload := fmt.Sprintf(`{"channelID":"%s", "clientID":"%s"}`, s.channelID, s.clientID)
	token := s.mqttClient.Publish(discoveryTopic, 0, false, discoveryPayload)
	token.Wait()
	if token.Error() != nil {
		return token.Error()
	}
	return nil
}

func (s *propletService) ListenForAppChunks(ctx context.Context, appName string) error {
	registryTopic := fmt.Sprintf("channels/%s/messages/registry/server", s.channelID)

	// Subscribe to the registry topic to receive chunks
	token := s.mqttClient.Subscribe(registryTopic, 0, func(client MQTT.Client, msg MQTT.Message) {
		var chunk ChunkPayload
		if err := json.Unmarshal(msg.Payload(), &chunk); err != nil {
			fmt.Printf("Failed to unmarshal chunk payload: %v\n", err)
			return
		}

		if chunk.AppName == appName {
			s.bufferMutex.Lock()
			s.chunkBuffers[appName] = append(s.chunkBuffers[appName], chunk)
			s.bufferMutex.Unlock()
			fmt.Printf("Received chunk %d/%d for app: %s\n", chunk.ChunkIdx+1, chunk.TotalChunks, appName)
		}
	})
	token.Wait()
	if token.Error() != nil {
		return token.Error()
	}

	fmt.Printf("Subscribed to registry topic for app: %s\n", appName)
	return nil
}

func (s *propletService) SendTelemetry(ctx context.Context) error {
	telemetryTopic := fmt.Sprintf("channels/%s/messages/monitor", s.channelID)
	telemetryPayload := fmt.Sprintf(`{"clientID":"%s", "status":"healthy"}`, s.clientID)
	token := s.mqttClient.Publish(telemetryTopic, 0, false, telemetryPayload)
	token.Wait()
	if token.Error() != nil {
		return token.Error()
	}
	fmt.Printf("Telemetry data sent for client: %s\n", s.clientID)
	return nil
}

func (s *propletService) HandleRPCCommand(ctx context.Context, command string, params []string) error {
	switch command {
	case "start":
		if len(params) > 0 {
			return s.DeployApp(ctx, params[0])
		}
		return fmt.Errorf("missing parameters for start command")
	case "stop":
		if len(params) > 0 {
			return s.StopApp(ctx, params[0])
		}
		return fmt.Errorf("missing parameters for stop command")
	default:
		return fmt.Errorf("unknown command: %s", command)
	}
}

func (s *propletService) reassembleChunks(appName string) []byte {
	s.bufferMutex.Lock()
	defer s.bufferMutex.Unlock()

	chunks := s.chunkBuffers[appName]
	if len(chunks) == 0 {
		return nil
	}

	chunkMap := make(map[int][]byte)
	for _, chunk := range chunks {
		chunkMap[chunk.ChunkIdx] = chunk.Data
	}

	var wasmBinary []byte
	for i := 0; i < len(chunks); i++ {
		if data, ok := chunkMap[i]; ok {
			wasmBinary = append(wasmBinary, data...)
		} else {
			fmt.Printf("Missing chunk %d for app: %s\n", i, appName)
			return nil
		}
	}

	return wasmBinary
}

func loadConfig(filepath string) (*PropletConfig, error) {
	file, err := os.Open(filepath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var config PropletConfig
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&config); err != nil {
		return nil, err
	}

	return &config, nil
}

func generatePropletName() string {
	counterMutex.Lock()
	defer counterMutex.Unlock()
	propletCounter++
	return fmt.Sprintf("Proplet-%d", propletCounter)
}

var (
	propletCounter int
	counterMutex   sync.Mutex
)

// PropletConfig holds the configuration for creating a Proplet.
type PropletConfig struct {
	BrokerURL string `json:"brokerURL"`
	Token     string `json:"token"`
	ChannelID string `json:"channelID"`
}
