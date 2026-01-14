package cli

import (
	"encoding/json"
	"fmt"

	"github.com/absmach/propeller/pkg/sdk"
	"github.com/absmach/propeller/task"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var flCmd = []cobra.Command{
	{
		Use:   "create <name>",
		Short: "Create federated learning task",
		Long:  `Create a federated learning task with train or infer mode.`,
		Run: func(cmd *cobra.Command, args []string) {
			if len(args) != 1 {
				logUsageCmd(*cmd, cmd.Use)
				return
			}

			mode, _ := cmd.Flags().GetString("mode")
			if mode != "train" && mode != "infer" {
				logErrorCmd(*cmd, fmt.Errorf("mode must be 'train' or 'infer'"))
				return
			}

			imageURL, _ := cmd.Flags().GetString("image-url")
			modelRef, _ := cmd.Flags().GetString("model-ref")
			rounds, _ := cmd.Flags().GetUint64("rounds")
			clientsPerRound, _ := cmd.Flags().GetUint64("clients-per-round")
			minClients, _ := cmd.Flags().GetUint64("min-clients")
			roundTimeout, _ := cmd.Flags().GetUint64("round-timeout")
			updateFormat, _ := cmd.Flags().GetString("update-format")
			localEpochs, _ := cmd.Flags().GetUint64("local-epochs")
			batchSize, _ := cmd.Flags().GetUint64("batch-size")
			learningRate, _ := cmd.Flags().GetFloat64("learning-rate")

			if updateFormat == "" {
				updateFormat = "f32-delta"
			}

			jobID := uuid.NewString()

			flSpecSDK := &sdk.FLSpec{
				JobID:          jobID,
				RoundID:        1,
				GlobalVersion:  uuid.NewString(),
				MinParticipants: minClients,
				RoundTimeoutSec: roundTimeout,
				ClientsPerRound: clientsPerRound,
				TotalRounds:    rounds,
				UpdateFormat:   updateFormat,
				ModelRef:       modelRef,
				LocalEpochs:    localEpochs,
				BatchSize:      batchSize,
				LearningRate:   learningRate,
			}

			t, err := psdk.CreateTask(sdk.Task{
				Name:     args[0],
				Kind:     "federated",
				Mode:     mode,
				ImageURL: imageURL,
				FL:       flSpecSDK,
			})
			if err != nil {
				logErrorCmd(*cmd, err)
				return
			}
			logJSONCmd(*cmd, t)
		},
	},
	{
		Use:   "status <task-id>",
		Short: "View federated learning task status",
		Long:  `View the status and progress of a federated learning task.`,
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

			if t.FL == nil {
				logErrorCmd(*cmd, fmt.Errorf("task is not a federated learning task"))
				return
			}

			status := map[string]interface{}{
				"task_id":     t.ID,
				"name":        t.Name,
				"state":       task.State(t.State).String(),
				"mode":        t.Mode,
				"job_id":      t.FL.JobID,
				"round_id":    t.FL.RoundID,
				"total_rounds": t.FL.TotalRounds,
			}

			if t.Results != nil {
				status["results"] = t.Results
			}

			statusJSON, _ := json.MarshalIndent(status, "", "  ")
			fmt.Println(string(statusJSON))
		},
	},
}

func NewFLCmd() *cobra.Command {
	cmd := cobra.Command{
		Use:   "fl [create|status]",
		Short: "Federated Learning tasks",
		Long:  `Create and manage federated learning tasks.`,
	}

	for i := range flCmd {
		cmd.AddCommand(&flCmd[i])
	}

	// Flags for create command
	createCmd := &flCmd[0]
	createCmd.Flags().StringP("mode", "m", "train", "Task mode: train or infer")
	createCmd.Flags().StringP("image-url", "i", "", "OCI image URL for the Wasm workload")
	createCmd.Flags().StringP("model-ref", "r", "", "Model artifact reference (OCI ref)")
	createCmd.Flags().Uint64P("rounds", "n", 3, "Total number of FL rounds")
	createCmd.Flags().Uint64P("clients-per-round", "c", 2, "Number of clients per round")
	createCmd.Flags().Uint64P("min-clients", "k", 2, "Minimum clients required for aggregation")
	createCmd.Flags().Uint64P("round-timeout", "t", 300, "Round timeout in seconds")
	createCmd.Flags().StringP("update-format", "f", "f32-delta", "Update format (e.g., f32-delta, json-f64)")
	createCmd.Flags().Uint64P("local-epochs", "e", 1, "Local training epochs")
	createCmd.Flags().Uint64P("batch-size", "b", 32, "Batch size for training")
	createCmd.Flags().Float64P("learning-rate", "l", 0.01, "Learning rate")

	return &cmd
}
