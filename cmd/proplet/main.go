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
	wasmBinary   []byte
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
		logger.Error("Failed to load configuration", slog.String("path", "proplet/config.json"), slog.Any("error", err))

		return fmt.Errorf("failed to load configuration: %w", err)
	}

	if cfg.RegistryURL != "" {
		if err := checkRegistryConnectivity(cfg.RegistryURL, logger); err != nil {
			logger.Error("Failed connectivity check for Registry URL", slog.String("url", cfg.RegistryURL), slog.Any("error", err))

			return fmt.Errorf("registry connectivity check failed: %w", err)
		}
		logger.Info("Registry connectivity verified", slog.String("url", cfg.RegistryURL))
	}

	if hasWASMFile {
		wasmBinary, err = loadWASMFile(wasmFilePath, logger)
		if err != nil {
			logger.Error("Failed to load WASM file", slog.String("wasm_file_path", wasmFilePath), slog.Any("error", err))

			return fmt.Errorf("failed to load WASM file: %w", err)
		}
		logger.Info("WASM binary loaded at startup", slog.Int("size_bytes", len(wasmBinary)))
	}

	if cfg.RegistryURL == "" && wasmBinary == nil {
		logger.Error("Neither a registry URL nor a WASM binary file was provided")

		return errors.New("missing registry URL and WASM binary file")
	}

	service, err := proplet.NewService(ctx, cfg, wasmBinary, logger)
	if err != nil {
		logger.Error("Error initializing service", slog.Any("error", err))

		return fmt.Errorf("service initialization error: %w", err)
	}

	if err := service.Run(ctx, logger); err != nil {
		logger.Error("Error running service", slog.Any("error", err))

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

func loadWASMFile(path string, logger *slog.Logger) ([]byte, error) {
	logger.Info("Loading WASM file", slog.String("path", path))
	wasmBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read WASM file: %w", err)
	}

	return wasmBytes, nil
}

func checkRegistryConnectivity(registryURL string, logger *slog.Logger) error {
	ctx, cancel := context.WithTimeout(context.Background(), registryTimeout)
	defer cancel()

	client := http.Client{}

	logger.Info("Checking registry connectivity", slog.String("url", registryURL))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, registryURL, http.NoBody)
	if err != nil {
		logger.Error("Failed to create HTTP request", slog.String("url", registryURL), slog.Any("error", err))

		return fmt.Errorf("failed to create HTTP request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		logger.Error("Failed to connect to registry", slog.String("url", registryURL), slog.Any("error", err))

		return fmt.Errorf("failed to connect to registry URL '%s': %w", registryURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.Error("Registry returned unexpected status", slog.String("url", registryURL), slog.Int("status_code", resp.StatusCode))

		return fmt.Errorf("registry URL '%s' returned status: %s", registryURL, resp.Status)
	}

	logger.Info("Registry connectivity verified", slog.String("url", registryURL))

	return nil
}
