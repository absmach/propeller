package proplet

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"net/url"
	"os"
	"sync"
	"time"
)

const aliveTimeout = 10 * time.Second

type Proplet struct {
	ID           string      `json:"id"`
	Name         string      `json:"name"`
	TaskCount    uint64      `json:"task_count"`
	Alive        bool        `json:"alive"`
	AliveHistory []time.Time `json:"alive_history"`
}

type PropletPage struct {
	Offset   uint64    `json:"offset"`
	Limit    uint64    `json:"limit"`
	Total    uint64    `json:"total"`
	Proplets []Proplet `json:"proplets"`
}

type PropletService struct {
	config        Config
	mqttService   *MQTTService
	runtime       Runtime
	wasmFilePath  string
	wasmBinary    []byte
	chunks        map[string][][]byte
	chunkMetadata map[string]*ChunkPayload
	chunksMutex   sync.Mutex
}

func (p *Proplet) SetAlive() {
	if len(p.AliveHistory) > 0 {
		lastAlive := p.AliveHistory[len(p.AliveHistory)-1]
		if time.Since(lastAlive) <= aliveTimeout {
			p.Alive = true

			return
		}
	}
	p.Alive = false
}

type Service interface {
	Run(ctx context.Context, logger *slog.Logger) error
	UpdateRegistry(ctx context.Context, registryURL, registryToken string) error
}

var _ Service = (*PropletService)(nil)

func (p *PropletService) Run(ctx context.Context, logger *slog.Logger) error {
	if err := p.mqttService.SubscribeToManagerTopics(ctx,
		func(topic string, msg map[string]interface{}) error {
			return p.handleStartCmd(ctx, topic, msg, logger)
		},
		func(topic string, msg map[string]interface{}) error {
			return p.handleStopCmd(ctx, topic, msg, logger)
		},
		func(topic string, msg map[string]interface{}) error {
			return p.registryUpdate(ctx, topic, msg, logger)
		},
	); err != nil {
		return fmt.Errorf("failed to subscribe to Manager topics: %w", err)
	}

	if err := p.mqttService.SubscribeToRegistryTopic(ctx, func(topic string, msg map[string]interface{}) error {
		return p.handleAppChunks(ctx, topic, msg)
	}); err != nil {
		return fmt.Errorf("failed to subscribe to registry topic: %w", err)
	}

	logger.Info("Proplet service is running.")
	<-ctx.Done()

	return nil
}

func (p *PropletService) UpdateRegistry(ctx context.Context, registryURL, registryToken string) error {
	if registryURL == "" {
		return fmt.Errorf("registry URL cannot be empty")
	}
	if _, err := url.ParseRequestURI(registryURL); err != nil {
		return fmt.Errorf("invalid registry URL '%s': %w", registryURL, err)
	}

	p.config.RegistryURL = registryURL
	p.config.RegistryToken = registryToken

	configData, err := json.MarshalIndent(p.config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize updated config: %w", err)
	}

	if err := os.WriteFile("proplet/config.json", configData, filePermissions); err != nil {
		return fmt.Errorf("failed to write updated config to file: %w", err)
	}

	log.Printf("App Registry updated and persisted: %s\n", registryURL)

	return nil
}
