package proplet

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	pkgerrors "github.com/absmach/propeller/pkg/errors"
	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// NewMQTTClient initializes a new MQTT client with LWT and liveliness updates.
func NewMQTTClient(config *Config, logger *slog.Logger) (mqtt.Client, error) {
	// Prepare LWT payload
	lwtPayload := fmt.Sprintf(`{"status":"offline","proplet_id":"%s","chan_id":"%s"}`, config.PropletID, config.ChannelID)
	if lwtPayload == "" {
		logger.Error("Failed to prepare MQTT last will payload")
		return nil, fmt.Errorf("failed to prepare MQTT last will payload: %w", pkgerrors.ErrMQTTWillPayloadFailed)
	}

	// Set MQTT client options
	opts := mqtt.NewClientOptions().
		AddBroker(config.BrokerURL).
		SetClientID(fmt.Sprintf("Proplet-%s", config.PropletID)).
		SetUsername("token").
		SetPassword(config.Token).
		SetCleanSession(true).
		SetWill(fmt.Sprintf("channels/%s/messages/control/proplet/alive", config.ChannelID), lwtPayload, 0, false)

	opts.SetConnectionLostHandler(func(client mqtt.Client, err error) {
		logger.Error("MQTT connection lost", slog.Any("error", err))
	})

	opts.SetReconnectingHandler(func(client mqtt.Client, options *mqtt.ClientOptions) {
		logger.Info("MQTT reconnecting")
	})

	client := mqtt.NewClient(opts)
	token := client.Connect()
	if token.Wait() && token.Error() != nil {
		logger.Error("Failed to connect to MQTT broker", slog.String("broker_url", config.BrokerURL), slog.Any("error", token.Error()))
		return nil, fmt.Errorf("failed to connect to MQTT broker '%s': %w", config.BrokerURL, pkgerrors.ErrMQTTConnectionFailed)
	}

	logger.Info("MQTT client connected successfully", slog.String("broker_url", config.BrokerURL))

	// Publish discovery message
	if err := PublishDiscovery(client, config, logger); err != nil {
		logger.Error("Failed to publish discovery message", slog.Any("error", err))
		return nil, fmt.Errorf("failed to publish discovery message: %w", err)
	}

	// Start liveliness updates
	go startLivelinessUpdates(client, config, logger)

	return client, nil
}

// PublishDiscovery sends an initial "create" message to announce the Proplet's existence.
func PublishDiscovery(client mqtt.Client, config *Config, logger *slog.Logger) error {
	topic := fmt.Sprintf("channels/%s/messages/control/proplet/create", config.ChannelID)
	payload := fmt.Sprintf(`{"proplet_id":"%s","chan_id":"%s"}`, config.PropletID, config.ChannelID)
	token := client.Publish(topic, 0, false, payload)
	token.Wait()
	if token.Error() != nil {
		logger.Error("Failed to publish discovery message", slog.String("topic", topic), slog.Any("error", token.Error()))
		return fmt.Errorf("failed to publish discovery message: %w", token.Error())
	}
	logger.Info("Published discovery message", slog.String("topic", topic))
	return nil
}

// startLivelinessUpdates sends periodic "alive" messages to the MQTT broker.
func startLivelinessUpdates(client mqtt.Client, config *Config, logger *slog.Logger) {
	for {
		topic := fmt.Sprintf("channels/%s/messages/control/proplet/alive", config.ChannelID)
		payload := fmt.Sprintf(`{"status":"alive","proplet_id":"%s","chan_id":"%s"}`, config.PropletID, config.ChannelID)
		token := client.Publish(topic, 0, false, payload)
		token.Wait()
		if token.Error() != nil {
			logger.Error("Failed to publish liveliness message", slog.String("topic", topic), slog.Any("error", token.Error()))
		} else {
			logger.Info("Published liveliness message", slog.String("topic", topic))
		}
		time.Sleep(10 * time.Second)
	}
}

// SubscribeToManagerTopics subscribes to relevant MQTT topics for Manager and registry interaction.
func SubscribeToManagerTopics(client mqtt.Client, config *Config, startHandler, stopHandler, registryHandler mqtt.MessageHandler, logger *slog.Logger) error {
	// Subscribe to the start command topic
	startTopic := fmt.Sprintf("channels/%s/messages/control/manager/start", config.ChannelID)
	if token := client.Subscribe(startTopic, 0, startHandler); token.Wait() && token.Error() != nil {
		logger.Error("Failed to subscribe to start topic", slog.String("topic", startTopic), slog.Any("error", token.Error()))
		return fmt.Errorf("failed to subscribe to start topic: %w", token.Error())
	}

	// Subscribe to the stop command topic
	stopTopic := fmt.Sprintf("channels/%s/messages/control/manager/stop", config.ChannelID)
	if token := client.Subscribe(stopTopic, 0, stopHandler); token.Wait() && token.Error() != nil {
		logger.Error("Failed to subscribe to stop topic", slog.String("topic", stopTopic), slog.Any("error", token.Error()))
		return fmt.Errorf("failed to subscribe to stop topic: %w", token.Error())
	}

	// Subscribe to the registry update topic
	registryUpdateTopic := fmt.Sprintf("channels/%s/messages/control/manager/updateRegistry", config.ChannelID)
	if token := client.Subscribe(registryUpdateTopic, 0, registryHandler); token.Wait() && token.Error() != nil {
		logger.Error("Failed to subscribe to registry update topic", slog.String("topic", registryUpdateTopic), slog.Any("error", token.Error()))
		return fmt.Errorf("failed to subscribe to registry update topic: %w", token.Error())
	}

	logger.Info("Subscribed to Manager topics",
		slog.String("start_topic", startTopic),
		slog.String("stop_topic", stopTopic),
		slog.String("registry_update_topic", registryUpdateTopic))
	return nil
}

// SubscribeToRegistryTopic subscribes to the Registry Proxy's response topic for chunks.
func SubscribeToRegistryTopic(client mqtt.Client, channelID string, handler mqtt.MessageHandler, logger *slog.Logger) error {
	topic := fmt.Sprintf("channels/%s/messages/registry/server", channelID)
	token := client.Subscribe(topic, 0, handler)
	token.Wait()
	if token.Error() != nil {
		logger.Error("Failed to subscribe to registry topic", slog.String("topic", topic), slog.Any("error", token.Error()))
		return fmt.Errorf("failed to subscribe to registry topic '%s': %w", topic, token.Error())
	}

	logger.Info("Subscribed to registry topic", slog.String("topic", topic))
	return nil
}

// PublishFetchRequest sends a fetch request to the Registry Proxy.
func PublishFetchRequest(client mqtt.Client, channelID string, appName string, logger *slog.Logger) error {
	topic := fmt.Sprintf("channels/%s/messages/registry/proplet", channelID)
	payload := map[string]string{"app_name": appName}
	data, err := json.Marshal(payload)
	if err != nil {
		logger.Error("Failed to marshal fetch request payload", slog.Any("error", err))
		return fmt.Errorf("failed to marshal fetch request payload: %w", err)
	}

	token := client.Publish(topic, 0, false, data)
	token.Wait()
	if token.Error() != nil {
		logger.Error("Failed to publish fetch request", slog.String("topic", topic), slog.Any("error", token.Error()))
		return fmt.Errorf("failed to publish fetch request: %w", token.Error())
	}

	logger.Info("Published fetch request", slog.String("app_name", appName), slog.String("topic", topic))
	return nil
}

// registryUpdate processes registry update commands.
func (p *PropletService) registryUpdate(client mqtt.Client, msg mqtt.Message, logger *slog.Logger) {
	var payload struct {
		RegistryURL   string `json:"registry_url"`
		RegistryToken string `json:"registry_token"`
	}
	if err := json.Unmarshal(msg.Payload(), &payload); err != nil {
		logger.Error("Invalid registry update payload", slog.Any("error", err))
		return
	}

	err := p.UpdateRegistry(context.Background(), payload.RegistryURL, payload.RegistryToken)
	ackTopic := fmt.Sprintf("channels/%s/messages/control/manager/registry", p.config.ChannelID)
	if err != nil {
		client.Publish(ackTopic, 0, false, fmt.Sprintf(`{"status":"failure","error":"%v"}`, err))
		logger.Error("Failed to update registry configuration",
			slog.String("ack_topic", ackTopic),
			slog.String("registry_url", payload.RegistryURL),
			slog.Any("error", err))
	} else {
		client.Publish(ackTopic, 0, false, `{"status":"success"}`)
		logger.Info("App Registry configuration updated successfully",
			slog.String("ack_topic", ackTopic),
			slog.String("registry_url", payload.RegistryURL))
	}
}
