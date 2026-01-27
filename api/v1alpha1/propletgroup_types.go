package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type PropletGroupSpec struct {
	// Selector is a label selector for matching proplets
	Selector PropletSelector `json:"selector"`

	// Scheduling defines the scheduling configuration
	Scheduling SchedulingConfig `json:"scheduling"`
}

type PropletSelector struct {
	// MatchLabels is a map of labels to match
	MatchLabels map[string]string `json:"matchLabels,omitempty"`
}

type SchedulingConfig struct {
	// Algorithm is the scheduling algorithm (round-robin, least-loaded, random)
	Algorithm string `json:"algorithm"`

	// MaxTasksPerProplet is the maximum number of tasks per proplet
	MaxTasksPerProplet int `json:"maxTasksPerProplet,omitempty"`
}

type PropletGroupStatus struct {
	// Proplets is the list of proplets in this group
	Proplets []PropletInfo `json:"proplets,omitempty"`

	// TotalProplets is the total number of proplets
	TotalProplets int `json:"totalProplets,omitempty"`

	// AvailableProplets is the number of available proplets
	AvailableProplets int `json:"availableProplets,omitempty"`
}

type PropletInfo struct {
	// ID is the proplet ID
	ID string `json:"id"`

	// Namespace is the Kubernetes namespace
	Namespace string `json:"namespace,omitempty"`

	// Alive indicates if the proplet is alive
	Alive bool `json:"alive"`

	// TaskCount is the current number of tasks
	TaskCount int `json:"taskCount"`

	// LastHeartbeat is the timestamp of the last heartbeat
	LastHeartbeat *metav1.Time `json:"lastHeartbeat,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Total",type="integer",JSONPath=".status.totalProplets"
//+kubebuilder:printcolumn:name="Available",type="integer",JSONPath=".status.availableProplets"
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

type PropletGroup struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   PropletGroupSpec   `json:"spec"`
	Status PropletGroupStatus `json:"status"`
}

//+kubebuilder:object:root=true

type PropletGroupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []PropletGroup `json:"items"`
}

//nolint:gochecknoinits // init() is required for Kubernetes scheme registration
func init() {
	SchemeBuilder.Register(&PropletGroup{}, &PropletGroupList{})
}
