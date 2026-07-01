package api

import (
	"encoding/base64"

	"github.com/absmach/propeller/manager"
	"github.com/absmach/propeller/pkg/task"
)

const redactedMarker = "<REDACTED>"

func redactFile(file []byte) string {
	if len(file) == 0 {
		return ""
	}

	encoded := base64.StdEncoding.EncodeToString(file)
	if len(encoded) <= 20 {
		return encoded
	}

	return encoded[:10] + redactedMarker + encoded[len(encoded)-10:]
}

type taskAlias task.Task

type redactedTask struct {
	taskAlias

	File string `json:"file,omitempty"`
}

func newRedactedTask(t task.Task) redactedTask {
	return redactedTask{taskAlias: taskAlias(t), File: redactFile(t.File)}
}

func redactTasks(tasks []task.Task) []redactedTask {
	if tasks == nil {
		return nil
	}

	out := make([]redactedTask, len(tasks))
	for i := range tasks {
		out[i] = newRedactedTask(tasks[i])
	}

	return out
}

type redactedJobSummary struct {
	manager.JobSummary

	Tasks []redactedTask `json:"tasks"`
}

func redactJobSummaries(jobs []manager.JobSummary) []redactedJobSummary {
	if jobs == nil {
		return nil
	}

	out := make([]redactedJobSummary, len(jobs))
	for i := range jobs {
		out[i] = redactedJobSummary{JobSummary: jobs[i], Tasks: redactTasks(jobs[i].Tasks)}
	}

	return out
}
