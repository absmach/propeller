package observability

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"time"

	"go.opentelemetry.io/otel/trace"
)

// TracingHandler wraps an slog.Handler to inject trace context into log records.
type TracingHandler struct {
	handler slog.Handler
}

// NewTracingHandler creates a new handler that injects trace context.
func NewTracingHandler(handler slog.Handler) *TracingHandler {
	return &TracingHandler{handler: handler}
}

// Enabled reports whether the handler handles records at the given level.
func (h *TracingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.handler.Enabled(ctx, level)
}

// Handle adds trace context to the record and delegates to the wrapped handler.
func (h *TracingHandler) Handle(ctx context.Context, r slog.Record) error {
	span := trace.SpanFromContext(ctx)
	if span.SpanContext().IsValid() {
		r.AddAttrs(
			slog.String(FieldTraceID, span.SpanContext().TraceID().String()),
			slog.String(FieldSpanID, span.SpanContext().SpanID().String()),
		)
	}

	return h.handler.Handle(ctx, r)
}

// WithAttrs returns a new Handler with the given attributes.
func (h *TracingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &TracingHandler{handler: h.handler.WithAttrs(attrs)}
}

// WithGroup returns a new Handler with the given group name.
func (h *TracingHandler) WithGroup(name string) slog.Handler {
	return &TracingHandler{handler: h.handler.WithGroup(name)}
}

// NewLogger creates a new logger with the given level and optional trace injection.
func NewLogger(level slog.Level, serviceName string, injectTraceContext bool) *slog.Logger {
	opts := &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			// Add service name to all logs
			if a.Key == slog.TimeKey {
				return slog.Attr{
					Key:   a.Key,
					Value: slog.StringValue(a.Value.Time().Format(time.RFC3339Nano)),
				}
			}

			return a
		},
	}

	baseHandler := slog.NewJSONHandler(os.Stdout, opts)

	// Add service name as default attribute
	handlerWithService := baseHandler.WithAttrs([]slog.Attr{
		slog.String(FieldService, serviceName),
	})

	handler := handlerWithService
	if injectTraceContext {
		handler = NewTracingHandler(handlerWithService)
	}

	return slog.New(handler)
}

// ParseLogLevel parses a string log level into slog.Level.
func ParseLogLevel(level string) (slog.Level, error) {
	var l slog.Level
	err := l.UnmarshalText([]byte(level))

	return l, err
}

// BufferedLogExporter buffers logs for batch export.
type BufferedLogExporter struct {
	mu        sync.Mutex
	records   []LogRecord
	batchSize int
	exportFn  func([]LogRecord) error
}

// LogRecord represents a log entry for export.
type LogRecord struct {
	Timestamp   time.Time
	Level       slog.Level
	Message     string
	Attributes  map[string]any
	TraceID     string
	SpanID      string
	ServiceName string
}

// NewBufferedLogExporter creates a new buffered log exporter.
func NewBufferedLogExporter(batchSize int, exportFn func([]LogRecord) error) *BufferedLogExporter {
	return &BufferedLogExporter{
		records:   make([]LogRecord, 0, batchSize),
		batchSize: batchSize,
		exportFn:  exportFn,
	}
}

// Add adds a log record to the buffer, flushing if needed.
func (e *BufferedLogExporter) Add(record LogRecord) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.records = append(e.records, record)

	if len(e.records) >= e.batchSize {
		return e.flushLocked()
	}

	return nil
}

// Flush exports all buffered records.
func (e *BufferedLogExporter) Flush() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	return e.flushLocked()
}

func (e *BufferedLogExporter) flushLocked() error {
	if len(e.records) == 0 {
		return nil
	}

	records := e.records
	e.records = make([]LogRecord, 0, e.batchSize)

	if e.exportFn != nil {
		return e.exportFn(records)
	}

	return nil
}

// ContextLogger returns a logger with context-specific fields.
func ContextLogger(logger *slog.Logger, ctx context.Context, fields ...any) *slog.Logger {
	args := make([]any, 0, len(fields)+4)
	args = append(args, fields...)

	// Add trace context if available
	span := trace.SpanFromContext(ctx)
	if span.SpanContext().IsValid() {
		args = append(args,
			slog.String(FieldTraceID, span.SpanContext().TraceID().String()),
			slog.String(FieldSpanID, span.SpanContext().SpanID().String()),
		)
	}

	return logger.With(args...)
}

// LogOperation logs the start and end of an operation with timing.
func LogOperation(logger *slog.Logger, operation string) func(err error) {
	start := time.Now()
	logger.Info("operation started", slog.String(FieldOperation, operation))

	return func(err error) {
		duration := time.Since(start)
		if err != nil {
			logger.Error("operation failed",
				slog.String(FieldOperation, operation),
				slog.Duration(FieldDuration, duration),
				slog.String(FieldError, err.Error()),
			)
		} else {
			logger.Info("operation completed",
				slog.String(FieldOperation, operation),
				slog.Duration(FieldDuration, duration),
			)
		}
	}
}
