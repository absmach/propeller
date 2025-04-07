package task

import "time"

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
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	State      State     `json:"state"`
	ImageURL   string    `json:"image_url,omitempty"`
	File       []byte    `json:"file,omitempty"`
	CLIArgs    []string  `json:"cli_args"`
	Inputs     []uint64  `json:"inputs,omitempty"`
	Results    any       `json:"results,omitempty"`
	Error      string    `json:"error,omitempty"`
	StartTime  time.Time `json:"start_time"`
	FinishTime time.Time `json:"finish_time"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type TaskPage struct {
	Offset uint64 `json:"offset"`
	Limit  uint64 `json:"limit"`
	Total  uint64 `json:"total"`
	Tasks  []Task `json:"tasks"`
}

func FilterRunningTasks(tasks []Task) []Task{
	var runningTasks []Task
	for _, t := range tasks {
		if t.State == Running{
			runningTasks = append(runningTasks, t)
		}
	}
	return runningTasks
}

func FilterCompletedTasks(tasks []Task) []Task{
	var completedTasks []Task
	for _, t := range tasks {
		if t.State == Completed{
			completedTasks = append(completedTasks, t)
		}
	}
	return completedTasks
}

func FilterFailedTasks(tasks []Task) []Task{
	var failedTasks []Task
	for _, t := range tasks {
		if t.State == Failed{
			failedTasks = append(failedTasks, t)
		}
	}
	return failedTasks
}

func FilterScheduledTasks(tasks []Task) []Task{
	var scheduledTasks []Task
	for _, t := range tasks {
		if t.State == Scheduled{
			scheduledTasks = append(scheduledTasks, t)
		}
	}
	return scheduledTasks
}
