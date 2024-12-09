package proplet

import (
	"fmt"
	"net/url"
	"time"

	pkgerrors "github.com/absmach/propeller/pkg/errors"
	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// NewMQTTClient initializes a new MQTT client with LWT and starts liveliness updates.
func NewMQTTClient(config *Config) (mqtt.Client, error) {
	if err := validateConfig(config); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	// Last Will and Testament payload
	lwtPayload := fmt.Sprintf(`{"status":"offline","proplet_id":"%s","chan_id":"%s"}`, config.PropletID, config.ChannelID)
	if lwtPayload == "" {
		return nil, fmt.Errorf("failed to prepare MQTT last will payload: %w", pkgerrors.ErrMQTTWillPayloadFailed)
	}

	// MQTT client options
	opts := mqtt.NewClientOptions().
		AddBroker(config.BrokerURL).
		SetClientID(fmt.Sprintf("Proplet-%s", config.PropletID)).
		SetUsername("token").
		SetPassword(config.Token).
		SetCleanSession(true).
		SetWill(fmt.Sprintf("channels/%s/messages/control/proplet/alive", config.ChannelID), lwtPayload, 0, false)

	// Handlers for connection events
	opts.SetConnectionLostHandler(func(client mqtt.Client, err error) {
		fmt.Printf("MQTT connection lost: %v\n", err)
	})

	opts.SetReconnectingHandler(func(client mqtt.Client, options *mqtt.ClientOptions) {
		fmt.Println("MQTT reconnecting...")
	})

	// Create and connect the client
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
		return fmt.Errorf("failed to publish discovery message to topic '%s': %w", topic, pkgerrors.ErrPublishDiscovery)
	}

	fmt.Printf("Discovery message published to topic '%s'\n", topic)
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
