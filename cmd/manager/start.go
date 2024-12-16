package manager

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"time"

	"github.com/absmach/magistrala/pkg/jaeger"
	"github.com/absmach/magistrala/pkg/prometheus"
	"github.com/absmach/magistrala/pkg/server"
	httpserver "github.com/absmach/magistrala/pkg/server/http"
	"github.com/absmach/propeller/manager"
	"github.com/absmach/propeller/manager/api"
	"github.com/absmach/propeller/manager/middleware"
	"github.com/absmach/propeller/pkg/mqtt"
	"github.com/absmach/propeller/pkg/scheduler"
	"github.com/absmach/propeller/pkg/storage"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
	"golang.org/x/sync/errgroup"
)

const svcName = "manager"

type Config struct {
	LogLevel    string
	InstanceID  string
	MQTTAddress string
	MQTTQoS     uint8
	MQTTTimeout time.Duration
	ChannelID   string
	ThingID     string
	ThingKey    string
	Server      server.Config
	OTELURL     url.URL
	TraceRatio  float64
}

func StartManager(ctx context.Context, cancel context.CancelFunc, cfg Config) error {
	g, ctx := errgroup.WithContext(ctx)

	var level slog.Level
	if err := level.UnmarshalText([]byte(cfg.LogLevel)); err != nil {
		return fmt.Errorf("failed to parse log level: %s", err.Error())
	}
	logHandler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
	})
	logger := slog.New(logHandler)
	slog.SetDefault(logger)

	var tp trace.TracerProvider
	switch {
	case cfg.OTELURL == (url.URL{}):
		tp = noop.NewTracerProvider()
	default:
		sdktp, err := jaeger.NewProvider(ctx, svcName, cfg.OTELURL, "", cfg.TraceRatio)
		if err != nil {
			return fmt.Errorf("failed to initialize opentelemetry: %s", err.Error())
		}
		defer func() {
			if err := sdktp.Shutdown(ctx); err != nil {
				slog.Error("error shutting down tracer provider", slog.Any("error", err))
			}
		}()
		tp = sdktp
	}
	tracer := tp.Tracer(svcName)

	mqttPubSub, err := mqtt.NewPubSub(cfg.MQTTAddress, cfg.MQTTQoS, svcName, cfg.ThingID, cfg.ThingKey, cfg.ChannelID, cfg.MQTTTimeout, logger)
	if err != nil {
		return fmt.Errorf("failed to initialize mqtt pubsub: %s", err.Error())
	}

	svc := manager.NewService(
		storage.NewInMemoryStorage(),
		storage.NewInMemoryStorage(),
		storage.NewInMemoryStorage(),
		scheduler.NewRoundRobin(),
		mqttPubSub,
		cfg.ChannelID,
		logger,
	)
	svc = middleware.Logging(logger, svc)
	svc = middleware.Tracing(tracer, svc)
	counter, latency := prometheus.MakeMetrics(svcName, "api")
	svc = middleware.Metrics(counter, latency, svc)

	if err := svc.Subscribe(ctx); err != nil {
		return fmt.Errorf("failed to subscribe to manager channel: %s", err.Error())
	}

	hs := httpserver.NewServer(ctx, cancel, svcName, cfg.Server, api.MakeHandler(svc, logger, cfg.InstanceID), logger)

	g.Go(func() error {
		return hs.Start()
	})

	g.Go(func() error {
		return server.StopSignalHandler(ctx, cancel, logger, svcName, hs)
	})

	if err := g.Wait(); err != nil {
		logger.Error(fmt.Sprintf("%s service exited with error: %s", svcName, err))
	}

	return nil
}
