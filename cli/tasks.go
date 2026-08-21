package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/absmach/propeller/pkg/sdk"
	"github.com/spf13/cobra"
)

var (
	defOffset uint64 = 0
	defLimit  uint64 = 10
)

var psdk sdk.SDK

func SetPropellerSDK(s sdk.SDK) {
	psdk = s
}

// taskFlags holds the values of the flags shared by the task create/update
// commands. Values are only applied to the payload when the user actually set
// the corresponding flag, so update does not clobber fields that were not
// provided.
type taskFlags struct {
	imageURL        string
	cliArgs         []string
	inputs          []string
	env             []string
	daemon          bool
	latent          bool
	encrypted       bool
	kbsResourcePath string
	propletID       string
	broadcast       bool
	priority        int
	mode            string
	kind            string
	schedule        string
	timezone        string
	isRecurring     bool
	metadata        []string
	wasiSecurity    string
	wasiPEP         string
}

func registerTaskFlags(cmd *cobra.Command, tf *taskFlags) {
	cmd.Flags().StringVar(&tf.imageURL, "image-url", "", "URL or OCI registry reference for the Wasm binary")
	cmd.Flags().StringSliceVar(&tf.cliArgs, "cli-args", nil, "CLI arguments to pass to wasmtime (comma-separated, e.g., -S,nn,--dir=/path::guest)")
	cmd.Flags().StringSliceVar(&tf.inputs, "inputs", nil, "Input values passed to the Wasm module (comma-separated)")
	cmd.Flags().StringSliceVar(&tf.env, "env", nil, "Environment variables KEY=VALUE (comma-separated or repeatable)")
	cmd.Flags().BoolVar(&tf.daemon, "daemon", false, "Run continuously until stopped")
	cmd.Flags().BoolVar(&tf.latent, "latent", false, "Precompile on start and keep resident for on-demand invocation")
	cmd.Flags().BoolVar(&tf.encrypted, "encrypted", false, "Wasm binary is encrypted (requires KBS)")
	cmd.Flags().StringVar(&tf.kbsResourcePath, "kbs-resource-path", "", "KBS resource path for encrypted binaries")
	cmd.Flags().StringVar(&tf.propletID, "proplet-id", "", "Target proplet ID (mutually exclusive with --broadcast)")
	cmd.Flags().BoolVar(&tf.broadcast, "broadcast", false, "Run on all proplets (mutually exclusive with --proplet-id)")
	cmd.Flags().IntVar(&tf.priority, "priority", 0, "Dispatch priority (0-100, default 50)")
	cmd.Flags().StringVar(&tf.mode, "mode", "", "Task mode (infer or train)")
	cmd.Flags().StringVar(&tf.kind, "kind", "", "Task kind (standard or federated)")
	cmd.Flags().StringVar(&tf.schedule, "schedule", "", "Cron expression for scheduled tasks")
	cmd.Flags().StringVar(&tf.timezone, "timezone", "", "Timezone for scheduled tasks (default UTC)")
	cmd.Flags().BoolVar(&tf.isRecurring, "is-recurring", false, "Re-run according to schedule")
	cmd.Flags().StringSliceVar(&tf.metadata, "metadata", nil, "Metadata KEY=VALUE (comma-separated or repeatable)")
	cmd.Flags().StringVar(&tf.wasiSecurity, "wasi-security", "", "Path to a TOML WASI security policy applied to the task's Wasmtime sandbox")
	cmd.Flags().StringVar(&tf.wasiPEP, "wasi-pep", "", "WASI policy enforcement point reference")
}

func taskFromFlags(cmd *cobra.Command, tf *taskFlags, id, name string) (sdk.Task, error) {
	t := sdk.Task{
		ID:   id,
		Name: name,
	}

	f := cmd.Flags()
	if f.Changed("image-url") {
		t.ImageURL = tf.imageURL
	}
	if f.Changed("cli-args") {
		t.CLIArgs = tf.cliArgs
	}
	if f.Changed("inputs") {
		t.Inputs = tf.inputs
	}
	if f.Changed("env") {
		t.Env = toMap(tf.env)
	}
	if f.Changed("daemon") {
		t.Daemon = tf.daemon
	}
	if f.Changed("latent") {
		t.Latent = tf.latent
	}
	if f.Changed("encrypted") {
		t.Encrypted = tf.encrypted
	}
	if f.Changed("kbs-resource-path") {
		t.KBSResourcePath = tf.kbsResourcePath
	}
	if f.Changed("proplet-id") {
		t.PropletID = tf.propletID
	}
	if f.Changed("broadcast") {
		t.Broadcast = tf.broadcast
	}
	if f.Changed("priority") {
		t.Priority = tf.priority
	}
	if f.Changed("mode") {
		t.Mode = tf.mode
	}
	if f.Changed("kind") {
		t.Kind = tf.kind
	}
	if f.Changed("schedule") {
		t.Schedule = tf.schedule
	}
	if f.Changed("timezone") {
		t.Timezone = tf.timezone
	}
	if f.Changed("is-recurring") {
		t.IsRecurring = tf.isRecurring
	}
	if f.Changed("metadata") {
		t.Metadata = toMapAny(tf.metadata)
	}

	if f.Changed("wasi-security") || f.Changed("wasi-pep") {
		t.ExtraConfig = make(map[string]any)
	}

	if f.Changed("wasi-security") {
		policy, err := os.ReadFile(tf.wasiSecurity)
		if err != nil {
			return sdk.Task{}, fmt.Errorf("failed to read WASI security policy: %w", err)
		}

		t.ExtraConfig["wasi_security"] = string(policy)
	}

	if f.Changed("wasi-pep") {
		t.ExtraConfig["wasi_pep"] = tf.wasiPEP
	}

	return t, nil
}

func toMap(pairs []string) map[string]string {
	m := make(map[string]string)
	for _, p := range pairs {
		k, v, ok := strings.Cut(p, "=")
		if ok {
			m[k] = v
		}
	}

	return m
}

func toMapAny(pairs []string) map[string]any {
	m := make(map[string]any)
	for _, p := range pairs {
		k, v, ok := strings.Cut(p, "=")
		if ok {
			m[k] = v
		}
	}

	return m
}

func newTaskCreateCmd() *cobra.Command {
	flags := taskFlags{}
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create task",
		Long: `Create task with optional CLI arguments for wasmtime.

Examples:
  # Create a basic task
  propeller-cli tasks create my-task

  # Create a wasi-nn task with OpenVINO
  propeller-cli tasks create wasi-nn-inference --cli-args="-S,nn,--dir=/home/proplet/fixture::fixture"

  # Create a task from an OCI image reference
  propeller-cli tasks create my-task --image-url docker.io/myorg/app:v1

  # Create a latent task that stays resident and is invoked on demand
  propeller-cli tasks create greet --latent`,
		Run: func(cmd *cobra.Command, args []string) {
			if len(args) != 1 {
				logUsageCmd(*cmd, cmd.Use)

				return
			}

			payload, err := taskFromFlags(cmd, &flags, "", args[0])
			if err != nil {
				logErrorCmd(*cmd, err)

				return
			}

			t, err := psdk.CreateTask(payload)
			if err != nil {
				logErrorCmd(*cmd, err)

				return
			}
			logJSONCmd(*cmd, t)
		},
	}
	registerTaskFlags(cmd, &flags)

	return cmd
}

func newTaskViewCmd() *cobra.Command {
	return &cobra.Command{
		Use:   cmdViewUse,
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
}

func newTaskListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   cmdListUse,
		Short: "List tasks",
		Long:  `List tasks with optional metadata filtering.`,
		Run: func(cmd *cobra.Command, args []string) {
			metaPairs, err := cmd.Flags().GetStringSlice("metadata")
			if err != nil {
				logErrorCmd(*cmd, err)

				return
			}

			page, err := psdk.ListTasks(sdk.PageMetadata{
				Offset:   defOffset,
				Limit:    defLimit,
				Metadata: toMapAny(metaPairs),
			})
			if err != nil {
				logErrorCmd(*cmd, err)

				return
			}
			logJSONCmd(*cmd, page)
		},
	}
	cmd.Flags().StringSlice("metadata", nil, "Filter by metadata KEY=VALUE (comma-separated or repeatable)")

	return cmd
}

func newTaskUpdateCmd() *cobra.Command {
	flags := taskFlags{}
	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update task",
		Long:  `Update task fields. Only the fields provided as flags are changed.`,
		Run: func(cmd *cobra.Command, args []string) {
			if len(args) != 1 {
				logUsageCmd(*cmd, cmd.Use)

				return
			}

			payload, err := taskFromFlags(cmd, &flags, args[0], "")
			if err != nil {
				logErrorCmd(*cmd, err)

				return
			}

			t, err := psdk.UpdateTask(payload)
			if err != nil {
				logErrorCmd(*cmd, err)

				return
			}
			logJSONCmd(*cmd, t)
		},
	}
	registerTaskFlags(cmd, &flags)

	return cmd
}

func newTaskDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   cmdDeleteUse,
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
}

func newTaskStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   cmdStartUse,
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
}

func newTaskStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   cmdStopUse,
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
}

func newTaskInvokeCmd() *cobra.Command {
	var env []string
	cmd := &cobra.Command{
		Use:   "invoke <id> [inputs...]",
		Short: "Invoke a latent task",
		Long: `Invoke a latent task with optional inputs.

Examples:
  # Invoke with positional inputs
  propeller-cli tasks invoke <id> '"world"'

  # Override environment variables for this invocation only
  propeller-cli tasks invoke <id> '"world"' --env GREETING=hola`,
		Run: func(cmd *cobra.Command, args []string) {
			if len(args) < 1 {
				logUsageCmd(*cmd, cmd.Use)

				return
			}

			results, err := psdk.InvokeTask(args[0], args[1:], toMap(env))
			if err != nil {
				logErrorCmd(*cmd, err)

				return
			}
			logJSONCmd(*cmd, results)
		},
	}
	cmd.Flags().StringSliceVar(&env, "env", nil, "Environment variables KEY=VALUE applied to this invocation only (comma-separated or repeatable)")

	return cmd
}

func newTaskMetricsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "metrics <id>",
		Short: "View task metrics",
		Long:  `View stored process metrics for a task.`,
		Run: func(cmd *cobra.Command, args []string) {
			if len(args) != 1 {
				logUsageCmd(*cmd, cmd.Use)

				return
			}

			page, err := psdk.GetTaskMetrics(args[0], defOffset, defLimit)
			if err != nil {
				logErrorCmd(*cmd, err)

				return
			}
			logJSONCmd(*cmd, page)
		},
	}
}

func newTaskResultsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "results <id>",
		Short: "View task results",
		Long:  `View stored execution results for a task.`,
		Run: func(cmd *cobra.Command, args []string) {
			if len(args) != 1 {
				logUsageCmd(*cmd, cmd.Use)

				return
			}

			results, err := psdk.GetTaskResults(args[0])
			if err != nil {
				logErrorCmd(*cmd, err)

				return
			}
			logJSONCmd(*cmd, results)
		},
	}
}

func newTaskUploadCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "upload <id> <file>",
		Short: "Upload Wasm file",
		Long:  `Upload a .wasm file (max 100 MB) for an existing task.`,
		Run: func(cmd *cobra.Command, args []string) {
			if len(args) != 2 {
				logUsageCmd(*cmd, cmd.Use)

				return
			}

			t, err := psdk.UploadTaskFile(args[0], args[1])
			if err != nil {
				logErrorCmd(*cmd, err)

				return
			}
			logJSONCmd(*cmd, t)
		},
	}
}

func NewTasksCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tasks [create|view|list|update|delete|start|stop|invoke|metrics|results|upload]",
		Short: "Tasks manager",
		Long:  `Create, view, list, update, delete, start, stop, invoke, and inspect tasks.`,
	}

	cmd.AddCommand(newTaskCreateCmd())
	cmd.AddCommand(newTaskViewCmd())
	cmd.AddCommand(newTaskListCmd())
	cmd.AddCommand(newTaskUpdateCmd())
	cmd.AddCommand(newTaskDeleteCmd())
	cmd.AddCommand(newTaskStartCmd())
	cmd.AddCommand(newTaskStopCmd())
	cmd.AddCommand(newTaskInvokeCmd())
	cmd.AddCommand(newTaskMetricsCmd())
	cmd.AddCommand(newTaskResultsCmd())
	cmd.AddCommand(newTaskUploadCmd())

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
