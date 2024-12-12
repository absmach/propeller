package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/absmach/propeller/proplet"
)

const registryTimeout = 30 * time.Second

var (
	wasmFilePath string
	logLevel     slog.Level
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	flag.StringVar(&wasmFilePath, "file", "", "Path to the WASM file")
	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := configureLogger("info")
	slog.SetDefault(logger)

	logger.Info("Starting Proplet service")

	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		sig := <-sigChan
		logger.Info("Received shutdown signal", slog.String("signal", sig.String()))
		cancel()
	}()

	hasWASMFile := wasmFilePath != ""

	cfg, err := proplet.LoadConfig("proplet/config.json", hasWASMFile)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	if cfg.RegistryURL != "" {
		if err := checkRegistryConnectivity(cfg.RegistryURL); err != nil {
			return fmt.Errorf("registry connectivity check failed: %w", err)
		}
	}

	if cfg.RegistryURL == "" && !hasWASMFile {
		return errors.New("missing registry URL and WASM file")
	}

	service, err := proplet.NewService(ctx, cfg, wasmFilePath, logger)
	if err != nil {
		return fmt.Errorf("service initialization error: %w", err)
	}

	if err := service.Run(ctx, logger); err != nil {
		return fmt.Errorf("service run error: %w", err)
	}

	return nil
}

func configureLogger(level string) *slog.Logger {
	if err := logLevel.UnmarshalText([]byte(level)); err != nil {
		log.Printf("Invalid log level: %s. Defaulting to info.\n", level)
		logLevel = slog.LevelInfo
	}

	logHandler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	})

	return slog.New(logHandler)
}

func checkRegistryConnectivity(registryURL string) error {
	ctx, cancel := context.WithTimeout(context.Background(), registryTimeout)
	defer cancel()

	client := http.Client{}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, registryURL, http.NoBody)
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to registry URL '%s': %w", registryURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("registry URL '%s' returned status: %s", registryURL, resp.Status)
	}

	return nil
}
