package api

import (
	"fmt"

	"github.com/absmach/propeller/pkg/cron"
	"github.com/absmach/propeller/task"
	apiutil "github.com/absmach/supermq/api/http/util"
)

type taskReq struct {
	task.Task `json:",inline"`
}

func (t *taskReq) validate() error {
	if t.Name == "" {
		return apiutil.ErrMissingName
	}

	if t.Schedule != "" {
		if err := cron.ValidateCronExpression(t.Schedule); err != nil {
			return fmt.Errorf("invalid cron expression: %w", err)
		}
	}

	if t.Priority < 0 || t.Priority > 100 {
		return fmt.Errorf("priority must be between 0 and 100, got %d", t.Priority)
	}

	return nil
}

type entityReq struct {
	id string
}

func (e *entityReq) validate() error {
	if e.id == "" {
		return apiutil.ErrMissingID
	}

	return nil
}

type listEntityReq struct {
	offset, limit uint64
}

func (e *listEntityReq) validate() error {
	return nil
}

type metricsReq struct {
	id            string
	offset, limit uint64
}

func (m *metricsReq) validate() error {
	if m.id == "" {
		return apiutil.ErrMissingID
	}

	return nil
}
