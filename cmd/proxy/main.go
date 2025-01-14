package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"time"

	"github.com/absmach/propeller/proxy"
	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
	"golang.org/x/sync/errgroup"
)

const (
	svcName = "proxy"
	pathEnv = ".env"
)

type envConfig struct {
	LogLevel    string        `env:"PROXY_LOG_LEVEL"           envDefault:"info"`
	MQTTAddress string        `env:"PROXY_MQTT_ADDRESS"        envDefault:"tcp://localhost:1883"`
	MQTTTimeout time.Duration `env:"PROXY_MQTT_TIMEOUT"        envDefault:"30s"`
	ThingID     string        `env:"PROXY_THING_ID"`
	ThingKey    string        `env:"PROXY_THING_KEY"`
	ChannelID   string        `env:"PROXY_CHANNEL_ID"`

	// HTTP Registry configuration
	ChunkSize    int    `env:"PROXY_CHUNK_SIZE"         envDefault:"512000"`
	Authenticate bool   `env:"PROXY_AUTHENTICATE"        envDefault:"false"`
	Token        string `env:"PROXY_REGISTRY_TOKEN"      envDefault:""`
	Username     string `env:"PROXY_REGISTRY_USERNAME"   envDefault:""`
	Password     string `env:"PROXY_REGISTRY_PASSWORD"   envDefault:""`
	RegistryURL  string `env:"PROXY_REGISTRY_URL,notEmpty"`
}

func main() {
	g, ctx := errgroup.WithContext(context.Background())

	if _, err := os.Stat(pathEnv); err == nil {
		_ = godotenv.Load(pathEnv)
	}

	cfg := envConfig{}
	if err := env.Parse(&cfg); err != nil {
		log.Fatalf("failed to load configuration : %s", err.Error())
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

	mqttCfg := proxy.MQTTProxyConfig{
		BrokerURL: cfg.MQTTAddress,
		Password:  cfg.ThingKey,
		PropletID: cfg.ThingID,
		ChannelID: cfg.ChannelID,
	}

	httpCfg := proxy.HTTPProxyConfig{
		ChunkSize:    cfg.ChunkSize,
		Authenticate: cfg.Authenticate,
		Token:        cfg.Token,
		Username:     cfg.Username,
		Password:     cfg.Password,
		RegistryURL:  cfg.RegistryURL,
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
