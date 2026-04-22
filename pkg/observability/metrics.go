// Package observability provides unified observability primitives for the Propeller platform.
// It includes application-level metrics, logging utilities, and tracing configuration
// with support for OpenTelemetry Collector as the backend.
//
//nolint:godoclint
package observability

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	namespace = "propeller"
)

// Manager metrics track task and proplet lifecycle events.
var (
	// TasksCreatedTotal counts the total number of tasks created, labeled by kind.
	TasksCreatedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "manager",
			Name:      "tasks_created_total",
			Help:      "Total number of tasks created",
		},
		[]string{"kind"},
	)

	// TasksStartedTotal counts the total number of tasks started.
	TasksStartedTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "manager",
			Name:      "tasks_started_total",
			Help:      "Total number of tasks started",
		},
	)

	// TasksCompletedTotal counts completed tasks, labeled by status (success/failed).
	TasksCompletedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "manager",
			Name:      "tasks_completed_total",
			Help:      "Total number of tasks completed",
		},
		[]string{"status"},
	)

	// ActiveTasks tracks currently running tasks.
	ActiveTasks = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "manager",
			Name:      "active_tasks",
			Help:      "Number of currently active tasks",
		},
	)

	// PropletRegistrations counts proplet registration events.
	PropletRegistrations = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "manager",
			Name:      "proplet_registrations_total",
			Help:      "Total number of proplet registrations",
		},
	)

	// PropletDeregistrations counts proplet deregistration/leave events.
	PropletDeregistrations = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "manager",
			Name:      "proplet_deregistrations_total",
			Help:      "Total number of proplet deregistrations",
		},
	)

	// JobsCreatedTotal counts jobs created.
	JobsCreatedTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "manager",
			Name:      "jobs_created_total",
			Help:      "Total number of jobs created",
		},
	)

	// JobsCompletedTotal counts completed jobs, labeled by status.
	JobsCompletedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "manager",
			Name:      "jobs_completed_total",
			Help:      "Total number of jobs completed",
		},
		[]string{"status"},
	)

	// WorkflowsCreatedTotal counts workflows created.
	WorkflowsCreatedTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "manager",
			Name:      "workflows_created_total",
			Help:      "Total number of workflows created",
		},
	)

	// WasmExecutionDuration tracks WASM task execution duration.
	WasmExecutionDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "manager",
			Name:      "wasm_execution_duration_seconds",
			Help:      "WASM task execution duration in seconds",
			Buckets:   prometheus.ExponentialBuckets(0.1, 2, 12), // 0.1s to ~400s
		},
		[]string{"task_name"},
	)

	// ErrorsTotal counts errors by type.
	ErrorsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "manager",
			Name:      "errors_total",
			Help:      "Total number of errors by type",
		},
		[]string{"error_type"},
	)

	// MQTTMessagesReceived counts MQTT messages received by topic type.
	MQTTMessagesReceived = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "manager",
			Name:      "mqtt_messages_received_total",
			Help:      "Total number of MQTT messages received",
		},
		[]string{"topic_type"},
	)

	// MQTTMessagesPublished counts MQTT messages published by topic type.
	MQTTMessagesPublished = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "manager",
			Name:      "mqtt_messages_published_total",
			Help:      "Total number of MQTT messages published",
		},
		[]string{"topic_type"},
	)

	// FLRoundsTotal counts federated learning rounds.
	FLRoundsTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "manager",
			Name:      "fl_rounds_total",
			Help:      "Total number of federated learning rounds",
		},
	)

	// FLUpdatesReceived counts FL updates received from proplets.
	FLUpdatesReceived = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "manager",
			Name:      "fl_updates_received_total",
			Help:      "Total number of federated learning updates received",
		},
	)

	// ConnectedProplets tracks currently connected proplets.
	ConnectedProplets = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "manager",
			Name:      "connected_proplets",
			Help:      "Number of currently connected proplets",
		},
	)
)

// Proxy metrics track container fetching and chunk transfer.
var (
	// ProxyContainerFetchesTotal counts container fetch requests.
	ProxyContainerFetchesTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "proxy",
			Name:      "container_fetches_total",
			Help:      "Total number of container fetch requests",
		},
	)

	// ProxyContainerFetchErrors counts failed container fetches.
	ProxyContainerFetchErrors = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "proxy",
			Name:      "container_fetch_errors_total",
			Help:      "Total number of container fetch errors",
		},
	)

	// ProxyChunksSentTotal counts chunks sent via MQTT.
	ProxyChunksSentTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "proxy",
			Name:      "chunks_sent_total",
			Help:      "Total number of WASM chunks sent via MQTT",
		},
	)

	// ProxyActiveFetches tracks concurrent fetch operations.
	ProxyActiveFetches = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "proxy",
			Name:      "active_fetches",
			Help:      "Number of active container fetch operations",
		},
	)

	// ProxyFetchDuration tracks container fetch duration.
	ProxyFetchDuration = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "proxy",
			Name:      "fetch_duration_seconds",
			Help:      "Container fetch duration in seconds",
			Buckets:   prometheus.ExponentialBuckets(0.5, 2, 10), // 0.5s to ~500s
		},
	)

	// ProxyBytesTransferred tracks total bytes transferred.
	ProxyBytesTransferred = promauto.NewCounter(
		prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "proxy",
			Name:      "bytes_transferred_total",
			Help:      "Total bytes transferred for WASM modules",
		},
	)
)

// Error type constants for consistent labeling.
const (
	ErrorTypeMQTT       = "mqtt"
	ErrorTypeStorage    = "storage"
	ErrorTypeTask       = "task"
	ErrorTypeProplet    = "proplet"
	ErrorTypeWorkflow   = "workflow"
	ErrorTypeJob        = "job"
	ErrorTypeFL         = "fl"
	ErrorTypeValidation = "validation"
)

// Task status constants for consistent labeling.
const (
	TaskStatusSuccess = "success"
	TaskStatusFailed  = "failed"
	TaskStatusTimeout = "timeout"
)

// Task kind constants for consistent labeling.
const (
	TaskKindStandard  = "standard"
	TaskKindFederated = "federated"
)

// RecordTaskCreated increments the task created counter.
func RecordTaskCreated(kind string) {
	TasksCreatedTotal.WithLabelValues(kind).Inc()
}

// RecordTaskStarted increments the task started counter and active tasks gauge.
func RecordTaskStarted() {
	TasksStartedTotal.Inc()
	ActiveTasks.Inc()
}

// RecordTaskCompleted increments the completed counter and decrements active tasks.
func RecordTaskCompleted(status string) {
	TasksCompletedTotal.WithLabelValues(status).Inc()
	ActiveTasks.Dec()
}

// RecordError increments the error counter for the given type.
func RecordError(errorType string) {
	ErrorsTotal.WithLabelValues(errorType).Inc()
}

// RecordPropletRegistered increments registration counter and connected gauge.
func RecordPropletRegistered() {
	PropletRegistrations.Inc()
	ConnectedProplets.Inc()
}

// RecordPropletDeregistered increments deregistration counter and decrements connected gauge.
func RecordPropletDeregistered() {
	PropletDeregistrations.Inc()
	ConnectedProplets.Dec()
}

// RecordJobCreated increments the job created counter.
func RecordJobCreated() {
	JobsCreatedTotal.Inc()
}

// RecordJobCompleted increments the job completed counter with status.
func RecordJobCompleted(status string) {
	JobsCompletedTotal.WithLabelValues(status).Inc()
}

// RecordWorkflowCreated increments the workflow created counter.
func RecordWorkflowCreated() {
	WorkflowsCreatedTotal.Inc()
}

// RecordMQTTReceived increments the MQTT received counter.
func RecordMQTTReceived(topicType string) {
	MQTTMessagesReceived.WithLabelValues(topicType).Inc()
}

// RecordMQTTPublished increments the MQTT published counter.
func RecordMQTTPublished(topicType string) {
	MQTTMessagesPublished.WithLabelValues(topicType).Inc()
}

// RecordFLRound increments the FL rounds counter.
func RecordFLRound() {
	FLRoundsTotal.Inc()
}

// RecordFLUpdate increments the FL updates counter.
func RecordFLUpdate() {
	FLUpdatesReceived.Inc()
}

// ObserveWasmExecution records the WASM execution duration.
func ObserveWasmExecution(taskName string, durationSeconds float64) {
	WasmExecutionDuration.WithLabelValues(taskName).Observe(durationSeconds)
}

// Proxy metric helpers.

// RecordContainerFetch increments the container fetch counter.
func RecordContainerFetch() {
	ProxyContainerFetchesTotal.Inc()
}

// RecordContainerFetchError increments the fetch error counter.
func RecordContainerFetchError() {
	ProxyContainerFetchErrors.Inc()
}

// RecordChunkSent increments the chunks sent counter.
func RecordChunkSent() {
	ProxyChunksSentTotal.Inc()
}

// RecordActiveFetchStart increments the active fetches gauge.
func RecordActiveFetchStart() {
	ProxyActiveFetches.Inc()
}

// RecordActiveFetchEnd decrements the active fetches gauge.
func RecordActiveFetchEnd() {
	ProxyActiveFetches.Dec()
}

// ObserveFetchDuration records the container fetch duration.
func ObserveFetchDuration(durationSeconds float64) {
	ProxyFetchDuration.Observe(durationSeconds)
}

// RecordBytesTransferred adds to the bytes transferred counter.
func RecordBytesTransferred(bytes float64) {
	ProxyBytesTransferred.Add(bytes)
}
