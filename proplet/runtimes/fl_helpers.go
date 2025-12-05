package runtimes

import (
	"encoding/base64"
	"fmt"
	"strconv"

	flpkg "github.com/absmach/propeller/pkg/fl"
)

func buildFLPayloadFromString(taskID, mode, propletID string, env map[string]string, rawOut string) map[string]any {
	// Backward-compatible inference behavior.
	if mode != "train" {
		return map[string]any{
			"task_id": taskID,
			"results": rawOut,
		}
	}

	envlp := flpkg.UpdateEnvelope{
		TaskID:        taskID,
		JobID:         env["FL_JOB_ID"],
		RoundID:       parseUint64(env, "FL_ROUND_ID"),
		GlobalVersion: env["FL_GLOBAL_VERSION"],
		PropletID:     propletID,
		NumSamples:    parseUint64(env, "FL_NUM_SAMPLES"),
		UpdateB64:     base64.StdEncoding.EncodeToString([]byte(rawOut)),
		Metrics:       nil,
		Format:        env["FL_FORMAT"],
	}

	return map[string]any{
		"task_id": taskID,
		"results": envlp,
	}
}

func buildFLPayloadFromUint64Slice(taskID, mode, propletID string, env map[string]string, results []uint64) map[string]any {
	if mode != "train" {
		return map[string]any{
			"task_id": taskID,
			"results": results,
		}
	}

	raw := fmt.Sprint(results)

	envlp := flpkg.UpdateEnvelope{
		TaskID:        taskID,
		JobID:         env["FL_JOB_ID"],
		RoundID:       parseUint64(env, "FL_ROUND_ID"),
		GlobalVersion: env["FL_GLOBAL_VERSION"],
		PropletID:     propletID,
		NumSamples:    parseUint64(env, "FL_NUM_SAMPLES"),
		UpdateB64:     base64.StdEncoding.EncodeToString([]byte(raw)),
		Metrics:       nil,
		Format:        env["FL_FORMAT"],
	}

	return map[string]any{
		"task_id": taskID,
		"results": envlp,
	}
}

func parseUint64(env map[string]string, key string) uint64 {
	if env == nil {
		return 0
	}
	s, ok := env[key]
	if !ok || s == "" {
		return 0
	}
	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0
	}
	return v
}
