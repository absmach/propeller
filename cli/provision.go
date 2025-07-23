package cli

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/0x6flab/namegenerator"
	"github.com/absmach/supermq/pkg/errors"
	smqSDK "github.com/absmach/supermq/pkg/sdk"
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
			username           string
			password           string
			err                error
			token              smqSDK.Token
			domainName         string
			domainRoute        string
			domainPermission   string
			domain             smqSDK.Domain
			managerClientName  string
			managerClient      smqSDK.Client
			numPropletsStr     string
			numProplets        int
			propletClients     []smqSDK.Client
			managerChannelName string
			managerChannel     smqSDK.Channel
		)
		form := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Enter your username?").
					Value(&username).
					Validate(func(str string) error {
						if str == "" {
							return errors.New("username is required")
						}

						return nil
					}),
				huh.NewInput().
					Title("Enter your password").
					EchoMode(huh.EchoModePassword).
					Value(&password).
					Validate(func(str string) error {
						if str == "" {
							return errors.New("password is required")
						}
						u := smqSDK.Login{
							Username: username,
							Password: password,
						}

						token, err = smqsdk.CreateToken(cmd.Context(), u)
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
					Title("Enter your domain route(leave empty to auto generate)").
					Value(&domainRoute),
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
						if domainRoute == "" {
							domainRoute = strings.ToLower(domainName)
						}
						domain = smqSDK.Domain{
							Name:       domainName,
							Route:      domainRoute,
							Permission: domainPermission,
						}
						domain, err = smqsdk.CreateDomain(cmd.Context(), domain, token.AccessToken)
						if err != nil {
							return errors.Wrap(errFailedToCreateDomain, err)
						}

						return nil
					}),
			),
			huh.NewGroup(
				huh.NewInput().
					Title("Enter your manager client name(leave empty to auto generate)").
					Value(&managerClientName).
					Validate(func(str string) error {
						if str == "" {
							managerClientName = namegen.Generate()
						}
						managerClient = smqSDK.Client{
							Name:   managerClientName,
							Tags:   []string{"manager", "propeller"},
							Status: "enabled",
						}
						managerClient, err = smqsdk.CreateClient(cmd.Context(), managerClient, domain.ID, token.AccessToken)
						if err != nil {
							return errors.Wrap(errFailedClientCreation, err)
						}

						return nil
					}),
			),
			huh.NewGroup(
				huh.NewInput().
					Title("Enter number of proplets to create (default: 1)").
					Value(&numPropletsStr).
					Validate(func(str string) error {
						switch str {
						case "":
							numProplets = 1
						default:
							numProplets, err = strconv.Atoi(str)
							if err != nil || numProplets < 1 {
								return errors.New("number of proplets must be a positive integer")
							}
						}

						propletClients = make([]smqSDK.Client, numProplets)
						for i := range numProplets {
							propletClientName := namegen.Generate()
							propletClient := smqSDK.Client{
								Name:   propletClientName,
								Tags:   []string{"proplet", "propeller"},
								Status: "enabled",
							}
							propletClient, err := smqsdk.CreateClient(cmd.Context(), propletClient, domain.ID, token.AccessToken)
							if err != nil {
								return errors.Wrap(errFailedClientCreation, err)
							}
							propletClients[i] = propletClient
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
						managerChannel, err = smqsdk.CreateChannel(cmd.Context(), managerChannel, domain.ID, token.AccessToken)
						if err != nil {
							return errors.Wrap(errFailedChannelCreation, err)
						}

						clientIDs := []string{managerClient.ID}
						for _, propletClient := range propletClients {
							clientIDs = append(clientIDs, propletClient.ID)
						}

						managerConns := smqSDK.Connection{
							ClientIDs:  clientIDs,
							ChannelIDs: []string{managerChannel.ID},
							Types:      []string{"publish", "subscribe"},
						}
						if err = smqsdk.Connect(cmd.Context(), managerConns, domain.ID, token.AccessToken); err != nil {
							return errors.Wrap(errFailedConnectionCreation, err)
						}

						return nil
					}),
			),
		)

		if err := form.Run(); err != nil {
			logErrorCmd(*cmd, errors.Wrap(errFailedConnectionCreation, err))

			return
		}

		configContent := fmt.Sprintf(`# SuperMQ Configuration

[manager]
domain_id = "%s"
client_id = "%s"
client_key = "%s"
channel_id = "%s"
`,
			domain.ID,
			managerClient.ID,
			managerClient.Credentials.Secret,
			managerChannel.ID,
		)

		for i, propletClient := range propletClients {
			var sectionName string
			switch len(propletClients) {
			case 1:
				sectionName = "[proplet]"
			default:
				sectionName = fmt.Sprintf("[proplet%d]", i+1)
			}

			propletConfig := fmt.Sprintf(`
%s
domain_id = "%s"
client_id = "%s"
client_key = "%s"
channel_id = "%s"
`,
				sectionName,
				domain.ID,
				propletClient.ID,
				propletClient.Credentials.Secret,
				managerChannel.ID,
			)
			configContent += propletConfig
		}

		if len(propletClients) > 0 {
			proxyConfig := fmt.Sprintf(`
[proxy]
domain_id = "%s"
client_id = "%s"
client_key = "%s"
channel_id = "%s"`,
				domain.ID,
				propletClients[0].ID,
				propletClients[0].Credentials.Secret,
				managerChannel.ID,
			)
			configContent += proxyConfig
		}

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
