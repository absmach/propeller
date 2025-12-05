package fl

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
