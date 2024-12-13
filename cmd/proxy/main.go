package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/absmach/propeller/proxy"
	"github.com/absmach/propeller/proxy/config"
	"golang.org/x/sync/errgroup"
)

const (
	svcName    = "proxy"
	mqttPrefix = "MQTT_REGISTRY_"
	httpPrefix = "HTTP_"
)

const (
	BrokerURL       = "localhost:1883"
	PropletID       = "72fd490b-f91f-47dc-aa0b-a65931719ee1"
	ChannelID       = "cb6cb9ae-ddcf-41ab-8f32-f3e93b3a3be2"
	PropletPassword = "3963a940-332e-4a18-aa57-bab4d4124ab0"

	RegistryURL      = "docker.io"
	Authenticate     = true
	RegistryUsername = "mrstevenyaga"
	RegistryPassword = "Nya@851612"
)

func main() {
	g, ctx := errgroup.WithContext(context.Background())

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	mqttCfg := config.MQTTProxyConfig{
		BrokerURL: BrokerURL,
		Password:  PropletPassword,
		PropletID: PropletID,
		ChannelID: ChannelID,
	}

	httpCfg := config.HTTPProxyConfig{
		RegistryURL:  RegistryURL,
		Authenticate: Authenticate,
		Username:     RegistryUsername,
		Password:     RegistryPassword,
	}

	logger.Info("successfully initialized MQTT and HTTP config")

	service, err := proxy.NewService(ctx, &mqttCfg, &httpCfg, logger)
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
