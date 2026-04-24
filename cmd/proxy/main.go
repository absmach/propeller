package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/absmach/propeller"
	"github.com/absmach/propeller/pkg/mqtt"
	"github.com/absmach/propeller/proxy"
	"github.com/absmach/supermq/pkg/jaeger"
	"github.com/caarlos0/env/v11"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
	"golang.org/x/sync/errgroup"
)

const (
	svcName        = "proxy"
	configPath     = "config.toml"
	defMetricsPort = "7071"
)

type config struct {
	LogLevel    string        `env:"PROXY_LOG_LEVEL"    envDefault:"info"`
	MQTTAddress string        `env:"PROXY_MQTT_ADDRESS" envDefault:"tcp://localhost:1883"`
	MQTTTimeout time.Duration `env:"PROXY_MQTT_TIMEOUT" envDefault:"30s"`
	MQTTQoS     byte          `env:"PROXY_MQTT_QOS"     envDefault:"2"`
	DomainID    string        `env:"PROXY_DOMAIN_ID"`
	ChannelID   string        `env:"PROXY_CHANNEL_ID"`
	ClientID    string        `env:"PROXY_CLIENT_ID"`
	ClientKey   string        `env:"PROXY_CLIENT_KEY"`
	// HTTP Registry configuration
	ChunkSize    int    `env:"PROXY_CHUNK_SIZE"            envDefault:"512000"`
	Authenticate bool   `env:"PROXY_AUTHENTICATE"          envDefault:"false"`
	Token        string `env:"PROXY_REGISTRY_TOKEN"        envDefault:""`
	Username     string `env:"PROXY_REGISTRY_USERNAME"     envDefault:""`
	Password     string `env:"PROXY_REGISTRY_PASSWORD"     envDefault:""`
	RegistryURL  string `env:"PROXY_REGISTRY_URL,notEmpty"`
	// Observability
	MetricsPort string  `env:"PROXY_METRICS_PORT" envDefault:"7071"`
	OTELURL     url.URL `env:"PROXY_OTEL_URL"`
	TraceRatio  float64 `env:"PROXY_TRACE_RATIO" envDefault:"0"`
}

func main() {
	cfg := config{}
	if err := env.Parse(&cfg); err != nil {
		log.Fatalf("failed to load configuration : %s", err.Error())
	}

	if cfg.DomainID == "" || cfg.ClientID == "" || cfg.ClientKey == "" || cfg.ChannelID == "" {
		_, err := os.Stat(configPath)
		switch err {
		case nil:
			conf, err := propeller.LoadConfig(configPath)
			if err != nil {
				log.Fatalf("failed to load TOML configuration: %s", err.Error())
			}
			cfg.DomainID = conf.Proxy.DomainID
			cfg.ClientID = conf.Proxy.ClientID
			cfg.ClientKey = conf.Proxy.ClientKey
			cfg.ChannelID = conf.Proxy.ChannelID
		default:
			log.Fatalf("failed to load TOML configuration: %s", err.Error())
		}
	}

	if cfg.DomainID == "" || cfg.ChannelID == "" || cfg.ClientID == "" || cfg.ClientKey == "" {
		log.Fatal("PROXY_DOMAIN_ID, PROXY_CHANNEL_ID, PROXY_CLIENT_ID, and PROXY_CLIENT_KEY must be set")
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

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	g, ctx := errgroup.WithContext(ctx)

	var tp trace.TracerProvider
	switch cfg.OTELURL {
	case url.URL{}:
		tp = noop.NewTracerProvider()
	default:
		sdktp, err := jaeger.NewProvider(ctx, svcName, cfg.OTELURL, "", cfg.TraceRatio)
		if err != nil {
			logger.Error("failed to initialize jaeger tracing", slog.String("error", err.Error()))
			//nolint:gocritic
			os.Exit(1)
		}
		defer func() {
			if err := sdktp.Shutdown(ctx); err != nil {
				logger.Error("error shutting down tracer provider", slog.Any("error", err))
			}
		}()
		tp = sdktp
	}
	_ = tp

	mqttPubSub, err := mqtt.NewPubSub(cfg.MQTTAddress, cfg.MQTTQoS, cfg.ClientID, cfg.ClientID, cfg.ClientKey, cfg.DomainID, cfg.ChannelID, cfg.MQTTTimeout, logger)
	if err != nil {
		logger.Error("failed to initialize mqtt client", slog.Any("error", err))

		return
	}
	defer func() {
		if err := mqttPubSub.Disconnect(ctx); err != nil {
			slog.Error("failed to disconnect MQTT client", "error", err)
		}
	}()

	httpCfg := proxy.HTTPProxyConfig{
		ChunkSize:    cfg.ChunkSize,
		Authenticate: cfg.Authenticate,
		Token:        cfg.Token,
		Username:     cfg.Username,
		Password:     cfg.Password,
		RegistryURL:  cfg.RegistryURL,
	}

	logger.Info("successfully initialized MQTT and HTTP config")

	service, err := proxy.NewService(ctx, mqttPubSub, cfg.DomainID, cfg.ChannelID, httpCfg, logger)
	if err != nil {
		logger.Error("failed to create proxy service", slog.Any("error", err))

		return
	}

	logger.Info("starting proxy service")

	if err := mqttPubSub.Subscribe(ctx, fmt.Sprintf(proxy.SubTopic, cfg.DomainID, cfg.ChannelID), handle(logger, service.ContainerChan())); err != nil {
		logger.Error("failed to subscribe to container requests", slog.Any("error", err))

		return
	}

	slog.Info("successfully subscribed to topic")

	// Start Prometheus metrics server
	metricsPort := cfg.MetricsPort
	if metricsPort == "" {
		metricsPort = defMetricsPort
	}
	g.Go(func() error {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.Handler())
		mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("OK"))
		})
		server := &http.Server{
			Addr:    ":" + metricsPort,
			Handler: mux,
		}
		logger.Info("starting metrics server", slog.String("port", metricsPort))

		return server.ListenAndServe()
	})

	g.Go(func() error {
		return service.StreamHTTP(ctx)
	})

	g.Go(func() error {
		return service.StreamMQTT(ctx)
	})

	if err := g.Wait(); err != nil {
		logger.Error(fmt.Sprintf("%s service exited with error: %s", svcName, err))
	}
}

func handle(logger *slog.Logger, containerChan chan<- string) func(topic string, msg map[string]any) error {
	return func(topic string, msg map[string]any) error {
		appName, ok := msg["app_name"].(string)
		if !ok {
			return errors.New("failed to unmarshal app_name")
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		select {
		case containerChan <- appName:
			logger.Info("Received container request", slog.String("app_name", appName))

			return nil
		case <-ctx.Done():
			logger.Error("Channel full, request timed out waiting for containerChan slot",
				slog.String("app_name", appName))

			return errors.New("timeout waiting for container channel slot")
		}
	}
}
