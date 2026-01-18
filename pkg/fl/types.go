package fl

import "time"

// RoundState represents the state of a federated learning round
type RoundState struct {
	RoundID   string
	ModelRef  string
	KOfN      int           // Minimum number of updates required
	TimeoutS  int           // Round timeout in seconds
	StartTime time.Time
	Updates   []Update      // Buffered updates
	Completed bool
}

// Update represents a model update from a client
type Update struct {
	RoundID      string                 `json:"round_id"`
	PropletID    string                 `json:"proplet_id"`
	BaseModelURI string                 `json:"base_model_uri"`
	NumSamples   int                    `json:"num_samples"`
	Metrics      map[string]interface{} `json:"metrics"`
	Update       map[string]interface{} `json:"update"` // Model weights/gradients
	ReceivedAt   time.Time              `json:"received_at,omitempty"`
}

// Task represents a federated learning task for a client
type Task struct {
	RoundID    string                 `json:"round_id"`
	ModelRef   string                 `json:"model_ref"`
	Config     map[string]interface{} `json:"config"`
	Hyperparams map[string]interface{} `json:"hyperparams,omitempty"`
}

// Model represents a machine learning model
type Model struct {
	Data     map[string]interface{} `json:"data"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// Aggregator interface for model aggregation
type Aggregator interface {
	Aggregate(updates []Update) (Model, error)
}
