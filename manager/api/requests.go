package api

import (
	"encoding/json"
	"errors"
	"fmt"

	apiutil "github.com/absmach/magistrala/api/http/util"
	"github.com/absmach/propeller/pkg/api"
	"github.com/absmach/propeller/pkg/cron"
	pkgerrors "github.com/absmach/propeller/pkg/errors"
	"github.com/absmach/propeller/pkg/task"
)

const maxMetadataBytes = 1048576 // 1MB

type taskReq struct {
	task.Task `json:",inline"`
}

func (t *taskReq) validate() error {
	if t.Name == "" {
		return apiutil.ErrMissingName
	}

	if t.RunIf != "" && t.RunIf != task.RunIfSuccess && t.RunIf != task.RunIfFailure {
		return apiutil.ErrValidation
	}

	if t.Schedule != "" {
		if err := cron.ValidateCronExpression(t.Schedule); err != nil {
			return fmt.Errorf("invalid cron expression: %w", err)
		}
	}

	if t.Priority < 0 || t.Priority > 100 {
		return fmt.Errorf("priority must be between 0 and 100, got %d", t.Priority)
	}

	if t.Broadcast && t.PropletID != "" {
		return fmt.Errorf("%w: broadcast and proplet_id are mutually exclusive", pkgerrors.ErrInvalidValue)
	}

	if len(t.Metadata) > 0 {
		b, err := json.Marshal(t.Metadata)
		if err != nil {
			return fmt.Errorf("invalid metadata: %w", err)
		}
		if len(b) > maxMetadataBytes {
			return errors.New("metadata exceeds 1MB limit")
		}
	}

	return nil
}

type workflowReq struct {
	Tasks []task.Task `json:"tasks"`
}

func (w *workflowReq) validate() error {
	if len(w.Tasks) == 0 {
		return apiutil.ErrValidation
	}

	for i := range w.Tasks {
		if w.Tasks[i].Name == "" {
			return apiutil.ErrMissingName
		}

		if w.Tasks[i].RunIf != "" && w.Tasks[i].RunIf != task.RunIfSuccess && w.Tasks[i].RunIf != task.RunIfFailure {
			return apiutil.ErrValidation
		}
	}

	return nil
}

type jobReq struct {
	Name          string      `json:"name"`
	Tasks         []task.Task `json:"tasks"`
	ExecutionMode string      `json:"execution_mode,omitempty"`
}

func (j *jobReq) validate() error {
	if len(j.Tasks) == 0 {
		return apiutil.ErrValidation
	}

	for i := range j.Tasks {
		if j.Tasks[i].Name == "" {
			return apiutil.ErrMissingName
		}

		if j.Tasks[i].RunIf != "" && j.Tasks[i].RunIf != task.RunIfSuccess && j.Tasks[i].RunIf != task.RunIfFailure {
			return apiutil.ErrValidation
		}
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

type listTasksReq struct {
	offset   uint64
	limit    uint64
	metadata task.Metadata
}

func (r *listTasksReq) validate() error {
	if r.limit > api.MaxLimitSize || r.limit < 1 {
		return apiutil.ErrLimitSize
	}

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
