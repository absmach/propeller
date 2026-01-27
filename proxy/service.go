package proxy

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	pkgmqtt "github.com/absmach/propeller/pkg/mqtt"
	"github.com/absmach/propeller/pkg/proplet"
)

const (
	chunkBuffer       = 10
	containerChanSize = 100 // Increased buffer to handle concurrent requests from multiple proplets

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
	fetching      map[string]bool // Track ongoing fetches to avoid duplicates
	fetchingMu    sync.Mutex      // Mutex to protect fetching map
}

func NewService(ctx context.Context, pubsub pkgmqtt.PubSub, domainID, channelID string, httpCfg HTTPProxyConfig, logger *slog.Logger) (*ProxyService, error) {
	return &ProxyService{
		orasconfig:    httpCfg,
		pubsub:        pubsub,
		domainID:      domainID,
		channelID:     channelID,
		logger:        logger,
		containerChan: make(chan string, containerChanSize),
		dataChan:      make(chan proplet.ChunkPayload, chunkBuffer),
		fetching:      make(map[string]bool),
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
			// Check if we're already fetching this container
			s.fetchingMu.Lock()
			if s.fetching[containerName] {
				s.fetchingMu.Unlock()
				s.logger.Debug("already fetching container, skipping duplicate request",
					slog.String("container", containerName))

				continue
			}

			// Mark as fetching
			s.fetching[containerName] = true
			s.fetchingMu.Unlock()

			// Fetch in a goroutine to allow concurrent processing
			go func(name string) {
				defer func() {
					// Remove from fetching map when done
					s.fetchingMu.Lock()
					delete(s.fetching, name)
					s.fetchingMu.Unlock()
				}()

				s.logger.Info("fetching container from registry",
					slog.String("container", name))

				chunks, err := s.orasconfig.FetchFromReg(ctx, name, s.orasconfig.ChunkSize)
				if err != nil {
					s.logger.Error("failed to fetch container",
						slog.String("container", name),
						slog.Any("error", err))

					return
				}

				s.logger.Info("successfully fetched container, sending chunks",
					slog.String("container", name),
					slog.Int("total_chunks", len(chunks)))

				// Send each chunk through the data channel
				for _, chunk := range chunks {
					select {
					case s.dataChan <- chunk:
						s.logger.Debug("sent container chunk to MQTT stream",
							slog.String("container", name),
							slog.Int("chunk", chunk.ChunkIdx),
							slog.Int("total", chunk.TotalChunks))
					case <-ctx.Done():
						return
					}
				}
			}(containerName)
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
