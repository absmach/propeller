package scheduler

import (
	"errors"

	"github.com/absmach/propeller/pkg/proplet"
	"github.com/absmach/propeller/task"
)

var (
	ErrNoProplet    = errors.New("no proplet was provided")
	ErrDeadProplers = errors.New("all proplets are dead")
)

type Scheduler interface {
	SelectProplet(t task.Task, proplets []proplet.Proplet) (proplet.Proplet, error)
}
