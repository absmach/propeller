package manager

import (
	"github.com/absmach/propeller/task"
)

type Manager struct {
	TaskDb        map[string][]task.Task
	Workers       []string
	WorkerTaskMap map[string][]string
	TaskWorkerMap map[string]string
}
