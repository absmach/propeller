package orchestration

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/absmach/propeller/task"
)

type FLCoordinator struct {
	stateStore   StateStore
	workExecutor WorkExecutor
	eventEmitter EventEmitter
	scheduler    Scheduler
}

func NewFLCoordinator(
	stateStore StateStore,
	workExecutor WorkExecutor,
	eventEmitter EventEmitter,
	scheduler Scheduler,
) *FLCoordinator {
	return &FLCoordinator{
		stateStore:   stateStore,
		workExecutor: workExecutor,
		eventEmitter: eventEmitter,
		scheduler:    scheduler,
	}
}

func (fc *FLCoordinator) StartRound(ctx context.Context, round Round) error {
	round.StartTime = time.Now()
	round.Status = RoundStatusRunning

	// Create round in state store
	if err := fc.stateStore.CreateRound(ctx, round); err != nil {
		return fmt.Errorf("failed to create round: %w", err)
	}

	// Emit round started event
	if err := fc.eventEmitter.EmitRoundStarted(ctx, round); err != nil {
		// Log but don't fail - event emission is non-critical
		_ = err
	}

	// Get available proplets
	proplets, _, err := fc.stateStore.ListProplets(ctx, 0, 1000)
	if err != nil {
		return fmt.Errorf("failed to list proplets: %w", err)
	}

	// Create and start tasks for each participant
	for _, participantID := range round.Participants {
		// Find the proplet
		var proplet Proplet
		found := false
		for _, p := range proplets {
			if p.ID == participantID {
				proplet = p
				found = true

				break
			}
		}

		if !found {
			return fmt.Errorf("proplet %s not found", participantID)
		}

		if !proplet.Alive {
			return fmt.Errorf("proplet %s is not alive", participantID)
		}

		// Create task for this participant
		t := task.Task{
			ID:        fmt.Sprintf("%s-%s", round.RoundID, participantID),
			Name:      fmt.Sprintf("fl-round-%s-%s", round.RoundID, participantID),
			Kind:      task.TaskKindStandard,
			State:     task.Pending,
			ImageURL:  round.TaskWasmImage,
			PropletID: participantID,
			CreatedAt: time.Now(),
		}

		// Set environment variables
		t.Env = make(map[string]string)
		t.Env["ROUND_ID"] = round.RoundID
		t.Env["MODEL_URI"] = round.ModelRef

		// Add hyperparameters as JSON string
		if round.Hyperparams != nil {
			hyperparamsJSON, err := json.Marshal(round.Hyperparams)
			if err == nil {
				t.Env["HYPERPARAMS"] = string(hyperparamsJSON)
			}
		}

		// Create task in state store
		if err := fc.stateStore.CreateTask(ctx, t); err != nil {
			return fmt.Errorf("failed to create task for participant %s: %w", participantID, err)
		}

		// Pin task to proplet
		if err := fc.stateStore.PinTaskToProplet(ctx, t.ID, participantID); err != nil {
			return fmt.Errorf("failed to pin task to proplet: %w", err)
		}

		// Start the task
		if err := fc.workExecutor.StartTask(ctx, t, proplet); err != nil {
			return fmt.Errorf("failed to start task for participant %s: %w", participantID, err)
		}
	}

	return nil
}

func (fc *FLCoordinator) CheckRoundStatus(ctx context.Context, roundID string) (Round, bool, error) {
	round, err := fc.stateStore.GetRound(ctx, roundID)
	if err != nil {
		return Round{}, false, fmt.Errorf("failed to get round: %w", err)
	}

	// Check if round has timed out
	if round.TimeoutS > 0 {
		elapsed := time.Since(round.StartTime)
		if elapsed > time.Duration(round.TimeoutS)*time.Second {
			round.Status = RoundStatusFailed
			if err := fc.stateStore.UpdateRound(ctx, round); err != nil {
				return round, false, err
			}

			return round, false, ErrRoundTimeout
		}
	}

	// Count completed tasks
	completedCount := 0
	participants := round.Participants

	for _, participantID := range participants {
		taskID := fmt.Sprintf("%s-%s", roundID, participantID)
		t, err := fc.stateStore.GetTask(ctx, taskID)
		if err != nil {
			continue
		}

		if t.State == task.Completed {
			completedCount++
		}
	}

	// Check if we have enough participants (k-of-n)
	if completedCount >= round.KOfN {
		round.Status = RoundStatusAggregating
		if err := fc.stateStore.UpdateRound(ctx, round); err != nil {
			return round, false, fmt.Errorf("failed to update round: %w", err)
		}

		return round, true, nil
	}

	return round, false, nil
}

func (fc *FLCoordinator) CompleteRound(ctx context.Context, roundID, aggregatedModelRef string) error {
	round, err := fc.stateStore.GetRound(ctx, roundID)
	if err != nil {
		return fmt.Errorf("failed to get round: %w", err)
	}

	now := time.Now()
	round.EndTime = &now
	round.Status = RoundStatusCompleted

	if err := fc.stateStore.UpdateRound(ctx, round); err != nil {
		return fmt.Errorf("failed to update round: %w", err)
	}

	if err := fc.eventEmitter.EmitRoundCompleted(ctx, round); err != nil {
		// Log but don't fail - event emission is non-critical
		_ = err
	}

	return nil
}
