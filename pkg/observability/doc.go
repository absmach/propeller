// Package observability provides unified observability primitives for the Propeller platform.
//
// This package provides:
//   - Application-level Prometheus metrics for Manager, Proxy, and Proplet services
//   - Standardized log field names for consistent structured logging
//   - Tracing utilities for span creation and context propagation
//   - Utilities for trace context injection in logs
//
// # Metrics
//
// The package defines Prometheus metrics for tracking:
//   - Task lifecycle events (created, started, completed, failed)
//   - Proplet registrations and connections
//   - Job and workflow execution
//   - WASM execution duration
//   - MQTT message throughput
//   - Proxy container fetch operations
//
// Example usage:
//
//	import "github.com/absmach/propeller/pkg/observability"
//
//	// Record a task creation
//	observability.RecordTaskCreated(observability.TaskKindStandard)
//
//	// Record task completion
//	observability.RecordTaskCompleted(observability.TaskStatusSuccess)
//
//	// Observe WASM execution time
//	observability.ObserveWasmExecution("my-task", 1.5) // 1.5 seconds
//
// # Tracing
//
// Distributed tracing is configured via the Jaeger provider from supermq.
// This package provides helper functions for span creation and context propagation.
//
// # Logging
//
// Create a logger with trace context injection:
//
//	logger := observability.NewLogger(slog.LevelInfo, "manager", true)
//
// Use standardized field names:
//
//	logger.Info("task started",
//		slog.String(observability.FieldTaskID, taskID),
//		slog.String(observability.FieldPropletID, propletID),
//	)
//
//nolint:godoclint
package observability
