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
	// Validate broker URL
	if _, err := url.ParseRequestURI(config.BrokerURL); err != nil {
		return nil, fmt.Errorf("invalid MQTT broker URL '%s': %w", config.BrokerURL, pkgerrors.ErrMQTTInvalidBrokerURL)
	}

	// Topics
	createTopic := fmt.Sprintf("channels/%s/messages/control/proplet/create", config.ChannelID)
	aliveTopic := fmt.Sprintf("channels/%s/messages/control/proplet/alive", config.ChannelID)

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
		SetWill(aliveTopic, lwtPayload, 0, false)

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

	// Publish to the "create" topic to announce the Proplet's existence
	createPayload := fmt.Sprintf(`{"proplet_id":"%s","chan_id":"%s"}`, config.PropletID, config.ChannelID)
	client.Publish(createTopic, 0, false, createPayload)

	// Start liveliness updates
	go startLivelinessUpdates(client, config, aliveTopic)

	return client, nil
}

// startLivelinessUpdates sends periodic "alive" messages to the MQTT broker.
func startLivelinessUpdates(client mqtt.Client, config *Config, aliveTopic string) {
	for {
		alivePayload := fmt.Sprintf(`{"status":"alive","proplet_id":"%s","chan_id":"%s"}`, config.PropletID, config.ChannelID)
		token := client.Publish(aliveTopic, 0, false, alivePayload)
		token.Wait()
		if token.Error() != nil {
			fmt.Printf("Failed to publish liveliness: %v\n", token.Error())
		}

		time.Sleep(10 * time.Second) // Publish every 10 seconds
	}
}
