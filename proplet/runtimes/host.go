package runtimes

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/absmach/propeller/pkg/mqtt"
	"github.com/absmach/propeller/proplet"
)

type hostRuntime struct {
	pubsub      mqtt.PubSub
	channelID   string
	logger      *slog.Logger
	wasmRuntime string
}

func NewHostRuntime(logger *slog.Logger, pubsub mqtt.PubSub, channelID, wasmRuntime string) proplet.Runtime {
	return &hostRuntime{
		pubsub:      pubsub,
		channelID:   channelID,
		logger:      logger,
		wasmRuntime: wasmRuntime,
	}
}

func (w *hostRuntime) StartApp(ctx context.Context, wasmBinary []byte, cliArgs []string, id, functionName string, args ...uint64) error {
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
	cmd := exec.Command(w.wasmRuntime, cliArgs...)
	results := bytes.Buffer{}
	cmd.Stdout = &results

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("error starting command: %w", err)
	}

	go func(fileName string) {
		var payload map[string]interface{}

		if err := cmd.Wait(); err != nil {
			w.logger.Error("failed to wait for command", slog.String("id", id), slog.String("error", err.Error()))
			payload = map[string]interface{}{
				"task_id": id,
				"error":   err.Error(),
				"results": results.String(),
			}
		} else {
			payload = map[string]interface{}{
				"task_id": id,
				"results": results.String(),
			}
		}

		topic := fmt.Sprintf(proplet.ResultsTopic, w.channelID)
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

func (w *hostRuntime) StopApp(ctx context.Context, id string) error {
	return nil
}
