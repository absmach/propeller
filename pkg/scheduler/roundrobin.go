package scheduler

import (
	"github.com/absmach/propeller/proplet"
	"github.com/absmach/propeller/task"
)

type roundRobin struct {
	LastProplet int
}

func NewRoundRobin() Scheduler {
	return &roundRobin{
		LastProplet: 0,
	}
}

func (r *roundRobin) SelectProplet(t task.Task, proplets []proplet.Proplet) (proplet.Proplet, error) {
	if len(proplets) == 0 {
		return proplet.Proplet{}, ErrNoproplet
	}
	if len(proplets) == 1 {
		return proplets[0], nil
	}

	r.LastProplet = (r.LastProplet + 1) % len(proplets)

	return proplets[r.LastProplet], nil
}
