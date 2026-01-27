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

type UpdateEnvelope struct {
	TaskID        string         `json:"task_id,omitempty"`
	JobID         string         `json:"job_id"`
	RoundID       uint64         `json:"round_id"`
	GlobalVersion string         `json:"global_version"`
	PropletID     string         `json:"proplet_id"`
	NumSamples    uint64         `json:"num_samples"`
	UpdateB64     string         `json:"update_b64"`
	Metrics       map[string]any `json:"metrics,omitempty"`
	Format        string         `json:"format,omitempty"`
}
