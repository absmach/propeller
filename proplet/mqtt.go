package proplet

import (
	"encoding/json"
	"fmt"
	"net/url"
	"time"

	pkgerrors "github.com/absmach/propeller/pkg/errors"
	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// NewMQTTClient initializes a new MQTT client with LWT and liveliness updates.
func NewMQTTClient(config *Config) (mqtt.Client, error) {
	if err := validateConfig(config); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	// Prepare LWT payload
	lwtPayload := fmt.Sprintf(`{"status":"offline","proplet_id":"%s","chan_id":"%s"}`, config.PropletID, config.ChannelID)
	if lwtPayload == "" {
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
		fmt.Printf("MQTT connection lost: %v\n", err)
	})

	opts.SetReconnectingHandler(func(client mqtt.Client, options *mqtt.ClientOptions) {
		fmt.Println("MQTT reconnecting...")
	})

	client := mqtt.NewClient(opts)
	token := client.Connect()
	if token.Wait() && token.Error() != nil {
		return nil, fmt.Errorf("failed to connect to MQTT broker '%s': %w", config.BrokerURL, pkgerrors.ErrMQTTConnectionFailed)
	}

	fmt.Println("MQTT client connected successfully.")

	// Publish discovery message
	if err := PublishDiscovery(client, config); err != nil {
		return nil, fmt.Errorf("failed to publish discovery message: %w", err)
	}

	// Start liveliness updates
	go startLivelinessUpdates(client, config)

	return client, nil
}

// validateConfig ensures that the provided configuration is valid and complete.
func validateConfig(config *Config) error {
	if config == nil {
		return fmt.Errorf("config is nil: %w", pkgerrors.ErrConfigValidation)
	}
	if config.ChannelID == "" {
		return fmt.Errorf("ChannelID is missing: %w", pkgerrors.ErrMissingChannelID)
	}
	if config.PropletID == "" {
		return fmt.Errorf("PropletID is missing: %w", pkgerrors.ErrMissingPropletID)
	}
	if config.BrokerURL == "" {
		return fmt.Errorf("BrokerURL is missing: %w", pkgerrors.ErrMissingValue)
	}
	if _, err := url.ParseRequestURI(config.BrokerURL); err != nil {
		return fmt.Errorf("invalid broker URL '%s': %w", config.BrokerURL, pkgerrors.ErrInvalidValue)
	}
	return nil
}

// publishDiscovery sends an initial "create" message to announce the Proplet's existence.
func PublishDiscovery(client mqtt.Client, config *Config) error {
	topic := fmt.Sprintf("channels/%s/messages/control/proplet/create", config.ChannelID)
	payload := fmt.Sprintf(`{"proplet_id":"%s","chan_id":"%s"}`, config.PropletID, config.ChannelID)
	token := client.Publish(topic, 0, false, payload)
	token.Wait()
	if token.Error() != nil {
		return fmt.Errorf("failed to publish discovery message: %w", token.Error())
	}
	fmt.Printf("Published discovery message to topic '%s'\n", topic)
	return nil
}

// startLivelinessUpdates sends periodic "alive" messages to the MQTT broker.
func startLivelinessUpdates(client mqtt.Client, config *Config) {
	for {
		topic := fmt.Sprintf("channels/%s/messages/control/proplet/alive", config.ChannelID)
		payload := fmt.Sprintf(`{"status":"alive","proplet_id":"%s","chan_id":"%s"}`, config.PropletID, config.ChannelID)
		token := client.Publish(topic, 0, false, payload)
		token.Wait()
		if token.Error() != nil {
			fmt.Printf("Failed to publish liveliness: %v\n", token.Error())
		}
		time.Sleep(10 * time.Second)
	}
}

// SubscribeToTopics subscribes to relevant MQTT topics for commands and registry interaction.
func SubscribeToTopics(client mqtt.Client, config *Config, commandHandler, registryHandler mqtt.MessageHandler) error {
	controlTopic := fmt.Sprintf("channels/%s/messages/control/manager", config.ChannelID)
	if token := client.Subscribe(controlTopic, 0, commandHandler); token.Wait() && token.Error() != nil {
		return fmt.Errorf("failed to subscribe to control topic: %w", token.Error())
	}

	registryTopic := fmt.Sprintf("channels/%s/messages/registry/proplet", config.ChannelID)
	if token := client.Subscribe(registryTopic, 0, registryHandler); token.Wait() && token.Error() != nil {
		return fmt.Errorf("failed to subscribe to registry topic: %w", token.Error())
	}

	fmt.Println("Subscribed to control and registry topics.")
	return nil
}

// PublishFetchRequest sends a fetch request to the Registry Proxy.
func PublishFetchRequest(client mqtt.Client, channelID string, appName string) error {
	topic := fmt.Sprintf("channels/%s/messages/registry/proplet/fetch", channelID)
	payload := map[string]string{"app_name": appName}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal fetch request payload: %w", err)
	}

	token := client.Publish(topic, 0, false, data)
	token.Wait()
	if token.Error() != nil {
		return fmt.Errorf("failed to publish fetch request: %w", token.Error())
	}

	fmt.Printf("Published fetch request for app '%s' to topic '%s'\n", appName, topic)
	return nil
}

// SubscribeToRegistryChunks subscribes to the Registry Proxy's response topic for chunks.
func SubscribeToRegistryChunks(client mqtt.Client, channelID string, handler mqtt.MessageHandler) error {
	topic := fmt.Sprintf("channels/%s/messages/registry/server", channelID)
	token := client.Subscribe(topic, 0, handler)
	token.Wait()
	if token.Error() != nil {
		return fmt.Errorf("failed to subscribe to registry server topic: %w", token.Error())
	}

	fmt.Printf("Subscribed to registry server chunks on topic '%s'\n", topic)
	return nil
}
