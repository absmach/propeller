package proplet

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/absmach/propeller/pkg/mqtt"
)

const (
	livelinessInterval = 10 * time.Second
	mqttTimeout        = 30 * time.Second
	qos                = 0
)

var (
	RegistryFailurePayload      = `{"status":"failure","error":"%v"}`
	RegistrySuccessPayload      = `{"status":"success"}`
	RegistryUpdateRequestTopic  = "channels/%s/messages/control/manager/updateRegistry"
	RegistryUpdateResponseTopic = "channels/%s/messages/control/proplet/updateRegistry"
	AliveTopic                  = "channels/%s/messages/control/proplet/alive"
	AlivePayload                = `{"status":"alive","proplet_id":"%s","chan_id":"%s"}`
	DiscoveryTopic              = "channels/%s/messages/control/proplet/create"
	DiscoveryPayload            = `{"proplet_id":"%s","chan_id":"%s"}`
	LWTTopic                    = "channels/%s/messages/control/proplet"
	LWTPayload                  = `{"status":"offline","proplet_id":"%s","chan_id":"%s"}`
	StartTopic                  = "channels/%s/messages/control/manager/start"
	StopTopic                   = "channels/%s/messages/control/manager/stop"
	RegistryResponseTopic       = "channels/%s/messages/registry/server"
	RegistryRequestTopic        = "channels/%s/messages/registry/proplet"
)

type MQTTService struct {
	pubsub mqtt.PubSub
	config Config
	logger *slog.Logger
}

func NewMQTTService(ctx context.Context, config Config, logger *slog.Logger) (*MQTTService, error) {
	lwtTopic := fmt.Sprintf(LWTTopic, config.ChannelID)
	lwtPayload := map[string]string{
		"status":     "offline",
		"proplet_id": config.PropletID,
		"chan_id":    config.ChannelID,
	}

	pubsub, err := mqtt.NewPubSub(
		config.BrokerURL,
		qos,
		"Proplet-"+config.PropletID,
		config.PropletID,
		config.Password,
		mqttTimeout,
		lwtTopic,
		lwtPayload,
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize MQTT PubSub: %w", err)
	}

	service := &MQTTService{
		pubsub: pubsub,
		config: config,
		logger: logger,
	}

	if err := service.PublishDiscoveryMessage(ctx); err != nil {
		logger.Error("Failed to publish discovery message", slog.Any("error", err))

		return nil, err
	}

	go service.StartLivelinessUpdates(ctx)

	return service, nil
}

func (m *MQTTService) StartLivelinessUpdates(ctx context.Context) {
	ticker := time.NewTicker(livelinessInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			err := m.pubsub.Publish(ctx, fmt.Sprintf(AliveTopic, m.config.ChannelID), map[string]string{
				"status":     "alive",
				"proplet_id": m.config.PropletID,
				"chan_id":    m.config.ChannelID,
			})
			if err != nil {
				m.logger.Error("Failed to publish liveliness message", slog.Any("error", err))
			} else {
				m.logger.Info("Published liveliness message")
			}
		}
	}
}

func (m *MQTTService) PublishDiscoveryMessage(ctx context.Context) error {
	topic := fmt.Sprintf(DiscoveryTopic, m.config.ChannelID)
	payload := map[string]string{
		"proplet_id": m.config.PropletID,
		"chan_id":    m.config.ChannelID,
	}
	if err := m.pubsub.Publish(ctx, topic, payload); err != nil {
		return fmt.Errorf("failed to publish discovery message: %w", err)
	}
	m.logger.Info("Discovery message published successfully")

	return nil
}

func (m *MQTTService) SubscribeToManagerTopics(ctx context.Context, startHandler, stopHandler, registryHandler mqtt.Handler) error {
	handlers := map[string]mqtt.Handler{
		fmt.Sprintf(StartTopic, m.config.ChannelID):                 startHandler,
		fmt.Sprintf(StopTopic, m.config.ChannelID):                  stopHandler,
		fmt.Sprintf(RegistryUpdateRequestTopic, m.config.ChannelID): registryHandler,
	}
	for topic, handler := range handlers {
		if err := m.pubsub.Subscribe(ctx, topic, handler); err != nil {
			return fmt.Errorf("failed to subscribe to topic %s: %w", topic, err)
		}
	}

	return nil
}

func (m *MQTTService) SubscribeToRegistryTopic(ctx context.Context, handler mqtt.Handler) error {
	topic := fmt.Sprintf(RegistryResponseTopic, m.config.ChannelID)
	if err := m.pubsub.Subscribe(ctx, topic, handler); err != nil {
		return fmt.Errorf("failed to subscribe to registry topic: %w", err)
	}

	return nil
}

func (m *MQTTService) PublishFetchRequest(ctx context.Context, appName string) error {
	topic := fmt.Sprintf(RegistryRequestTopic, m.config.ChannelID)
	payload := map[string]string{"app_name": appName}
	if err := m.pubsub.Publish(ctx, topic, payload); err != nil {
		return fmt.Errorf("failed to publish fetch request: %w", err)
	}
	m.logger.Info("Fetch request published successfully")

	return nil
}

func (m *MQTTService) Close() error {
	return m.pubsub.Close()
}
