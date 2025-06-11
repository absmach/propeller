package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/url"
	"os"
	"time"

	"github.com/absmach/propeller"
	"github.com/absmach/propeller/manager"
	"github.com/absmach/propeller/manager/api"
	"github.com/absmach/propeller/manager/middleware"
	"github.com/absmach/propeller/pkg/mqtt"
	"github.com/absmach/propeller/pkg/scheduler"
	"github.com/absmach/propeller/pkg/storage"
	"github.com/absmach/supermq/pkg/jaeger"
	"github.com/absmach/supermq/pkg/prometheus"
	"github.com/absmach/supermq/pkg/server"
	httpserver "github.com/absmach/supermq/pkg/server/http"
	"github.com/caarlos0/env/v11"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
	"golang.org/x/sync/errgroup"
)

const (
	svcName       = "manager"
	defHTTPPort   = "7070"
	envPrefixHTTP = "MANAGER_HTTP_"
	configPath    = "config.toml"
)

type config struct {
	LogLevel    string        `env:"MANAGER_LOG_LEVEL"           envDefault:"info"`
	InstanceID  string        `env:"MANAGER_INSTANCE_ID"`
	MQTTAddress string        `env:"MANAGER_MQTT_ADDRESS"        envDefault:"tcp://localhost:1883"`
	MQTTQoS     uint8         `env:"MANAGER_MQTT_QOS"            envDefault:"2"`
	MQTTTimeout time.Duration `env:"MANAGER_MQTT_TIMEOUT"        envDefault:"30s"`
	DomainID    string        `env:"MANAGER_DOMAIN_ID"`
	ChannelID   string        `env:"MANAGER_CHANNEL_ID"`
	ClientID    string        `env:"MANAGER_CLIENT_ID"`
	ClientKey   string        `env:"MANAGER_CLIENT_KEY"`
	Server      server.Config
	OTELURL     url.URL `env:"MANAGER_OTEL_URL"`
	TraceRatio  float64 `env:"MANAGER_TRACE_RATIO" envDefault:"0"`
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
			cfg.DomainID = conf.Manager.DomainID
			cfg.ClientID = conf.Manager.ClientID
			cfg.ClientKey = conf.Manager.ClientKey
			cfg.ChannelID = conf.Manager.ChannelID
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

	var tp trace.TracerProvider
	switch cfg.OTELURL {
	case url.URL{}:
		tp = noop.NewTracerProvider()
	default:
		sdktp, err := jaeger.NewProvider(ctx, svcName, cfg.OTELURL, "", cfg.TraceRatio)
		if err != nil {
			logger.Error("failed to initialize opentelemetry", slog.String("error", err.Error()))

			return
		}
		defer func() {
			if err := sdktp.Shutdown(ctx); err != nil {
				logger.Error("error shutting down tracer provider", slog.Any("error", err))
			}
		}()
		tp = sdktp
	}
	tracer := tp.Tracer(svcName)

	mqttPubSub, err := mqtt.NewPubSub(cfg.MQTTAddress, cfg.MQTTQoS, svcName, cfg.ClientID, cfg.ClientKey, cfg.DomainID, cfg.ChannelID, cfg.MQTTTimeout, logger)
	if err != nil {
		logger.Error("failed to initialize mqtt pubsub", slog.String("error", err.Error()))

		return
	}

	svc := manager.NewService(
		storage.NewInMemoryStorage(),
		storage.NewInMemoryStorage(),
		storage.NewInMemoryStorage(),
		scheduler.NewRoundRobin(),
		mqttPubSub,
		cfg.DomainID,
		cfg.ChannelID,
		logger,
	)
	svc = middleware.Logging(logger, svc)
	svc = middleware.Tracing(tracer, svc)
	counter, latency := prometheus.MakeMetrics(svcName, "api")
	svc = middleware.Metrics(counter, latency, svc)

	if err := svc.Subscribe(ctx); err != nil {
		logger.Error("failed to subscribe to manager channel", slog.String("error", err.Error()))

		return
	}

	httpServerConfig := server.Config{Port: defHTTPPort}
	if err := env.ParseWithOptions(&httpServerConfig, env.Options{Prefix: envPrefixHTTP}); err != nil {
		logger.Error(fmt.Sprintf("failed to load %s HTTP server configuration : %s", svcName, err.Error()))

		return
	}

	hs := httpserver.NewServer(ctx, cancel, svcName, httpServerConfig, api.MakeHandler(svc, logger, cfg.InstanceID), logger)

	g.Go(func() error {
		return hs.Start()
	})

	g.Go(func() error {
		return server.StopSignalHandler(ctx, cancel, logger, svcName, hs)
	})

	if err := g.Wait(); err != nil {
		logger.Error(fmt.Sprintf("%s service exited with error: %s", svcName, err))
	}
}
