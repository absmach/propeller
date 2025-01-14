package cli

import (
	"fmt"
	"os"

	"github.com/absmach/supermq/pkg/errors"
	smqSDK "github.com/absmach/supermq/pkg/sdk"
	"github.com/spf13/cobra"
)

var (
	errFailedToCreateToken      = errors.New("failed to create access token")
	errFailedToCreateDomain     = errors.New("failed to create domain")
	errFailedChannelCreation    = errors.New("failed to create channel")
	errFailedClientCreation     = errors.New("failed to create client")
	errFailedConnectionCreation = errors.New("failed to create connection")

	smqSDKInstance smqSDK.SDK
)

// SetSuperMQSDK sets supermq SDK instance.
func SetSuperMQSDK(s smqSDK.SDK) {
	smqSDKInstance = s
}

type Result struct {
	ManagerThing   smqSDK.Client  `json:"manager_thing,omitempty"`
	ManagerChannel smqSDK.Channel `json:"manager_channel,omitempty"`
	PropletThing   smqSDK.Client  `json:"proplet_thing,omitempty"`
	PropletChannel smqSDK.Channel `json:"proplet_channel,omitempty"`
}

var provisionCmd = &cobra.Command{
	Use:   "provision",
	Short: "Provision resources",
	Long:  `Provision necessary resources for Propeller operation.`,
	Run: func(cmd *cobra.Command, args []string) {
		u := smqSDK.Login{
			Username: "admin@example.com",
			Password: "12345678",
		}

		tkn, err := smqSDKInstance.CreateToken(u)
		if err != nil {
			logErrorCmd(*cmd, errors.Wrap(errFailedToCreateToken, err))
			return
		}

		domain := smqSDK.Domain{
			Name:       "demo",
			Alias:      "demo",
			Permission: "admin",
		}

		domain, err = smqSDKInstance.CreateDomain(domain, tkn.AccessToken)
		if err != nil {
			logErrorCmd(*cmd, errors.Wrap(errFailedToCreateDomain, err))
			return
		}

		managerThing := smqSDK.Client{
			Name:   "Propeller Manager",
			Tags:   []string{"manager", "propeller"},
			Status: "enabled",
		}

		managerThing, err = smqSDKInstance.CreateClient(managerThing, domain.ID, tkn.AccessToken)
		if err != nil {
			logErrorCmd(*cmd, errors.Wrap(errFailedClientCreation, err))
			return
		}

		propletThing := smqSDK.Client{
			Name:   "Propeller Proplet",
			Tags:   []string{"proplet", "propeller"},
			Status: "enabled",
		}

		propletThing, err = smqSDKInstance.CreateClient(propletThing, domain.ID, tkn.AccessToken)
		if err != nil {
			logErrorCmd(*cmd, errors.Wrap(errFailedClientCreation, err))
			return
		}

		managerChannel := smqSDK.Channel{
			Name:   "Propeller Manager",
			Status: "enabled",
		}
		managerChannel, err = smqSDKInstance.CreateChannel(managerChannel, domain.ID, tkn.AccessToken)
		if err != nil {
			logErrorCmd(*cmd, errors.Wrap(errFailedChannelCreation, err))
			return
		}

		conns := smqSDK.Connection{
			ClientIDs: []string{
				managerThing.ID,
				propletThing.ID,
			},
			ChannelIDs: []string{
				managerChannel.ID,
			},
			Types: []string{
				"publish",
				"subscribe",
			},
		}
		err = smqSDKInstance.Connect(conns, domain.ID, tkn.AccessToken)
		if err != nil {
			logErrorCmd(*cmd, errors.Wrap(errFailedConnectionCreation, err))
			return
		}

		res := Result{
			ManagerThing:   managerThing,
			ManagerChannel: managerChannel,
			PropletThing:   propletThing,
			PropletChannel: managerChannel,
		}

		// Create config.toml
		configContent := fmt.Sprintf(`# SuperMQ Configuration File

[manager]
thing_id = "%s"
thing_key = "%s"
channel_id = "%s"

[proplet]
thing_id = "%s"
thing_key = "%s"
channel_id = "%s"

[proxy]
thing_id = "%s"
thing_key = "%s"
channel_id = "%s"`,
			managerThing.ID,
			managerThing.Credentials.Secret,
			managerChannel.ID,
			propletThing.ID,
			propletThing.Credentials.Secret,
			managerChannel.ID,
			propletThing.ID,
			propletThing.Credentials.Secret,
			managerChannel.ID,
		)

		if err := os.WriteFile("config.toml", []byte(configContent), 0644); err != nil {
			logErrorCmd(*cmd, errors.New("failed to create config file"))
			return
		}

		logJSONCmd(*cmd, res)
	},
}

func NewProvisionCmd() *cobra.Command {
	return provisionCmd
}
