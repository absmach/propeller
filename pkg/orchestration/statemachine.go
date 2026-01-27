package orchestration

import (
	"context"
	"slices"
	"time"

	"github.com/absmach/propeller/task"
)

type StateMachine struct{}

func NewStateMachine() *StateMachine {
	return &StateMachine{}
}

func (sm *StateMachine) ValidateTransition(from, to task.State) bool {
	validTransitions := map[task.State][]task.State{
		task.Pending:   {task.Scheduled, task.Failed},
		task.Scheduled: {task.Running, task.Failed},
		task.Running:   {task.Completed, task.Failed},
		task.Completed: {}, // Terminal state
		task.Failed:    {}, // Terminal state
	}

	allowed, ok := validTransitions[from]
	if !ok {
		return false
	}

	return slices.Contains(allowed, to)
}

func (sm *StateMachine) TransitionTask(ctx context.Context, t *Task, newState task.State) error {
	if !sm.ValidateTransition(t.State, newState) {
		return ErrInvalidStateTransition
	}

	now := time.Now()
	t.State = newState
	t.UpdatedAt = now

	switch newState {
	case task.Pending:
		// No additional action needed for pending state
	case task.Scheduled:
		// No additional action needed for scheduled state
	case task.Running:
		if t.StartTime.IsZero() {
			t.StartTime = now
		}
	case task.Completed, task.Failed:
		if t.FinishTime.IsZero() {
			t.FinishTime = now
		}
	}

	return nil
}

func (sm *StateMachine) MarkTaskPending(ctx context.Context, t *Task) error {
	return sm.TransitionTask(ctx, t, task.Pending)
}

func (sm *StateMachine) MarkTaskScheduled(ctx context.Context, t *Task) error {
	return sm.TransitionTask(ctx, t, task.Scheduled)
}

func (sm *StateMachine) MarkTaskRunning(ctx context.Context, t *Task) error {
	return sm.TransitionTask(ctx, t, task.Running)
}

func (sm *StateMachine) MarkTaskCompleted(ctx context.Context, t *Task, results any) error {
	if err := sm.TransitionTask(ctx, t, task.Completed); err != nil {
		return err
	}
	t.Results = results

	return nil
}

func (sm *StateMachine) MarkTaskFailed(ctx context.Context, t *Task, errorMsg string) error {
	if err := sm.TransitionTask(ctx, t, task.Failed); err != nil {
		return err
	}
	t.Error = errorMsg

	return nil
}

func (sm *StateMachine) IsTerminalState(state task.State) bool {
	return state == task.Completed || state == task.Failed
}
