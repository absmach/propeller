package task

import (
	"time"

	"github.com/absmach/propeller/pkg/proplet"
)

type State uint8

const (
	Pending State = iota
	Scheduled
	Running
	Completed
	Failed
)

func (s State) String() string {
	switch s {
	case Pending:
		return "Pending"
	case Scheduled:
		return "Scheduled"
	case Running:
		return "Running"
	case Completed:
		return "Completed"
	case Failed:
		return "Failed"
	default:
		return "Unknown"
	}
}

type Mode string

const (
	ModeInfer Mode = "infer"
	ModeTrain Mode = "train"
)

type TaskKind string

const (
	TaskKindStandard TaskKind = "standard"
	// Deprecated: TaskKindFederated is deprecated. FL is now implemented as an external sample application.
	// See examples/fl-demo/ for the new FML architecture.
	TaskKindFederated TaskKind = "federated"
)

// Deprecated: FLSpec is deprecated. FL is now implemented as an external sample application.
// Manager no longer handles FL-specific logic (aggregation, round management, etc.).
// See examples/fl-demo/ for the new FML architecture where FL is handled by an external coordinator.
type FLSpec struct {
	JobID         string `json:"job_id"`
	RoundID       uint64 `json:"round_id"`
	GlobalVersion string `json:"global_version"`

	MinParticipants uint64 `json:"min_participants,omitempty"`
	RoundTimeoutSec uint64 `json:"round_timeout_sec,omitempty"`
	ClientsPerRound uint64 `json:"clients_per_round,omitempty"`
	TotalRounds     uint64 `json:"total_rounds,omitempty"`

	Algorithm    string         `json:"algorithm,omitempty"`
	UpdateFormat string         `json:"update_format,omitempty"`
	Hyperparams  map[string]any `json:"hyperparams,omitempty"`
	ModelRef     string         `json:"model_ref,omitempty"`

	// Training hyperparameters
	LocalEpochs  uint64  `json:"local_epochs,omitempty"`
	BatchSize    uint64  `json:"batch_size,omitempty"`
	LearningRate float64 `json:"learning_rate,omitempty"`
}

type Task struct {
	ID                string                     `json:"id"`
	Name              string                     `json:"name"`
	Kind              TaskKind                   `json:"kind,omitempty"`
	State             State                      `json:"state"`
	ImageURL          string                     `json:"image_url,omitempty"`
	File              []byte                     `json:"file,omitempty"`
	CLIArgs           []string                   `json:"cli_args"`
	Inputs            []uint64                   `json:"inputs,omitempty"`
	Env               map[string]string          `json:"env,omitempty"`
	Daemon            bool                       `json:"daemon"`
	Encrypted         bool                       `json:"encrypted"`
	KBSResourcePath   string                     `json:"kbs_resource_path,omitempty"`
	PropletID         string                     `json:"proplet_id,omitempty"`
	Results           any                        `json:"results,omitempty"`
	Error             string                     `json:"error,omitempty"`
	MonitoringProfile *proplet.MonitoringProfile `json:"monitoring_profile,omitempty"`
	StartTime         time.Time                  `json:"start_time"`
	FinishTime        time.Time                  `json:"finish_time"`
	CreatedAt         time.Time                  `json:"created_at"`
	UpdatedAt         time.Time                  `json:"updated_at"`
	Mode              Mode                       `json:"mode,omitempty"`
	FL                *FLSpec                    `json:"fl,omitempty"`
}

type TaskPage struct {
	Offset uint64 `json:"offset"`
	Limit  uint64 `json:"limit"`
	Total  uint64 `json:"total"`
	Tasks  []Task `json:"tasks"`
}
