package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/absmach/propeller/task"
	MQTT "github.com/eclipse/paho.mqtt.golang"
)

type Proplet struct {
	ClientID     string
	ChannelID    string
	MQTTClient   MQTT.Client
	chunkBuffers map[string][]ChunkPayload
	bufferMutex  sync.Mutex
}

type RPCRequest struct {
	Method string        `json:"method"`
	Params []interface{} `json:"params"`
	ID     int           `json:"id"`
}

type ChunkPayload struct {
	AppName     string `json:"appName"`
	ChunkIdx    int    `json:"chunkIdx"`
	TotalChunks int    `json:"totalChunks"`
	Data        []byte `json:"data"`
}

func NewProplet(clientID, channelID, brokerURL, token string) (*Proplet, error) {
	opts := MQTT.NewClientOptions()
	opts.AddBroker(brokerURL)
	opts.SetClientID(clientID)
	opts.SetUsername(token)

	client := MQTT.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		return nil, token.Error()
	}

	return &Proplet{
		ClientID:     clientID,
		ChannelID:    channelID,
		MQTTClient:   client,
		chunkBuffers: make(map[string][]ChunkPayload),
	}, nil
}

func (p *Proplet) Publish(topic, payload string) {
	token := p.MQTTClient.Publish(topic, 0, false, payload)
	token.Wait()
	if token.Error() != nil {
		fmt.Printf("Failed to publish message: %s\n", token.Error())
	}
}

func (p *Proplet) SubscribeToTopics() error {
	controlTopic := fmt.Sprintf("channels/%s/messages/control/manager", p.ChannelID)
	registryTopic := fmt.Sprintf("channels/%s/messages/registry/proplet", p.ChannelID)
	discoveryTopic := fmt.Sprintf("channels/%s/messages/discovery", p.ChannelID)

	if token := p.MQTTClient.Subscribe(controlTopic, 0, p.handleControlMessage); token.Wait() && token.Error() != nil {
		return token.Error()
	}

	if token := p.MQTTClient.Subscribe(registryTopic, 0, p.handleRegistryMessage); token.Wait() && token.Error() != nil {
		return token.Error()
	}

	// Publish discovery message to let Manager know Proplet is online
	discoveryPayload := fmt.Sprintf(`{"channelID":"%s", "clientID":"%s"}`, p.ChannelID, p.ClientID)
	p.Publish(discoveryTopic, discoveryPayload)

	return nil
}

func (p *Proplet) handleControlMessage(client MQTT.Client, msg MQTT.Message) {
	fmt.Printf("Control Message Received: %s\n", string(msg.Payload()))

	// Parse the JSON payload into RPCRequest struct
	var request RPCRequest
	if err := json.Unmarshal(msg.Payload(), &request); err != nil {
		fmt.Printf("Failed to parse control message: %s\n", err)
		return
	}

	// Determine the action based on the method field in the RPC request
	switch request.Method {
	case "start":
		if len(request.Params) < 1 {
			fmt.Println("Invalid parameters for start method")
			return
		}

		appName, ok := request.Params[0].(string)
		if !ok {
			fmt.Println("Invalid app name format")
			return
		}

		fmt.Printf("Received request to start app: %s\n", appName)

		// Check if all chunks for the app are already received
		p.bufferMutex.Lock()
		chunks, allChunksReceived := p.chunkBuffers[appName]
		p.bufferMutex.Unlock()

		if allChunksReceived && len(chunks) > 0 {
			// Reassemble and deploy the Wasm binary immediately if already available
			wasmBinary := p.reassembleChunks(appName)
			if wasmBinary != nil {
				p.deployWasmApp(wasmBinary)
			} else {
				fmt.Printf("Failed to reassemble chunks for app: %s\n", appName)
			}
		} else {
			fmt.Printf("Waiting for all chunks to be received for app: %s\n", appName)
		}

	case "stop":
		if len(request.Params) < 1 {
			fmt.Println("Invalid parameters for stop method")
			return
		}

		appName, ok := request.Params[0].(string)
		if !ok {
			fmt.Println("Invalid app name format")
			return
		}

		// Stop the Wasm task by calling the appropriate worker method
		w := NewWasmWorker(fmt.Sprintf("%s-Worker", appName))
		err := w.StopTask(context.Background(), fmt.Sprintf("%s-task", appName))
		if err != nil {
			fmt.Printf("Failed to stop task: %s\n", err)
			return
		}

		fmt.Printf("Stopped Wasm Task: %s\n", appName)

	default:
		fmt.Printf("Unknown method: %s\n", request.Method)
	}
}

func (p *Proplet) handleRegistryMessage(client MQTT.Client, msg MQTT.Message) {
	fmt.Printf("Registry Message Received: %s\n", string(msg.Payload()))

	// Parse the incoming chunk
	var payload ChunkPayload
	if err := json.Unmarshal(msg.Payload(), &payload); err != nil {
		fmt.Printf("Failed to parse chunk payload: %s\n", err)
		return
	}

	// Store the chunk in the appropriate app buffer
	p.bufferMutex.Lock()
	p.chunkBuffers[payload.AppName] = append(p.chunkBuffers[payload.AppName], payload)
	p.bufferMutex.Unlock()

	// Check if all chunks are received
	if len(p.chunkBuffers[payload.AppName]) == payload.TotalChunks {
		fmt.Printf("Received all chunks for app: %s\n", payload.AppName)

		// Reassemble the Wasm binary and deploy
		wasmBinary := p.reassembleChunks(payload.AppName)
		if wasmBinary != nil {
			p.deployWasmApp(wasmBinary)
		} else {
			fmt.Printf("Failed to reassemble chunks for app: %s\n", payload.AppName)
		}

		// Clear the buffer after successful reassembly
		p.bufferMutex.Lock()
		delete(p.chunkBuffers, payload.AppName)
		p.bufferMutex.Unlock()
	}
}

func (p *Proplet) reassembleChunks(appName string) []byte {
	p.bufferMutex.Lock()
	defer p.bufferMutex.Unlock()

	chunks := p.chunkBuffers[appName]
	if len(chunks) == 0 {
		return nil
	}

	// Sort chunks by ChunkIdx to ensure proper ordering
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

func (p *Proplet) deployWasmApp(wasmBinary []byte) {
	fmt.Printf("Deploying Wasm app...\n")

	// Implement the logic to instantiate and run the Wasm app using the worker's Wasm runtime (wazero).
	ctx := context.Background()
	w := NewWasmWorker(fmt.Sprintf("%s-WasmWorker", p.ClientID))

	task := task.Task{
		ID:    fmt.Sprintf("%s-task", p.ClientID),
		Name:  "WasmAppDeployment",
		State: task.Pending,
		Function: task.Function{
			File: wasmBinary,
			Name: "main", // Assuming "main" is the entry function in the Wasm module
		},
	}

	err := w.StartTask(ctx, task)
	if err != nil {
		fmt.Printf("Failed to start Wasm task: %s\n", err)
		return
	}

	fmt.Printf("Successfully deployed Wasm app.\n")
}
