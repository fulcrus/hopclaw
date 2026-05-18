package wire

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestNewLoggerAppliesDefaults(t *testing.T) {
	t.Parallel()

	l := NewLogger(Config{Enabled: true})

	if l.config.MaxEntries != defaultMaxEntries {
		t.Fatalf("MaxEntries = %d, want %d", l.config.MaxEntries, defaultMaxEntries)
	}
	if l.config.MaxBodyBytes != defaultMaxBodyBytes {
		t.Fatalf("MaxBodyBytes = %d, want %d", l.config.MaxBodyBytes, defaultMaxBodyBytes)
	}
	if l.config.RetentionTime != defaultRetentionTime {
		t.Fatalf("RetentionTime = %v, want %v", l.config.RetentionTime, defaultRetentionTime)
	}
	if !l.IsEnabled() {
		t.Fatal("expected logger to be enabled")
	}
}

func TestNewLoggerRespectsCustomConfig(t *testing.T) {
	t.Parallel()

	l := NewLogger(Config{
		Enabled:       true,
		MaxEntries:    50,
		MaxBodyBytes:  128,
		RetentionTime: 30 * time.Minute,
	})

	if l.config.MaxEntries != 50 {
		t.Fatalf("MaxEntries = %d, want 50", l.config.MaxEntries)
	}
	if l.config.MaxBodyBytes != 128 {
		t.Fatalf("MaxBodyBytes = %d, want 128", l.config.MaxBodyBytes)
	}
	if l.config.RetentionTime != 30*time.Minute {
		t.Fatalf("RetentionTime = %v, want 30m", l.config.RetentionTime)
	}
}

func TestLogAndRecent(t *testing.T) {
	t.Parallel()

	l := NewLogger(Config{Enabled: true, MaxEntries: 10})

	l.Log(Entry{Provider: "openai", Direction: DirectionRequest, Method: "POST", URL: "https://api.openai.com/v1/chat"})
	l.Log(Entry{Provider: "anthropic", Direction: DirectionRequest, Method: "POST", URL: "https://api.anthropic.com/v1/messages"})

	entries := l.Recent(5)
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
	if entries[0].Provider != "openai" {
		t.Fatalf("entries[0].Provider = %q, want openai", entries[0].Provider)
	}
	if entries[1].Provider != "anthropic" {
		t.Fatalf("entries[1].Provider = %q, want anthropic", entries[1].Provider)
	}
}

func TestLogAssignsIDAndTimestamp(t *testing.T) {
	t.Parallel()

	l := NewLogger(Config{Enabled: true, MaxEntries: 10})
	l.Log(Entry{Provider: "openai", Direction: DirectionRequest})

	entries := l.Recent(1)
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}
	if entries[0].ID == "" {
		t.Fatal("expected entry ID to be set")
	}
	if entries[0].RecordedAt.IsZero() {
		t.Fatal("expected entry timestamp to be set")
	}
}

func TestCircularBufferEviction(t *testing.T) {
	t.Parallel()

	const maxEntries = 5
	l := NewLogger(Config{Enabled: true, MaxEntries: maxEntries})

	for i := 0; i < maxEntries+3; i++ {
		l.Log(Entry{
			Provider:  "openai",
			Direction: DirectionRequest,
			URL:       fmt.Sprintf("https://api.openai.com/req/%d", i),
		})
	}

	entries := l.Recent(maxEntries + 10) // ask for more than exist
	if len(entries) != maxEntries {
		t.Fatalf("len(entries) = %d, want %d", len(entries), maxEntries)
	}

	// The oldest 3 entries should have been evicted; first remaining URL is req/3.
	if entries[0].URL != "https://api.openai.com/req/3" {
		t.Fatalf("entries[0].URL = %q, want req/3", entries[0].URL)
	}
	if entries[maxEntries-1].URL != "https://api.openai.com/req/7" {
		t.Fatalf("entries[last].URL = %q, want req/7", entries[maxEntries-1].URL)
	}
}

func TestQueryByProvider(t *testing.T) {
	t.Parallel()

	l := NewLogger(Config{Enabled: true, MaxEntries: 10})
	l.Log(Entry{Provider: "openai", Direction: DirectionRequest})
	l.Log(Entry{Provider: "anthropic", Direction: DirectionRequest})
	l.Log(Entry{Provider: "openai", Direction: DirectionResponse, Status: 200})

	results := l.Query(QueryFilter{Provider: "openai"})
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
	for _, r := range results {
		if r.Provider != "openai" {
			t.Fatalf("unexpected provider %q", r.Provider)
		}
	}
}

func TestQueryByDirection(t *testing.T) {
	t.Parallel()

	l := NewLogger(Config{Enabled: true, MaxEntries: 10})
	l.Log(Entry{Provider: "openai", Direction: DirectionRequest})
	l.Log(Entry{Provider: "openai", Direction: DirectionResponse, Status: 200})
	l.Log(Entry{Provider: "openai", Direction: DirectionRequest})

	results := l.Query(QueryFilter{Direction: DirectionResponse})
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].Direction != DirectionResponse {
		t.Fatalf("results[0].Direction = %q", results[0].Direction)
	}
}

func TestQueryBySessionID(t *testing.T) {
	t.Parallel()

	l := NewLogger(Config{Enabled: true, MaxEntries: 10})
	l.Log(Entry{Provider: "openai", Direction: DirectionRequest, SessionID: "sess-1"})
	l.Log(Entry{Provider: "openai", Direction: DirectionRequest, SessionID: "sess-2"})
	l.Log(Entry{Provider: "openai", Direction: DirectionRequest, SessionID: "sess-1"})

	results := l.Query(QueryFilter{SessionID: "sess-1"})
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
}

func TestQueryWithLimit(t *testing.T) {
	t.Parallel()

	l := NewLogger(Config{Enabled: true, MaxEntries: 10})
	for i := 0; i < 5; i++ {
		l.Log(Entry{Provider: "openai", Direction: DirectionRequest})
	}

	results := l.Query(QueryFilter{Limit: 2})
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
}

func TestQueryBySince(t *testing.T) {
	t.Parallel()

	l := NewLogger(Config{Enabled: true, MaxEntries: 10})

	past := time.Now().UTC().Add(-10 * time.Second)
	l.Log(Entry{Provider: "openai", Direction: DirectionRequest, RecordedAt: past})

	future := time.Now().UTC().Add(10 * time.Second)
	l.Log(Entry{Provider: "openai", Direction: DirectionRequest, RecordedAt: future})

	results := l.Query(QueryFilter{Since: time.Now().UTC()})
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
}

func TestSanitizeHeadersRedactsSensitiveHeaders(t *testing.T) {
	t.Parallel()

	headers := map[string]string{
		"Authorization":   "Bearer sk-abc123",
		"Content-Type":    "application/json",
		"X-Api-Key":       "secret-key",
		"Api-Key":         "another-secret",
		"X-HopClaw-Token": "token-value",
		"Accept":          "application/json",
	}

	sanitized := SanitizeHeaders(headers, nil)

	if sanitized["Authorization"] != "[REDACTED]" {
		t.Fatalf("Authorization = %q, want [REDACTED]", sanitized["Authorization"])
	}
	if sanitized["X-Api-Key"] != "[REDACTED]" {
		t.Fatalf("X-Api-Key = %q, want [REDACTED]", sanitized["X-Api-Key"])
	}
	if sanitized["Api-Key"] != "[REDACTED]" {
		t.Fatalf("Api-Key = %q, want [REDACTED]", sanitized["Api-Key"])
	}
	if sanitized["X-HopClaw-Token"] != "[REDACTED]" {
		t.Fatalf("X-HopClaw-Token = %q, want [REDACTED]", sanitized["X-HopClaw-Token"])
	}
	if sanitized["Content-Type"] != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", sanitized["Content-Type"])
	}
	if sanitized["Accept"] != "application/json" {
		t.Fatalf("Accept = %q, want application/json", sanitized["Accept"])
	}
}

func TestSanitizeHeadersCustomRedactList(t *testing.T) {
	t.Parallel()

	headers := map[string]string{
		"X-Custom-Secret": "my-secret",
		"Content-Type":    "application/json",
	}

	sanitized := SanitizeHeaders(headers, []string{"x-custom-secret"})

	if sanitized["X-Custom-Secret"] != "[REDACTED]" {
		t.Fatalf("X-Custom-Secret = %q, want [REDACTED]", sanitized["X-Custom-Secret"])
	}
	if sanitized["Content-Type"] != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", sanitized["Content-Type"])
	}
}

func TestSanitizeHeadersNilInput(t *testing.T) {
	t.Parallel()

	result := SanitizeHeaders(nil, nil)
	if result != nil {
		t.Fatalf("expected nil, got %v", result)
	}
}

func TestTruncateBodyLimitsSize(t *testing.T) {
	t.Parallel()

	body := strings.NewReader("hello, this is a long body that should be truncated")
	result := TruncateBody(body, 5)

	if result != "hello" {
		t.Fatalf("result = %q, want %q", result, "hello")
	}
}

func TestTruncateBodyShortBody(t *testing.T) {
	t.Parallel()

	body := strings.NewReader("hi")
	result := TruncateBody(body, 1024)

	if result != "hi" {
		t.Fatalf("result = %q, want %q", result, "hi")
	}
}

func TestTruncateBodyZeroMax(t *testing.T) {
	t.Parallel()

	body := strings.NewReader("some content")
	result := TruncateBody(body, 0)
	if result != "" {
		t.Fatalf("result = %q, want empty", result)
	}
}

func TestEnableDisable(t *testing.T) {
	t.Parallel()

	l := NewLogger(Config{Enabled: false, MaxEntries: 10})

	if l.IsEnabled() {
		t.Fatal("expected logger to be disabled")
	}

	// Entries should not be recorded when disabled.
	l.Log(Entry{Provider: "openai", Direction: DirectionRequest})
	if len(l.Recent(10)) != 0 {
		t.Fatal("expected no entries when disabled")
	}

	l.Enable()
	if !l.IsEnabled() {
		t.Fatal("expected logger to be enabled after Enable()")
	}

	l.Log(Entry{Provider: "openai", Direction: DirectionRequest})
	if len(l.Recent(10)) != 1 {
		t.Fatal("expected 1 entry after enabling")
	}

	l.Disable()
	if l.IsEnabled() {
		t.Fatal("expected logger to be disabled after Disable()")
	}

	l.Log(Entry{Provider: "openai", Direction: DirectionRequest})
	if len(l.Recent(10)) != 1 {
		t.Fatal("expected still 1 entry after disabling")
	}
}

func TestStatsComputation(t *testing.T) {
	t.Parallel()

	l := NewLogger(Config{Enabled: true, MaxEntries: 100})

	now := time.Now().UTC()
	l.Log(Entry{Provider: "openai", Direction: DirectionRequest, RecordedAt: now.Add(-2 * time.Second)})
	l.Log(Entry{Provider: "openai", Direction: DirectionResponse, Status: 200, Duration: 100 * time.Millisecond, RecordedAt: now.Add(-1 * time.Second)})
	l.Log(Entry{Provider: "anthropic", Direction: DirectionRequest, RecordedAt: now})
	l.Log(Entry{Provider: "anthropic", Direction: DirectionResponse, Status: 500, Duration: 200 * time.Millisecond, Error: "server error", RecordedAt: now})

	s := l.Stats()

	if s.TotalEntries != 4 {
		t.Fatalf("TotalEntries = %d, want 4", s.TotalEntries)
	}
	if s.RequestCount != 2 {
		t.Fatalf("RequestCount = %d, want 2", s.RequestCount)
	}
	if s.ResponseCount != 2 {
		t.Fatalf("ResponseCount = %d, want 2", s.ResponseCount)
	}
	if s.ErrorCount != 1 {
		t.Fatalf("ErrorCount = %d, want 1", s.ErrorCount)
	}
	expectedAvg := 150 * time.Millisecond
	if s.AvgDuration != expectedAvg {
		t.Fatalf("AvgDuration = %v, want %v", s.AvgDuration, expectedAvg)
	}
	if s.ByProvider["openai"] != 2 {
		t.Fatalf("ByProvider[openai] = %d, want 2", s.ByProvider["openai"])
	}
	if s.ByProvider["anthropic"] != 2 {
		t.Fatalf("ByProvider[anthropic] = %d, want 2", s.ByProvider["anthropic"])
	}
}

func TestStatsEmptyLogger(t *testing.T) {
	t.Parallel()

	l := NewLogger(Config{Enabled: true, MaxEntries: 10})
	s := l.Stats()

	if s.TotalEntries != 0 {
		t.Fatalf("TotalEntries = %d, want 0", s.TotalEntries)
	}
	if s.ByProvider == nil {
		t.Fatal("expected ByProvider map to be initialized")
	}
}

func TestLogRequestLogResponsePairing(t *testing.T) {
	t.Parallel()

	l := NewLogger(Config{Enabled: true, MaxEntries: 10})

	ctx := WithSessionID(context.Background(), "sess-42")
	ctx = WithRunID(ctx, "run-7")

	reqBody := strings.NewReader(`{"model":"gpt-4"}`)
	reqID := l.LogRequest(ctx, "openai", "POST", "https://api.openai.com/v1/chat", map[string]string{
		"Content-Type":  "application/json",
		"Authorization": "Bearer sk-test",
	}, reqBody)

	if reqID == "" {
		t.Fatal("expected non-empty request ID")
	}

	respBody := strings.NewReader(`{"id":"chatcmpl-1","choices":[]}`)
	l.LogResponse(reqID, 200, map[string]string{
		"Content-Type": "application/json",
	}, respBody, 150*time.Millisecond, nil)

	entries := l.Recent(10)
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}

	req := entries[0]
	if req.Direction != DirectionRequest {
		t.Fatalf("req.Direction = %q, want request", req.Direction)
	}
	if req.Provider != "openai" {
		t.Fatalf("req.Provider = %q", req.Provider)
	}
	if req.SessionID != "sess-42" {
		t.Fatalf("req.SessionID = %q", req.SessionID)
	}
	if req.RunID != "run-7" {
		t.Fatalf("req.RunID = %q", req.RunID)
	}
	// Authorization header should be redacted.
	if req.Headers["Authorization"] != "[REDACTED]" {
		t.Fatalf("req.Headers[Authorization] = %q, want [REDACTED]", req.Headers["Authorization"])
	}

	resp := entries[1]
	if resp.Direction != DirectionResponse {
		t.Fatalf("resp.Direction = %q, want response", resp.Direction)
	}
	if resp.Status != 200 {
		t.Fatalf("resp.Status = %d, want 200", resp.Status)
	}
	if resp.Duration != 150*time.Millisecond {
		t.Fatalf("resp.Duration = %v", resp.Duration)
	}
	// Response should inherit provider/session/run from request.
	if resp.Provider != "openai" {
		t.Fatalf("resp.Provider = %q, want openai", resp.Provider)
	}
	if resp.SessionID != "sess-42" {
		t.Fatalf("resp.SessionID = %q", resp.SessionID)
	}
	if resp.RunID != "run-7" {
		t.Fatalf("resp.RunID = %q", resp.RunID)
	}
}

func TestLogResponseWithError(t *testing.T) {
	t.Parallel()

	l := NewLogger(Config{Enabled: true, MaxEntries: 10})

	reqID := l.LogRequest(context.Background(), "openai", "POST", "https://api.openai.com/v1/chat", nil, nil)
	l.LogResponse(reqID, 0, nil, nil, 50*time.Millisecond, errors.New("connection reset"))

	entries := l.Recent(10)
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
	resp := entries[1]
	if resp.Error != "connection reset" {
		t.Fatalf("resp.Error = %q, want %q", resp.Error, "connection reset")
	}
}

func TestClear(t *testing.T) {
	t.Parallel()

	l := NewLogger(Config{Enabled: true, MaxEntries: 10})
	l.Log(Entry{Provider: "openai", Direction: DirectionRequest})
	l.Log(Entry{Provider: "openai", Direction: DirectionRequest})

	if len(l.Recent(10)) != 2 {
		t.Fatal("expected 2 entries before clear")
	}

	l.Clear()
	if len(l.Recent(10)) != 0 {
		t.Fatal("expected 0 entries after clear")
	}

	// Logger should still work after clear.
	l.Log(Entry{Provider: "openai", Direction: DirectionRequest})
	if len(l.Recent(10)) != 1 {
		t.Fatal("expected 1 entry after logging post-clear")
	}
}

func TestProviderFilter(t *testing.T) {
	t.Parallel()

	l := NewLogger(Config{
		Enabled:    true,
		MaxEntries: 10,
		Providers:  []string{"openai"},
	})

	l.Log(Entry{Provider: "openai", Direction: DirectionRequest})
	l.Log(Entry{Provider: "anthropic", Direction: DirectionRequest})
	l.Log(Entry{Provider: "OpenAI", Direction: DirectionRequest}) // case-insensitive

	entries := l.Recent(10)
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2 (only openai)", len(entries))
	}
}

func TestRecentReturnsChronologicalOrder(t *testing.T) {
	t.Parallel()

	l := NewLogger(Config{Enabled: true, MaxEntries: 10})

	for i := 0; i < 5; i++ {
		l.Log(Entry{Provider: "openai", Direction: DirectionRequest, URL: fmt.Sprintf("url-%d", i)})
	}

	entries := l.Recent(3)
	if len(entries) != 3 {
		t.Fatalf("len(entries) = %d, want 3", len(entries))
	}
	if entries[0].URL != "url-2" {
		t.Fatalf("entries[0].URL = %q, want url-2", entries[0].URL)
	}
	if entries[2].URL != "url-4" {
		t.Fatalf("entries[2].URL = %q, want url-4", entries[2].URL)
	}
}

func TestLogRedactsHeadersAutomatically(t *testing.T) {
	t.Parallel()

	l := NewLogger(Config{Enabled: true, MaxEntries: 10})
	l.Log(Entry{
		Provider:  "openai",
		Direction: DirectionRequest,
		Headers: map[string]string{
			"Authorization": "Bearer sk-secret",
			"Content-Type":  "application/json",
		},
	})

	entries := l.Recent(1)
	if entries[0].Headers["Authorization"] != "[REDACTED]" {
		t.Fatalf("Authorization = %q, want [REDACTED]", entries[0].Headers["Authorization"])
	}
	if entries[0].Headers["Content-Type"] != "application/json" {
		t.Fatalf("Content-Type = %q", entries[0].Headers["Content-Type"])
	}
}

func TestRecentZeroCount(t *testing.T) {
	t.Parallel()

	l := NewLogger(Config{Enabled: true, MaxEntries: 10})
	l.Log(Entry{Provider: "openai", Direction: DirectionRequest})

	entries := l.Recent(0)
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1 (0 means all)", len(entries))
	}
}

func TestQueryByRunID(t *testing.T) {
	t.Parallel()

	l := NewLogger(Config{Enabled: true, MaxEntries: 10})
	l.Log(Entry{Provider: "openai", Direction: DirectionRequest, RunID: "run-1"})
	l.Log(Entry{Provider: "openai", Direction: DirectionRequest, RunID: "run-2"})
	l.Log(Entry{Provider: "openai", Direction: DirectionRequest, RunID: "run-1"})

	results := l.Query(QueryFilter{RunID: "run-2"})
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].RunID != "run-2" {
		t.Fatalf("RunID = %q, want run-2", results[0].RunID)
	}
}

func TestCircularBufferWrapAround(t *testing.T) {
	t.Parallel()

	l := NewLogger(Config{Enabled: true, MaxEntries: 3})

	// Fill exactly, then overwrite all entries twice to exercise wraparound.
	for i := 0; i < 9; i++ {
		l.Log(Entry{Provider: "openai", Direction: DirectionRequest, URL: fmt.Sprintf("url-%d", i)})
	}

	entries := l.Recent(3)
	if len(entries) != 3 {
		t.Fatalf("len(entries) = %d, want 3", len(entries))
	}
	// Should contain the last 3: url-6, url-7, url-8.
	if entries[0].URL != "url-6" {
		t.Fatalf("entries[0].URL = %q, want url-6", entries[0].URL)
	}
	if entries[1].URL != "url-7" {
		t.Fatalf("entries[1].URL = %q, want url-7", entries[1].URL)
	}
	if entries[2].URL != "url-8" {
		t.Fatalf("entries[2].URL = %q, want url-8", entries[2].URL)
	}
}
