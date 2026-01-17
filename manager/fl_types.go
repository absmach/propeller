package manager

import (
	"time"

	"github.com/absmach/propeller/pkg/fl"
)

// FLTask represents a federated learning task for a client
type FLTask struct {
	RoundID    string                 `json:"round_id"`
	ModelRef   string                 `json:"model_ref"`
	Config     map[string]interface{} `json:"config"`
	Hyperparams map[string]interface{} `json:"hyperparams,omitempty"`
}

// FLUpdate represents a model update from a client
type FLUpdate = fl.Update

// RoundStatus represents the status of a federated learning round
type RoundStatus struct {
	RoundID    string `json:"round_id"`
	Completed  bool   `json:"completed"`
	NumUpdates int    `json:"num_updates"`
	KOfN       int    `json:"k_of_n"`
	ModelVersion int  `json:"model_version,omitempty"`
}

// Model represents a machine learning model in the registry
type Model struct {
	Version  int                    `json:"version"`
	Data     map[string]interface{} `json:"data"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt time.Time             `json:"created_at"`
}
