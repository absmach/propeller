package propellerd

import (
	"context"
	"time"

	"github.com/absmach/magistrala/pkg/server"
	"github.com/absmach/propeller/cmd/manager"
	"github.com/spf13/cobra"
)

var (
	logLevel    = "info"
	port        = "7070"
	channelID   = ""
	thingID     = ""
	thingKey    = ""
	mqttAddress = "tcp://localhost:1883"
	mqttQOS     = 2
	mqttTimeout = 30 * time.Second
)

var managerCmd = []cobra.Command{
	{
		Use:   "start",
		Short: "Start manager",
		Long:  `Start manager.`,
		Run: func(cmd *cobra.Command, _ []string) {
			cfg := manager.Config{
				LogLevel: logLevel,
				Server: server.Config{
					Port: port,
				},
				ChannelID:   channelID,
				ThingID:     thingID,
				ThingKey:    thingKey,
				MQTTAddress: mqttAddress,
				MQTTQOS:     uint8(mqttQOS),
				MQTTTimeout: mqttTimeout,
			}
			ctx, cancel := context.WithCancel(cmd.Context())
			if err := manager.StartManager(ctx, cancel, cfg); err != nil {
				cmd.PrintErrf("failed to start manager: %s", err.Error())
			}
			cancel()
		},
	},
}

func NewManagerCmd() *cobra.Command {
	cmd := cobra.Command{
		Use:   "manager [start]",
		Short: "Manager management",
		Long:  `Create manager for Propeller.`,
	}

	for i := range managerCmd {
		cmd.AddCommand(&managerCmd[i])
	}

	cmd.PersistentFlags().StringVarP(
		&logLevel,
		"log-level",
		"l",
		logLevel,
		"Log level",
	)

	cmd.PersistentFlags().StringVarP(
		&port,
		"port",
		"p",
		port,
		"Manager HTTP Server Port",
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

	cmd.PersistentFlags().StringVarP(
		&mqttAddress,
		"mqtt-address",
		"m",
		mqttAddress,
		"MQTT Address",
	)

	cmd.PersistentFlags().IntVarP(
		&mqttQOS,
		"mqtt-qos",
		"q",
		mqttQOS,
		"MQTT QOS",
	)

	cmd.PersistentFlags().DurationVarP(
		&mqttTimeout,
		"mqtt-timeout",
		"o",
		mqttTimeout,
		"MQTT Timeout",
	)

	return &cmd
}
