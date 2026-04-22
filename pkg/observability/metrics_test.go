//nolint:testpackage
package observability

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

//nolint:paralleltest
func TestMetricsRegistered(t *testing.T) {
	// Verify that metrics are registered by checking they can be collected
	tests := []struct {
		name    string
		collect func() prometheus.Collector
	}{
		{"TasksCreatedTotal", func() prometheus.Collector { return TasksCreatedTotal }},
		{"TasksStartedTotal", func() prometheus.Collector { return TasksStartedTotal }},
		{"TasksCompletedTotal", func() prometheus.Collector { return TasksCompletedTotal }},
		{"ActiveTasks", func() prometheus.Collector { return ActiveTasks }},
		{"PropletRegistrations", func() prometheus.Collector { return PropletRegistrations }},
		{"PropletDeregistrations", func() prometheus.Collector { return PropletDeregistrations }},
		{"JobsCreatedTotal", func() prometheus.Collector { return JobsCreatedTotal }},
		{"JobsCompletedTotal", func() prometheus.Collector { return JobsCompletedTotal }},
		{"WorkflowsCreatedTotal", func() prometheus.Collector { return WorkflowsCreatedTotal }},
		{"WasmExecutionDuration", func() prometheus.Collector { return WasmExecutionDuration }},
		{"ErrorsTotal", func() prometheus.Collector { return ErrorsTotal }},
		{"ConnectedProplets", func() prometheus.Collector { return ConnectedProplets }},
		{"ProxyContainerFetchesTotal", func() prometheus.Collector { return ProxyContainerFetchesTotal }},
		{"ProxyContainerFetchErrors", func() prometheus.Collector { return ProxyContainerFetchErrors }},
		{"ProxyChunksSentTotal", func() prometheus.Collector { return ProxyChunksSentTotal }},
		{"ProxyActiveFetches", func() prometheus.Collector { return ProxyActiveFetches }},
		{"ProxyFetchDuration", func() prometheus.Collector { return ProxyFetchDuration }},
		{"ProxyBytesTransferred", func() prometheus.Collector { return ProxyBytesTransferred }},
	}

	//nolint:paralleltest
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector := tt.collect()
			if collector == nil {
				t.Errorf("%s metric is nil", tt.name)
			}
		})
	}
}

//nolint:paralleltest
func TestRecordTaskCreated(t *testing.T) {
	// Record a task and verify no panic
	RecordTaskCreated(TaskKindStandard)
	RecordTaskCreated(TaskKindFederated)
}

//nolint:paralleltest
func TestRecordTaskLifecycle(t *testing.T) {
	// Test task lifecycle recording
	RecordTaskStarted()
	RecordTaskCompleted(TaskStatusSuccess)

	RecordTaskStarted()
	RecordTaskCompleted(TaskStatusFailed)
}

//nolint:paralleltest
func TestRecordPropletLifecycle(t *testing.T) {
	// Test proplet lifecycle recording
	RecordPropletRegistered()
	RecordPropletDeregistered()
}

//nolint:paralleltest
func TestRecordJobLifecycle(t *testing.T) {
	// Test job lifecycle recording
	RecordJobCreated()
	RecordJobCompleted(TaskStatusSuccess)
}

//nolint:paralleltest
func TestRecordWorkflowCreated(t *testing.T) {
	RecordWorkflowCreated()
}

//nolint:paralleltest
func TestRecordError(t *testing.T) {
	RecordError(ErrorTypeMQTT)
	RecordError(ErrorTypeStorage)
	RecordError(ErrorTypeTask)
	RecordError(ErrorTypeProplet)
}

//nolint:paralleltest
func TestObserveWasmExecution(t *testing.T) {
	ObserveWasmExecution("test-task", 1.5)
}

//nolint:paralleltest
func TestRecordMQTT(t *testing.T) {
	RecordMQTTReceived(TopicTypeControl)
	RecordMQTTPublished(TopicTypeResults)
}

//nolint:paralleltest
func TestRecordFL(t *testing.T) {
	RecordFLRound()
	RecordFLUpdate()
}

//nolint:paralleltest
func TestProxyMetrics(t *testing.T) {
	RecordContainerFetch()
	RecordContainerFetchError()
	RecordChunkSent()
	RecordActiveFetchStart()
	RecordActiveFetchEnd()
	ObserveFetchDuration(2.5)
	RecordBytesTransferred(1024.0)
}

//nolint:paralleltest
func TestConstants(t *testing.T) {
	// Verify constants are defined
	constants := []string{
		ErrorTypeMQTT, ErrorTypeStorage, ErrorTypeTask, ErrorTypeProplet,
		ErrorTypeWorkflow, ErrorTypeJob, ErrorTypeFL, ErrorTypeValidation,
		TaskStatusSuccess, TaskStatusFailed, TaskStatusTimeout,
		TaskKindStandard, TaskKindFederated,
	}

	for _, c := range constants {
		if c == "" {
			t.Error("empty constant found")
		}
	}
}
