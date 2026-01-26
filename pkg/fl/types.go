package fl

import "time"

type RoundState struct {
	RoundID   string
	ModelRef  string
	KOfN      int
	TimeoutS  int
	StartTime time.Time
	Updates   []Update
	Completed bool
}

type Update struct {
	RoundID      string         `json:"round_id"`
	PropletID    string         `json:"proplet_id"`
	BaseModelURI string         `json:"base_model_uri"`
	NumSamples   int            `json:"num_samples"`
	Metrics      map[string]any `json:"metrics"`
	Update       map[string]any `json:"update"`
	ReceivedAt   time.Time      `json:"received_at"`
}

type Task struct {
	RoundID     string         `json:"round_id"`
	ModelRef    string         `json:"model_ref"`
	Config      map[string]any `json:"config"`
	Hyperparams map[string]any `json:"hyperparams,omitempty"`
}

type Model struct {
	Data     map[string]any `json:"data"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type Aggregator interface {
	Aggregate(updates []Update) (Model, error)
}
