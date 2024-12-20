package proxy

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/absmach/propeller/proplet"
	"github.com/absmach/propeller/task"
)

const chunkBuffer = 10

type ProxyService struct {
	orasconfig    *HTTPProxyConfig
	mqttClient    *RegistryClient
	logger        *slog.Logger
	containerChan chan task.URLValue
	dataChan      chan proplet.ChunkPayload
}

func NewService(ctx context.Context, mqttCfg *MQTTProxyConfig, httpCfg *HTTPProxyConfig, logger *slog.Logger) (*ProxyService, error) {
	mqttClient, err := NewMQTTClient(mqttCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize MQTT client: %w", err)
	}

	return &ProxyService{
		orasconfig:    httpCfg,
		mqttClient:    mqttClient,
		logger:        logger,
		containerChan: make(chan task.URLValue, 1),
		dataChan:      make(chan proplet.ChunkPayload, chunkBuffer),
	}, nil
}

func (s *ProxyService) MQTTClient() *RegistryClient {
	return s.mqttClient
}

func (s *ProxyService) ContainerChan() chan task.URLValue {
	return s.containerChan
}

func (s *ProxyService) StreamHTTP(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case containerName := <-s.containerChan:
			chunks, err := s.orasconfig.FetchFromReg(ctx, containerName, s.orasconfig.ChunkSize)
			if err != nil {
				s.logger.Error("failed to fetch container",
					slog.Any("container name", containerName),
					slog.Any("error", err))

				continue
			}

			// Send each chunk through the data channel
			for _, chunk := range chunks {
				select {
				case s.dataChan <- chunk:
					s.logger.Info("sent container chunk to MQTT stream",
						slog.Any("container", containerName),
						slog.Int("chunk", chunk.ChunkIdx),
						slog.Int("total", chunk.TotalChunks))
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}
	}
}

func (s *ProxyService) StreamMQTT(ctx context.Context) error {
	containerChunks := make(map[string]int)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case chunk := <-s.dataChan:
			if err := s.mqttClient.PublishContainer(ctx, chunk); err != nil {
				s.logger.Error("failed to publish container chunk",
					slog.Any("error", err),
					slog.Int("chunk", chunk.ChunkIdx),
					slog.Int("total", chunk.TotalChunks))

				continue
			}

			containerChunks[chunk.AppName]++

			if containerChunks[chunk.AppName] == chunk.TotalChunks {
				s.logger.Info("successfully sent all chunks",
					slog.String("container", chunk.AppName),
					slog.Int("total_chunks", chunk.TotalChunks))
				delete(containerChunks, chunk.AppName)
			}
		}
	}
}
