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
	"sync"

	"github.com/absmach/propeller/pkg/mqtt"
	"github.com/absmach/propeller/proplet"
)

type hostRuntime struct {
	pubsub      mqtt.PubSub
	domainID    string
	channelID   string
	logger      *slog.Logger
	wasmRuntime string

	mu   sync.Mutex
	proc map[string]*exec.Cmd
}

func NewHostRuntime(logger *slog.Logger, pubsub mqtt.PubSub, domainID, channelID, wasmRuntime string) proplet.Runtime {
	return &hostRuntime{
		pubsub:      pubsub,
		domainID:    domainID,
		channelID:   channelID,
		logger:      logger,
		wasmRuntime: wasmRuntime,
		proc:        make(map[string]*exec.Cmd),
	}
}

func (w *hostRuntime) StartApp(ctx context.Context, config proplet.StartConfig) error {
	currentDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("error getting current directory: %w", err)
	}

	wasmPath := filepath.Join(currentDir, config.ID+".wasm")
	f, err := os.Create(wasmPath)
	if err != nil {
		return fmt.Errorf("error creating file: %w", err)
	}

	if _, err = f.Write(config.WasmBinary); err != nil {
		_ = f.Close()
		_ = os.Remove(wasmPath)

		return fmt.Errorf("error writing to file: %w", err)
	}

	if err := f.Close(); err != nil {
		_ = os.Remove(wasmPath)

		return fmt.Errorf("error closing file: %w", err)
	}

	cliArgs := append([]string(nil), config.CLIArgs...)
	cliArgs = append(cliArgs, wasmPath)
	for _, a := range config.Args {
		cliArgs = append(cliArgs, strconv.FormatUint(a, 10))
	}

	cmd := exec.Command(w.wasmRuntime, cliArgs...)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if config.Env != nil {
		cmd.Env = os.Environ()
		for key, value := range config.Env {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", key, value))
		}
	}

	if err := cmd.Start(); err != nil {
		_ = os.Remove(wasmPath)

		return fmt.Errorf("error starting command: %w", err)
	}

	w.mu.Lock()
	w.proc[config.ID] = cmd
	w.mu.Unlock()

	if !config.Daemon {
		go w.wait(ctx, cmd, wasmPath, config.ID, &stdout, &stderr, config.Mode, config.PropletID, config.Env)
	}

	return nil
}

func (w *hostRuntime) StopApp(ctx context.Context, id string) error {
	w.mu.Lock()
	cmd, ok := w.proc[id]
	w.mu.Unlock()

	if !ok || cmd == nil || cmd.Process == nil {
		return nil
	}

	_ = cmd.Process.Kill()

	return nil
}

func (w *hostRuntime) wait(
	ctx context.Context,
	cmd *exec.Cmd,
	fileName, id string,
	stdout, stderr *bytes.Buffer,
	mode, propletID string,
	env map[string]string,
) {
	defer func() {
		w.mu.Lock()
		delete(w.proc, id)
		w.mu.Unlock()

		if err := os.Remove(fileName); err != nil {
			w.logger.Error(
				"failed to remove file",
				slog.String("fileName", fileName),
				slog.String("error", err.Error()),
			)
		}
	}()

	waitErr := cmd.Wait()

	outStr := stdout.String()
	errStr := stderr.String()

	payload := buildFLPayloadFromString(id, mode, propletID, env, outStr)

	if waitErr != nil {
		w.logger.Error(
			"failed to wait for command",
			slog.String("id", id),
			slog.String("error", waitErr.Error()),
		)
		if errStr != "" {
			payload["stderr"] = errStr
		}
		payload["error"] = waitErr.Error()
	} else if errStr != "" {
		payload["stderr"] = errStr
	}

	topic := fmt.Sprintf(proplet.ResultsTopic, w.domainID, w.channelID)
	if err := w.pubsub.Publish(ctx, topic, payload); err != nil {
		w.logger.Error(
			"failed to publish results",
			slog.String("id", id),
			slog.String("error", err.Error()),
		)

		return
	}

	w.logger.Info("Finished running app", slog.String("id", id))
}
