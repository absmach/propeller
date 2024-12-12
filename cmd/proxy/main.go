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
	svcName    = "proxy"
	mqttPrefix = "MQTT_REGISTRY_"
	httpPrefix = "HTTP_"
)

func main() {
	g, ctx := errgroup.WithContext(context.Background())

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	err := godotenv.Load("cmd/proxy/.env")
	if err != nil {
		panic(err)
	}

	mqttCfg, err := config.LoadMQTTConfig(env.Options{Prefix: mqttPrefix})
	if err != nil {
		logger.Error("failed to load MQTT configuration", slog.Any("error", err))

		return
	}

	logger.Info("successfully loaded MQTT config")

	httpCfg, err := config.LoadHTTPConfig(env.Options{Prefix: httpPrefix})
	if err != nil {
		logger.Error("failed to load HTTP configuration", slog.Any("error", err))

		return
	}

	logger.Info("successfully loaded HTTP config")

	service, err := proxy.NewService(ctx, mqttCfg, httpCfg, logger)
	if err != nil {
		logger.Error("failed to create proxy service", "error", err)

		return
	}

	logger.Info("starting proxy service")

	if err := start(ctx, g, service); err != nil {
		logger.Error(fmt.Sprintf("%s service exited with error: %s", svcName, err))
	}
}

func start(ctx context.Context, g *errgroup.Group, s *proxy.ProxyService) error {
	slog.Info("connecting...")

	if err := s.MQTTClient().Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect to MQTT broker: %w", err)
	}

	slog.Info("successfully connected to broker")

	defer func() {
		if err := s.MQTTClient().Disconnect(ctx); err != nil {
			slog.Error("failed to disconnect MQTT client", "error", err)
		}
	}()

	if err := s.MQTTClient().Subscribe(ctx, s.ContainerChan()); err != nil {
		return fmt.Errorf("failed to subscribe to container requests: %w", err)
	}

	slog.Info("successfully subscribed to topic")

	g.Go(func() error {
		return s.StreamHTTP(ctx)
	})

	g.Go(func() error {
		return s.StreamMQTT(ctx)
	})

	return g.Wait()
}
