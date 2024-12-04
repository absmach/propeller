package scheduler

import (
	"github.com/absmach/propeller/task"
	"github.com/absmach/propeller/worker"
)

type roundRobin struct {
	LastWorker int
}

func NewRoundRobin() Scheduler {
	return &roundRobin{
		LastWorker: 0,
	}
}

func (r *roundRobin) SelectWorker(t task.Task, workers []worker.Worker) (worker.Worker, error) {
	if len(workers) == 0 {
		return worker.Worker{}, ErrNoWorker
	}
	if len(workers) == 1 {
		return workers[0], nil
	}

	r.LastWorker = (r.LastWorker + 1) % len(workers)

	return workers[r.LastWorker], nil
}
