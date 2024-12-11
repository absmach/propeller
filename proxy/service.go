package proxy

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/absmach/propeller/proxy/config"
	"github.com/absmach/propeller/proxy/mqtt"
)

type ProxyService struct {
	orasconfig    *config.HTTPProxyConfig
	mqttClient    *mqtt.RegistryClient
	logger        *slog.Logger
	containerChan chan string
	dataChan      chan []byte
}

func NewService(ctx context.Context, cfgM *config.MQTTProxyConfig, cfgH *config.HTTPProxyConfig, logger *slog.Logger) (*ProxyService, error) {
	mqttClient, err := mqtt.NewMQTTClient(cfgM)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize MQTT client: %w", err)
	}

	return &ProxyService{
		orasconfig:    cfgH,
		mqttClient:    mqttClient,
		logger:        logger,
		containerChan: make(chan string, 1),
		dataChan:      make(chan []byte, 1),
	}, nil
}

func (s *ProxyService) MQTTClient() *mqtt.RegistryClient {
	return s.mqttClient
}

func (s *ProxyService) ContainerChan() chan string {
	return s.containerChan
}

func (s *ProxyService) StreamHTTP(ctx context.Context, errs chan error) {
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

func (s *ProxyService) StreamMQTT(ctx context.Context, errs chan error) {
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
