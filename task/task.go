package task

import (
	"time"
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
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	State        State     `json:"state"`
	DownloadFile string    `json:"download_file,omitempty"`
	File         []byte    `json:"file,omitempty"`
	Inputs       []uint64  `json:"inputs,omitempty"`
	Results      []uint64  `json:"results,omitempty"`
	StartTime    time.Time `json:"start_time"`
	FinishTime   time.Time `json:"finish_time"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type TaskPage struct {
	Offset uint64 `json:"offset"`
	Limit  uint64 `json:"limit"`
	Total  uint64 `json:"total"`
	Tasks  []Task `json:"tasks"`
}
