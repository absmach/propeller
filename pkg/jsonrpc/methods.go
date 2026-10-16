// Copyright (c) Abstract Machines
// SPDX-License-Identifier: Apache-2.0

package jsonrpc

import "github.com/absmach/propeller/pkg/task"

const (
	MethodTaskCreate  = "task.create"
	MethodTaskGet     = "task.get"
	MethodTaskList    = "task.list"
	MethodTaskUpdate  = "task.update"
	MethodTaskDelete  = "task.delete"
	MethodTaskStart   = "task.start"
	MethodTaskStop    = "task.stop"
	MethodTaskResults = "task.results"
	MethodTaskMetrics = "task.metrics"

	MethodPropletGet          = "proplet.get"
	MethodPropletList         = "proplet.list"
	MethodPropletDelete       = "proplet.delete"
	MethodPropletSDF          = "proplet.sdf"
	MethodPropletMetrics      = "proplet.metrics"
	MethodPropletAliveHistory = "proplet.aliveHistory"

	MethodJobCreate = "job.create"
	MethodJobGet    = "job.get"
	MethodJobList   = "job.list"
	MethodJobStart  = "job.start"
	MethodJobStop   = "job.stop"

	MethodWorkflowCreate = "workflow.create"
)

type IDParams struct {
	ID string `json:"id"`
}

type ListParams struct {
	Offset   uint64         `json:"offset,omitempty"`
	Limit    uint64         `json:"limit,omitempty"`
	Status   string         `json:"status,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type EntityListParams struct {
	ID     string `json:"id"`
	Offset uint64 `json:"offset,omitempty"`
	Limit  uint64 `json:"limit,omitempty"`
}

type JobParams struct {
	Name          string      `json:"name"`
	Tasks         []task.Task `json:"tasks"`
	ExecutionMode string      `json:"execution_mode,omitempty"`
}

type WorkflowParams struct {
	Tasks []task.Task `json:"tasks"`
}

type JobResult struct {
	JobID string      `json:"job_id"`
	Tasks []task.Task `json:"tasks"`
}

type ResultsResult struct {
	Results any `json:"results"`
}

type Ack struct {
	OK bool `json:"ok"`
}

func NewAck() Ack {
	return Ack{OK: true}
}
