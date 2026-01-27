package orchestration

import (
	"time"

	"github.com/absmach/propeller/pkg/proplet"
	"github.com/absmach/propeller/task"
)

type Task = task.Task

type TaskState = task.State

type Proplet = proplet.Proplet

type Round struct {
	RoundID        string
	FederatedJobID string
	ModelRef       string
	TaskWasmImage  string
	Participants   []string
	Hyperparams    map[string]any
	KOfN           int
	TimeoutS       int
	StartTime      time.Time
	EndTime        *time.Time
	Status         RoundStatus
}

type RoundStatus string

const (
	RoundStatusPending     RoundStatus = "Pending"
	RoundStatusRunning     RoundStatus = "Running"
	RoundStatusAggregating RoundStatus = "Aggregating"
	RoundStatusCompleted   RoundStatus = "Completed"
	RoundStatusFailed      RoundStatus = "Failed"
)

type TaskStatus struct {
	TaskID    string
	PropletID string
	State     TaskState
	StartTime *time.Time
	EndTime   *time.Time
	Error     string
}

type ParticipantStatus struct {
	PropletID      string
	TaskID         string
	Status         string
	UpdateReceived bool
	LastUpdate     *time.Time
}
