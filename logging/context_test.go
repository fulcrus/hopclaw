package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Tests - WithFields
// ---------------------------------------------------------------------------

func TestWithFields(t *testing.T) {
	ctx := context.Background()

	ctx = WithFields(ctx,
		slog.String("request_id", "req-123"),
		slog.String("user_id", "user-456"),
	)

	attrs := FieldsFromContext(ctx)
	if len(attrs) != 2 {
		t.Fatalf("FieldsFromContext() returned %d attrs, want 2", len(attrs))
	}
	if attrs[0].Key != "request_id" || attrs[0].Value.String() != "req-123" {
		t.Fatalf("attrs[0] = %v, want request_id=req-123", attrs[0])
	}
	if attrs[1].Key != "user_id" || attrs[1].Value.String() != "user-456" {
		t.Fatalf("attrs[1] = %v, want user_id=user-456", attrs[1])
	}
}

func TestWithFieldsNilContext(t *testing.T) {
	//lint:ignore SA1012 intentionally testing nil context handling
	attrs := FieldsFromContext(nil)
	if attrs != nil {
		t.Fatalf("FieldsFromContext(nil) = %v, want nil", attrs)
	}
}

func TestWithFieldsEmptyContext(t *testing.T) {
	ctx := context.Background()
	attrs := FieldsFromContext(ctx)
	if attrs != nil {
		t.Fatalf("FieldsFromContext(empty) = %v, want nil", attrs)
	}
}

// ---------------------------------------------------------------------------
// Tests - Nested WithFields
// ---------------------------------------------------------------------------

func TestFieldsNesting(t *testing.T) {
	ctx := context.Background()

	// First layer.
	ctx = WithFields(ctx, slog.String("request_id", "req-123"))

	// Second layer — fields should accumulate.
	ctx = WithFields(ctx, slog.String("session_id", "ses-789"))

	attrs := FieldsFromContext(ctx)
	if len(attrs) != 2 {
		t.Fatalf("FieldsFromContext() returned %d attrs, want 2", len(attrs))
	}

	found := make(map[string]string, len(attrs))
	for _, a := range attrs {
		found[a.Key] = a.Value.String()
	}
	if found["request_id"] != "req-123" {
		t.Fatalf("request_id = %q, want %q", found["request_id"], "req-123")
	}
	if found["session_id"] != "ses-789" {
		t.Fatalf("session_id = %q, want %q", found["session_id"], "ses-789")
	}
}

func TestFieldsTripleNesting(t *testing.T) {
	ctx := context.Background()
	ctx = WithFields(ctx, slog.String("a", "1"))
	ctx = WithFields(ctx, slog.String("b", "2"))
	ctx = WithFields(ctx, slog.String("c", "3"))

	attrs := FieldsFromContext(ctx)
	if len(attrs) != 3 {
		t.Fatalf("FieldsFromContext() returned %d attrs, want 3", len(attrs))
	}
}

// ---------------------------------------------------------------------------
// Tests - FromContext
// ---------------------------------------------------------------------------

func TestFromContext(t *testing.T) {
	// Set up a JSON buffer to capture output.
	var buf bytes.Buffer
	opts := &slog.HandlerOptions{Level: slog.LevelDebug}
	inner := slog.NewJSONHandler(&buf, opts)
	handler := newRedactHandler(inner, defaultRedactKeys)
	handler2 := newSubsystemLevelHandler(handler, &globalLevel, &subsystemLevel)
	slog.SetDefault(slog.New(handler2))
	globalLevel.Set(slog.LevelDebug)
	subsystemLevel.reset()

	ctx := context.Background()
	ctx = WithFields(ctx,
		slog.String("request_id", "req-abc"),
		slog.String("trace_id", "trace-xyz"),
	)

	logger := FromContext(ctx)
	logger.Info("context test")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("json.Unmarshal() error = %v\nraw: %s", err, buf.String())
	}
	if v := entry["request_id"]; v != "req-abc" {
		t.Fatalf("request_id = %v, want %q", v, "req-abc")
	}
	if v := entry["trace_id"]; v != "trace-xyz" {
		t.Fatalf("trace_id = %v, want %q", v, "trace-xyz")
	}
	if v := entry["msg"]; v != "context test" {
		t.Fatalf("msg = %v, want %q", v, "context test")
	}
}

func TestFromContextEmpty(t *testing.T) {
	// FromContext with no fields should return default logger (no panic).
	logger := FromContext(context.Background())
	if logger == nil {
		t.Fatal("FromContext(empty) returned nil")
	}
}

func TestFromContextRedaction(t *testing.T) {
	// Ensure context fields are still subject to redaction.
	var buf bytes.Buffer
	opts := &slog.HandlerOptions{Level: slog.LevelDebug}
	inner := slog.NewJSONHandler(&buf, opts)
	handler := newRedactHandler(inner, []string{"token"})
	handler2 := newSubsystemLevelHandler(handler, &globalLevel, &subsystemLevel)
	slog.SetDefault(slog.New(handler2))
	globalLevel.Set(slog.LevelDebug)
	subsystemLevel.reset()

	ctx := WithFields(context.Background(), slog.String("token", "secret-val"))
	logger := FromContext(ctx)
	logger.Info("redact test")

	out := buf.String()
	if strings.Contains(out, "secret-val") {
		t.Fatalf("token value should be redacted: %s", out)
	}
	if !strings.Contains(out, redactedPlaceholder) {
		t.Fatalf("redacted placeholder should be present: %s", out)
	}
}

func TestWithTraceID(t *testing.T) {
	ctx := WithTraceID(context.Background(), "trace-abc")

	if got := TraceIDFromContext(ctx); got != "trace-abc" {
		t.Fatalf("TraceIDFromContext() = %q, want %q", got, "trace-abc")
	}
}

func TestTraceIDFromContextEmpty(t *testing.T) {
	if got := TraceIDFromContext(context.Background()); got != "" {
		t.Fatalf("TraceIDFromContext() = %q, want empty", got)
	}
}
