package mqtt

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

const (
	connTimeout    = 10
	reconnTimeout  = 1
	disconnTimeout = 250
)

var (
	errPublishTimeout     = errors.New("failed to publish due to timeout reached")
	errSubscribeTimeout   = errors.New("failed to subscribe due to timeout reached")
	errUnsubscribeTimeout = errors.New("failed to unsubscribe due to timeout reached")
	errEmptyTopic         = errors.New("empty topic")
	errEmptyID            = errors.New("empty ID")

	aliveTopicTemplate = "m/%s/c/%s/control/proplet/alive"
	lwtPayloadTemplate = `{"status":"offline","proplet_id":"%s"}`
)

type pubsub struct {
	client  mqtt.Client
	qos     byte
	timeout time.Duration
	logger  *slog.Logger
}

type Handler func(topic string, msg map[string]interface{}) error

type PubSub interface {
	Publish(ctx context.Context, topic string, msg any) error
	Subscribe(ctx context.Context, topic string, handler Handler) error
	Unsubscribe(ctx context.Context, topic string) error
	Disconnect(ctx context.Context) error
}

func NewPubSub(url string, qos byte, id, username, password, domainID, channelID string, timeout time.Duration, caPath, certPath, keyPath string, logger *slog.Logger) (PubSub, error) {
	if id == "" {
		return nil, errEmptyID
	}

	client, err := newClient(url, id, username, password, domainID, channelID, timeout, caPath, certPath, keyPath, logger)
	if err != nil {
		return nil, err
	}

	return &pubsub{
		client:  client,
		qos:     qos,
		timeout: timeout,
		logger:  logger,
	}, nil
}

func (ps *pubsub) Publish(ctx context.Context, topic string, msg any) error {
	if topic == "" {
		return errEmptyTopic
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	token := ps.client.Publish(topic, ps.qos, false, data)
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

	token := ps.client.Subscribe(topic, ps.qos, ps.mqttHandler(handler))
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

	token := ps.client.Unsubscribe(topic)
	if token.Error() != nil {
		return token.Error()
	}

	if ok := token.WaitTimeout(ps.timeout); !ok {
		return errUnsubscribeTimeout
	}

	return nil
}

func (ps *pubsub) Disconnect(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		ps.client.Disconnect(disconnTimeout)

		return nil
	}
}

func newClient(address, id, username, password, domainID, channelID string, timeout time.Duration, caPath, certPath, keyPath string, logger *slog.Logger) (mqtt.Client, error) {
	opts := mqtt.NewClientOptions().
		AddBroker(address).
		SetClientID(id).
		SetUsername(username).
		SetPassword(password).
		SetCleanSession(true).
		SetAutoReconnect(true).
		SetConnectTimeout(connTimeout * time.Second).
		SetMaxReconnectInterval(reconnTimeout * time.Minute)

	if err := applyTLSConfig(opts, caPath, certPath, keyPath); err != nil {
		return nil, err
	}

	if channelID != "" {
		topic := fmt.Sprintf(aliveTopicTemplate, domainID, channelID)
		lwtPayload := fmt.Sprintf(lwtPayloadTemplate, id)
		opts.SetWill(topic, lwtPayload, 0, false)
	}

	opts.SetOnConnectHandler(func(_ mqtt.Client) {
		logger.Info("MQTT connection established")
	})

	opts.SetConnectionLostHandler(func(_ mqtt.Client, err error) {
		args := []any{}
		if err != nil {
			args = append(args, slog.Any("error", err))
		}

		logger.Info("MQTT connection lost", args...)
	})

	opts.SetReconnectingHandler(func(_ mqtt.Client, options *mqtt.ClientOptions) {
		args := []any{}
		if options != nil {
			args = append(args,
				slog.String("client_id", options.ClientID),
				slog.String("username", options.Username),
			)
		}

		logger.Info("MQTT reconnecting", args...)
	})

	client := mqtt.NewClient(opts)

	token := client.Connect()
	if token.Error() != nil {
		return nil, errors.Join(errors.New("failed to connect to MQTT broker"), token.Error())
	}

	if ok := token.WaitTimeout(timeout); !ok {
		return nil, errors.New("timeout reached while connecting to MQTT broker")
	}

	return client, nil
}

func applyTLSConfig(opts *mqtt.ClientOptions, caPath, certPath, keyPath string) error {
	if caPath == "" {
		return nil
	}

	caCert, err := os.ReadFile(caPath)
	if err != nil {
		return fmt.Errorf("failed to read CA certificate: %w", err)
	}

	caCertPool := x509.NewCertPool()
	if ok := caCertPool.AppendCertsFromPEM(caCert); !ok {
		return errors.New("failed to parse CA certificate")
	}

	tlsConfig := &tls.Config{
		InsecureSkipVerify: false,
		RootCAs:            caCertPool,
	}

	if certPath != "" && keyPath != "" {
		cert, err := tls.LoadX509KeyPair(certPath, keyPath)
		if err != nil {
			return fmt.Errorf("failed to load client key pair: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	opts.SetTLSConfig(tlsConfig)

	return nil
}

func (ps *pubsub) mqttHandler(h Handler) mqtt.MessageHandler {
	return func(_ mqtt.Client, m mqtt.Message) {
		var msg map[string]interface{}
		if err := json.Unmarshal(m.Payload(), &msg); err != nil {
			ps.logger.Warn(fmt.Sprintf("Failed to unmarshal received message: %s", err))

			return
		}

		if err := h(m.Topic(), msg); err != nil {
			ps.logger.Warn(fmt.Sprintf("Failed to handle MQTT message: %s", err))
		}
	}
}
