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

const (
	connTimeout    = 10
	reconnTimeout  = 1
	disconnTimeout = 250
	pubTopic       = "channels/%s/messages/registry/server"
	subTopic       = "channels/%s/messages/registry/proplet"
)

type RegistryClient struct {
	client mqtt.Client
	config *config.MQTTProxyConfig
}

func NewMQTTClient(cfg *config.MQTTProxyConfig) (*RegistryClient, error) {
	opts := mqtt.NewClientOptions().
		AddBroker(cfg.BrokerURL).
		SetClientID("Proplet-" + cfg.PropletID).
		SetUsername(cfg.PropletID).
		SetPassword(cfg.Password).
		SetCleanSession(true).
		SetAutoReconnect(true).
		SetConnectTimeout(connTimeout * time.Second).
		SetMaxReconnectInterval(reconnTimeout * time.Minute)

	opts.SetConnectionLostHandler(func(client mqtt.Client, err error) {
		log.Printf("MQTT connection lost: %v\n", err)
	})

	opts.SetReconnectingHandler(func(client mqtt.Client, options *mqtt.ClientOptions) {
		log.Println("MQTT reconnecting...")
	})

	opts.SetOnConnectHandler(func(client mqtt.Client) {
		log.Println("MQTT connection established successfully")
	})

	client := mqtt.NewClient(opts)

	return &RegistryClient{
		client: client,
		config: cfg,
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
	handler := func(client mqtt.Client, msg mqtt.Message) {
		data := msg.Payload()

		payLoad := struct {
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

	x := fmt.Sprintf(subTopic, c.config.ChannelID)
	log.Println(x)

	token := c.client.Subscribe(fmt.Sprintf(subTopic, c.config.ChannelID), 1, handler)
	if err := token.Error(); err != nil {
		return fmt.Errorf("failed to subscribe to %s: %w", subTopic, err)
	}

	return nil
}

func (c *RegistryClient) PublishContainer(ctx context.Context, chunk config.ChunkPayload) error {
	data, err := json.Marshal(chunk)
	if err != nil {
		return fmt.Errorf("failed to marshal chunk payload: %w", err)
	}

	token := c.client.Publish(fmt.Sprintf(pubTopic, c.config.ChannelID), 1, false, data)

	select {
	case <-token.Done():
		if err := token.Error(); err != nil {
			return fmt.Errorf("failed to publish container chunk: %w", err)
		}

		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *RegistryClient) Disconnect(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		c.client.Disconnect(disconnTimeout)

		return nil
	}
}
