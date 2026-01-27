package v1alpha1

import (
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type FederatedJobSpec struct {
	// ExperimentID is a unique identifier for the experiment
	ExperimentID string `json:"experimentId"`

	// ModelRef is the OCI reference to the initial model
	ModelRef string `json:"modelRef"`

	// TaskWasmImage is the OCI reference to the WASM image for the FL client task
	TaskWasmImage string `json:"taskWasmImage"`

	// Participants is a list of proplet IDs that will participate in the federated learning
	Participants []ParticipantSpec `json:"participants"`

	// Hyperparams are hyperparameters for the training
	// +kubebuilder:pruning:PreserveUnknownFields
	Hyperparams *apiextensionsv1.JSON `json:"hyperparams,omitempty"`

	// KOfN is the minimum number of participants required for aggregation
	KOfN int `json:"kOfN"`

	// TimeoutSeconds is the timeout for each round in seconds
	TimeoutSeconds int `json:"timeoutSeconds"`

	// Rounds defines the round configuration
	Rounds RoundConfig `json:"rounds"`

	// Aggregator defines the aggregation algorithm
	Aggregator AggregatorConfig `json:"aggregator"`
}

type ParticipantSpec struct {
	// PropletID is the ID of the proplet
	PropletID string `json:"propletId"`

	// Namespace is the Kubernetes namespace where the proplet is located
	Namespace string `json:"namespace,omitempty"`
}

type RoundConfig struct {
	// Total is the total number of rounds
	Total int `json:"total"`

	// Strategy is the round execution strategy (sequential or parallel)
	Strategy string `json:"strategy,omitempty"`
}

type AggregatorConfig struct {
	// Algorithm is the aggregation algorithm (e.g., "fedavg")
	Algorithm string `json:"algorithm"`

	// Config contains algorithm-specific configuration
	// +kubebuilder:pruning:PreserveUnknownFields
	Config *apiextensionsv1.JSON `json:"config,omitempty"`
}

type FederatedJobStatus struct {
	// Phase is the current phase of the job
	Phase string `json:"phase,omitempty"`

	// CurrentRound is the current round number
	CurrentRound int `json:"currentRound,omitempty"`

	// CompletedRounds is the number of completed rounds
	CompletedRounds int `json:"completedRounds,omitempty"`

	// Conditions represent the latest available observations of the object's state
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Participants is the status of each participant
	Participants []ParticipantStatus `json:"participants,omitempty"`

	// AggregatedModelRef is the OCI reference to the latest aggregated model
	AggregatedModelRef string `json:"aggregatedModelRef,omitempty"`
}

type ParticipantStatus struct {
	// PropletID is the ID of the proplet
	PropletID string `json:"propletId"`

	// Status is the current status of the participant
	Status string `json:"status,omitempty"`

	// LastUpdate is the timestamp of the last update
	LastUpdate *metav1.Time `json:"lastUpdate,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
//+kubebuilder:printcolumn:name="Current Round",type="integer",JSONPath=".status.currentRound"
//+kubebuilder:printcolumn:name="Completed Rounds",type="integer",JSONPath=".status.completedRounds"
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

type FederatedJob struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   FederatedJobSpec   `json:"spec"`
	Status FederatedJobStatus `json:"status"`
}

//+kubebuilder:object:root=true

type FederatedJobList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []FederatedJob `json:"items"`
}

//nolint:gochecknoinits // init() is required for Kubernetes scheme registration
func init() {
	SchemeBuilder.Register(&FederatedJob{}, &FederatedJobList{})
}
