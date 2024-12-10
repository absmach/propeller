package proplet

import (
	"context"
	"time"

	"github.com/absmach/propeller/task"
)

const aliveTimeout = 10 * time.Second

type Service interface {
	StartTask(ctx context.Context, task task.Task) error
	RunTask(ctx context.Context, taskID string) ([]uint64, error)
	StopTask(ctx context.Context, taskID string) error
	RemoveTask(ctx context.Context, taskID string) error
}

type Proplet struct {
	ID           string      `json:"id"`
	Name         string      `json:"name"`
	TaskCount    uint64      `json:"task_count"`
	Alive        bool        `json:"alive"`
	AliveHistory []time.Time `json:"alive_history"`
}

func (p *Proplet) SetAlive() {
	if len(p.AliveHistory) > 0 {
		lastAlive := p.AliveHistory[len(p.AliveHistory)-1]
		if time.Since(lastAlive) <= aliveTimeout {
			p.Alive = true

			return
		}
	}
	p.Alive = false
}

type PropletPage struct {
	Offset   uint64    `json:"offset"`
	Limit    uint64    `json:"limit"`
	Total    uint64    `json:"total"`
	Proplets []Proplet `json:"proplets"`
}
