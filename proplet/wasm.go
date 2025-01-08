package proplet

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/absmach/propeller/pkg/mqtt"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

var resultsTopic = "channels/%s/messages/control/proplet/results"

type Runtime interface {
	StartApp(ctx context.Context, wasmBinary []byte, cliArgs []string, id, functionName string, args ...uint64) error
	StopApp(ctx context.Context, id string) error
}

type wazeroRuntime struct {
	mutex           sync.Mutex
	runtimes        map[string]wazero.Runtime
	pubsub          mqtt.PubSub
	channelID       string
	logger          *slog.Logger
	hostWasmRuntime string
}

func NewWazeroRuntime(logger *slog.Logger, pubsub mqtt.PubSub, channelID, hostWasmRuntime string) Runtime {
	return &wazeroRuntime{
		runtimes:        make(map[string]wazero.Runtime),
		pubsub:          pubsub,
		channelID:       channelID,
		logger:          logger,
		hostWasmRuntime: hostWasmRuntime,
	}
}

func (w *wazeroRuntime) StartApp(ctx context.Context, wasmBinary []byte, cliArgs []string, id, functionName string, args ...uint64) error {
	if w.hostWasmRuntime != "" {
		return w.runOnHostRuntime(ctx, wasmBinary, cliArgs, id, args...)
	}

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

		topic := fmt.Sprintf(resultsTopic, w.channelID)
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

func (w *wazeroRuntime) runOnHostRuntime(ctx context.Context, wasmBinary []byte, cliArgs []string, id string, args ...uint64) error {
	currentDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("error getting current directory: %w", err)
	}
	f, err := os.Create(filepath.Join(currentDir, id+".wasm"))
	if err != nil {
		return fmt.Errorf("error creating file: %w", err)
	}

	if _, err = f.Write(wasmBinary); err != nil {
		return fmt.Errorf("error writing to file: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("error closing file: %w", err)
	}

	cliArgs = append(cliArgs, filepath.Join(currentDir, id+".wasm"))
	for i := range args {
		cliArgs = append(cliArgs, strconv.FormatUint(args[i], 10))
	}
	cmd := exec.Command(w.hostWasmRuntime, cliArgs...)
	results := bytes.Buffer{}
	cmd.Stdout = &results

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("error starting command: %w", err)
	}

	go func(fileName string) {
		if err := cmd.Wait(); err != nil {
			w.logger.Error("failed to wait for command", slog.String("id", id), slog.String("error", err.Error()))
		}

		payload := map[string]interface{}{
			"task_id": id,
			"results": results.String(),
		}

		topic := fmt.Sprintf(resultsTopic, w.channelID)
		if err := w.pubsub.Publish(ctx, topic, payload); err != nil {
			w.logger.Error("failed to publish results", slog.String("id", id), slog.String("error", err.Error()))

			return
		}

		if err := os.Remove(fileName); err != nil {
			w.logger.Error("failed to remove file", slog.String("fileName", fileName), slog.String("error", err.Error()))
		}

		w.logger.Info("Finished running app", slog.String("id", id))
	}(filepath.Join(currentDir, id+".wasm"))

	return nil
}
