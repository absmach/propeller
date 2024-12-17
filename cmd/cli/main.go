package main

import (
	"log"

	"github.com/absmach/propeller/pkg/sdk"
	"github.com/absmach/propeller/propellerd"
	"github.com/spf13/cobra"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "propeller-cli",
		Short: "Propeller CLI",
		Long:  `Propeller CLI is a command line interface for interacting with Propeller components.`,
		PersistentPreRun: func(_ *cobra.Command, _ []string) {
			sdkConf := sdk.Config{
				ManagerURL:      propellerd.DefManagerURL,
				TLSVerification: propellerd.DefTLSVerification,
			}
			s := sdk.NewSDK(sdkConf)
			propellerd.SetSDK(s)
		},
	}

	tasksCmd := propellerd.NewTasksCmd()

	rootCmd.AddCommand(tasksCmd)

	if err := rootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
}
