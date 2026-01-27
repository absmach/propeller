package orchestration

import (
	"context"
	"errors"

	"github.com/absmach/propeller/pkg/proplet"
	"github.com/absmach/propeller/task"
)

var (
	ErrNoProplet    = errors.New("no proplet was provided")
	ErrDeadProplets = errors.New("all proplets are dead")
)

type RoundRobinScheduler struct {
	lastProplet int
}

func NewRoundRobinScheduler() Scheduler {
	return &RoundRobinScheduler{
		lastProplet: 0,
	}
}

func (r *RoundRobinScheduler) SelectProplet(ctx context.Context, t task.Task, proplets []proplet.Proplet) (proplet.Proplet, error) {
	if len(proplets) == 0 {
		return proplet.Proplet{}, ErrNoProplet
	}

	alive := 0
	for i := range proplets {
		if proplets[i].Alive {
			alive++
		}
	}
	if alive == 0 {
		return proplet.Proplet{}, ErrDeadProplets
	}

	if len(proplets) == 1 {
		return proplets[0], nil
	}

	r.lastProplet = (r.lastProplet + 1) % len(proplets)

	p := proplets[r.lastProplet]
	if !p.Alive {
		// Recursively try next proplet
		return r.SelectProplet(ctx, t, proplets)
	}

	return p, nil
}
