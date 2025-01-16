package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"time"

	"github.com/absmach/magistrala/pkg/server"
	"github.com/absmach/propeller/pkg/config"
	"github.com/absmach/propeller/pkg/mqtt"
	"github.com/absmach/propeller/proplet"
	"github.com/absmach/propeller/proplet/runtimes"
	"github.com/caarlos0/env/v11"
	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
)

const (
	svcName    = "proplet"
	configPath = "config.toml"
)

type envConfig struct {
	LogLevel            string        `env:"PROPLET_LOG_LEVEL"           envDefault:"info"`
	InstanceID          string        `env:"PROPLET_INSTANCE_ID"`
	MQTTAddress         string        `env:"PROPLET_MQTT_ADDRESS"        envDefault:"tcp://localhost:1883"`
	MQTTTimeout         time.Duration `env:"PROPLET_MQTT_TIMEOUT"        envDefault:"30s"`
	MQTTQoS             byte          `env:"PROPLET_MQTT_QOS"            envDefault:"2"`
	LivelinessInterval  time.Duration `env:"PROPLET_LIVELINESS_INTERVAL" envDefault:"10s"`
	ExternalWasmRuntime string        `env:"PROPLET_EXTERNAL_WASM_RUNTIME"     envDefault:""`
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	g, ctx := errgroup.WithContext(ctx)

	var conf *config.Config
	if _, err := os.Stat(configPath); err == nil {
		var err error
		conf, err = config.LoadConfig(configPath)
		if err != nil {
			log.Fatalf("failed to load TOML configuration: %s", err.Error())
		}
	}

	cfg := envConfig{}
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

	var thingID, thingKey, channelID string
	if conf != nil {
		thingID = conf.Proplet.ThingID
		thingKey = conf.Proplet.ThingKey
		channelID = conf.Proplet.ChannelID
	}

	mqttPubSub, err := mqtt.NewPubSub(cfg.MQTTAddress, cfg.MQTTQoS, cfg.InstanceID, thingID, thingKey, channelID, cfg.MQTTTimeout, logger)
	if err != nil {
		logger.Error("failed to initialize mqtt client", slog.Any("error", err))

		return
	}

	var runtime proplet.Runtime
	switch cfg.ExternalWasmRuntime != "" {
	case true:
		runtime = runtimes.NewHostRuntime(logger, mqttPubSub, channelID, cfg.ExternalWasmRuntime)
	default:
		runtime = runtimes.NewWazeroRuntime(logger, mqttPubSub, channelID)
	}

	service, err := proplet.NewService(ctx, channelID, thingID, thingKey, cfg.LivelinessInterval, mqttPubSub, logger, runtime)
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
