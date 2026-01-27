package controllers

import (
	"encoding/json"
	"fmt"

	propellerv1alpha1 "github.com/absmach/propeller/api/v1alpha1"
	flpkg "github.com/absmach/propeller/pkg/fl"
)

func extractUpdateFromTask(wasmTask *propellerv1alpha1.WasmTask, roundIDStr string) (flpkg.UpdateEnvelope, bool) {
	if wasmTask.Status.Results == nil {
		return flpkg.UpdateEnvelope{}, false
	}

	var resultData map[string]any
	if err := json.Unmarshal(wasmTask.Status.Results.Raw, &resultData); err != nil {
		return flpkg.UpdateEnvelope{}, false
	}

	env, err := ExtractFLUpdateFromResult(resultData)
	if err != nil {
		return flpkg.UpdateEnvelope{}, false
	}

	if env.RoundID == 0 {
		return env, true
	}

	var roundIDNum uint64
	if _, err := fmt.Sscanf(roundIDStr, "round-%d", &roundIDNum); err != nil {
		if _, err2 := fmt.Sscanf(roundIDStr, "%d", &roundIDNum); err2 != nil {
			return env, true
		}
	}

	if env.RoundID == roundIDNum || roundIDNum == 0 {
		return env, true
	}

	return flpkg.UpdateEnvelope{}, false
}

func processCompletedParticipant(
	participant *propellerv1alpha1.RoundParticipantStatus,
	wasmTask *propellerv1alpha1.WasmTask,
	round *propellerv1alpha1.TrainingRound,
) (flpkg.UpdateEnvelope, bool) {
	if participant.UpdateReceived {
		return flpkg.UpdateEnvelope{}, false
	}

	env, ok := extractUpdateFromTask(wasmTask, round.Spec.RoundID)
	if !ok {
		return flpkg.UpdateEnvelope{}, false
	}

	return env, true
}
