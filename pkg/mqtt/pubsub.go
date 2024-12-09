package mqtt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

var (
	errConnect            = errors.New("failed to connect to MQTT broker")
	errPublishTimeout     = errors.New("failed to publish due to timeout reached")
	errSubscribeTimeout   = errors.New("failed to subscribe due to timeout reached")
	errUnsubscribeTimeout = errors.New("failed to unsubscribe due to timeout reached")
	errEmptyTopic         = errors.New("empty topic")
	errEmptyID            = errors.New("empty ID")
)

type pubsub struct {
	address  string
	qos      byte
	id       string
	username string
	password string
	timeout  time.Duration
	logger   *slog.Logger
}

type Handler func(topic string, msg map[string]interface{}) error

type PubSub interface {
	Publish(ctx context.Context, topic string, msg any) error
	Subscribe(ctx context.Context, topic string, handler Handler) error
	Unsubscribe(ctx context.Context, topic string) error
}

func NewPubSub(url string, qos byte, id, username, password string, timeout time.Duration, logger *slog.Logger) (PubSub, error) {
	if id == "" {
		return nil, errEmptyID
	}

	return &pubsub{
		address:  url,
		qos:      qos,
		id:       id,
		username: username,
		password: password,
		timeout:  timeout,
		logger:   logger,
	}, nil
}

func (ps *pubsub) Publish(ctx context.Context, topic string, msg any) error {
	if topic == "" {
		return errEmptyTopic
	}

	client, err := newClient(ps.address, ps.id, ps.username, ps.password, ps.timeout)
	if err != nil {
		return err
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	token := client.Publish(topic, ps.qos, false, data)
	if token.Error() != nil {
		return token.Error()
	}

	if ok := token.WaitTimeout(ps.timeout); !ok {
		return errPublishTimeout
	}

	return nil
}

func (ps *pubsub) Subscribe(ctx context.Context, topic string, handler Handler) error {
	if topic == "" {
		return errEmptyTopic
	}

	client, err := newClient(ps.address, ps.id, ps.username, ps.password, ps.timeout)
	if err != nil {
		return err
	}

	token := client.Subscribe(topic, ps.qos, ps.mqttHandler(handler))
	if token.Error() != nil {
		return token.Error()
	}
	if ok := token.WaitTimeout(ps.timeout); !ok {
		return errSubscribeTimeout
	}

	return nil
}

func (ps *pubsub) Unsubscribe(ctx context.Context, topic string) error {
	if topic == "" {
		return errEmptyTopic
	}

	client, err := newClient(ps.address, ps.id, ps.username, ps.password, ps.timeout)
	if err != nil {
		return err
	}

	token := client.Unsubscribe(topic)
	if token.Error() != nil {
		return token.Error()
	}

	if ok := token.WaitTimeout(ps.timeout); !ok {
		return errUnsubscribeTimeout
	}

	return nil
}

func (ps *pubsub) Close() error {
	return nil
}

func newClient(address, id, username, password string, timeout time.Duration) (mqtt.Client, error) {
	opts := mqtt.NewClientOptions().
		SetUsername(username).
		SetPassword(password).
		AddBroker(address).
		SetClientID(id)
	client := mqtt.NewClient(opts)

	token := client.Connect()
	if token.Error() != nil {
		return nil, token.Error()
	}

	if ok := token.WaitTimeout(timeout); !ok {
		return nil, errConnect
	}

	return client, nil
}

func (ps *pubsub) mqttHandler(h Handler) mqtt.MessageHandler {
	return func(_ mqtt.Client, m mqtt.Message) {
		var msg map[string]interface{}
		if err := json.Unmarshal(m.Payload(), &msg); err != nil {
			ps.logger.Warn(fmt.Sprintf("Failed to unmarshal received message: %s", err))

			return
		}

		if err := h(m.Topic(), msg); err != nil {
			ps.logger.Warn(fmt.Sprintf("Failed to handle Magistrala message: %s", err))
		}

		m.Ack()
	}
}
