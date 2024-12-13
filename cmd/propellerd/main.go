package main

import (
	"log"

	"github.com/absmach/propeller/pkg/sdk"
	"github.com/absmach/propeller/propellerd"
	"github.com/spf13/cobra"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "propellerd",
		Short: "Propeller Daemon",
		Long:  `Propeller Daemon is a daemon that manages the lifecycle of Propeller components.`,
		PersistentPreRun: func(_ *cobra.Command, _ []string) {
			sdkConf := sdk.Config{
				ManagerURL:      propellerd.DefManagerURL,
				TLSVerification: propellerd.DefTLSVerification,
			}
			s := sdk.NewSDK(sdkConf)
			propellerd.SetSDK(s)
		},
	}

	managerCmd := propellerd.NewManagerCmd()
	tasksCmd := propellerd.NewTasksCmd()
	propletCmd := propellerd.NewPropletCmd()

	rootCmd.AddCommand(managerCmd)
	rootCmd.AddCommand(tasksCmd)
	rootCmd.AddCommand(propletCmd)

	if err := rootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
}
