package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/absmach/propeller/proxy"
	"github.com/absmach/propeller/proxy/config"
	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
	"golang.org/x/sync/errgroup"
)

const (
	mqttPrefix = "MQTT_REGISTRY_"
	httpPrefix = "HTTP_"
	chanSize   = 2
)

func main() {
	g, ctx := errgroup.WithContext(context.Background())

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	err := godotenv.Load()
	if err != nil {
		panic(err)
	}

	mqttCfg, err := config.LoadMQTTConfig(env.Options{Prefix: mqttPrefix})
	if err != nil {
		logger.Error("Failed to load MQTT configuration", slog.Any("error", err))

		return
	}

	httpCfg, err := config.LoadHTTPConfig(env.Options{Prefix: httpPrefix})
	if err != nil {
		logger.Error("Failed to load HTTP configuration", slog.Any("error", err))

		return
	}

	service, err := proxy.NewService(ctx, mqttCfg, httpCfg, logger)
	if err != nil {
		logger.Error("failed to create proxy service", "error", err)

		return
	}

	g.Go(func() error {
		return start(ctx, service)
	})
}

func start(ctx context.Context, s *proxy.ProxyService) error {
	errs := make(chan error, chanSize)

	if err := s.MQTTClient().Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect to MQTT broker: %w", err)
	}

	defer func() {
		if err := s.MQTTClient().Disconnect(ctx); err != nil {
			slog.Error("failed to disconnect MQTT client", "error", err)
		}
	}()

	if err := s.MQTTClient().Subscribe(ctx, s.ContainerChan()); err != nil {
		return fmt.Errorf("failed to subscribe to container requests: %w", err)
	}

	go s.StreamHTTP(ctx, errs)
	go s.StreamMQTT(ctx, errs)

	return <-errs
}
