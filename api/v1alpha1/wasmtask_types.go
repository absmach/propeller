package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type WasmTaskSpec struct {
	// ImageURL is the OCI reference to the WASM image
	ImageURL string `json:"imageUrl"`

	// File is the WASM binary file (base64 encoded)
	// If provided, this takes precedence over ImageURL
	File []byte `json:"file,omitempty"`

	// CLIArgs are command-line arguments for the WASM workload
	CLIArgs []string `json:"cliArgs,omitempty"`

	// Inputs are input data for the workload
	Inputs []uint64 `json:"inputs,omitempty"`

	// Env is a map of environment variables
	Env map[string]string `json:"env,omitempty"`

	// Daemon indicates if the task should run as a daemon
	Daemon bool `json:"daemon,omitempty"`

	// PropletID is the specific proplet to run the task on
	// If empty, a proplet will be selected by the scheduler
	PropletID string `json:"propletId,omitempty"`

	// PropletGroupRef is a reference to a PropletGroup for scheduling
	PropletGroupRef *corev1.LocalObjectReference `json:"propletGroupRef,omitempty"`

	// Mode is the execution mode (infer, train)
	Mode string `json:"mode,omitempty"`

	// MonitoringProfile defines monitoring configuration
	MonitoringProfile *MonitoringProfile `json:"monitoringProfile,omitempty"`

	// Resources defines resource requirements
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// RestartPolicy defines the restart policy
	RestartPolicy corev1.RestartPolicy `json:"restartPolicy,omitempty"`
}

type MonitoringProfile struct {
	// Enabled enables monitoring
	Enabled bool `json:"enabled,omitempty"`

	// Interval is the monitoring interval in seconds
	Interval int `json:"interval,omitempty"`

	// Metrics are the metrics to collect
	Metrics []string `json:"metrics,omitempty"`
}

type WasmTaskStatus struct {
	// Phase is the current phase of the task
	Phase string `json:"phase,omitempty"`

	// PropletID is the proplet where the task is running
	PropletID string `json:"propletId,omitempty"`

	// JobRef is a reference to the K8s Job executing this task
	JobRef *corev1.ObjectReference `json:"jobRef,omitempty"`

	// StartTime is when the task started
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// FinishTime is when the task finished
	FinishTime *metav1.Time `json:"finishTime,omitempty"`

	// Results contains the task results
	// +kubebuilder:pruning:PreserveUnknownFields
	Results *apiextensionsv1.JSON `json:"results,omitempty"`

	// Error contains error information if the task failed
	Error string `json:"error,omitempty"`

	// Conditions represent the latest available observations of the object's state
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
//+kubebuilder:printcolumn:name="Proplet",type="string",JSONPath=".status.propletId"
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

type WasmTask struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   WasmTaskSpec   `json:"spec"`
	Status WasmTaskStatus `json:"status"`
}

//+kubebuilder:object:root=true

type WasmTaskList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []WasmTask `json:"items"`
}

//nolint:gochecknoinits // init() is required for Kubernetes scheme registration
func init() {
	SchemeBuilder.Register(&WasmTask{}, &WasmTaskList{})
}
