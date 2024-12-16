package proplet

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/absmach/propeller/pkg/mqtt"
	"github.com/absmach/propeller/proplet"
)

func StartProplet(ctx context.Context, cancel context.CancelFunc, cfg proplet.Config) error {
	var level slog.Level
	if err := level.UnmarshalText([]byte(cfg.LogLevel)); err != nil {
		return fmt.Errorf("failed to parse log level: %s", err.Error())
	}
	logHandler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
	})
	logger := slog.New(logHandler)
	slog.SetDefault(logger)

	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		sig := <-sigChan
		logger.Info("Received shutdown signal", slog.String("signal", sig.String()))
		cancel()
	}()

	if cfg.RegistryURL != "" {
		if err := checkRegistryConnectivity(ctx, cfg.RegistryURL, cfg.RegistryTimeout); err != nil {
			return errors.Join(errors.New("failed to connect to registry URL"), err)
		}

		logger.Info("successfully connected to registry URL", slog.String("url", cfg.RegistryURL))
	}

	mqttPubSub, err := mqtt.NewPubSub(cfg.MQTTAddress, cfg.MQTTQoS, cfg.InstanceID, cfg.ThingID, cfg.ThingKey, cfg.ChannelID, cfg.MQTTTimeout, logger)
	if err != nil {
		return errors.Join(errors.New("failed to initialize mqtt client"), err)
	}
	wazero := proplet.NewWazeroRuntime(logger, mqttPubSub, cfg.ChannelID)

	service, err := proplet.NewService(ctx, cfg, mqttPubSub, logger, wazero)
	if err != nil {
		return errors.Join(errors.New("failed to initialize service"), err)
	}

	if err := service.Run(ctx, logger); err != nil {
		return errors.Join(errors.New("failed to run service"), err)
	}

	return nil
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
		return fmt.Errorf("fegistry returned unexpected status: %d", resp.StatusCode)
	}

	return nil
}
