package manager

import (
	"github.com/absmach/propeller/pkg/fl"
)

type FLTask struct {
	RoundID    string                 `json:"round_id"`
	ModelRef   string                 `json:"model_ref"`
	Config     map[string]interface{} `json:"config"`
	Hyperparams map[string]interface{} `json:"hyperparams,omitempty"`
}

type FLUpdate = fl.Update

type RoundStatus struct {
	RoundID    string `json:"round_id"`
	Completed  bool   `json:"completed"`
	NumUpdates int    `json:"num_updates"`
	KOfN       int    `json:"k_of_n"`
	ModelVersion int  `json:"model_version,omitempty"`
}

type ExperimentConfig struct {
	ExperimentID string                 `json:"experiment_id"`
	RoundID      string                 `json:"round_id"`
	ModelRef     string                 `json:"model_ref"`
	Participants []string               `json:"participants"`
	Hyperparams  map[string]interface{} `json:"hyperparams"`
	KOfN         int                    `json:"k_of_n"`
	TimeoutS     int                    `json:"timeout_s"`
	TaskWasmImage string                `json:"task_wasm_image,omitempty"`
}
