package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/absmach/magistrala/pkg/server"
	"github.com/absmach/propeller/pkg/mqtt"
	"github.com/absmach/propeller/proplet"
	"github.com/caarlos0/env/v11"
	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
)

const svcName = "proplet"

type config struct {
	LogLevel           string        `env:"PROPLET_LOG_LEVEL"           envDefault:"info"`
	InstanceID         string        `env:"PROPLET_INSTANCE_ID"`
	MQTTAddress        string        `env:"PROPLET_MQTT_ADDRESS"        envDefault:"tcp://localhost:1883"`
	MQTTTimeout        time.Duration `env:"PROPLET_MQTT_TIMEOUT"        envDefault:"30s"`
	MQTTQoS            byte          `env:"PROPLET_MQTT_QOS"            envDefault:"2"`
	LivelinessInterval time.Duration `env:"PROPLET_LIVELINESS_INTERVAL" envDefault:"10s"`
	RegistryURL        string        `env:"PROPLET_REGISTRY_URL"`
	RegistryToken      string        `env:"PROPLET_REGISTRY_TOKEN"`
	RegistryTimeout    time.Duration `env:"PROPLET_REGISTRY_TIMEOUT"    envDefault:"30s"`
	ChannelID          string        `env:"PROPLET_CHANNEL_ID,notEmpty"`
	ThingID            string        `env:"PROPLET_THING_ID,notEmpty"`
	ThingKey           string        `env:"PROPLET_THING_KEY,notEmpty"`
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	g, ctx := errgroup.WithContext(ctx)

	cfg := config{}
	if err := env.Parse(&cfg); err != nil {
		log.Fatalf("failed to load configuration : %s", err.Error())
	}

	if cfg.InstanceID == "" {
		cfg.InstanceID = uuid.NewString()
	}

	var level slog.Level
	if err := level.UnmarshalText([]byte(cfg.LogLevel)); err != nil {
		log.Fatalf("failed to parse log level: %s", err.Error())
	}
	logHandler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
	})
	logger := slog.New(logHandler)
	slog.SetDefault(logger)

	if cfg.RegistryURL != "" {
		if err := checkRegistryConnectivity(ctx, cfg.RegistryURL, cfg.RegistryTimeout); err != nil {
			logger.Error("failed to connect to registry URL", slog.String("url", cfg.RegistryURL), slog.Any("error", err))

			return
		}

		logger.Info("successfully connected to registry URL", slog.String("url", cfg.RegistryURL))
	}

	mqttPubSub, err := mqtt.NewPubSub(cfg.MQTTAddress, cfg.MQTTQoS, cfg.InstanceID, cfg.ThingID, cfg.ThingKey, cfg.ChannelID, cfg.MQTTTimeout, logger)
	if err != nil {
		logger.Error("failed to initialize mqtt client", slog.Any("error", err))

		return
	}
	wazero := proplet.NewWazeroRuntime(logger, mqttPubSub, cfg.ChannelID)

	service, err := proplet.NewService(ctx, cfg.ChannelID, cfg.ThingID, cfg.ThingKey, cfg.RegistryURL, cfg.RegistryToken, cfg.LivelinessInterval, mqttPubSub, logger, wazero)
	if err != nil {
		logger.Error("failed to initialize service", slog.Any("error", err))

		return
	}

	if err := service.Run(ctx, logger); err != nil {
		logger.Error("failed to run service", slog.Any("error", err))

		return
	}

	g.Go(func() error {
		return server.StopSignalHandler(ctx, cancel, logger, svcName)
	})

	if err := g.Wait(); err != nil {
		logger.Error(fmt.Sprintf("%s service exited with error: %s", svcName, err))
	}
}

func checkRegistryConnectivity(ctx context.Context, registryURL string, registryTimeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, registryTimeout)
	defer cancel()

	client := http.DefaultClient

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, registryURL, http.NoBody)
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to registry URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("registry returned unexpected status: %d", resp.StatusCode)
	}

	return nil
}
