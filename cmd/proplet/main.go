package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/absmach/propeller/proplet"
	"log/slog"
)

func main() {
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

	// Initialize and run the Proplet service
	service, err := newService(ctx, "proplet/config.json", logger)
	if err != nil {
		logger.Error("Error initializing Proplet", slog.Any("error", err))
		os.Exit(1)
	}

	if err := service.Run(ctx); err != nil {
		logger.Error("Error running Proplet", slog.Any("error", err))
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

func newService(ctx context.Context, configPath string, logger *slog.Logger) (*proplet.PropletService, error) {
	logger.Info("Loading configuration", slog.String("path", configPath))
	config, err := proplet.LoadConfig(configPath)
	if err != nil {
		logger.Error("Failed to load configuration", slog.String("path", configPath), slog.Any("error", err))
		return nil, fmt.Errorf("failed to load configuration: %w", err)
	}

	logger.Info("Configuration loaded", slog.String("registry_url", config.RegistryURL))

	// Check connectivity to configured OCI Registry
	if err := checkRegistryConnectivity(config.RegistryURL, logger); err != nil {
		logger.Error("Failed connectivity check for App Registry", slog.String("url", config.RegistryURL), slog.Any("error", err))
		return nil, fmt.Errorf("failed connectivity check for App Registry: %w", err)
	}

	logger.Info("Initializing Proplet service")
	propletService, err := proplet.NewPropletService(ctx, config)
	if err != nil {
		logger.Error("Failed to initialize Proplet", slog.Any("error", err))
		return nil, fmt.Errorf("failed to initialize Proplet: %w", err)
	}

	logger.Info("Proplet initialized successfully", slog.String("registry_url", config.RegistryURL))
	return propletService, nil
}

// checkRegistryConnectivity tests if the Registry URL is reachable.
func checkRegistryConnectivity(registryURL string, logger *slog.Logger) error {
	client := &http.Client{
		Timeout: 5 * time.Second,
	}

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
