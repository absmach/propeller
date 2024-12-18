package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"

	"github.com/absmach/propeller/proxy"
	"github.com/absmach/propeller/proxy/config"
	"github.com/caarlos0/env/v11"
	"golang.org/x/sync/errgroup"
)

const svcName = "proxy"

func main() {
	g, ctx := errgroup.WithContext(context.Background())

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	mqttCfg := config.MQTTProxyConfig{}
	if err := env.Parse(&mqttCfg); err != nil {
		log.Fatalf("failed to load mqtt config : %s", err.Error())
	}

	if err := mqttCfg.Validate(); err != nil {
		log.Fatalf("failed to validate mqtt config : %s", err.Error())

		return
	}

	httpCfg := config.HTTPProxyConfig{}
	if err := env.Parse(&httpCfg); err != nil {
		log.Fatalf("failed to load http config : %s", err.Error())
	}

	if err := httpCfg.Validate(); err != nil {
		log.Fatalf("failed to validate http config : %s", err.Error())

		return
	}

	logger.Info("successfully initialized MQTT and HTTP config")

	service, err := proxy.NewService(ctx, &mqttCfg, &httpCfg, logger)
	if err != nil {
		logger.Error("failed to create proxy service", slog.Any("error", err))

		return
	}

	logger.Info("starting proxy service")

	if err := start(ctx, g, service); err != nil {
		logger.Error(fmt.Sprintf("%s service exited with error: %s", svcName, err))
	}
}

func start(ctx context.Context, g *errgroup.Group, s *proxy.ProxyService) error {
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
