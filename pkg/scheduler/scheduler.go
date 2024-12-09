package scheduler

import (
	"errors"

	"github.com/absmach/propeller/proplet"
	"github.com/absmach/propeller/task"
)

var ErrNoproplet = errors.New("no proplet was provided")

type Scheduler interface {
	SelectProplet(t task.Task, proplets []proplet.Proplet) (proplet.Proplet, error)
}
