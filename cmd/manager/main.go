package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/absmach/propeller"
	"github.com/absmach/propeller/manager"
	"github.com/absmach/propeller/manager/api"
	"github.com/absmach/propeller/manager/middleware"
	"github.com/absmach/propeller/pkg/mqtt"
	"github.com/absmach/propeller/pkg/plugin"
	"github.com/absmach/propeller/pkg/scheduler"
	pkgserver "github.com/absmach/propeller/pkg/server"
	"github.com/absmach/propeller/pkg/storage"
	"github.com/absmach/propeller/pkg/telemetry"
	"github.com/caarlos0/env/v11"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
	"golang.org/x/sync/errgroup"
)

const (
	svcName         = "manager"
	defHTTPPort     = "7070"
	envPrefixHTTP   = "MANAGER_HTTP_"
	configPath      = "config.toml"
	shutdownTimeout = 30 * time.Second
)

type config struct {
	LogLevel        string        `env:"MANAGER_LOG_LEVEL"              envDefault:"info"`
	MQTTAddress     string        `env:"MANAGER_MQTT_ADDRESS"           envDefault:"tcp://localhost:1883"`
	MQTTQoS         uint8         `env:"MANAGER_MQTT_QOS"               envDefault:"2"`
	MQTTTimeout     time.Duration `env:"MANAGER_MQTT_TIMEOUT"           envDefault:"30s"`
	MQTTTLSCAPath   string        `env:"MANAGER_MQTT_TLS_CA_CERT"`
	MQTTTLSCertPath string        `env:"MANAGER_MQTT_TLS_CLIENT_CERT"`
	MQTTTLSKeyPath  string        `env:"MANAGER_MQTT_TLS_CLIENT_KEY"`
	MQTTTLSInsecure bool          `env:"MANAGER_MQTT_TLS_INSECURE_SKIP_VERIFY"`
	TenantID        string        `env:"MANAGER_TENANT_ID"`
	ChannelID       string        `env:"MANAGER_CHANNEL_ID"`
	EntityID        string        `env:"MANAGER_ENTITY_ID"`
	APIKey          string        `env:"MANAGER_API_KEY"`
	CoordinatorURL  string        `env:"MANAGER_COORDINATOR_URL"`
	Server          pkgserver.Config
	OTELURL         url.URL `env:"MANAGER_OTEL_URL"`
	TraceRatio      float64 `env:"MANAGER_TRACE_RATIO" envDefault:"0"`
	PluginDir       string  `env:"MANAGER_PLUGIN_DIR"`
	JSONRPCControl  bool    `env:"MANAGER_JSONRPC_CONTROL_PLANE" envDefault:"false"`
}

func main() {
	exitCode := 0
	defer func() {
		if exitCode != 0 {
			os.Exit(exitCode)
		}
	}()

	cfg := config{}
	if err := env.Parse(&cfg); err != nil {
		log.Printf("failed to load configuration : %s", err.Error())
		exitCode = 1

		return
	}

	if err := ensureManagerCredentials(&cfg); err != nil {
		log.Printf("%s", err.Error())
		exitCode = 1

		return
	}

	var level slog.Level
	if err := level.UnmarshalText([]byte(cfg.LogLevel)); err != nil {
		log.Printf("failed to parse log level: %s", err.Error())
		exitCode = 1

		return
	}
	logHandler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
	})
	logger := slog.New(logHandler)
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	g, ctx := errgroup.WithContext(ctx)

	tp, shutdown, err := initTracerProvider(ctx, cfg, logger)
	if err != nil {
		exitCode = 1

		return
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		shutdown(shutdownCtx)
	}()
	tracer := tp.Tracer(svcName)

	var mqttTLS *mqtt.TLSConfig
	if cfg.MQTTTLSCAPath != "" || cfg.MQTTTLSCertPath != "" || cfg.MQTTTLSKeyPath != "" || cfg.MQTTTLSInsecure {
		mqttTLS = &mqtt.TLSConfig{
			CAPath:             cfg.MQTTTLSCAPath,
			CertPath:           cfg.MQTTTLSCertPath,
			KeyPath:            cfg.MQTTTLSKeyPath,
			InsecureSkipVerify: cfg.MQTTTLSInsecure,
		}
	}

	mqttPubSub, err := mqtt.NewPubSub(cfg.MQTTAddress, cfg.MQTTQoS, cfg.EntityID, cfg.EntityID, cfg.APIKey, cfg.TenantID, cfg.ChannelID, cfg.MQTTTimeout, logger, mqttTLS)
	if err != nil {
		logger.Error("failed to initialize mqtt pubsub", slog.String("error", err.Error()))
		exitCode = 1

		return
	}

	storageCfg := storage.Config{}
	if err := env.Parse(&storageCfg); err != nil {
		logger.Error("failed to load storage configuration", slog.String("error", err.Error()))
		exitCode = 1

		return
	}

	repos, err := storage.NewRepositories(storageCfg)
	if err != nil {
		logger.Error("failed to initialize storage", slog.String("error", err.Error()))
		exitCode = 1

		return
	}
	if repos.Closer != nil {
		defer func() {
			if err := repos.Closer.Close(); err != nil {
				logger.Error("database close error", slog.Any("error", err))
			}
		}()
	}

	pluginRegistry, err := plugin.LoadDirectory(ctx, cfg.PluginDir, logger)
	if err != nil {
		logger.Error("failed to load plugins", slog.String("error", err.Error()))
		exitCode = 1

		return
	}
	defer func() {
		if err := pluginRegistry.Close(context.Background()); err != nil {
			logger.Error("plugin registry close error", slog.Any("error", err))
		}
	}()

	svc, cronScheduler, workflowCoordinator := manager.NewService(
		repos,
		scheduler.NewRoundRobin(),
		mqttPubSub,
		cfg.TenantID,
		cfg.ChannelID,
		cfg.CoordinatorURL,
		logger,
		pluginRegistry,
		manager.WithJSONRPCControlPlane(cfg.JSONRPCControl),
	)
	svc = middleware.Plugin(pluginRegistry, logger, svc)
	svc = middleware.Logging(logger, svc)
	svc = middleware.Tracing(tracer, svc)
	counter, latency := telemetry.MakeMetrics(svcName, "api")
	svc = middleware.Metrics(counter, latency, svc)
	cronScheduler.SetService(svc)
	workflowCoordinator.SetService(svc)

	if err := svc.Subscribe(ctx); err != nil {
		logger.Error("failed to subscribe to manager channel", slog.String("error", err.Error()))
		exitCode = 1

		return
	}

	if err := svc.RecoverInterruptedTasks(ctx); err != nil {
		logger.Warn("failed to recover interrupted tasks from previous run", slog.Any("error", err))
	}

	g.Go(func() error {
		return cronScheduler.Start(ctx)
	})

	httpServerConfig := pkgserver.Config{Port: defHTTPPort}
	if err := env.ParseWithOptions(&httpServerConfig, env.Options{Prefix: envPrefixHTTP}); err != nil {
		logger.Error(fmt.Sprintf("failed to load %s HTTP server configuration : %s", svcName, err.Error()))
		exitCode = 1

		return
	}

	hs := pkgserver.NewServer(ctx, stop, svcName, httpServerConfig, api.MakeHandler(svc, logger, cfg.EntityID), logger)

	g.Go(func() error {
		return hs.Start()
	})

	g.Go(func() error {
		return pkgserver.StopSignalHandler(ctx, stop, logger, svcName, hs)
	})

	if err := g.Wait(); err != nil {
		logger.Error(fmt.Sprintf("%s service exited with error: %s", svcName, err))
		exitCode = 1
	}

	logger.Info("initiating graceful shutdown")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer shutdownCancel()

	if err := svc.Shutdown(shutdownCtx); err != nil {
		logger.Error("service shutdown error", slog.Any("error", err))
	}

	if err := mqttPubSub.Disconnect(shutdownCtx); err != nil {
		logger.Error("mqtt disconnect error", slog.Any("error", err))
	}

	logger.Info("graceful shutdown complete")
}

func ensureManagerCredentials(cfg *config) error {
	if cfg.TenantID == "" || cfg.EntityID == "" || cfg.APIKey == "" || cfg.ChannelID == "" {
		_, err := os.Stat(configPath)
		switch err {
		case nil:
			conf, err := propeller.LoadConfig(configPath)
			if err != nil {
				return fmt.Errorf("failed to load TOML configuration: %w", err)
			}
			cfg.TenantID = conf.Manager.TenantID
			cfg.EntityID = conf.Manager.EntityID
			cfg.APIKey = conf.Manager.APIKey
			cfg.ChannelID = conf.Manager.ChannelID
		default:
			return fmt.Errorf("failed to load TOML configuration: %w", err)
		}
	}

	if cfg.TenantID == "" || cfg.ChannelID == "" || cfg.EntityID == "" || cfg.APIKey == "" {
		return errors.New("MANAGER_TENANT_ID, MANAGER_CHANNEL_ID, MANAGER_ENTITY_ID, and MANAGER_API_KEY must be set")
	}

	return nil
}

func initTracerProvider(ctx context.Context, cfg config, logger *slog.Logger) (trace.TracerProvider, func(context.Context), error) {
	switch cfg.OTELURL {
	case url.URL{}:
		return noop.NewTracerProvider(), func(context.Context) {}, nil
	default:
		sdktp, err := telemetry.NewTracerProvider(ctx, svcName, cfg.OTELURL, "", cfg.TraceRatio)
		if err != nil {
			logger.Error("failed to initialize opentelemetry", slog.String("error", err.Error()))

			return nil, nil, err
		}
		otel.SetTracerProvider(sdktp)
		otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))

		return sdktp, func(ctx context.Context) {
			if err := sdktp.Shutdown(ctx); err != nil {
				logger.Error("error shutting down tracer provider", slog.Any("error", err))
			}
		}, nil
	}
}
