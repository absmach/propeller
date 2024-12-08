package main

import (
	"log"

	"github.com/absmach/propeller/propellerd"
	"github.com/spf13/cobra"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "propellerd",
		Short: "Propeller Daemon",
		Long:  `Propeller Daemon is a daemon that manages the lifecycle of Propeller components.`,
	}

	managerCmd := propellerd.NewManagerCmd()

	rootCmd.AddCommand(managerCmd)

	if err := rootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
}
