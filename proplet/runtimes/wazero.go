package runtimes

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/absmach/propeller/pkg/mqtt"
	"github.com/absmach/propeller/proplet"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

type wazeroRuntime struct {
	mutex     sync.Mutex
	runtimes  map[string]wazero.Runtime
	pubsub    mqtt.PubSub
	channelID string
	logger    *slog.Logger
}

func NewWazeroRuntime(logger *slog.Logger, pubsub mqtt.PubSub, channelID string) proplet.Runtime {
	return &wazeroRuntime{
		runtimes:  make(map[string]wazero.Runtime),
		pubsub:    pubsub,
		channelID: channelID,
		logger:    logger,
	}
}

func (w *wazeroRuntime) StartApp(ctx context.Context, wasmBinary []byte, cliArgs []string, id, functionName string, args ...uint64) error {
	r := wazero.NewRuntime(ctx)

	w.mutex.Lock()
	w.runtimes[id] = r
	w.mutex.Unlock()

	// Instantiate WASI, which implements host functions needed for TinyGo to
	// implement `panic`.
	wasi_snapshot_preview1.MustInstantiate(ctx, r)

	module, err := r.Instantiate(ctx, wasmBinary)
	if err != nil {
		return errors.Join(errors.New("failed to instantiate Wasm module"), err)
	}

	function := module.ExportedFunction(functionName)
	if function == nil {
		return errors.New("failed to find exported function")
	}

	go func() {
		results, err := function.Call(ctx, args...)
		if err != nil {
			w.logger.Error("failed to call function", slog.String("id", id), slog.String("function", functionName), slog.String("error", err.Error()))

			return
		}

		if err := w.StopApp(ctx, id); err != nil {
			w.logger.Error("failed to stop app", slog.String("id", id), slog.String("error", err.Error()))
		}

		payload := map[string]interface{}{
			"task_id": id,
			"results": results,
		}

		topic := fmt.Sprintf(proplet.ResultsTopic, w.channelID)
		if err := w.pubsub.Publish(ctx, topic, payload); err != nil {
			w.logger.Error("failed to publish results", slog.String("id", id), slog.String("error", err.Error()))

			return
		}

		w.logger.Info("Finished running app", slog.String("id", id))
	}()

	return nil
}

func (w *wazeroRuntime) StopApp(ctx context.Context, id string) error {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	r, exists := w.runtimes[id]
	if !exists {
		return errors.New("there is no runtime for this id")
	}

	if err := r.Close(ctx); err != nil {
		return err
	}

	delete(w.runtimes, id)

	return nil
}
