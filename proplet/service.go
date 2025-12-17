package proplet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"sync"
	"time"

	pkgmqtt "github.com/absmach/propeller/pkg/mqtt"
	"github.com/absmach/propeller/task"
)

const (
	pollingInterval = 5 * time.Second
	chunkTTL        = 5 * time.Minute
)

var (
	aliveTopicTemplate        = "m/%s/c/%s/control/proplet/alive"
	discoveryTopicTemplate    = "m/%s/c/%s/control/proplet/create"
	startTopicTemplate        = "m/%s/c/%s/control/manager/start"
	stopTopicTemplate         = "m/%s/c/%s/control/manager/stop"
	registryResponseTopic     = "m/%s/c/%s/registry/server"
	fetchRequestTopicTemplate = "m/%s/c/%s/registry/proplet"
)

type chunkAssemblyState struct {
	chunks      map[int][]byte
	totalChunks int
	createdAt   time.Time
}

func newChunkAssemblyState(totalChunks int) *chunkAssemblyState {
	return &chunkAssemblyState{
		chunks:      make(map[int][]byte),
		totalChunks: totalChunks,
		createdAt:   time.Now(),
	}
}

func (s *chunkAssemblyState) isComplete() bool {
	return len(s.chunks) == s.totalChunks
}

func (s *chunkAssemblyState) isExpired(ttl time.Duration) bool {
	return time.Since(s.createdAt) > ttl
}

func (s *chunkAssemblyState) assemble() []byte {
	var wasmBinary []byte
	for i := range s.totalChunks {
		if chunk, exists := s.chunks[i]; exists {
			wasmBinary = append(wasmBinary, chunk...)
		}
	}

	return wasmBinary
}

type PropletService struct {
	domainID           string
	channelID          string
	clientID           string
	clientKey          string
	k8sNamespace       string
	livelinessInterval time.Duration
	pubsub             pkgmqtt.PubSub
	chunkAssembly      map[string]*chunkAssemblyState
	chunksMutex        sync.Mutex
	runtime            Runtime
	logger             *slog.Logger
}

type ChunkPayload struct {
	AppName     string `json:"app_name"`
	ChunkIdx    int    `json:"chunk_idx"`
	TotalChunks int    `json:"total_chunks"`
	Data        []byte `json:"data"`
}

func NewService(ctx context.Context, domainID, channelID, clientID, clientKey, k8sNamespace string, livelinessInterval time.Duration, pubsub pkgmqtt.PubSub, logger *slog.Logger, runtime Runtime) (*PropletService, error) {
	topic := fmt.Sprintf(discoveryTopicTemplate, domainID, channelID)
	payload := map[string]interface{}{
		"namespace":  k8sNamespace,
		"proplet_id": clientID,
	}
	if err := pubsub.Publish(ctx, topic, payload); err != nil {
		return nil, errors.Join(errors.New("failed to publish discovery"), err)
	}

	p := &PropletService{
		domainID:           domainID,
		channelID:          channelID,
		clientID:           clientID,
		clientKey:          clientKey,
		k8sNamespace:       k8sNamespace,
		livelinessInterval: livelinessInterval,
		pubsub:             pubsub,
		chunkAssembly:      make(map[string]*chunkAssemblyState),
		runtime:            runtime,
		logger:             logger,
	}

	go p.startLivelinessUpdates(ctx)
	go p.startChunkExpiryTask(ctx)

	return p, nil
}

func (p *PropletService) Run(ctx context.Context, logger *slog.Logger) error {
	topic := fmt.Sprintf(startTopicTemplate, p.domainID, p.channelID)
	if err := p.pubsub.Subscribe(ctx, topic, p.handleStartCommand(ctx)); err != nil {
		return fmt.Errorf("failed to subscribe to start topic: %w", err)
	}

	topic = fmt.Sprintf(stopTopicTemplate, p.domainID, p.channelID)
	if err := p.pubsub.Subscribe(ctx, topic, p.handleStopCommand(ctx)); err != nil {
		return fmt.Errorf("failed to subscribe to stop topic: %w", err)
	}

	topic = fmt.Sprintf(registryResponseTopic, p.domainID, p.channelID)
	if err := p.pubsub.Subscribe(ctx, topic, p.handleChunk(ctx)); err != nil {
		return fmt.Errorf("failed to subscribe to registry topics: %w", err)
	}

	logger.Info("Proplet service is running.")
	<-ctx.Done()

	return nil
}

func (p *PropletService) startChunkExpiryTask(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			p.logger.Info("stopping chunk expiry task")

			return
		case <-ticker.C:
			p.chunksMutex.Lock()
			var expired []string
			for appName, state := range p.chunkAssembly {
				if state.isExpired(chunkTTL) {
					expired = append(expired, appName)
				}
			}
			for _, appName := range expired {
				if state, exists := p.chunkAssembly[appName]; exists {
					p.logger.Warn("expired incomplete chunk assembly",
						slog.String("app_name", appName),
						slog.Int("received_chunks", len(state.chunks)),
						slog.Int("total_chunks", state.totalChunks))
					delete(p.chunkAssembly, appName)
				}
			}
			p.chunksMutex.Unlock()
		}
	}
}

func (p *PropletService) startLivelinessUpdates(ctx context.Context) {
	ticker := time.NewTicker(p.livelinessInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			p.logger.Info("stopping liveliness updates")

			return
		case <-ticker.C:
			topic := fmt.Sprintf(aliveTopicTemplate, p.domainID, p.channelID)
			payload := map[string]interface{}{
				"status":     "alive",
				"namespace":  p.k8sNamespace,
				"proplet_id": p.clientID,
			}

			if err := p.pubsub.Publish(ctx, topic, payload); err != nil {
				p.logger.Error("failed to publish liveliness message", slog.Any("error", err))
			}

			p.logger.Debug("Published liveliness message", slog.String("topic", topic))
		}
	}
}

func (p *PropletService) handleStartCommand(ctx context.Context) func(topic string, msg map[string]interface{}) error {
	return func(topic string, msg map[string]interface{}) error {
		data, err := json.Marshal(msg)
		if err != nil {
			return err
		}

		var payload task.Task
		if err := json.Unmarshal(data, &payload); err != nil {
			return err
		}

		req := startRequest{
			ID:           payload.ID,
			CLIArgs:      payload.CLIArgs,
			FunctionName: payload.Name,
			WasmFile:     payload.File,
			imageURL:     payload.ImageURL,
			Params:       payload.Inputs,
			Daemon:       payload.Daemon,
			Env:          payload.Env,
		}
		if err := req.Validate(); err != nil {
			return err
		}

		p.logger.Info("Received start command", slog.String("app_name", req.FunctionName))

		if req.WasmFile != nil {
			config := StartConfig{
				ID:           req.ID,
				FunctionName: req.FunctionName,
				Daemon:       req.Daemon,
				WasmBinary:   req.WasmFile,
				CLIArgs:      req.CLIArgs,
				Env:          req.Env,
				Args:         req.Params,
			}
			if err := p.runtime.StartApp(ctx, config); err != nil {
				return err
			}

			return nil
		}

		pl := map[string]interface{}{
			"app_name": req.imageURL,
		}
		tp := fmt.Sprintf(fetchRequestTopicTemplate, p.domainID, p.channelID)
		if err := p.pubsub.Publish(ctx, tp, pl); err != nil {
			return err
		}

		go func() {
			p.logger.Info("Waiting for chunks", slog.String("app_name", req.imageURL))

			for {
				p.chunksMutex.Lock()
				state, exists := p.chunkAssembly[req.imageURL]
				isComplete := exists && state.isComplete()
				p.chunksMutex.Unlock()

				if isComplete {
					p.logger.Info("All chunks received, deploying app", slog.String("app_name", req.imageURL))

					p.chunksMutex.Lock()
					wasmBinary := state.assemble()

					delete(p.chunkAssembly, req.imageURL)
					p.chunksMutex.Unlock()

					config := StartConfig{
						ID:           req.ID,
						FunctionName: req.FunctionName,
						Daemon:       req.Daemon,
						WasmBinary:   wasmBinary,
						CLIArgs:      req.CLIArgs,
						Env:          req.Env,
						Args:         req.Params,
					}
					if err := p.runtime.StartApp(ctx, config); err != nil {
						p.logger.Error("Failed to start app", slog.String("app_name", req.imageURL), slog.Any("error", err))
					}

					break
				}

				time.Sleep(pollingInterval)
			}
		}()

		return nil
	}
}

func (p *PropletService) handleStopCommand(ctx context.Context) func(topic string, msg map[string]interface{}) error {
	return func(topic string, msg map[string]interface{}) error {
		data, err := json.Marshal(msg)
		if err != nil {
			return err
		}

		var req stopRequest
		if err := json.Unmarshal(data, &req); err != nil {
			return err
		}

		if err := req.Validate(); err != nil {
			return err
		}

		if err := p.runtime.StopApp(ctx, req.ID); err != nil {
			return err
		}

		return nil
	}
}

func (p *PropletService) handleChunk(_ context.Context) func(topic string, msg map[string]interface{}) error {
	return func(topic string, msg map[string]interface{}) error {
		data, err := json.Marshal(msg)
		if err != nil {
			return err
		}

		var chunk ChunkPayload
		if err := json.Unmarshal(data, &chunk); err != nil {
			return err
		}

		if err := chunk.Validate(); err != nil {
			return err
		}

		p.chunksMutex.Lock()
		defer p.chunksMutex.Unlock()

		state, exists := p.chunkAssembly[chunk.AppName]
		if !exists {
			state = newChunkAssemblyState(chunk.TotalChunks)
			p.chunkAssembly[chunk.AppName] = state
		}

		if state.totalChunks != chunk.TotalChunks {
			p.logger.Warn("chunk total_chunks mismatch",
				slog.String("app_name", chunk.AppName),
				slog.Int("expected", state.totalChunks),
				slog.Int("got", chunk.TotalChunks))

			return fmt.Errorf("chunk total_chunks mismatch for '%s'", chunk.AppName)
		}

		if _, exists := state.chunks[chunk.ChunkIdx]; exists {
			p.logger.Debug("duplicate chunk, ignoring",
				slog.Int("chunk_idx", chunk.ChunkIdx),
				slog.String("app_name", chunk.AppName))
		} else {
			state.chunks[chunk.ChunkIdx] = chunk.Data
			log.Printf("Stored chunk %d/%d for app '%s' (%d/%d chunks received)\n",
				chunk.ChunkIdx+1, chunk.TotalChunks, chunk.AppName,
				len(state.chunks), state.totalChunks)
		}

		return nil
	}
}

func (c *ChunkPayload) Validate() error {
	if c.AppName == "" {
		return errors.New("chunk validation: app_name is required but missing")
	}
	if c.ChunkIdx < 0 || c.TotalChunks <= 0 {
		return fmt.Errorf("chunk validation: invalid chunk_idx (%d) or total_chunks (%d)", c.ChunkIdx, c.TotalChunks)
	}
	if len(c.Data) == 0 {
		return errors.New("chunk validation: data is empty")
	}

	return nil
}
