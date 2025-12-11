package proxy

import (
	"context"
	"fmt"
	"log/slog"

	pkgmqtt "github.com/absmach/propeller/pkg/mqtt"
	"github.com/absmach/propeller/proplet"
)

const (
	chunkBuffer = 10

	connTimeout    = 10
	reconnTimeout  = 1
	disconnTimeout = 250
	PubTopic       = "m/%s/c/%s/registry/server"
	SubTopic       = "m/%s/c/%s/registry/proplet"
)

type ProxyService struct {
	orasconfig    HTTPProxyConfig
	pubsub        pkgmqtt.PubSub
	domainID      string
	channelID     string
	logger        *slog.Logger
	containerChan chan string
	dataChan      chan proplet.ChunkPayload
}

func NewService(ctx context.Context, pubsub pkgmqtt.PubSub, domainID, channelID string, httpCfg HTTPProxyConfig, logger *slog.Logger) (*ProxyService, error) {
	return &ProxyService{
		orasconfig:    httpCfg,
		pubsub:        pubsub,
		domainID:      domainID,
		channelID:     channelID,
		logger:        logger,
		containerChan: make(chan string, 1),
		dataChan:      make(chan proplet.ChunkPayload, chunkBuffer),
	}, nil
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
			if err := s.pubsub.Publish(ctx, fmt.Sprintf(PubTopic, s.domainID, s.channelID), chunk); err != nil {
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
