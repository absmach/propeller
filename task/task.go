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

type Task struct {
	ID                string                     `json:"id"`
	Name              string                     `json:"name"`
	State             State                      `json:"state"`
	ImageURL          string                     `json:"image_url,omitempty"`
	File              []byte                     `json:"file,omitempty"`
	CLIArgs           []string                   `json:"cli_args"`
	Inputs            []uint64                   `json:"inputs,omitempty"`
	Env               map[string]string          `json:"env,omitempty"`
	Daemon            bool                       `json:"daemon"`
	Encrypted         bool                       `json:"encrypted"`
	PropletID         string                     `json:"proplet_id,omitempty"`
	Results           any                        `json:"results,omitempty"`
	Error             string                     `json:"error,omitempty"`
	MonitoringProfile *proplet.MonitoringProfile `json:"monitoring_profile,omitempty"`
	StartTime         time.Time                  `json:"start_time"`
	FinishTime        time.Time                  `json:"finish_time"`
	CreatedAt         time.Time                  `json:"created_at"`
	UpdatedAt         time.Time                  `json:"updated_at"`
}

type TaskPage struct {
	Offset uint64 `json:"offset"`
	Limit  uint64 `json:"limit"`
	Total  uint64 `json:"total"`
	Tasks  []Task `json:"tasks"`
}
