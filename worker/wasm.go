package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/absmach/propeller/task"
	MQTT "github.com/eclipse/paho.mqtt.golang"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

var _ Service = (*Worker)(nil)

type Worker struct {
	mu         sync.Mutex
	Name       string
	Db         map[string]task.Task
	TaskCount  int
	runtimes   map[string]wazero.Runtime
	functions  map[string]api.Function
	mqttClient MQTT.Client
	channelID  string
}

func NewWasmWorker(name string, mqttClient MQTT.Client, channelID string) *Worker {
	return &Worker{
		Name:       name,
		Db:         make(map[string]task.Task),
		TaskCount:  0,
		runtimes:   make(map[string]wazero.Runtime),
		functions:  make(map[string]api.Function),
		mqttClient: mqttClient,
		channelID:  channelID,
	}
}

func (w *Worker) StartTask(ctx context.Context, task task.Task) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	r := wazero.NewRuntime(ctx)
	wasi_snapshot_preview1.MustInstantiate(ctx, r)

	module, err := r.Instantiate(ctx, task.Function.File)
	if err != nil {
		w.publishTelemetry(map[string]interface{}{
			"taskID":    task.ID,
			"state":     "Error",
			"error":     err.Error(),
			"timestamp": time.Now(),
		})
		return err
	}

	function := module.ExportedFunction(task.Function.Name)
	if function == nil {
		w.publishTelemetry(map[string]interface{}{
			"taskID":    task.ID,
			"state":     "Error",
			"error":     fmt.Sprintf("function %q not found", task.Function.Name),
			"timestamp": time.Now(),
		})
		return fmt.Errorf("function %q not found", task.Function.Name)
	}

	w.TaskCount++
	w.runtimes[task.ID] = r
	w.functions[task.ID] = function
	w.Db[task.ID] = task

	// Publish task start telemetry
	w.publishTelemetry(map[string]interface{}{
		"taskID":    task.ID,
		"state":     "Started",
		"timestamp": time.Now(),
	})

	return nil
}

func (w *Worker) RunTask(ctx context.Context, taskID string) ([]uint64, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	task, ok := w.Db[taskID]
	if !ok {
		w.publishTelemetry(map[string]interface{}{
			"taskID":    taskID,
			"state":     "Error",
			"error":     fmt.Sprintf("task %q not found", taskID),
			"timestamp": time.Now(),
		})
		return nil, fmt.Errorf("task %q not found", taskID)
	}

	function := w.functions[task.ID]

	result, err := function.Call(ctx, task.Function.Inputs...)
	if err != nil {
		w.publishTelemetry(map[string]interface{}{
			"taskID":    task.ID,
			"state":     "Error",
			"error":     err.Error(),
			"timestamp": time.Now(),
		})
		return nil, err
	}

	r := w.runtimes[task.ID]
	if err := r.Close(ctx); err != nil {
		w.publishTelemetry(map[string]interface{}{
			"taskID":    task.ID,
			"state":     "Error",
			"error":     err.Error(),
			"timestamp": time.Now(),
		})
		return nil, err
	}

	w.publishTelemetry(map[string]interface{}{
		"taskID":    task.ID,
		"state":     "Completed",
		"results":   result,
		"timestamp": time.Now(),
	})

	return result, nil
}

func (w *Worker) StopTask(ctx context.Context, appName string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	var taskID string
	for id, t := range w.Db {
		if t.Name == appName {
			taskID = id
			break
		}
	}

	if taskID == "" {
		w.publishTelemetry(map[string]interface{}{
			"appName":   appName,
			"state":     "Error",
			"error":     fmt.Sprintf("task for app %q not found", appName),
			"timestamp": time.Now(),
		})
		return fmt.Errorf("task for app %q not found", appName)
	}

	r, exists := w.runtimes[taskID]
	if !exists {
		w.publishTelemetry(map[string]interface{}{
			"taskID":    taskID,
			"state":     "Error",
			"error":     fmt.Sprintf("runtime for task %q not found", taskID),
			"timestamp": time.Now(),
		})
		return fmt.Errorf("runtime for task %q not found", taskID)
	}

	if err := r.Close(ctx); err != nil {
		w.publishTelemetry(map[string]interface{}{
			"taskID":    taskID,
			"state":     "Error",
			"error":     err.Error(),
			"timestamp": time.Now(),
		})
		return err
	}

	delete(w.Db, taskID)
	delete(w.runtimes, taskID)
	delete(w.functions, taskID)
	w.TaskCount--

	w.publishTelemetry(map[string]interface{}{
		"taskID":    taskID,
		"state":     "Stopped",
		"timestamp": time.Now(),
	})

	return nil
}

func (w *Worker) DeployApp(ctx context.Context, appName string) error {
	fmt.Printf("Deploying app: %s\n", appName)
	w.publishTelemetry(map[string]interface{}{
		"appName":   appName,
		"state":     "Deploying",
		"timestamp": time.Now(),
	})
	return nil
}

func (w *Worker) PublishDiscovery(ctx context.Context) error {
	topic := fmt.Sprintf("channels/%s/messages/discovery", w.channelID)
	discoveryPayload := fmt.Sprintf(`{"channelID":"%s", "clientID":"%s"}`, w.channelID, w.Name)
	token := w.mqttClient.Publish(topic, 0, false, discoveryPayload)
	token.Wait()
	if token.Error() != nil {
		w.publishTelemetry(map[string]interface{}{
			"state":     "DiscoveryError",
			"error":     token.Error().Error(),
			"timestamp": time.Now(),
		})
		return token.Error()
	}

	w.publishTelemetry(map[string]interface{}{
		"state":     "DiscoveryPublished",
		"timestamp": time.Now(),
	})
	return nil
}

func (w *Worker) ListenForAppChunks(ctx context.Context, appName string) error {
	topic := fmt.Sprintf("channels/%s/messages/registry/server", w.channelID)
	token := w.mqttClient.Subscribe(topic, 0, func(client MQTT.Client, msg MQTT.Message) {
		fmt.Printf("Received chunk for app: %s\n", appName)
		// Handle chunk processing here
	})
	token.Wait()
	if token.Error() != nil {
		w.publishTelemetry(map[string]interface{}{
			"appName":   appName,
			"state":     "Error",
			"error":     token.Error().Error(),
			"timestamp": time.Now(),
		})
		return token.Error()
	}

	w.publishTelemetry(map[string]interface{}{
		"appName":   appName,
		"state":     "ListeningForChunks",
		"timestamp": time.Now(),
	})
	return nil
}

func (w *Worker) SendTelemetry(ctx context.Context) error {
	topic := fmt.Sprintf("channels/%s/messages/monitor", w.channelID)
	telemetryPayload := fmt.Sprintf(`{"clientID":"%s", "status":"healthy"}`, w.Name)
	token := w.mqttClient.Publish(topic, 0, false, telemetryPayload)
	token.Wait()
	if token.Error() != nil {
		w.publishTelemetry(map[string]interface{}{
			"state":     "TelemetryError",
			"error":     token.Error().Error(),
			"timestamp": time.Now(),
		})
		return token.Error()
	}

	w.publishTelemetry(map[string]interface{}{
		"state":     "TelemetrySent",
		"timestamp": time.Now(),
	})
	return nil
}

func (w *Worker) HandleRPCCommand(ctx context.Context, command string, params []string) error {
	switch command {
	case "start":
		if len(params) > 0 {
			w.publishTelemetry(map[string]interface{}{
				"command":   "start",
				"appName":   params[0],
				"timestamp": time.Now(),
			})
			return w.DeployApp(ctx, params[0])
		}
		return fmt.Errorf("missing parameters for start command")
	case "stop":
		if len(params) > 0 {
			w.publishTelemetry(map[string]interface{}{
				"command":   "stop",
				"appName":   params[0],
				"timestamp": time.Now(),
			})
			return w.StopApp(ctx, params[0])
		}
		return fmt.Errorf("missing parameters for stop command")
	default:
		w.publishTelemetry(map[string]interface{}{
			"command":   command,
			"state":     "UnknownCommand",
			"timestamp": time.Now(),
		})
		return fmt.Errorf("unknown command: %s", command)
	}
}

func (w *Worker) StopApp(ctx context.Context, appName string) error {
	return w.StopTask(ctx, appName)
}

func (w *Worker) publishTelemetry(telemetryData map[string]interface{}) {
	topic := fmt.Sprintf("channels/%s/messages/monitor", w.channelID)
	payload, _ := json.Marshal(telemetryData)
	w.mqttClient.Publish(topic, 0, false, payload)
}
