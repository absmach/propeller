package cli

import (
	"fmt"
	"os"

	smqSDK "github.com/absmach/magistrala/pkg/sdk/go"
	"github.com/absmach/supermq/pkg/errors"
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

const filePermission = 0o644

// SetSuperMQSDK sets supermq SDK instance.
func SetSuperMQSDK(s smqSDK.SDK) {
	smqSDKInstance = s
}

type Result struct {
	ManagerThing   smqSDK.Thing   `json:"manager_thing,omitempty"`
	ManagerChannel smqSDK.Channel `json:"manager_channel,omitempty"`
	PropletThing   smqSDK.Thing   `json:"proplet_thing,omitempty"`
	PropletChannel smqSDK.Channel `json:"proplet_channel,omitempty"`
}

var provisionCmd = &cobra.Command{
	Use:   "provision",
	Short: "Provision resources",
	Long:  `Provision necessary resources for Propeller operation.`,
	Run: func(cmd *cobra.Command, args []string) {
		u := smqSDK.Login{
			Identity: "admin@example.com",
			Secret:   "12345678",
		}

		tkn, err := smqSDKInstance.CreateToken(u)
		if err != nil {
			logErrorCmd(*cmd, errors.Wrap(errFailedToCreateToken, err))

			return
		}
		logSuccessCmd(*cmd, "Successfully created access token")

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
		logSuccessCmd(*cmd, "Successfully created domain")

		managerThing := smqSDK.Thing{
			Name:   "Propeller Manager",
			Tags:   []string{"manager", "propeller"},
			Status: "enabled",
		}

		managerThing, err = smqSDKInstance.CreateThing(managerThing, domain.ID, tkn.AccessToken)
		if err != nil {
			logErrorCmd(*cmd, errors.Wrap(errFailedClientCreation, err))

			return
		}
		logSuccessCmd(*cmd, "Successfully created manager client")

		propletThing := smqSDK.Thing{
			Name:   "Propeller Proplet",
			Tags:   []string{"proplet", "propeller"},
			Status: "enabled",
		}

		propletThing, err = smqSDKInstance.CreateThing(propletThing, domain.ID, tkn.AccessToken)
		if err != nil {
			logErrorCmd(*cmd, errors.Wrap(errFailedClientCreation, err))

			return
		}
		logSuccessCmd(*cmd, "Successfully created proplet client")

		managerChannel := smqSDK.Channel{
			Name:   "Propeller Manager",
			Status: "enabled",
		}
		managerChannel, err = smqSDKInstance.CreateChannel(managerChannel, domain.ID, tkn.AccessToken)
		if err != nil {
			logErrorCmd(*cmd, errors.Wrap(errFailedChannelCreation, err))

			return
		}
		logSuccessCmd(*cmd, "Successfully created manager channel")

		managerConns := smqSDK.Connection{
			ThingID:   managerThing.ID,
			ChannelID: managerChannel.ID,
		}
		err = smqSDKInstance.Connect(managerConns, domain.ID, tkn.AccessToken)
		if err != nil {
			logErrorCmd(*cmd, errors.Wrap(errFailedConnectionCreation, err))

			return
		}
		logSuccessCmd(*cmd, "Successfully created manager connections")

		propletConns := smqSDK.Connection{
			ThingID:   propletThing.ID,
			ChannelID: managerChannel.ID,
		}

		err = smqSDKInstance.Connect(propletConns, domain.ID, tkn.AccessToken)
		if err != nil {
			logErrorCmd(*cmd, errors.Wrap(errFailedConnectionCreation, err))

			return
		}
		logSuccessCmd(*cmd, "Successfully created proplet connections")

		res := Result{
			ManagerThing:   managerThing,
			ManagerChannel: managerChannel,
			PropletThing:   propletThing,
			PropletChannel: managerChannel,
		}

		configContent := fmt.Sprintf(`# SuperMQ Environment Configuration

# Manager Configuration
MANAGER_THING_ID=%s
MANAGER_THING_KEY=%s
MANAGER_CHANNEL_ID=%s

# Proplet Configuration
PROPLET_THING_ID=%s
PROPLET_THING_KEY=%s
PROPLET_CHANNEL_ID=%s

# Proxy Configuration
PROXY_THING_ID=%s
PROXY_THING_KEY=%s
PROXY_CHANNEL_ID=%s`,
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

		if err := os.WriteFile(".env", []byte(configContent), filePermission); err != nil {
			logErrorCmd(*cmd, errors.New("failed to create .env file"))

			return
		}
		logSuccessCmd(*cmd, "Successfully created .env file")

		logJSONCmd(*cmd, res)
	},
}

func NewProvisionCmd() *cobra.Command {
	return provisionCmd
}
