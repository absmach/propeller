package main

import (
	"log"

	"github.com/absmach/propeller/cli"
	"github.com/absmach/propeller/pkg/sdk"
	smqsdk "github.com/absmach/supermq/pkg/sdk"
	"github.com/spf13/cobra"
)

func main() {
	msgContentType := string(smqsdk.CTJSONSenML)
	smqSDKConf := smqsdk.Config{
		UsersURL:       "http://localhost:9002",
		ClientsURL:     "http://localhost:9006",
		DomainsURL:     "http://localhost:9003",
		ChannelsURL:    "http://localhost:9005",
		MsgContentType: smqsdk.ContentType(msgContentType),
	}

	rootCmd := &cobra.Command{
		Use:   "propeller-cli",
		Short: "Propeller CLI",
		Long:  `Propeller CLI is a command line interface for interacting with Propeller components.`,
		PersistentPreRun: func(_ *cobra.Command, _ []string) {
			// Initialize Propeller SDK
			sdkConf := sdk.Config{
				ManagerURL:      cli.DefManagerURL,
				TLSVerification: cli.DefTLSVerification,
			}
			s := sdk.NewSDK(sdkConf)
			cli.SetSDK(s)

			// Initialize SuperMQ SDK
			if smqSDKConf.MsgContentType == "" {
				smqSDKConf.MsgContentType = smqsdk.ContentType(msgContentType)
			}
			smqs := smqsdk.NewSDK(smqSDKConf)
			cli.SetSuperMQSDK(smqs)
		},
	}

	tasksCmd := cli.NewTasksCmd()
	provisionCmd := cli.NewProvisionCmd()

	rootCmd.AddCommand(tasksCmd, provisionCmd)

	if err := rootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
}
