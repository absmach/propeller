package main

import (
	"log"

	smqsdk "github.com/absmach/magistrala/pkg/sdk/go"
	"github.com/absmach/propeller/cli"
	"github.com/absmach/propeller/pkg/sdk"
	"github.com/spf13/cobra"
)

var (
	tlsVerification = false
	managerURL      = "http://localhost:7070"
	usersURL        = "http://localhost:9002"
	thingsURL       = "http://localhost:9000"
	domainsURL      = "http://localhost:8189"
	msgContentType  = string(smqsdk.CTJSONSenML)
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

			smqSDKConf := smqsdk.Config{
				UsersURL:       usersURL,
				ThingsURL:      thingsURL,
				DomainsURL:     domainsURL,
				MsgContentType: smqsdk.ContentType(msgContentType),
			}

			if smqSDKConf.MsgContentType == "" {
				smqSDKConf.MsgContentType = smqsdk.ContentType(msgContentType)
			}
			sdk := smqsdk.NewSDK(smqSDKConf)
			cli.SetSuperMQSDK(sdk)
		},
	}

	tasksCmd := cli.NewTasksCmd()
	provisionCmd := cli.NewProvisionCmd()

	rootCmd.AddCommand(tasksCmd, provisionCmd)

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
		&usersURL,
		"users-url",
		"u",
		usersURL,
		"Users service URL",
	)

	rootCmd.PersistentFlags().StringVarP(
		&thingsURL,
		"things-url",
		"t",
		thingsURL,
		"Things service URL",
	)

	rootCmd.PersistentFlags().StringVarP(
		&domainsURL,
		"domains-url",
		"d",
		domainsURL,
		"Domains service URL",
	)

	rootCmd.PersistentFlags().StringVarP(
		&msgContentType,
		"content-type",
		"c",
		msgContentType,
		"Message content type",
	)

	if err := rootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
}
