package observability

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// StartSpan creates a new span with the given name and attributes.
func StartSpan(ctx context.Context, tracer trace.Tracer, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	//nolint:spancheck // Helper function deliberately returning the unended span directly to the caller.
	return tracer.Start(ctx, name, trace.WithAttributes(attrs...))
}

// SpanFromContext returns the current span from context.
func SpanFromContext(ctx context.Context) trace.Span {
	return trace.SpanFromContext(ctx)
}

// AddSpanAttributes adds attributes to the current span.
func AddSpanAttributes(ctx context.Context, attrs ...attribute.KeyValue) {
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(attrs...)
}

// RecordSpanError records an error on the current span.
func RecordSpanError(ctx context.Context, err error) {
	span := trace.SpanFromContext(ctx)
	span.RecordError(err)
}

// Common attribute helpers for consistent span attributes.

// TaskAttributes returns common task-related span attributes.
func TaskAttributes(taskID, taskName string) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String(FieldTaskID, taskID),
		attribute.String("task_name", taskName),
	}
}

// PropletAttributes returns common proplet-related span attributes.
func PropletAttributes(propletID string) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String(FieldPropletID, propletID),
	}
}

// JobAttributes returns common job-related span attributes.
func JobAttributes(jobID, jobName string) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String(FieldJobID, jobID),
		attribute.String("job_name", jobName),
	}
}

// PaginationAttributes returns pagination-related span attributes.
func PaginationAttributes(offset, limit uint64) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.Int64(FieldOffset, int64(offset)),
		attribute.Int64(FieldLimit, int64(limit)),
	}
}
