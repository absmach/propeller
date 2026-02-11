package api

import (
	"net/http"

	"github.com/absmach/propeller/manager"
	"github.com/absmach/propeller/pkg/proplet"
	"github.com/absmach/propeller/task"
	"github.com/absmach/supermq"
)

var (
	_ supermq.Response = (*propletResponse)(nil)
	_ supermq.Response = (*listpropletResponse)(nil)
	_ supermq.Response = (*taskResponse)(nil)
	_ supermq.Response = (*listTaskResponse)(nil)
	_ supermq.Response = (*messageResponse)(nil)
	_ supermq.Response = (*taskMetricsResponse)(nil)
	_ supermq.Response = (*propletMetricsResponse)(nil)
)

type propletResponse struct {
	proplet.Proplet

	created bool
	deleted bool
}

func (w propletResponse) Code() int {
	if w.created {
		return http.StatusCreated
	}
	if w.deleted {
		return http.StatusNoContent
	}

	return http.StatusOK
}

func (w propletResponse) Headers() map[string]string {
	if w.created {
		return map[string]string{
			"Location": "/proplets/" + w.ID,
		}
	}

	return map[string]string{}
}

func (w propletResponse) Empty() bool {
	return w.deleted
}

type listpropletResponse struct {
	proplet.PropletPage
}

func (l listpropletResponse) Code() int {
	return http.StatusOK
}

func (l listpropletResponse) Headers() map[string]string {
	return map[string]string{}
}

func (l listpropletResponse) Empty() bool {
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
	return t.deleted
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

type messageResponse map[string]any

func (w messageResponse) Code() int {
	return http.StatusOK
}

func (w messageResponse) Headers() map[string]string {
	return map[string]string{}
}

func (w messageResponse) Empty() bool {
	return false
}

type taskMetricsResponse struct {
	manager.TaskMetricsPage
}

func (t taskMetricsResponse) Code() int {
	return http.StatusOK
}

func (t taskMetricsResponse) Headers() map[string]string {
	return map[string]string{}
}

func (t taskMetricsResponse) Empty() bool {
	return false
}

type propletMetricsResponse struct {
	manager.PropletMetricsPage
}

func (p propletMetricsResponse) Code() int {
	return http.StatusOK
}

func (p propletMetricsResponse) Headers() map[string]string {
	return map[string]string{}
}

func (p propletMetricsResponse) Empty() bool {
	return false
}
