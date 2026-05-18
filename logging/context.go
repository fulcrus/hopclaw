package logging

import (
	"context"
	"log/slog"
)

// ---------------------------------------------------------------------------
// Context-based field propagation
// ---------------------------------------------------------------------------

// contextKey is the unexported key used to store slog attrs in a context.
type contextKey struct{}

// WithFields returns a new context carrying the given slog attrs. Fields
// accumulate when nesting WithFields calls — existing attrs are preserved
// and the new attrs are appended.
func WithFields(ctx context.Context, attrs ...slog.Attr) context.Context {
	existing := FieldsFromContext(ctx)
	merged := make([]slog.Attr, 0, len(existing)+len(attrs))
	merged = append(merged, existing...)
	merged = append(merged, attrs...)
	return context.WithValue(ctx, contextKey{}, merged)
}

// FieldsFromContext extracts attrs previously stored by WithFields.
// Returns nil if no attrs are present.
func FieldsFromContext(ctx context.Context) []slog.Attr {
	if ctx == nil {
		return nil
	}
	attrs, _ := ctx.Value(contextKey{}).([]slog.Attr)
	return attrs
}

// FromContext returns a logger enriched with all context fields. If no fields
// are present the default logger is returned unchanged.
func FromContext(ctx context.Context) *slog.Logger {
	attrs := FieldsFromContext(ctx)
	if len(attrs) == 0 {
		return slog.Default()
	}
	args := make([]any, len(attrs))
	for i, a := range attrs {
		args[i] = a
	}
	return slog.Default().With(args...)
}

// ---------------------------------------------------------------------------
// Convenience context enrichers
// ---------------------------------------------------------------------------

const (
	// AttrKeyRequestID is the structured log key for HTTP request IDs.
	AttrKeyRequestID = "request_id"
	// AttrKeySessionID is the structured log key for session IDs.
	AttrKeySessionID = "session_id"
	// AttrKeyRunID is the structured log key for run IDs.
	AttrKeyRunID = "run_id"
	// AttrKeyTraceID is the structured log key for distributed trace correlation.
	AttrKeyTraceID = "trace_id"
)

// WithRequestID returns a context carrying a request_id log field.
func WithRequestID(ctx context.Context, id string) context.Context {
	return WithFields(ctx, slog.String(AttrKeyRequestID, id))
}

// WithSessionID returns a context carrying a session_id log field.
func WithSessionID(ctx context.Context, id string) context.Context {
	return WithFields(ctx, slog.String(AttrKeySessionID, id))
}

// WithRunID returns a context carrying a run_id log field.
func WithRunID(ctx context.Context, id string) context.Context {
	return WithFields(ctx, slog.String(AttrKeyRunID, id))
}

// WithTraceID returns a context carrying a trace_id log field.
func WithTraceID(ctx context.Context, id string) context.Context {
	return WithFields(ctx, slog.String(AttrKeyTraceID, id))
}

// TraceIDFromContext extracts the trace_id field from context, if present.
func TraceIDFromContext(ctx context.Context) string {
	for _, attr := range FieldsFromContext(ctx) {
		if attr.Key == AttrKeyTraceID {
			return attr.Value.String()
		}
	}
	return ""
}
