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

type Function struct {
	File   []byte
	Name   string
	Inputs []uint64
}

type Task struct {
	ID         string
	Name       string
	State      State
	Function   Function
	StartTime  time.Time
	FinishTime time.Time
}
