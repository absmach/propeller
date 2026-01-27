package cli

import (
	"github.com/absmach/propeller/pkg/sdk"
	"github.com/spf13/cobra"
)

var (
	defOffset uint64 = 0
	defLimit  uint64 = 10
	cliArgs   []string
)

var psdk sdk.SDK

func SetPropellerSDK(s sdk.SDK) {
	psdk = s
}

func NewTasksCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tasks [create|view|update|delete|start|stop]",
		Short: "Tasks manager",
		Long:  `Create, view, update, delete, start, stop tasks.`,
	}

	// Create command with cli-args flag for wasmtime arguments
	createCmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create task",
		Long: `Create task with optional CLI arguments for wasmtime.

Examples:
  # Create a basic task
  propeller-cli tasks create my-task

  # Create a wasi-nn task with OpenVINO
  propeller-cli tasks create wasi-nn-inference --cli-args="-S,nn,--dir=/home/proplet/fixture::fixture"`,
		Run: func(cmd *cobra.Command, args []string) {
			if len(args) != 1 {
				logUsageCmd(*cmd, cmd.Use)

				return
			}

			t, err := psdk.CreateTask(sdk.Task{
				Name:    args[0],
				CLIArgs: cliArgs,
			})
			if err != nil {
				logErrorCmd(*cmd, err)

				return
			}
			logJSONCmd(*cmd, t)
		},
	}

	// Add flags to create command
	createCmd.Flags().StringSliceVar(
		&cliArgs,
		"cli-args",
		[]string{},
		"CLI arguments to pass to wasmtime (comma-separated, e.g., -S,nn,--dir=/path::guest)",
	)

	viewCmd := &cobra.Command{
		Use:   "view <id>",
		Short: "View task",
		Long:  `View task.`,
		Run: func(cmd *cobra.Command, args []string) {
			if len(args) != 1 {
				logUsageCmd(*cmd, cmd.Use)

				return
			}

			t, err := psdk.GetTask(args[0])
			if err != nil {
				logErrorCmd(*cmd, err)

				return
			}
			logJSONCmd(*cmd, t)
		},
	}

	updateCmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update task",
		Long:  `Update task.`,
		Run: func(cmd *cobra.Command, args []string) {
			if len(args) != 1 {
				logUsageCmd(*cmd, cmd.Use)
				return
			}

			t, err := psdk.UpdateTask(sdk.Task{
				ID: args[0],
			})
			if err != nil {
				logErrorCmd(*cmd, err)

				return
			}
			logJSONCmd(*cmd, t)
		},
	}

	deleteCmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete task",
		Long:  `Delete task.`,
		Run: func(cmd *cobra.Command, args []string) {
			if len(args) != 1 {
				logUsageCmd(*cmd, cmd.Use)

				return
			}

			if err := psdk.DeleteTask(args[0]); err != nil {
				logErrorCmd(*cmd, err)

				return
			}
			logOKCmd(*cmd)
		},
	}

	startCmd := &cobra.Command{
		Use:   "start <id>",
		Short: "Start task",
		Long:  `Start task.`,
		Run: func(cmd *cobra.Command, args []string) {
			if len(args) != 1 {
				logUsageCmd(*cmd, cmd.Use)

				return
			}

			if err := psdk.StartTask(args[0]); err != nil {
				logErrorCmd(*cmd, err)

				return
			}
			logOKCmd(*cmd)
		},
	}

	stopCmd := &cobra.Command{
		Use:   "stop <id>",
		Short: "Stop task",
		Long:  `Stop task.`,
		Run: func(cmd *cobra.Command, args []string) {
			if len(args) != 1 {
				logUsageCmd(*cmd, cmd.Use)

				return
			}

			if err := psdk.StopTask(args[0]); err != nil {
				logErrorCmd(*cmd, err)

				return
			}
			logOKCmd(*cmd)
		},
	}

	// Add all subcommands
	cmd.AddCommand(createCmd)
	cmd.AddCommand(viewCmd)
	cmd.AddCommand(updateCmd)
	cmd.AddCommand(deleteCmd)
	cmd.AddCommand(startCmd)
	cmd.AddCommand(stopCmd)

	// Global flags for pagination
	cmd.PersistentFlags().Uint64VarP(
		&defOffset,
		"offset",
		"o",
		defOffset,
		"Offset",
	)

	cmd.PersistentFlags().Uint64VarP(
		&defLimit,
		"limit",
		"l",
		defLimit,
		"Limit",
	)

	return cmd
}
