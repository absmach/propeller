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
	domainID    string
	channelID   string
	logger      *slog.Logger
	wasmRuntime string
}

func NewHostRuntime(logger *slog.Logger, pubsub mqtt.PubSub, domainID, channelID, wasmRuntime string) proplet.Runtime {
	return &hostRuntime{
		pubsub:      pubsub,
		domainID:    domainID,
		channelID:   channelID,
		logger:      logger,
		wasmRuntime: wasmRuntime,
	}
}

func (w *hostRuntime) StartApp(ctx context.Context, config proplet.StartConfig) error {
	currentDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("error getting current directory: %w", err)
	}
	f, err := os.Create(filepath.Join(currentDir, config.ID+".wasm"))
	if err != nil {
		return fmt.Errorf("error creating file: %w", err)
	}

	if _, err = f.Write(config.WasmBinary); err != nil {
		return fmt.Errorf("error writing to file: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("error closing file: %w", err)
	}

	cliArgs := config.CLIArgs

	cliArgs = append(cliArgs, filepath.Join(currentDir, config.ID+".wasm"))
	for i := range config.Args {
		cliArgs = append(cliArgs, strconv.FormatUint(config.Args[i], 10))
	}
	cmd := exec.Command(w.wasmRuntime, cliArgs...)
	results := bytes.Buffer{}
	cmd.Stdout = &results

	if config.Env != nil {
		cmd.Env = os.Environ()
		for key, value := range config.Env {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", key, value))
		}
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("error starting command: %w", err)
	}

	if !config.Daemon {
		go w.wait(ctx, cmd, filepath.Join(currentDir, config.ID+".wasm"), config.ID, &results)
	}

	return nil
}

func (w *hostRuntime) StopApp(ctx context.Context, id string) error {
	return nil
}

func (w *hostRuntime) wait(ctx context.Context, cmd *exec.Cmd, fileName, id string, results *bytes.Buffer) {
	defer func() {
		if err := os.Remove(fileName); err != nil {
			w.logger.Error("failed to remove file", slog.String("fileName", fileName), slog.String("error", err.Error()))
		}
	}()

	var payload map[string]any
	if err := cmd.Wait(); err != nil {
		w.logger.Error("failed to wait for command", slog.String("id", id), slog.String("error", err.Error()))
		payload = map[string]any{
			"task_id": id,
			"error":   err.Error(),
			"results": results.String(),
		}
	} else {
		payload = map[string]any{
			"task_id": id,
			"results": results.String(),
		}
	}

	topic := fmt.Sprintf(proplet.ResultsTopic, w.domainID, w.channelID)
	if err := w.pubsub.Publish(ctx, topic, payload); err != nil {
		w.logger.Error("failed to publish results", slog.String("id", id), slog.String("error", err.Error()))

		return
	}

	w.logger.Info("Finished running app", slog.String("id", id))
}
