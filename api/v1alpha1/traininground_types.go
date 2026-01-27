package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type TrainingRoundSpec struct {
	// RoundID is a unique identifier for this round
	RoundID string `json:"roundId"`

	// FederatedJobRef is a reference to the parent FederatedJob
	FederatedJobRef corev1.LocalObjectReference `json:"federatedJobRef"`

	// ModelRef is the OCI reference to the model for this round
	ModelRef string `json:"modelRef"`

	// TaskWasmImage is the OCI reference to the WASM image for the FL client task
	TaskWasmImage string `json:"taskWasmImage"`

	// Participants is a list of proplet IDs participating in this round
	Participants []string `json:"participants"`

	// Hyperparams are hyperparameters for this round
	// +kubebuilder:pruning:PreserveUnknownFields
	Hyperparams *apiextensionsv1.JSON `json:"hyperparams,omitempty"`

	// KOfN is the minimum number of participants required for aggregation
	KOfN int `json:"kOfN"`

	// TimeoutSeconds is the timeout for this round in seconds
	TimeoutSeconds int `json:"timeoutSeconds"`
}

type TrainingRoundStatus struct {
	// Phase is the current phase of the round
	Phase string `json:"phase,omitempty"`

	// StartTime is when the round started
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// EndTime is when the round ended
	EndTime *metav1.Time `json:"endTime,omitempty"`

	// Participants is the status of each participant
	Participants []RoundParticipantStatus `json:"participants,omitempty"`

	// UpdatesReceived is the number of updates received
	UpdatesReceived int `json:"updatesReceived,omitempty"`

	// UpdatesRequired is the number of updates required (k-of-n)
	UpdatesRequired int `json:"updatesRequired,omitempty"`

	// AggregatedModelRef is the OCI reference to the aggregated model
	AggregatedModelRef string `json:"aggregatedModelRef,omitempty"`

	// Conditions represent the latest available observations of the object's state
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

type RoundParticipantStatus struct {
	// PropletID is the ID of the proplet
	PropletID string `json:"propletId"`

	// TaskRef is a reference to the K8s Job for this participant's task
	TaskRef *corev1.ObjectReference `json:"taskRef,omitempty"`

	// Status is the current status of the participant
	Status string `json:"status,omitempty"`

	// UpdateReceived indicates if an update has been received from this participant
	UpdateReceived bool `json:"updateReceived,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
//+kubebuilder:printcolumn:name="Updates",type="string",JSONPath=".status.updatesReceived"
//+kubebuilder:printcolumn:name="Required",type="integer",JSONPath=".status.updatesRequired"
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

type TrainingRound struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   TrainingRoundSpec   `json:"spec"`
	Status TrainingRoundStatus `json:"status"`
}

//+kubebuilder:object:root=true

type TrainingRoundList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []TrainingRound `json:"items"`
}

//nolint:gochecknoinits // init() is required for Kubernetes scheme registration
func init() {
	SchemeBuilder.Register(&TrainingRound{}, &TrainingRoundList{})
}
