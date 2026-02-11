package manager

import (
	"context"

	"github.com/absmach/propeller/pkg/proplet"
	"github.com/absmach/propeller/pkg/task"
)

type Service interface {
	GetProplet(ctx context.Context, propletID string) (proplet.Proplet, error)
	ListProplets(ctx context.Context, offset, limit uint64) (proplet.PropletPage, error)
	SelectProplet(ctx context.Context, task task.Task) (proplet.Proplet, error)
	DeleteProplet(ctx context.Context, propletID string) error

	CreateTask(ctx context.Context, task task.Task) (task.Task, error)
	CreateWorkflow(ctx context.Context, tasks []task.Task) ([]task.Task, error)
	CreateJob(ctx context.Context, name string, tasks []task.Task, executionMode string) (string, []task.Task, error)
	GetTask(ctx context.Context, taskID string) (task.Task, error)
	GetJob(ctx context.Context, jobID string) ([]task.Task, error)
	ListJobs(ctx context.Context, offset, limit uint64) (JobPage, error)
	StartJob(ctx context.Context, jobID string) error
	StopJob(ctx context.Context, jobID string) error
	ListTasks(ctx context.Context, offset, limit uint64) (task.TaskPage, error)
	UpdateTask(ctx context.Context, task task.Task) (task.Task, error)
	DeleteTask(ctx context.Context, taskID string) error
	StartTask(ctx context.Context, taskID string) error
	StopTask(ctx context.Context, taskID string) error

	GetTaskResults(ctx context.Context, taskID string) (any, error)
	GetParentResults(ctx context.Context, taskID string) (map[string]any, error)

	GetTaskMetrics(ctx context.Context, taskID string, offset, limit uint64) (TaskMetricsPage, error)
	GetPropletMetrics(ctx context.Context, propletID string, offset, limit uint64) (PropletMetricsPage, error)

	// Orchestrator/Experiment Config API (Manager acts as Orchestrator per diagram)
	// Step 1: Configure experiment with FL Coordinator
	ConfigureExperiment(ctx context.Context, config ExperimentConfig) error

	// Federated Learning Forwarding API (workload-agnostic)
	// Note: In the diagram, clients call FL Coordinator directly (Steps 3 & 7)
	// These endpoints exist for compatibility/MQTT forwarding
	GetFLTask(ctx context.Context, roundID, propletID string) (FLTask, error)
	PostFLUpdate(ctx context.Context, update FLUpdate) error
	PostFLUpdateCBOR(ctx context.Context, updateData []byte) error
	GetRoundStatus(ctx context.Context, roundID string) (RoundStatus, error)

	// Note: Round completion notifications are handled by FL Coordinator directly.
	// Coordinators publish MQTT notifications to "fl/rounds/next" topic.
	// See ROUND_COMPLETION_NOTIFICATION_FLOW.md for details.

	Subscribe(ctx context.Context) error

	Shutdown(ctx context.Context) error
	RecoverInterruptedTasks(ctx context.Context) error
}
