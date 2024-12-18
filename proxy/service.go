package proxy

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/absmach/propeller/proplet"
	"github.com/absmach/propeller/proxy/config"
	"github.com/absmach/propeller/proxy/mqtt"
)

const chunkBuffer = 10

type ProxyService struct {
	orasconfig    *config.HTTPProxyConfig
	mqttClient    *mqtt.RegistryClient
	logger        *slog.Logger
	containerChan chan string
	dataChan      chan proplet.ChunkPayload
}

func NewService(ctx context.Context, mqttCfg *config.MQTTProxyConfig, httpCfg *config.HTTPProxyConfig, logger *slog.Logger) (*ProxyService, error) {
	mqttClient, err := mqtt.NewMQTTClient(mqttCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize MQTT client: %w", err)
	}

	logger.Info("successfully initialized MQTT client")

	return &ProxyService{
		orasconfig:    httpCfg,
		mqttClient:    mqttClient,
		logger:        logger,
		containerChan: make(chan string, 1),
		dataChan:      make(chan proplet.ChunkPayload, chunkBuffer),
	}, nil
}

func (s *ProxyService) MQTTClient() *mqtt.RegistryClient {
	return s.mqttClient
}

func (s *ProxyService) ContainerChan() chan string {
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
				s.logger.Error("failed to fetch container", "container", containerName, "error", err)

				continue
			}

			// Send each chunk through the data channel
			for _, chunk := range chunks {
				select {
				case s.dataChan <- chunk:
					s.logger.Info("sent container chunk to MQTT stream",
						"container", containerName,
						"chunk", chunk.ChunkIdx,
						"total", chunk.TotalChunks)
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
					"error", err,
					"chunk", chunk.ChunkIdx,
					"total", chunk.TotalChunks)

				continue
			}

			s.logger.Info("published container chunk",
				"chunk_name", chunk.AppName,
				"chunk_no", chunk.ChunkIdx,
				"total", chunk.TotalChunks)

			containerChunks[chunk.AppName]++

			if containerChunks[chunk.AppName] == chunk.TotalChunks {
				s.logger.Info("successfully sent all chunks",
					"container", chunk.AppName,
					"total_chunks", chunk.TotalChunks)
				delete(containerChunks, chunk.AppName)
			}
		}
	}
}
