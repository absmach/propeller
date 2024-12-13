package proplet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

var (
	RegistryFailurePayload = `{"status":"failure","error":"%s"}`
	RegistrySuccessPayload = `{"status":"success"}`
	lwtPayloadTemplate     = `{"status":"offline","proplet_id":"%s","mg_channel_id":"%s"}`

	RegistryAckTopicTemplate  = "channels/%s/messages/control/manager/registry"
	aliveTopicTemplate        = "channels/%s/messages/control/proplet/alive"
	discoveryTopicTemplate    = "channels/%s/messages/control/proplet/create"
	startTopicTemplate        = "channels/%s/messages/control/manager/start"
	stopTopicTemplate         = "channels/%s/messages/control/manager/stop"
	registryResponseTopic     = "channels/%s/messages/registry/server"
	fetchRequestTopicTemplate = "channels/%s/messages/registry/proplet"
)

type Client interface {
	SubscribeToManagerTopics(startHandler, stopHandler, registryHandler mqtt.MessageHandler) error
	SubscribeToRegistryTopic(handler mqtt.MessageHandler) error
	PublishFetchRequest(appName string) error
	PublishResults(taskID string, results []uint64) error
}

type mqttClient struct {
	client mqtt.Client
	config Config
	logger *slog.Logger
}

func NewMQTTClient(ctx context.Context, cfg Config, logger *slog.Logger) (Client, error) {
	topic := fmt.Sprintf(aliveTopicTemplate, cfg.ChannelID)
	lwtPayload := fmt.Sprintf(lwtPayloadTemplate, cfg.ThingID, cfg.ChannelID)

	opts := mqtt.NewClientOptions().
		AddBroker(cfg.MQTTAddress).
		SetClientID(cfg.ID).
		SetUsername(cfg.ThingID).
		SetPassword(cfg.ThingKey).
		SetCleanSession(true).
		SetWill(topic, lwtPayload, 0, false)

	opts.SetConnectionLostHandler(func(client mqtt.Client, err error) {
		args := []any{}
		if err != nil {
			args = append(args, slog.Any("error", err))
		}

		logger.Info("MQTT connection lost", args...)
	})

	opts.SetReconnectingHandler(func(client mqtt.Client, options *mqtt.ClientOptions) {
		args := []any{}
		if options != nil {
			args = append(args,
				slog.String("client_id", options.ClientID),
				slog.String("username", options.Username),
			)
		}

		logger.Info("MQTT reconnecting", args...)
	})

	client := mqtt.NewClient(opts)

	token := client.Connect()
	if ok := token.WaitTimeout(cfg.MQTTTimeout); !ok {
		return nil, errors.New("timeout reached while connecting to MQTT broker")
	}
	if token.Error() != nil {
		return nil, fmt.Errorf("failed to connect to %s MQTT broker with error %s", cfg.MQTTAddress, token.Error().Error())
	}

	mc := &mqttClient{
		client: client,
		config: cfg,
		logger: logger,
	}

	if err := mc.publishDiscovery(); err != nil {
		return nil, err
	}

	go mc.startLivelinessUpdates(ctx, logger)

	logger.Info("MQTT client connected successfully", slog.String("broker_url", cfg.MQTTAddress))

	return mc, nil
}

func (mc *mqttClient) publishDiscovery() error {
	topic := fmt.Sprintf(discoveryTopicTemplate, mc.config.ChannelID)
	payload := map[string]interface{}{
		"proplet_id":    mc.config.ThingID,
		"mg_channel_id": mc.config.ChannelID,
	}

	return mc.publish(topic, payload)
}

func (mc *mqttClient) startLivelinessUpdates(ctx context.Context, logger *slog.Logger) {
	ticker := time.NewTicker(mc.config.LivelinessInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Info("stopping liveliness updates")

			return
		case <-ticker.C:
			topic := fmt.Sprintf(aliveTopicTemplate, mc.config.ChannelID)
			payload := map[string]interface{}{
				"status":        "alive",
				"proplet_id":    mc.config.ThingID,
				"mg_channel_id": mc.config.ChannelID,
			}

			if err := mc.publish(topic, payload); err != nil {
				logger.Error("failed to publish liveliness message", slog.Any("error", err))
			}

			logger.Debug("Published liveliness message", slog.String("topic", topic))
		}
	}
}

func (mc *mqttClient) SubscribeToManagerTopics(startHandler, stopHandler, registryHandler mqtt.MessageHandler) error {
	startTopic := fmt.Sprintf(startTopicTemplate, mc.config.ChannelID)
	stopTopic := fmt.Sprintf(stopTopicTemplate, mc.config.ChannelID)

	if err := mc.subscribe(startTopic, startHandler); err != nil {
		return err
	}

	if err := mc.subscribe(stopTopic, stopHandler); err != nil {
		return err
	}

	return nil
}

func (mc *mqttClient) SubscribeToRegistryTopic(handler mqtt.MessageHandler) error {
	topic := fmt.Sprintf(registryResponseTopic, mc.config.ChannelID)

	return mc.subscribe(topic, handler)
}

func (mc *mqttClient) subscribe(topic string, handler mqtt.MessageHandler) error {
	token := mc.client.Subscribe(topic, mc.config.MQTTQoS, handler)
	if ok := token.WaitTimeout(mc.config.MQTTTimeout); !ok {
		return fmt.Errorf("timeout reached while subscribing to %s topic", topic)
	}
	if token.Error() != nil {
		return fmt.Errorf("failed to subscribe to %s topic: %w", topic, token.Error())
	}

	return nil
}

func (mc *mqttClient) PublishFetchRequest(appName string) error {
	payload := map[string]interface{}{
		"app_name": appName,
	}
	topic := fmt.Sprintf(fetchRequestTopicTemplate, mc.config.ChannelID)

	return mc.publish(topic, payload)
}

func (mc *mqttClient) PublishResults(taskID string, results []uint64) error {
	payload := map[string]interface{}{
		"task_id": taskID,
		"results": results,
	}

	topic := fmt.Sprintf(resultsTopic, mc.config.ChannelID)

	return mc.publish(topic, payload)
}

func (mc *mqttClient) publish(topic string, payload map[string]interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return errors.Join(errors.New("failed to marshal results payload"), err)
	}

	token := mc.client.Publish(topic, 0, false, data)
	if ok := token.WaitTimeout(mc.config.MQTTTimeout); !ok {
		return errors.New("timeout reached while publishing results")
	}
	if token.Error() != nil {
		return errors.Join(errors.New("failed to publish results"), token.Error())
	}

	return nil
}
