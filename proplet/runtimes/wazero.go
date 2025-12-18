package runtimes

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"

	"github.com/absmach/propeller/pkg/mqtt"
	"github.com/absmach/propeller/proplet"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

const (
	wasmStartFunc = "_initialize"
	wasmMalloc    = "malloc"
	wasmInitWts   = "init_weights"
	flGlobalB64   = "FL_GLOBAL_UPDATE_B64"
)

type wazeroRuntime struct {
	mutex     sync.Mutex
	runtimes  map[string]wazero.Runtime
	pubsub    mqtt.PubSub
	domainID  string
	channelID string
	logger    *slog.Logger
}

func NewWazeroRuntime(logger *slog.Logger, pubsub mqtt.PubSub, domainID, channelID string) proplet.Runtime {
	return &wazeroRuntime{
		runtimes:  make(map[string]wazero.Runtime),
		pubsub:    pubsub,
		domainID:  domainID,
		channelID: channelID,
		logger:    logger,
	}
}

func (w *wazeroRuntime) StartApp(ctx context.Context, config proplet.StartConfig) error {
	r := wazero.NewRuntime(ctx)

	w.mutex.Lock()
	w.runtimes[config.ID] = r
	w.mutex.Unlock()

	wasi_snapshot_preview1.MustInstantiate(ctx, r)

	cfg := w.buildModuleConfig(config)
	module, err := r.InstantiateWithConfig(ctx, config.WasmBinary, cfg)
	if err != nil {
		_ = w.StopApp(ctx, config.ID)

		return errors.Join(errors.New("failed to instantiate Wasm module"), err)
	}

	if config.Mode == modeTrain && config.FL != nil {
		w.injectGlobalModel(ctx, module, config)
	}

	function := module.ExportedFunction(config.FunctionName)
	if function == nil {
		_ = w.StopApp(ctx, config.ID)

		return errors.New("failed to find exported function")
	}

	go w.runAndPublish(ctx, module, function, config)

	return nil
}

func (w *wazeroRuntime) StopApp(ctx context.Context, id string) error {
	w.mutex.Lock()
	r, exists := w.runtimes[id]
	if !exists {
		w.mutex.Unlock()

		return nil
	}
	delete(w.runtimes, id)
	w.mutex.Unlock()

	if err := r.Close(ctx); err != nil {
		return err
	}

	return nil
}

func (w *wazeroRuntime) buildModuleConfig(config proplet.StartConfig) wazero.ModuleConfig {
	cfg := wazero.NewModuleConfig().WithStartFunctions(wasmStartFunc)

	argv := make([]string, 0, 1+len(config.CLIArgs)+len(config.Args))
	argv = append(argv, config.FunctionName)
	argv = append(argv, config.CLIArgs...)
	for _, a := range config.Args {
		argv = append(argv, strconv.FormatUint(a, 10))
	}
	cfg = cfg.WithArgs(argv...)

	for k, v := range config.Env {
		cfg = cfg.WithEnv(k, v)
	}

	return cfg
}

func (w *wazeroRuntime) injectGlobalModel(ctx context.Context, module api.Module, config proplet.StartConfig) {
	globalB64 := ""
	if config.Env != nil {
		globalB64 = config.Env[flGlobalB64]
	}
	if globalB64 == "" {
		return
	}

	weightsBytes, err := base64.StdEncoding.DecodeString(globalB64)
	if err != nil || len(weightsBytes) == 0 {
		return
	}

	malloc := module.ExportedFunction(wasmMalloc)
	if malloc == nil {
		return
	}

	res, err := malloc.Call(ctx, uint64(len(weightsBytes)))
	if err != nil || len(res) == 0 {
		return
	}

	ptr := res[0]
	if ok := module.Memory().Write(uint32(ptr), weightsBytes); !ok {
		w.logger.Warn("Failed to write global weights into Wasm memory", slog.String("task_id", config.ID))

		return
	}

	initFunc := module.ExportedFunction(wasmInitWts)
	if initFunc == nil {
		return
	}

	_, _ = initFunc.Call(ctx, ptr, uint64(len(weightsBytes)))
	w.logger.Info("Injected global model into Wasm", slog.String("task_id", config.ID))
}

func (w *wazeroRuntime) runAndPublish(ctx context.Context, module api.Module, function api.Function, config proplet.StartConfig) {
	callArgs := append([]uint64(nil), config.Args...)
	results, callErr := function.Call(ctx, callArgs...)
	if callErr != nil {
		w.logger.Error("failed to call function",
			slog.String("id", config.ID),
			slog.String("function", config.FunctionName),
			slog.String("error", callErr.Error()),
		)

		payload := buildFLPayloadFromUint64Slice(config.ID, config.Mode, config.PropletID, config.Env, results)
		payload["error"] = callErr.Error()

		_ = w.pubsub.Publish(ctx, fmt.Sprintf(proplet.ResultsTopic, w.domainID, w.channelID), payload)
		_ = w.StopApp(ctx, config.ID)

		return
	}

	payload := w.buildResultsPayload(module, config, results)

	if !config.Daemon {
		if err := w.StopApp(ctx, config.ID); err != nil {
			w.logger.Error("failed to stop app",
				slog.String("id", config.ID),
				slog.String("error", err.Error()),
			)
		}
	}

	topic := fmt.Sprintf(proplet.ResultsTopic, w.domainID, w.channelID)
	if err := w.pubsub.Publish(ctx, topic, payload); err != nil {
		w.logger.Error("failed to publish results",
			slog.String("id", config.ID),
			slog.String("error", err.Error()),
		)

		return
	}

	w.logger.Info("Finished running app", slog.String("id", config.ID))
}

func (w *wazeroRuntime) buildResultsPayload(module api.Module, config proplet.StartConfig, results []uint64) map[string]any {
	if config.Mode != modeTrain || len(results) < 2 {
		return buildFLPayloadFromUint64Slice(config.ID, config.Mode, config.PropletID, config.Env, results)
	}

	ptr := uint32(results[0])
	size := uint32(results[1])

	b, ok := module.Memory().Read(ptr, size)
	if !ok {
		w.logger.Error("failed to read update from Wasm memory", slog.String("id", config.ID))

		payload := buildFLPayloadFromUint64Slice(config.ID, config.Mode, config.PropletID, config.Env, results)
		payload["error"] = "failed to read update from Wasm memory"

		return payload
	}

	return buildFLPayloadFromString(config.ID, config.Mode, config.PropletID, config.Env, string(b))
}
