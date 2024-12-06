package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/absmach/propeller/proplet"
	config "github.com/absmach/propeller/proplet/repository"
)

const registryTimeout = 5 * time.Second

func main() {
	// Parse the WASM file path from command-line arguments
	wasmFilePath := flag.String("file", "", "Path to the WASM file")
	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger := configureLogger("info")
	slog.SetDefault(logger)

	logger.Info("Starting Proplet service")

	// Graceful shutdown on interrupt signal
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		sig := <-sigChan
		logger.Info("Received shutdown signal", slog.String("signal", sig.String()))
		cancel()
	}()

	// Check if a WASM file is provided
	hasWASMFile := *wasmFilePath != ""

	// Load configuration
	cfg, err := config.LoadConfig("proplet/repository/config.json", hasWASMFile)
	if err != nil {
		logger.Error("Failed to load configuration", slog.String("path", "proplet/repository/config.json"), slog.Any("error", err))
		os.Exit(1)
	}

	// Check Registry connectivity if URL is provided
	if cfg.RegistryURL != "" {
		if err := checkRegistryConnectivity(cfg.RegistryURL, logger); err != nil {
			logger.Error("Failed connectivity check for Registry URL", slog.String("url", cfg.RegistryURL), slog.Any("error", err))
			os.Exit(1)
		}
		logger.Info("Registry connectivity verified", slog.String("url", cfg.RegistryURL))
	}

	// Load WASM binary if provided
	var wasmBinary []byte
	if hasWASMFile {
		wasmBinary, err = loadWASMFile(*wasmFilePath, logger)
		if err != nil {
			logger.Error("Failed to load WASM file", slog.String("wasm_file_path", *wasmFilePath), slog.Any("error", err))
			os.Exit(1)
		}
		logger.Info("WASM binary loaded at startup", slog.Int("size_bytes", len(wasmBinary)))
	}

	// Handle fallback case for empty registry URL and binary
	if cfg.RegistryURL == "" && wasmBinary == nil {
		logger.Error("Neither a registry URL nor a WASM binary file was provided")
		os.Exit(1)
	}

	// Initialize and run the service
	service, err := proplet.NewService(ctx, cfg, wasmBinary, logger)
	if err != nil {
		logger.Error("Error initializing service", slog.Any("error", err))
		os.Exit(1)
	}

	if err := service.Run(ctx, logger); err != nil {
		logger.Error("Error running service", slog.Any("error", err))
	}
}

func configureLogger(level string) *slog.Logger {
	var logLevel slog.Level
	if err := logLevel.UnmarshalText([]byte(level)); err != nil {
		fmt.Printf("Invalid log level: %s. Defaulting to info.\n", level)
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
	client := &http.Client{Timeout: registryTimeout}

	logger.Info("Checking registry connectivity", slog.String("url", registryURL))
	resp, err := client.Get(registryURL)
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
