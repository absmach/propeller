package proplet

import (
	"fmt"
	"net/url"

	pkgerrors "github.com/absmach/propeller/pkg/errors"
	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// NewMQTTClient initializes a new MQTT client with LWT.
func NewMQTTClient(config *Config) (mqtt.Client, error) {
	if _, err := url.ParseRequestURI(config.BrokerURL); err != nil {
		return nil, fmt.Errorf("invalid MQTT broker URL '%s': %w", config.BrokerURL, pkgerrors.ErrMQTTInvalidBrokerURL)
	}

	topic := fmt.Sprintf("channels/%s/messages/proplets", config.ChannelID)
	willPayload := fmt.Sprintf(`{"status":"offline","PropletID":"%s","ChanID":"%s"}`, config.PropletID, config.ChannelID)
	if willPayload == "" {
		return nil, fmt.Errorf("failed to prepare MQTT last will payload: %w", pkgerrors.ErrMQTTWillPayloadFailed)
	}

	opts := mqtt.NewClientOptions().
		AddBroker(config.BrokerURL).
		SetClientID(fmt.Sprintf("Proplet-%s", config.PropletID)).
		SetUsername("token").
		SetPassword(config.Token).
		SetCleanSession(true).
		SetWill(topic, willPayload, 0, false)

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
	return client, nil
}
