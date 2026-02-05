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
	Skipped
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
	case Skipped:
		return "Skipped"
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
	TaskKindStandard  TaskKind = "standard"
	TaskKindFederated TaskKind = "federated"
)

const (
	RunIfSuccess = "success"
	RunIfFailure = "failure"
)

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
	DependsOn         []string                   `json:"depends_on,omitempty"`
	RunIf             string                     `json:"run_if,omitempty"`
	WorkflowID        string                     `json:"workflow_id,omitempty"`
	JobID             string                     `json:"job_id,omitempty"`
	Results           any                        `json:"results,omitempty"`
	Error             string                     `json:"error,omitempty"`
	MonitoringProfile *proplet.MonitoringProfile `json:"monitoring_profile,omitempty"`
	StartTime         time.Time                  `json:"start_time"`
	FinishTime        time.Time                  `json:"finish_time"`
	CreatedAt         time.Time                  `json:"created_at"`
	UpdatedAt         time.Time                  `json:"updated_at"`
	Mode              Mode                       `json:"mode,omitempty"`
}

type TaskPage struct {
	Offset uint64 `json:"offset"`
	Limit  uint64 `json:"limit"`
	Total  uint64 `json:"total"`
	Tasks  []Task `json:"tasks"`
}
