package mqtt

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/absmach/propeller/proxy"
	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type RegistryClient struct {
	client      mqtt.Client
	config      *proxy.MQTTProxyConfig
	messageChan chan string
}

func NewMQTTClient(config *proxy.MQTTProxyConfig) (*RegistryClient, error) {
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

	regClient := &RegistryClient{
		client:      client,
		config:      config,
		messageChan: make(chan string, 1),
	}

	return regClient, nil
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

	return c.subscribe(ctx)
}

func (c *RegistryClient) subscribe(ctx context.Context) error {
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
			log.Fatalf("failed unmarshalling")
			return
		}
		packageName := payLoad.Appname
		select {
		case c.messageChan <- packageName:
			log.Printf("Received package name: %s", packageName)
		case <-ctx.Done():
			return
		default:
			log.Println("Package name channel full, dropping message")
		}
	}

	token := c.client.Subscribe(subTopic, 1, handler)
	if err := token.Error(); err != nil {
		return fmt.Errorf("failed to subscribe to %s: %w", subTopic, err)
	}

	return nil
}

// PublishOCIContainer publishes OCI container information
func (c *RegistryClient) PublishOCIContainer(ctx context.Context, containerData []byte) error {
	pubTopic := fmt.Sprintf("channels/%s/messages/registry/server", c.config.ChannelID)

	token := c.client.Publish(pubTopic, 1, false, containerData)

	select {
	case <-token.Done():
		if err := token.Error(); err != nil {
			return fmt.Errorf("failed to publish OCI container: %w", err)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *RegistryClient) WaitForPackageName(ctx context.Context) (string, error) {
	select {
	case packageName := <-c.messageChan:
		return packageName, nil
	case <-ctx.Done():
		return "", ctx.Err()
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
