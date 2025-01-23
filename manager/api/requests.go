package api

import (
	apiutil "github.com/absmach/supermq/api/http/util"
	"github.com/absmach/propeller/task"
)

type taskReq struct {
	task.Task `json:",inline"`
}

func (t *taskReq) validate() error {
	if t.Name == "" {
		return apiutil.ErrMissingName
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
