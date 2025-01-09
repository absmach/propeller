package main

import (
	"log"

	"github.com/absmach/propeller/cli"
	"github.com/absmach/propeller/pkg/sdk"
	"github.com/spf13/cobra"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "propeller-cli",
		Short: "Propeller CLI",
		Long:  `Propeller CLI is a command line interface for interacting with Propeller components.`,
		PersistentPreRun: func(_ *cobra.Command, _ []string) {
			sdkConf := sdk.Config{
				ManagerURL:      cli.DefManagerURL,
				TLSVerification: cli.DefTLSVerification,
			}
			s := sdk.NewSDK(sdkConf)
			cli.SetSDK(s)
		},
	}

	tasksCmd := cli.NewTasksCmd()

	rootCmd.AddCommand(tasksCmd)

	if err := rootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
}
