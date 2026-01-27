package orchestration

import (
	"context"
)

type StateStore interface {
	// Task operations
	CreateTask(ctx context.Context, task Task) error
	GetTask(ctx context.Context, taskID string) (Task, error)
	UpdateTask(ctx context.Context, task Task) error
	DeleteTask(ctx context.Context, taskID string) error
	ListTasks(ctx context.Context, offset, limit uint64) ([]Task, uint64, error)

	// Proplet operations
	CreateProplet(ctx context.Context, proplet Proplet) error
	GetProplet(ctx context.Context, propletID string) (Proplet, error)
	UpdateProplet(ctx context.Context, proplet Proplet) error
	ListProplets(ctx context.Context, offset, limit uint64) ([]Proplet, uint64, error)

	// Task-Proplet mapping
	PinTaskToProplet(ctx context.Context, taskID, propletID string) error
	GetPropletForTask(ctx context.Context, taskID string) (string, error)
	UnpinTaskFromProplet(ctx context.Context, taskID string) error

	// Round operations (for FL)
	CreateRound(ctx context.Context, round Round) error
	GetRound(ctx context.Context, roundID string) (Round, error)
	UpdateRound(ctx context.Context, round Round) error
	ListRounds(ctx context.Context, federatedJobID string) ([]Round, error)
}

type WorkExecutor interface {
	// StartTask starts a task on a specific proplet
	StartTask(ctx context.Context, task Task, proplet Proplet) error

	// StopTask stops a running task
	StopTask(ctx context.Context, taskID string, propletID string) error

	// GetTaskStatus retrieves the current status of a task
	GetTaskStatus(ctx context.Context, taskID string) (TaskStatus, error)
}

type EventEmitter interface {
	// Task events
	EmitTaskCreated(ctx context.Context, task Task) error
	EmitTaskStarted(ctx context.Context, task Task, proplet Proplet) error
	EmitTaskCompleted(ctx context.Context, task Task) error
	EmitTaskFailed(ctx context.Context, task Task, errMsg string) error

	// Round events
	EmitRoundStarted(ctx context.Context, round Round) error
	EmitRoundCompleted(ctx context.Context, round Round) error
	EmitRoundFailed(ctx context.Context, round Round, errMsg string) error

	// Proplet events
	EmitPropletRegistered(ctx context.Context, proplet Proplet) error
	EmitPropletHeartbeat(ctx context.Context, propletID string) error
}

type Scheduler interface {
	// SelectProplet selects a proplet for a given task
	SelectProplet(ctx context.Context, task Task, proplets []Proplet) (Proplet, error)
}
