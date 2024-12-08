package api

import (
	"net/http"

	"github.com/absmach/magistrala"
	"github.com/absmach/propeller/task"
	"github.com/absmach/propeller/worker"
)

var (
	_ magistrala.Response = (*workerResponse)(nil)
	_ magistrala.Response = (*listWorkerResponse)(nil)
	_ magistrala.Response = (*taskResponse)(nil)
	_ magistrala.Response = (*listTaskResponse)(nil)
)

type workerResponse struct {
	worker.Worker
	created bool
	deleted bool
}

func (w workerResponse) Code() int {
	if w.created {
		return http.StatusCreated
	}
	if w.deleted {
		return http.StatusNoContent
	}

	return http.StatusOK
}

func (w workerResponse) Headers() map[string]string {
	if w.created {
		return map[string]string{
			"Location": "/tasks/" + w.ID,
		}
	}

	return map[string]string{}
}

func (w workerResponse) Empty() bool {
	return false
}

type listWorkerResponse struct {
	worker.WorkerPage
}

func (l listWorkerResponse) Code() int {
	return http.StatusOK
}

func (l listWorkerResponse) Headers() map[string]string {
	return map[string]string{}
}

func (l listWorkerResponse) Empty() bool {
	return false
}

type taskResponse struct {
	task.Task
	created bool
	deleted bool
}

func (t taskResponse) Code() int {
	if t.created {
		return http.StatusCreated
	}
	if t.deleted {
		return http.StatusNoContent
	}

	return http.StatusOK
}

func (t taskResponse) Headers() map[string]string {
	if t.created {
		return map[string]string{
			"Location": "/tasks/" + t.ID,
		}
	}

	return map[string]string{}
}

func (t taskResponse) Empty() bool {
	return false
}

type listTaskResponse struct {
	task.TaskPage
}

func (l listTaskResponse) Code() int {
	return http.StatusOK
}

func (l listTaskResponse) Headers() map[string]string {
	return map[string]string{}
}

func (l listTaskResponse) Empty() bool {
	return false
}
