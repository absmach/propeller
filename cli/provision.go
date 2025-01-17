package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/0x6flab/namegenerator"
	smqSDK "github.com/absmach/magistrala/pkg/sdk/go"
	"github.com/absmach/supermq/pkg/errors"
	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
)

var (
	errFailedToCreateToken      = errors.New("failed to create access token")
	errFailedToCreateDomain     = errors.New("failed to create domain")
	errFailedChannelCreation    = errors.New("failed to create channel")
	errFailedClientCreation     = errors.New("failed to create client")
	errFailedConnectionCreation = errors.New("failed to create connection")

	smqsdk   smqSDK.SDK
	namegen  = namegenerator.NewGenerator()
	fileName = "config.toml"
)

const filePermission = 0o644

func SetSuperMQSDK(sdk smqSDK.SDK) {
	smqsdk = sdk
}

var provisionCmd = &cobra.Command{
	Use:   "provision",
	Short: "Provision resources",
	Long:  `Provision necessary resources for Propeller operation.`,
	Run: func(cmd *cobra.Command, args []string) {
		var (
			identity           string
			secret             string
			err                error
			token              smqSDK.Token
			domainName         string
			domainAlias        string
			domainPermission   string
			domain             smqSDK.Domain
			managerThingName   string
			managerThing       smqSDK.Thing
			propletThingName   string
			propletThing       smqSDK.Thing
			managerChannelName string
			managerChannel     smqSDK.Channel
		)
		form := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Enter your identity (e-mail)?").
					Value(&identity).
					Validate(func(str string) error {
						if str == "" {
							return errors.New("identity is required")
						}

						return nil
					}),
				huh.NewInput().
					Title("Enter your secret").
					EchoMode(huh.EchoModePassword).
					Value(&secret).
					Validate(func(str string) error {
						if str == "" {
							return errors.New("secret is required")
						}
						u := smqSDK.Login{
							Identity: identity,
							Secret:   secret,
						}

						token, err = smqsdk.CreateToken(u)
						if err != nil {
							return errors.Wrap(errFailedToCreateToken, err)
						}

						return nil
					}),
			),
			huh.NewGroup(
				huh.NewInput().
					Title("Enter your domain name(leave empty to auto generate)").
					Value(&domainName),
				huh.NewInput().
					Title("Enter your domain alias(leave empty to auto generate)").
					Value(&domainAlias),
				huh.NewSelect[string]().
					Title("Select your domain permission").
					Options(
						huh.NewOption("admin", "admin"),
						huh.NewOption("edit", "edit"),
						huh.NewOption("view", "view"),
					).
					Value(&domainPermission).
					Validate(func(str string) error {
						if domainName == "" {
							domainName = namegen.Generate()
						}
						if domainAlias == "" {
							domainAlias = strings.ToLower(domainName)
						}
						domain = smqSDK.Domain{
							Name:       domainName,
							Alias:      domainAlias,
							Permission: domainPermission,
						}
						domain, err = smqsdk.CreateDomain(domain, token.AccessToken)
						if err != nil {
							return errors.Wrap(errFailedToCreateDomain, err)
						}

						return nil
					}),
			),
			huh.NewGroup(
				huh.NewInput().
					Title("Enter your manager thing name(leave empty to auto generate)").
					Value(&managerThingName).
					Validate(func(str string) error {
						if str == "" {
							managerThingName = namegen.Generate()
						}
						managerThing = smqSDK.Thing{
							Name:   managerThingName,
							Tags:   []string{"manager", "propeller"},
							Status: "enabled",
						}
						managerThing, err = smqsdk.CreateThing(managerThing, domain.ID, token.AccessToken)
						if err != nil {
							return errors.Wrap(errFailedClientCreation, err)
						}

						return nil
					}),
			),
			huh.NewGroup(
				huh.NewInput().
					Title("Enter your proplet thing name(leave empty to auto generate)").
					Value(&propletThingName).
					Validate(func(str string) error {
						if str == "" {
							propletThingName = namegen.Generate()
						}
						propletThing = smqSDK.Thing{
							Name:   propletThingName,
							Tags:   []string{"proplet", "propeller"},
							Status: "enabled",
						}
						propletThing, err = smqsdk.CreateThing(propletThing, domain.ID, token.AccessToken)
						if err != nil {
							return errors.Wrap(errFailedClientCreation, err)
						}

						return nil
					}),
			), huh.NewGroup(
				huh.NewInput().
					Title("Enter your manager channel name(leave empty to auto generate)").
					Value(&managerChannelName).
					Validate(func(str string) error {
						if str == "" {
							managerChannelName = namegen.Generate()
						}
						managerChannel = smqSDK.Channel{
							Name:   managerChannelName,
							Status: "enabled",
						}
						managerChannel, err = smqsdk.CreateChannel(managerChannel, domain.ID, token.AccessToken)
						if err != nil {
							return errors.Wrap(errFailedChannelCreation, err)
						}

						managerConns := smqSDK.Connection{
							ThingID:   managerThing.ID,
							ChannelID: managerChannel.ID,
						}
						if err = smqsdk.Connect(managerConns, domain.ID, token.AccessToken); err != nil {
							return errors.Wrap(errFailedConnectionCreation, err)
						}

						propletConns := smqSDK.Connection{
							ThingID:   propletThing.ID,
							ChannelID: managerChannel.ID,
						}
						if err = smqsdk.Connect(propletConns, domain.ID, token.AccessToken); err != nil {
							return errors.Wrap(errFailedConnectionCreation, err)
						}

						return nil
					}),
			),
		)

		if err := form.Run(); err != nil {
			logErrorCmd(*cmd, errors.Wrap(errFailedConnectionCreation, err))
		}

		configContent := fmt.Sprintf(`# SuperMQ Configuration

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

		if err := os.WriteFile(fileName, []byte(configContent), filePermission); err != nil {
			logErrorCmd(*cmd, errors.New(fmt.Sprintf("failed to create %s file", fileName)))

			return
		}

		logSuccessCmd(*cmd, fmt.Sprintf("Successfully created %s file", fileName))
	},
}

func NewProvisionCmd() *cobra.Command {
	provisionCmd.PersistentFlags().StringVarP(
		&fileName,
		"file-name",
		"f",
		fileName,
		"The name of the file to create",
	)

	return provisionCmd
}
