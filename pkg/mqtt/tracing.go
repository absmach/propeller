package mqtt

import (
	"context"
	"encoding/json"

	"go.opentelemetry.io/otel/trace"
)

const (
	// TraceIDKey is the JSON key for trace ID in MQTT payloads.
	TraceIDKey = "_trace_id"
	// SpanIDKey is the JSON key for span ID in MQTT payloads.
	SpanIDKey = "_span_id"
	// TraceFlagsKey is the JSON key for trace flags in MQTT payloads.
	TraceFlagsKey = "_trace_flags"
)

// TraceContext represents trace context that can be embedded in MQTT messages.
type TraceContext struct {
	TraceID    string `json:"_trace_id,omitempty"`
	SpanID     string `json:"_span_id,omitempty"`
	TraceFlags string `json:"_trace_flags,omitempty"`
}

// InjectTraceContext injects trace context from the context into a map payload.
// This modifies the payload in place by adding trace context fields.
func InjectTraceContext(ctx context.Context, payload map[string]any) {
	span := trace.SpanFromContext(ctx)
	if span == nil {
		return
	}

	spanCtx := span.SpanContext()
	if !spanCtx.IsValid() {
		return
	}

	payload[TraceIDKey] = spanCtx.TraceID().String()
	payload[SpanIDKey] = spanCtx.SpanID().String()
	payload[TraceFlagsKey] = spanCtx.TraceFlags().String()
}

// InjectTraceContextJSON injects trace context into a JSON payload byte slice.
// Returns the modified payload or the original if injection fails.
func InjectTraceContextJSON(ctx context.Context, payload []byte) []byte {
	span := trace.SpanFromContext(ctx)
	if span == nil {
		return payload
	}

	spanCtx := span.SpanContext()
	if !spanCtx.IsValid() {
		return payload
	}

	// Parse the existing JSON
	var data map[string]any
	if err := json.Unmarshal(payload, &data); err != nil {
		// Not valid JSON or not an object, return as-is
		return payload
	}

	// Inject trace context
	data[TraceIDKey] = spanCtx.TraceID().String()
	data[SpanIDKey] = spanCtx.SpanID().String()
	data[TraceFlagsKey] = spanCtx.TraceFlags().String()

	// Re-marshal
	modified, err := json.Marshal(data)
	if err != nil {
		return payload
	}

	return modified
}

// ExtractTraceContext extracts trace context from a map payload.
// Returns a new context with the extracted trace context, or the original context if not found.
func ExtractTraceContext(ctx context.Context, payload map[string]any) context.Context {
	traceIDStr, ok := payload[TraceIDKey].(string)
	if !ok || traceIDStr == "" {
		return ctx
	}

	spanIDStr, ok := payload[SpanIDKey].(string)
	if !ok || spanIDStr == "" {
		return ctx
	}

	traceID, err := trace.TraceIDFromHex(traceIDStr)
	if err != nil {
		return ctx
	}

	spanID, err := trace.SpanIDFromHex(spanIDStr)
	if err != nil {
		return ctx
	}

	// Parse trace flags if present
	var flags trace.TraceFlags
	if flagsStr, ok := payload[TraceFlagsKey].(string); ok && len(flagsStr) == 2 {
		// TraceFlags is a single byte represented as 2 hex chars
		var flagByte byte
		for i := range 2 {
			c := flagsStr[i]
			switch {
			case c >= '0' && c <= '9':
				flagByte = flagByte*16 + (c - '0')
			case c >= 'a' && c <= 'f':
				flagByte = flagByte*16 + (c - 'a' + 10)
			case c >= 'A' && c <= 'F':
				flagByte = flagByte*16 + (c - 'A' + 10)
			}
		}
		flags = trace.TraceFlags(flagByte)
	}

	// Create a span context from the extracted data
	spanCtx := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: flags,
		Remote:     true,
	})

	return trace.ContextWithRemoteSpanContext(ctx, spanCtx)
}

// ExtractTraceContextJSON extracts trace context from a JSON payload.
// Returns a new context with the extracted trace context, or the original context if not found.
func ExtractTraceContextJSON(ctx context.Context, payload []byte) context.Context {
	var data map[string]any
	if err := json.Unmarshal(payload, &data); err != nil {
		return ctx
	}

	return ExtractTraceContext(ctx, data)
}

// RemoveTraceContext removes trace context fields from a map payload.
// Useful when forwarding messages without exposing internal trace context.
func RemoveTraceContext(payload map[string]any) {
	delete(payload, TraceIDKey)
	delete(payload, SpanIDKey)
	delete(payload, TraceFlagsKey)
}
