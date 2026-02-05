package scheduler

import (
	"sort"

	"github.com/absmach/propeller/pkg/proplet"
	"github.com/absmach/propeller/task"
)

const defaultPriority = 50

type priorityScheduler struct {
	base Scheduler
}

func NewPriority(base Scheduler) Scheduler {
	return &priorityScheduler{
		base: base,
	}
}

func (ps *priorityScheduler) SelectProplet(t task.Task, proplets []proplet.Proplet) (proplet.Proplet, error) {
	return ps.base.SelectProplet(t, proplets)
}

func GetReadyTasksByPriority(tasks []task.Task) []task.Task {
	sorted := make([]task.Task, len(tasks))
	copy(sorted, tasks)

	sort.Slice(sorted, func(i, j int) bool {
		priorityI := sorted[i].Priority
		priorityJ := sorted[j].Priority

		if priorityI == 0 {
			priorityI = defaultPriority
		}
		if priorityJ == 0 {
			priorityJ = defaultPriority
		}

		if priorityI != priorityJ {
			return priorityI > priorityJ
		}

		return sorted[i].CreatedAt.Before(sorted[j].CreatedAt)
	})

	return sorted
}
