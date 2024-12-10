package proxy

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/absmach/propeller/proxy/config"
	orasHTTP "github.com/absmach/propeller/proxy/http"
	"github.com/absmach/propeller/proxy/mqtt"
)

type ProxyService struct {
	orasconfig    orasHTTP.Config
	mqttClient    *mqtt.RegistryClient
	logger        *slog.Logger
	containerChan chan string
	dataChan      chan []byte
}

func NewService(ctx context.Context, cfg *config.MQTTProxyConfig, logger *slog.Logger) (*ProxyService, error) {
	mqttClient, err := mqtt.NewMQTTClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize MQTT client: %w", err)
	}

	config, err := orasHTTP.Init()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize oras http client: %w", err)
	}

	return &ProxyService{
		orasconfig:    *config,
		mqttClient:    mqttClient,
		logger:        logger,
		containerChan: make(chan string, 1),
		dataChan:      make(chan []byte, 1),
	}, nil
}

func (s *ProxyService) Start(ctx context.Context) error {
	errs := make(chan error, 2)

	if err := s.mqttClient.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect to MQTT broker: %w", err)
	}
	defer s.mqttClient.Disconnect(ctx)

	if err := s.mqttClient.Subscribe(ctx, s.containerChan); err != nil {
		return fmt.Errorf("failed to subscribe to container requests: %w", err)
	}

	go s.streamHTTP(ctx, errs)
	go s.streamMQTT(ctx, errs)

	return <-errs
}

func (s *ProxyService) streamHTTP(ctx context.Context, errs chan error) {
	for {
		select {
		case <-ctx.Done():
			errs <- ctx.Err()
			return
		case containerName := <-s.containerChan:
			data, err := s.orasconfig.FetchFromReg(ctx, containerName)
			if err != nil {
				s.logger.Error("failed to fetch container", "container", containerName, "error", err)
				continue
			}

			select {
			case s.dataChan <- data:
				s.logger.Info("sent container data to MQTT stream", "container", containerName)
			case <-ctx.Done():
				errs <- ctx.Err()
				return
			}
		}
	}
}

func (s *ProxyService) streamMQTT(ctx context.Context, errs chan error) {
	for {
		select {
		case <-ctx.Done():
			errs <- ctx.Err()
			return
		case data := <-s.dataChan:
			if err := s.mqttClient.PublishContainer(ctx, data); err != nil {
				s.logger.Error("failed to publish container data", "error", err)
				continue
			}
			s.logger.Info("published container data")
		}
	}
}
