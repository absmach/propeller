package propellerd

import (
	"github.com/absmach/propeller/pkg/sdk"
	"github.com/spf13/cobra"
)

var (
	DefTLSVerification        = false
	DefManagerURL             = "http://localhost:7070"
	defOffset          uint64 = 0
	defLimit           uint64 = 10
)

var psdk sdk.SDK

func SetSDK(s sdk.SDK) {
	psdk = s
}

var tasksCmd = []cobra.Command{
	{
		Use:   "create <name>",
		Short: "Create task",
		Long:  `Create task.`,
		Run: func(cmd *cobra.Command, args []string) {
			if len(args) != 1 {
				logUsageCmd(*cmd, cmd.Use)

				return
			}

			t, err := psdk.CreateTask(sdk.Task{
				Name: args[0],
			})
			if err != nil {
				logErrorCmd(*cmd, err)

				return
			}
			logJSONCmd(*cmd, t)
		},
	},
	{
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
	},
	{
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
	},
	{
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
	},
	{
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
	},
	{
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
	},
}

func NewTasksCmd() *cobra.Command {
	cmd := cobra.Command{
		Use:   "tasks [create|view|update|delete|start|stop]",
		Short: "Tasks manager",
		Long:  `Create, view,  update, delete, start, stop tasks.`,
	}

	for i := range tasksCmd {
		cmd.AddCommand(&tasksCmd[i])
	}

	cmd.PersistentFlags().StringVarP(
		&DefManagerURL,
		"manager-url",
		"m",
		DefManagerURL,
		"Manager URL",
	)

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

	cmd.PersistentFlags().BoolVarP(
		&DefTLSVerification,
		"tls-verification",
		"v",
		DefTLSVerification,
		"TLS Verification",
	)

	return &cmd
}
