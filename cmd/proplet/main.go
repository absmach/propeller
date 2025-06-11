package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"time"

	"github.com/absmach/propeller"
	"github.com/absmach/propeller/pkg/mqtt"
	"github.com/absmach/propeller/proplet"
	"github.com/absmach/propeller/proplet/runtimes"
	"github.com/absmach/supermq/pkg/server"
	"github.com/caarlos0/env/v11"
	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
)

const (
	svcName    = "proplet"
	configPath = "config.toml"
)

type config struct {
	LogLevel            string        `env:"PROPLET_LOG_LEVEL"             envDefault:"info"`
	InstanceID          string        `env:"PROPLET_INSTANCE_ID"`
	MQTTAddress         string        `env:"PROPLET_MQTT_ADDRESS"          envDefault:"tcp://localhost:1883"`
	MQTTTimeout         time.Duration `env:"PROPLET_MQTT_TIMEOUT"          envDefault:"30s"`
	MQTTQoS             byte          `env:"PROPLET_MQTT_QOS"              envDefault:"2"`
	LivelinessInterval  time.Duration `env:"PROPLET_LIVELINESS_INTERVAL"   envDefault:"10s"`
	DomainID            string        `env:"PROPLET_DOMAIN_ID"`
	ChannelID           string        `env:"PROPLET_CHANNEL_ID"`
	ClientID            string        `env:"PROPLET_CLIIENT_ID"`
	ClientKey           string        `env:"PROPLET_CLIIENT_KEY"`
	ExternalWasmRuntime string        `env:"PROPLET_EXTERNAL_WASM_RUNTIME" envDefault:""`
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

	if cfg.ClientID == "" || cfg.ClientKey == "" || cfg.ChannelID == "" {
		_, err := os.Stat(configPath)
		switch err {
		case nil:
			conf, err := propeller.LoadConfig(configPath)
			if err != nil {
				log.Fatalf("failed to load TOML configuration: %s", err.Error())
			}
			cfg.DomainID = conf.Proplet.DomainID
			cfg.ClientID = conf.Proplet.ClientID
			cfg.ClientKey = conf.Proplet.ClientKey
			cfg.ChannelID = conf.Proplet.ChannelID
		default:
			log.Fatalf("failed to load TOML configuration: %s", err.Error())
		}
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

	mqttPubSub, err := mqtt.NewPubSub(cfg.MQTTAddress, cfg.MQTTQoS, cfg.InstanceID, cfg.ClientID, cfg.ClientKey, cfg.DomainID, cfg.ChannelID, cfg.MQTTTimeout, logger)
	if err != nil {
		logger.Error("failed to initialize mqtt client", slog.Any("error", err))

		return
	}

	var runtime proplet.Runtime
	switch cfg.ExternalWasmRuntime != "" {
	case true:
		runtime = runtimes.NewHostRuntime(logger, mqttPubSub, cfg.DomainID, cfg.ChannelID, cfg.ExternalWasmRuntime)
	default:
		runtime = runtimes.NewWazeroRuntime(logger, mqttPubSub, cfg.DomainID, cfg.ChannelID)
	}

	service, err := proplet.NewService(ctx, cfg.DomainID, cfg.ChannelID, cfg.ClientID, cfg.ClientKey, cfg.LivelinessInterval, mqttPubSub, logger, runtime)
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
