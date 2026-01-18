package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

var flCmd = []cobra.Command{
	{
		Use:   "round-start",
		Short: "Start a federated learning round",
		Long:  `Publish a round start message to trigger FL training. This is a sample FML application command.`,
		Run: func(cmd *cobra.Command, args []string) {
			roundID, _ := cmd.Flags().GetString("round-id")
			modelURI, _ := cmd.Flags().GetString("model-uri")
			taskWasmImage, _ := cmd.Flags().GetString("task-wasm-image")
			participants, _ := cmd.Flags().GetStringSlice("participants")
			kOfN, _ := cmd.Flags().GetInt("k-of-n")
			timeoutS, _ := cmd.Flags().GetInt("timeout-s")

			// Build hyperparams
			hyperparams := make(map[string]interface{})
			if epochs, _ := cmd.Flags().GetInt("epochs"); epochs > 0 {
				hyperparams["epochs"] = epochs
			}
			if lr, _ := cmd.Flags().GetFloat64("learning-rate"); lr > 0 {
				hyperparams["lr"] = lr
			}
			if batchSize, _ := cmd.Flags().GetInt("batch-size"); batchSize > 0 {
				hyperparams["batch_size"] = batchSize
			}

			// Validate required fields
			if roundID == "" {
				logErrorCmd(*cmd, fmt.Errorf("round-id is required"))
				return
			}
			if modelURI == "" {
				logErrorCmd(*cmd, fmt.Errorf("model-uri is required"))
				return
			}
			if taskWasmImage == "" {
				logErrorCmd(*cmd, fmt.Errorf("task-wasm-image is required"))
				return
			}
			if len(participants) == 0 {
				logErrorCmd(*cmd, fmt.Errorf("at least one participant is required"))
				return
			}

			// Build round start message
			roundStart := map[string]interface{}{
				"round_id":        roundID,
				"model_uri":       modelURI,
				"task_wasm_image": taskWasmImage,
				"participants":    participants,
				"k_of_n":          kOfN,
				"timeout_s":       timeoutS,
			}

			if len(hyperparams) > 0 {
				roundStart["hyperparams"] = hyperparams
			}

			roundStartJSON, err := json.MarshalIndent(roundStart, "", "  ")
			if err != nil {
				logErrorCmd(*cmd, err)
				return
			}

			fmt.Println("Round start message (publish to fl/rounds/start):")
			fmt.Println(string(roundStartJSON))
			fmt.Println("\nNote: This is a sample FML application. Use an MQTT client to publish this message.")
		},
	},
}

func NewFLCmd() *cobra.Command {
	cmd := cobra.Command{
		Use:   "fl",
		Short: "Federated Learning (sample FML application)",
		Long:  `Sample FML application commands. FL is implemented as an external application, not a core Propeller feature.`,
	}

	for i := range flCmd {
		cmd.AddCommand(&flCmd[i])
	}

	// Flags for round-start command
	roundStartCmd := &flCmd[0]
	roundStartCmd.Flags().StringP("round-id", "r", "", "Round identifier (required)")
	roundStartCmd.Flags().StringP("model-uri", "m", "", "Model URI (required)")
	roundStartCmd.Flags().StringP("task-wasm-image", "i", "", "Task Wasm image OCI ref (required)")
	roundStartCmd.Flags().StringSliceP("participants", "p", []string{}, "List of proplet IDs (required)")
	roundStartCmd.Flags().IntP("k-of-n", "k", 3, "Minimum participants required for aggregation")
	roundStartCmd.Flags().IntP("timeout-s", "t", 30, "Round timeout in seconds")
	roundStartCmd.Flags().IntP("epochs", "e", 1, "Local training epochs")
	roundStartCmd.Flags().Float64P("learning-rate", "l", 0.01, "Learning rate")
	roundStartCmd.Flags().IntP("batch-size", "b", 16, "Batch size")

	return &cmd
}
