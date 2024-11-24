package job

import "github.com/absmach/propeller/task"

type Job struct {
	ID          string
	Name        string
	Description string
	Tasks       []task.Task
}
