package orchestration

import (
	"context"

	"github.com/absmach/propeller/pkg/proplet"
	"github.com/absmach/propeller/pkg/scheduler"
	"github.com/absmach/propeller/task"
)

type LegacySchedulerAdapter struct {
	legacy scheduler.Scheduler
}

func NewLegacySchedulerAdapter(legacy scheduler.Scheduler) Scheduler {
	return &LegacySchedulerAdapter{
		legacy: legacy,
	}
}

func (a *LegacySchedulerAdapter) SelectProplet(ctx context.Context, t task.Task, proplets []proplet.Proplet) (proplet.Proplet, error) {
	return a.legacy.SelectProplet(t, proplets)
}
