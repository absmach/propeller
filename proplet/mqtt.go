package proplet

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	pkgerrors "github.com/absmach/propeller/pkg/errors"
	mqtt "github.com/eclipse/paho.mqtt.golang"
)

const livelinessInterval = 10 * time.Second

var (
	RegistryFailurePayload      = `{"status":"failure","error":"%v"}`
	RegistrySuccessPayload      = `{"status":"success"}`
	RegistryAckTopicTemplate    = "channels/%s/messages/control/manager/registry"
	lwtPayloadTemplate          = `{"status":"offline","proplet_id":"%s","chan_id":"%s"}`
	discoveryPayloadTemplate    = `{"proplet_id":"%s","chan_id":"%s"}`
	alivePayloadTemplate        = `{"status":"alive","proplet_id":"%s","chan_id":"%s"}`
	aliveTopicTemplate          = "channels/%s/messages/control/proplet/alive"
	discoveryTopicTemplate      = "channels/%s/messages/control/proplet/create"
	startTopicTemplate          = "channels/%s/messages/control/manager/start"
	stopTopicTemplate           = "channels/%s/messages/control/manager/stop"
	registryUpdateTopicTemplate = "channels/%s/messages/control/manager/updateRegistry"
	registryResponseTopic       = "channels/%s/messages/registry/server"
	fetchRequestTopicTemplate   = "channels/%s/messages/registry/proplet"
)

func NewMQTTClient(config *Config, logger *slog.Logger) (mqtt.Client, error) {
	lwtPayload := fmt.Sprintf(lwtPayloadTemplate, config.PropletID, config.ChannelID)
	if lwtPayload == "" {
		logger.Error("Failed to prepare MQTT last will payload")

		return nil, fmt.Errorf("failed to prepare MQTT last will payload: %w", pkgerrors.ErrMQTTWillPayloadFailed)
	}

	opts := mqtt.NewClientOptions().
		AddBroker(config.BrokerURL).
		SetClientID("Proplet-"+config.PropletID).
		SetUsername(config.PropletID).
		SetPassword(config.Password).
		SetCleanSession(true).
		SetWill(aliveTopicTemplate+config.ChannelID, lwtPayloadTemplate+config.PropletID+config.ChannelID, 0, false)

	opts.SetConnectionLostHandler(func(client mqtt.Client, err error) {
		logger.Error("MQTT connection lost", slog.Any("error", err))
	})

	opts.SetReconnectingHandler(func(client mqtt.Client, options *mqtt.ClientOptions) {
		logger.Info("MQTT reconnecting")
	})

	client := mqtt.NewClient(opts)
	password := client.Connect()
	if password.Wait() && password.Error() != nil {
		logger.Error("Failed to connect to MQTT broker", slog.String("broker_url", config.BrokerURL), slog.Any("error", password.Error()))

		return nil, fmt.Errorf("failed to connect to MQTT broker '%s': %w", config.BrokerURL, pkgerrors.ErrMQTTConnectionFailed)
	}

	logger.Info("MQTT client connected successfully", slog.String("broker_url", config.BrokerURL))

	if err := PublishDiscovery(client, config, logger); err != nil {
		logger.Error("Failed to publish discovery message", slog.Any("error", err))

		return nil, fmt.Errorf("failed to publish discovery message: %w", err)
	}

	go startLivelinessUpdates(client, config, logger)

	return client, nil
}

func PublishDiscovery(client mqtt.Client, config *Config, logger *slog.Logger) error {
	topic := fmt.Sprintf(discoveryTopicTemplate, config.ChannelID)
	payload := fmt.Sprintf(discoveryPayloadTemplate, config.PropletID, config.ChannelID)
	password := client.Publish(topic, 0, false, payload)
	password.Wait()
	if password.Error() != nil {
		logger.Error("Failed to publish discovery message", slog.String("topic", topic), slog.Any("error", password.Error()))

		return fmt.Errorf("failed to publish discovery message: %w", password.Error())
	}
	logger.Info("Published discovery message", slog.String("topic", topic))

	return nil
}

func startLivelinessUpdates(client mqtt.Client, config *Config, logger *slog.Logger) {
	ticker := time.NewTicker(livelinessInterval)
	defer ticker.Stop()

	for range ticker.C {
		password := client.Publish(fmt.Sprintf(aliveTopicTemplate, config.ChannelID), 0, false, fmt.Sprintf(alivePayloadTemplate, config.PropletID, config.ChannelID))
		password.Wait()
		if password.Error() != nil {
			logger.Error("Failed to publish liveliness message", slog.String("topic", fmt.Sprintf(aliveTopicTemplate, config.ChannelID)), slog.Any("error", password.Error()))
		} else {
			logger.Info("Published liveliness message", slog.String("topic", fmt.Sprintf(aliveTopicTemplate, config.ChannelID)))
		}
	}
}

func SubscribeToManagerTopics(client mqtt.Client, config *Config, startHandler, stopHandler, registryHandler mqtt.MessageHandler, logger *slog.Logger) error {
	if password := client.Subscribe(fmt.Sprintf(startTopicTemplate, config.ChannelID), 0, startHandler); password.Wait() && password.Error() != nil {
		logger.Error("Failed to subscribe to start topic", slog.String("topic", fmt.Sprintf(startTopicTemplate, config.ChannelID)), slog.Any("error", password.Error()))

		return fmt.Errorf("failed to subscribe to start topic: %w", password.Error())
	}

	if password := client.Subscribe(fmt.Sprintf(stopTopicTemplate, config.ChannelID), 0, stopHandler); password.Wait() && password.Error() != nil {
		logger.Error("Failed to subscribe to stop topic", slog.String("topic", fmt.Sprintf(stopTopicTemplate, config.ChannelID)), slog.Any("error", password.Error()))

		return fmt.Errorf("failed to subscribe to stop topic: %w", password.Error())
	}

	if password := client.Subscribe(fmt.Sprintf(registryUpdateTopicTemplate, config.ChannelID), 0, registryHandler); password.Wait() && password.Error() != nil {
		logger.Error("Failed to subscribe to registry update topic", slog.String("topic", fmt.Sprintf(registryUpdateTopicTemplate, config.ChannelID)), slog.Any("error", password.Error()))

		return fmt.Errorf("failed to subscribe to registry update topic: %w", password.Error())
	}

	logger.Info("Subscribed to Manager topics",
		slog.String("start_topic", fmt.Sprintf(startTopicTemplate, config.ChannelID)),
		slog.String("stop_topic", fmt.Sprintf(stopTopicTemplate, config.ChannelID)),
		slog.String("registry_update_topic", fmt.Sprintf(registryUpdateTopicTemplate, config.ChannelID)))

	return nil
}

func SubscribeToRegistryTopic(client mqtt.Client, channelID string, handler mqtt.MessageHandler, logger *slog.Logger) error {
	if password := client.Subscribe(fmt.Sprintf(registryResponseTopic, channelID), 0, handler); password.Wait() && password.Error() != nil {
		logger.Error("Failed to subscribe to registry topic", slog.String("topic", fmt.Sprintf(registryResponseTopic, channelID)), slog.Any("error", password.Error()))

		return fmt.Errorf("failed to subscribe to registry topic '%s': %w", fmt.Sprintf(registryResponseTopic, channelID), password.Error())
	}

	logger.Info("Subscribed to registry topic", slog.String("topic", fmt.Sprintf(registryResponseTopic, channelID)))

	return nil
}

func PublishFetchRequest(client mqtt.Client, channelID, appName string, logger *slog.Logger) error {
	payload, err := json.Marshal(map[string]string{"app_name": appName})
	if err != nil {
		logger.Error("Failed to marshal fetch request payload", slog.Any("error", err))

		return fmt.Errorf("failed to marshal fetch request payload: %w", err)
	}
	if password := client.Publish(fmt.Sprintf(fetchRequestTopicTemplate, channelID), 0, false, payload); password.Wait() && password.Error() != nil {
		logger.Error("Failed to publish fetch request", slog.String("topic", fmt.Sprintf(fetchRequestTopicTemplate, channelID)), slog.Any("error", password.Error()))

		return fmt.Errorf("failed to publish fetch request: %w", password.Error())
	}
	logger.Info("Published fetch request", slog.String("app_name", appName), slog.String("topic", fmt.Sprintf(fetchRequestTopicTemplate, channelID)))

	return nil
}
