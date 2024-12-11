package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/absmach/propeller/proxy"
	"github.com/absmach/propeller/proxy/config"
	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

const (
	mqttPrefix = "MQTT_"
	httpPrefix = "HTTP_"
)

func main() {
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Set up signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// Loading .env file to environment
	err := godotenv.Load()
	if err != nil {
		panic(err)
	}

	cfgM, err := config.LoadMQTTConfig(env.Options{Prefix: mqttPrefix})
	if err != nil {
		logger.Error("Failed to load MQTT configuration", slog.Any("error", err))
		os.Exit(1)
	}

	cfgH, err := config.LoadHTTPConfig(env.Options{Prefix: httpPrefix})
	if err != nil {
		logger.Error("Failed to load HTTP configuration",  slog.Any("error", err))
		os.Exit(1)
	}

	// Create proxy service
	service, err := proxy.NewService(ctx, cfgM,cfgH, logger)
	if err != nil {
		logger.Error("failed to create proxy service", "error", err)
		os.Exit(1)
	}

	// Start the service
	go func() {
		if err := start(ctx, service); err != nil {
			logger.Error("service error", "error", err)
			cancel()
		}
	}()

	// Wait for signal
	<-sigChan
	cancel()
}

func start(ctx context.Context, s *proxy.ProxyService) error {
	errs := make(chan error, 2)

	if err := s.MQTTClient().Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect to MQTT broker: %w", err)
	}
	defer s.MQTTClient().Disconnect(ctx)

	if err := s.MQTTClient().Subscribe(ctx, s.ContainerChan()); err != nil {
		return fmt.Errorf("failed to subscribe to container requests: %w", err)
	}

	go s.StreamHTTP(ctx, errs)
	go s.StreamMQTT(ctx, errs)

	return <-errs
}
