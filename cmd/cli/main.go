package main

import (
	"log"

	"github.com/absmach/propeller/cli"
	"github.com/absmach/propeller/pkg/atomsdk"
	"github.com/absmach/propeller/pkg/sdk"
	"github.com/spf13/cobra"
)

var (
	tlsVerification = false
	managerURL      = "http://localhost:7070"
	atomURL         = "http://localhost:8080"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "propeller-cli",
		Short: "Propeller CLI",
		Long:  `Propeller CLI is a command line interface for interacting with Propeller components.`,
		PersistentPreRun: func(_ *cobra.Command, _ []string) {
			sdkConf := sdk.Config{
				ManagerURL:      managerURL,
				TLSVerification: tlsVerification,
			}
			s := sdk.NewSDK(sdkConf)
			cli.SetPropellerSDK(s)

			atomSDK := atomsdk.New(atomsdk.Config{
				AtomURL: atomURL,
			})
			cli.SetAtomSDK(atomSDK)
		},
	}

	tasksCmd := cli.NewTasksCmd()
	provisionCmd := cli.NewProvisionCmd()
	propletsCmd := cli.NewPropletsCmd()
	jobsCmd := cli.NewJobsCmd()
	workflowsCmd := cli.NewWorkflowsCmd()
	flCmd := cli.NewFLCmd()
	healthCmd := cli.NewHealthCmd()
	rpcCmd := cli.NewRPCCmd()

	rootCmd.AddCommand(tasksCmd, provisionCmd, propletsCmd, jobsCmd, workflowsCmd, flCmd, healthCmd, rpcCmd)

	rootCmd.PersistentFlags().StringVarP(
		&managerURL,
		"manager-url",
		"m",
		managerURL,
		"Manager URL",
	)

	rootCmd.PersistentFlags().BoolVarP(
		&tlsVerification,
		"tls-verification",
		"v",
		tlsVerification,
		"TLS Verification",
	)

	rootCmd.PersistentFlags().StringVarP(
		&atomURL,
		"atom-url",
		"a",
		atomURL,
		"Atom (identity & authorization) URL",
	)

	if err := rootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
}
