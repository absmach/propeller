package cli

import (
	"encoding/json"

	"github.com/spf13/cobra"
)

func NewRPCCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rpc [call|methods]",
		Short: "JSON-RPC client",
		Long:  `Call manager methods over JSON-RPC.`,
	}

	cmd.AddCommand(newRPCCallCmd())

	return cmd
}

func newRPCCallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "call <method> [params]",
		Short: "Call a JSON-RPC method",
		Long: `Call a JSON-RPC method on the manager.

Params are a JSON object or array. When omitted the method is called
without params.

Examples:
  propeller-cli rpc call proplet.list
  propeller-cli rpc call task.start '{"id":"b1d10738-c5d7-4ff1-8f4d-b9328ce6f040"}'
  propeller-cli rpc call task.list '{"limit":5}'`,
		Run: func(cmd *cobra.Command, args []string) {
			if len(args) < 1 || len(args) > 2 {
				logUsageCmd(*cmd, cmd.Use)

				return
			}

			var params any
			if len(args) == 2 {
				if err := json.Unmarshal([]byte(args[1]), &params); err != nil {
					logErrorCmd(*cmd, err)

					return
				}
			}

			result, err := psdk.Call(args[0], params)
			if err != nil {
				logErrorCmd(*cmd, err)

				return
			}

			var decoded any
			if err := json.Unmarshal(result, &decoded); err != nil {
				logErrorCmd(*cmd, err)

				return
			}
			logJSONCmd(*cmd, decoded)
		},
	}
}
