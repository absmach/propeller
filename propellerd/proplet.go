package propellerd

import (
	"context"
	"log/slog"
	"time"

	propletcmd "github.com/absmach/propeller/cmd/proplet"
	"github.com/absmach/propeller/proplet"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var (
	livelinessInterval = 10 * time.Second
	registryURL        = ""
	registryToken      = ""
	id                 = uuid.NewString()
)

var propletCmd = []cobra.Command{
	{
		Use:   "start",
		Short: "Start manager",
		Long:  `Start manager.`,
		Run: func(cmd *cobra.Command, _ []string) {
			cfg := proplet.Config{
				LogLevel:           logLevel,
				InstanceID:         id,
				MQTTTimeout:        mqttTimeout,
				MQTTQoS:            uint8(mqttQOS),
				LivelinessInterval: livelinessInterval,
				MQTTAddress:        mqttAddress,
				RegistryURL:        registryURL,
				RegistryToken:      registryToken,
				ChannelID:          channelID,
				ThingID:            thingID,
				ThingKey:           thingKey,
			}
			if err := cfg.Validate(); err != nil {
				slog.Error("invalid config", slog.Any("error", err))

				return
			}

			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()
			if err := propletcmd.StartProplet(ctx, cancel, cfg); err != nil {
				slog.Error("failed to start manager", slog.String("error", err.Error()))
			}
		},
	},
}

func NewPropletCmd() *cobra.Command {
	cmd := cobra.Command{
		Use:   "proplet [start]",
		Short: "Proplet management",
		Long:  `Start proplet for Propeller.`,
	}

	for i := range propletCmd {
		cmd.AddCommand(&propletCmd[i])
	}

	cmd.PersistentFlags().StringVarP(
		&logLevel,
		"log-level",
		"l",
		logLevel,
		"Log level",
	)

	cmd.PersistentFlags().StringVarP(
		&id,
		"id",
		"i",
		id,
		"Proplet ID",
	)

	cmd.PersistentFlags().DurationVarP(
		&mqttTimeout,
		"mqtt-timeout",
		"o",
		mqttTimeout,
		"MQTT Timeout",
	)

	cmd.PersistentFlags().IntVarP(
		&mqttQOS,
		"mqtt-qos",
		"q",
		mqttQOS,
		"MQTT QOS",
	)

	cmd.PersistentFlags().DurationVarP(
		&livelinessInterval,
		"liveliness-interval",
		"I",
		livelinessInterval,
		"Liveliness Interval",
	)

	cmd.PersistentFlags().StringVarP(
		&mqttAddress,
		"mqtt-address",
		"m",
		mqttAddress,
		"MQTT Address",
	)

	cmd.PersistentFlags().StringVarP(
		&registryURL,
		"registry-url",
		"r",
		registryURL,
		"Registry URL",
	)

	cmd.PersistentFlags().StringVarP(
		&registryToken,
		"registry-token",
		"T",
		registryToken,
		"Registry Token",
	)

	cmd.PersistentFlags().StringVarP(
		&channelID,
		"channel-id",
		"c",
		channelID,
		"Manager Channel ID",
	)

	cmd.PersistentFlags().StringVarP(
		&thingID,
		"thing-id",
		"t",
		thingID,
		"Manager Thing ID",
	)

	cmd.PersistentFlags().StringVarP(
		&thingKey,
		"thing-key",
		"k",
		thingKey,
		"Thing Key",
	)

	return &cmd
}
