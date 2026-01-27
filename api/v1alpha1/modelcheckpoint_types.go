package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type ModelCheckpointSpec struct {
	// FederatedJobRef is a reference to the parent FederatedJob
	FederatedJobRef corev1.LocalObjectReference `json:"federatedJobRef"`

	// RoundRef is a reference to the TrainingRound that produced this checkpoint
	RoundRef *corev1.LocalObjectReference `json:"roundRef,omitempty"`

	// ModelRef is the OCI reference to the model artifact
	ModelRef string `json:"modelRef"`

	// Algorithm is the aggregation algorithm used
	Algorithm string `json:"algorithm"`

	// Metadata contains additional metadata about the checkpoint
	Metadata CheckpointMetadata `json:"metadata"`
}

type CheckpointMetadata struct {
	// TotalSamples is the total number of samples used
	TotalSamples int `json:"totalSamples,omitempty"`

	// NumParticipants is the number of participants
	NumParticipants int `json:"numParticipants,omitempty"`

	// Metrics contains training metrics
	// +kubebuilder:pruning:PreserveUnknownFields
	Metrics *apiextensionsv1.JSON `json:"metrics,omitempty"`
}

type ModelCheckpointStatus struct {
	// Phase is the current phase of the checkpoint
	Phase string `json:"phase,omitempty"`

	// UploadTime is when the checkpoint was uploaded
	UploadTime *metav1.Time `json:"uploadTime,omitempty"`

	// SizeBytes is the size of the checkpoint in bytes
	SizeBytes int64 `json:"sizeBytes,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
//+kubebuilder:printcolumn:name="Size",type="string",JSONPath=".status.sizeBytes"
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

type ModelCheckpoint struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   ModelCheckpointSpec   `json:"spec"`
	Status ModelCheckpointStatus `json:"status"`
}

//+kubebuilder:object:root=true

type ModelCheckpointList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []ModelCheckpoint `json:"items"`
}

//nolint:gochecknoinits // init() is required for Kubernetes scheme registration
func init() {
	SchemeBuilder.Register(&ModelCheckpoint{}, &ModelCheckpointList{})
}
