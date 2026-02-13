package sdk_test

import (
	"encoding/json"
	"testing"

	"github.com/absmach/propeller/pkg/sdk"
)

func TestTaskCLIArgsJSONRoundTrip(t *testing.T) {
	task := sdk.Task{
		ID:   "task-123",
		Name: "wasi-nn-inference",
		CLIArgs: []string{
			"-S", "nn",
			"--dir=/home/proplet/fixture::fixture",
		},
		Env: map[string]string{
			"OPENVINO_DIR": "/opt/intel/openvino",
		},
	}

	data, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("failed to marshal task: %v", err)
	}

	var decoded sdk.Task
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal task: %v", err)
	}

	if len(decoded.CLIArgs) != 3 {
		t.Fatalf("expected 3 cli_args, got %d", len(decoded.CLIArgs))
	}
	if decoded.CLIArgs[0] != "-S" || decoded.CLIArgs[1] != "nn" {
		t.Errorf("unexpected cli_args: %v", decoded.CLIArgs)
	}
	if decoded.CLIArgs[2] != "--dir=/home/proplet/fixture::fixture" {
		t.Errorf("unexpected dir arg: %s", decoded.CLIArgs[2])
	}
	if decoded.Env["OPENVINO_DIR"] != "/opt/intel/openvino" {
		t.Errorf("unexpected env: %v", decoded.Env)
	}
}

func TestTaskCLIArgsOmitEmpty(t *testing.T) {
	task := sdk.Task{
		Name: "basic-task",
	}

	data, err := json.Marshal(task)
	if err != nil {
		t.Fatalf("failed to marshal task: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal raw: %v", err)
	}

	if _, exists := raw["cli_args"]; exists {
		t.Error("cli_args should be omitted when empty")
	}
	if _, exists := raw["env"]; exists {
		t.Error("env should be omitted when empty")
	}
}

func TestTaskJSONFieldNames(t *testing.T) {
	jsonStr := `{
		"name": "test",
		"cli_args": ["-S", "nn"],
		"env": {"KEY": "VAL"}
	}`

	var task sdk.Task
	if err := json.Unmarshal([]byte(jsonStr), &task); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if len(task.CLIArgs) != 2 {
		t.Fatalf("expected 2 cli_args, got %d", len(task.CLIArgs))
	}
	if task.Env["KEY"] != "VAL" {
		t.Errorf("unexpected env: %v", task.Env)
	}
}
