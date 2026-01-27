package executor

import (
	"context"
	"errors"

	"github.com/absmach/propeller/pkg/mqtt"
	"github.com/absmach/propeller/pkg/orchestration"
	"github.com/absmach/propeller/task"
)

type MQTTWorkExecutor struct {
	pubsub     mqtt.PubSub
	startTopic string
	stopTopic  string
}

func NewMQTTWorkExecutor(pubsub mqtt.PubSub, startTopic, stopTopic string) orchestration.WorkExecutor {
	return &MQTTWorkExecutor{
		pubsub:     pubsub,
		startTopic: startTopic,
		stopTopic:  stopTopic,
	}
}

func (e *MQTTWorkExecutor) StartTask(ctx context.Context, t orchestration.Task, proplet orchestration.Proplet) error {
	payload := map[string]any{
		"id":                 t.ID,
		"name":               t.Name,
		"state":              t.State,
		"image_url":          t.ImageURL,
		"file":               t.File,
		"inputs":             t.Inputs,
		"cli_args":           t.CLIArgs,
		"daemon":             t.Daemon,
		"env":                t.Env,
		"monitoring_profile": t.MonitoringProfile,
		"proplet_id":         proplet.ID,
	}

	return e.pubsub.Publish(ctx, e.startTopic, payload)
}

func (e *MQTTWorkExecutor) StopTask(ctx context.Context, taskID, propletID string) error {
	payload := map[string]any{
		"id":         taskID,
		"proplet_id": propletID,
	}

	return e.pubsub.Publish(ctx, e.stopTopic, payload)
}

func (e *MQTTWorkExecutor) GetTaskStatus(ctx context.Context, taskID string) (orchestration.TaskStatus, error) {
	// This would need to query the state store through a callback or interface
	// For now, return a basic status
	return orchestration.TaskStatus{
		TaskID: taskID,
		State:  task.Pending,
	}, errors.New("task status not directly available via MQTT executor")
}
