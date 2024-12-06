package scheduler

import (
	"errors"

	"github.com/absmach/propeller/task"
	"github.com/absmach/propeller/worker"
)

var ErrNoWorker = errors.New("no worker was provided")

type Scheduler interface {
	SelectWorker(t task.Task, workers []worker.Worker) (worker.Worker, error)
}
