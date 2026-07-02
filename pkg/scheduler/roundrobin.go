package scheduler

import (
	"sync"

	"github.com/absmach/propeller/pkg/proplet"
	"github.com/absmach/propeller/pkg/task"
)

type roundRobin struct {
	mu          sync.Mutex
	LastProplet int
}

func NewRoundRobin() Scheduler {
	return &roundRobin{
		LastProplet: 0,
	}
}

func (r *roundRobin) SelectProplet(t task.Task, proplets []proplet.Proplet) (proplet.Proplet, error) {
	if len(proplets) == 0 {
		return proplet.Proplet{}, ErrNoProplet
	}

	alive := 0
	for i := range proplets {
		if proplets[i].Alive {
			alive += 1
		}
	}
	if alive == 0 {
		return proplet.Proplet{}, ErrDeadProplers
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for range proplets {
		r.LastProplet = (r.LastProplet + 1) % len(proplets)
		if proplets[r.LastProplet].Alive {
			return proplets[r.LastProplet], nil
		}
	}

	return proplet.Proplet{}, ErrDeadProplers
}
