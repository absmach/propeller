package api

import (
	"github.com/absmach/magistrala/pkg/apiutil"
	"github.com/absmach/propeller/task"
	"github.com/absmach/propeller/worker"
)

type workerReq struct {
	worker.Worker `json:",inline"`
}

func (w *workerReq) validate() error {
	return nil
}

type taskReq struct {
	task.Task `json:",inline"`
}

func (t *taskReq) validate() error {
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