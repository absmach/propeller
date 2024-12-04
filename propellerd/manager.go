package propellerd

import (
	"context"

	"github.com/absmach/magistrala/pkg/server"
	"github.com/absmach/propeller/cmd/manager"
	"github.com/spf13/cobra"
)

var managerCmd = []cobra.Command{
	{
		Use:   "start",
		Short: "Start manager",
		Long:  `Start manager.`,
		Run: func(cmd *cobra.Command, _ []string) {
			cfg := manager.Config{
				LogLevel: "info",
				Server: server.Config{
					Port: "8080",
				},
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

	return &cmd
}
