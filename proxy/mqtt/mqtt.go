package mqtt

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/absmach/propeller/proxy/config"
	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type RegistryClient struct {
	client mqtt.Client
	config *config.MQTTProxyConfig
}

func NewMQTTClient(config *config.MQTTProxyConfig) (*RegistryClient, error) {
	opts := mqtt.NewClientOptions().
		AddBroker(config.BrokerURL).
		SetClientID(fmt.Sprintf("Proplet-%s", config.PropletID)).
		SetUsername(config.PropletID).
		SetPassword(config.Password).
		SetCleanSession(true).
		SetAutoReconnect(true).
		SetConnectTimeout(10 * time.Second).
		SetMaxReconnectInterval(1 * time.Minute)

	opts.SetConnectionLostHandler(func(client mqtt.Client, err error) {
		log.Printf("MQTT connection lost: %v\n", err)
	})

	opts.SetReconnectingHandler(func(client mqtt.Client, options *mqtt.ClientOptions) {
		log.Println("MQTT reconnecting...")
	})

	client := mqtt.NewClient(opts)

	return &RegistryClient{
		client: client,
		config: config,
	}, nil
}

func (c *RegistryClient) Connect(ctx context.Context) error {
	token := c.client.Connect()

	select {
	case <-token.Done():
		if err := token.Error(); err != nil {
			return fmt.Errorf("MQTT connection failed: %w", err)
		}
	case <-ctx.Done():
		return ctx.Err()
	}

	return nil
}

func (c *RegistryClient) Subscribe(ctx context.Context, containerChan chan<- string) error {
	// Subscribe to container requests
	subTopic := fmt.Sprintf("channels/%s/message/registry/proplet", c.config.ChannelID)

	handler := func(client mqtt.Client, msg mqtt.Message) {
		data := msg.Payload()

		var payLoad = struct {
			Appname string `json:"app_name"`
		}{
			Appname: "",
		}

		err := json.Unmarshal(data, &payLoad)
		if err != nil {
			log.Printf("failed unmarshalling: %v", err)
			return
		}

		select {
		case containerChan <- payLoad.Appname:
			log.Printf("Received container request: %s", payLoad.Appname)
		case <-ctx.Done():
			return
		default:
			log.Println("Channel full, dropping container request")
		}
	}

	token := c.client.Subscribe(subTopic, 1, handler)
	if err := token.Error(); err != nil {
		return fmt.Errorf("failed to subscribe to %s: %w", subTopic, err)
	}

	return nil
}

// PublishContainer publishes container data to the server channel
func (c *RegistryClient) PublishContainer(ctx context.Context, containerData []byte) error {
	pubTopic := fmt.Sprintf("channels/%s/messages/registry/server", c.config.ChannelID)

	token := c.client.Publish(pubTopic, 1, false, containerData)

	select {
	case <-token.Done():
		if err := token.Error(); err != nil {
			return fmt.Errorf("failed to publish container: %w", err)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *RegistryClient) Disconnect(ctx context.Context) error {
	disconnectChan := make(chan error, 1)

	go func() {
		c.client.Disconnect(250)
		disconnectChan <- nil
	}()

	select {
	case err := <-disconnectChan:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
