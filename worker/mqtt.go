package worker

import (
	"fmt"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// NewMQTTClient initializes a new MQTT client with LWT.
func NewMQTTClient(config *Config) (mqtt.Client, error) {
	topic := fmt.Sprintf("channels/%s/messages/proplets", config.ChannelID)
	willPayload := fmt.Sprintf(`{"status":"offline","PropletID":"%s","ChanID":"%s"}`, config.PropletID, config.ChannelID)

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
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		return nil, token.Error()
	}

	return client, nil
}
