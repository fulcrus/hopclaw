package contextengine

import (
	"context"
	"strings"
	"sync"
	"testing"
)

// mockMemoryWriter records Set calls for verification.
type mockMemoryWriter struct {
	mu      sync.Mutex
	entries map[string]string
	setErr  error
}

func newMockMemoryWriter() *mockMemoryWriter {
	return &mockMemoryWriter{entries: make(map[string]string)}
}

func (m *mockMemoryWriter) Set(_ context.Context, key, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.setErr != nil {
		return m.setErr
	}
	m.entries[key] = value
	return nil
}

func (m *mockMemoryWriter) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.entries)
}

func (m *mockMemoryWriter) hasValueContaining(substr string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, v := range m.entries {
		if strings.Contains(v, substr) {
			return true
		}
	}
	return false
}

func TestMemoryFlushHook_ExtractsAnnotatedSignals(t *testing.T) {
	t.Parallel()
	writer := newMockMemoryWriter()
	hook := NewMemoryFlushHook(writer)

	discarding := []Message{
		{
			Role:    RoleUser,
			Content: "We decided on PostgreSQL.",
			Metadata: map[string]any{
				MetadataKeySignalDecisions: []string{"Use PostgreSQL."},
			},
		},
		{Role: RoleAssistant, Content: "Noted, switching to PostgreSQL."},
	}
	session := &Session{ID: "sess-001"}

	if err := hook(context.Background(), discarding, session); err != nil {
		t.Fatalf("hook error: %v", err)
	}
	if !writer.hasValueContaining("Use PostgreSQL.") {
		t.Fatal("expected annotated decision signal to be flushed")
	}
}

func TestMemoryFlushHook_ExtractsPinnedFacts(t *testing.T) {
	t.Parallel()
	writer := newMockMemoryWriter()
	hook := NewMemoryFlushHook(writer)

	discarding := []Message{
		{
			Role:    RoleUser,
			Content: "remember this",
			Metadata: map[string]any{
				"pinned_fact": "The API endpoint is /v2/users",
			},
		},
	}
	session := &Session{ID: "sess-002"}

	if err := hook(context.Background(), discarding, session); err != nil {
		t.Fatalf("hook error: %v", err)
	}
	if !writer.hasValueContaining("/v2/users") {
		t.Fatal("expected pinned fact to be flushed")
	}
}

func TestMemoryFlushHook_ExtractsIdentifiers(t *testing.T) {
	t.Parallel()
	writer := newMockMemoryWriter()
	hook := NewMemoryFlushHook(writer)

	discarding := []Message{
		{
			Role:    RoleTool,
			Content: "Result: commit abc12345def6 at /usr/local/bin/app https://example.com/api/v2",
		},
	}
	session := &Session{ID: "sess-003"}

	if err := hook(context.Background(), discarding, session); err != nil {
		t.Fatalf("hook error: %v", err)
	}
	if !writer.hasValueContaining("abc12345def6") && !writer.hasValueContaining("example.com") {
		t.Fatal("expected identifiers to be flushed")
	}
}

func TestMemoryFlushHook_NilWriter(t *testing.T) {
	t.Parallel()
	hook := NewMemoryFlushHook(nil)
	if hook != nil {
		t.Fatal("expected nil hook for nil writer")
	}
}

func TestMemoryFlushHook_EmptyMessages(t *testing.T) {
	t.Parallel()
	writer := newMockMemoryWriter()
	hook := NewMemoryFlushHook(writer)

	if err := hook(context.Background(), nil, &Session{ID: "sess-004"}); err != nil {
		t.Fatalf("hook error: %v", err)
	}
	if writer.count() != 0 {
		t.Fatalf("expected 0 entries for empty messages, got %d", writer.count())
	}
}

func TestMemoryFlushHook_ChineseSignalMetadata(t *testing.T) {
	t.Parallel()
	writer := newMockMemoryWriter()
	hook := NewMemoryFlushHook(writer)

	discarding := []Message{
		{
			Role:    RoleUser,
			Content: "缓存策略已经确认。",
			Metadata: map[string]any{
				MetadataKeySignalDecisions:   []string{"我们决定使用Redis作为缓存。"},
				MetadataKeySignalConstraints: []string{"必须保证高可用。"},
			},
		},
	}
	session := &Session{ID: "sess-005"}

	if err := hook(context.Background(), discarding, session); err != nil {
		t.Fatalf("hook error: %v", err)
	}
	if !writer.hasValueContaining("我们决定使用Redis作为缓存。") || !writer.hasValueContaining("必须保证高可用。") {
		t.Fatal("expected structured Chinese signals to be flushed")
	}
}

func TestCompact_CallsPreCompactHook(t *testing.T) {
	t.Parallel()
	var hookCalled bool
	var hookMessages []Message

	engine := NewSlidingWindowEngine(Config{
		CompactKeepLastN:    1,
		CompactSummaryChars: 500,
		PreCompactHook: func(_ context.Context, discarding []Message, _ *Session) error {
			hookCalled = true
			hookMessages = discarding
			return nil
		},
	}, nil)

	session := &Session{
		Messages: []Message{
			{Role: RoleUser, Content: "msg 1"},
			{Role: RoleAssistant, Content: "msg 2"},
			{Role: RoleUser, Content: "msg 3"},
		},
	}

	if err := engine.Compact(context.Background(), session, CompactManual); err != nil {
		t.Fatalf("Compact() error = %v", err)
	}
	if !hookCalled {
		t.Fatal("PreCompactHook was not called")
	}
	if len(hookMessages) != 2 {
		t.Fatalf("expected 2 discarded messages passed to hook, got %d", len(hookMessages))
	}
}

func TestCompact_HookErrorNonFatal(t *testing.T) {
	t.Parallel()

	engine := NewSlidingWindowEngine(Config{
		CompactKeepLastN:    1,
		CompactSummaryChars: 500,
		PreCompactHook: func(_ context.Context, _ []Message, _ *Session) error {
			return context.DeadlineExceeded
		},
	}, nil)

	session := &Session{
		Messages: []Message{
			{Role: RoleUser, Content: "msg 1"},
			{Role: RoleAssistant, Content: "msg 2"},
			{Role: RoleUser, Content: "msg 3"},
		},
	}

	if err := engine.Compact(context.Background(), session, CompactEmergency); err != nil {
		t.Fatalf("Compact() should succeed despite hook error, got: %v", err)
	}
	if len(session.Messages) != 1 {
		t.Fatalf("expected 1 message after compact, got %d", len(session.Messages))
	}
	if session.Summary == "" {
		t.Fatal("expected summary to be generated despite hook failure")
	}
}

func TestCompact_OnCompactEvent(t *testing.T) {
	t.Parallel()
	var received CompactEvent

	engine := NewSlidingWindowEngine(Config{
		CompactKeepLastN:    1,
		CompactSummaryChars: 500,
		OnCompact: func(evt CompactEvent) {
			received = evt
		},
	}, nil)

	session := &Session{
		Messages: []Message{
			{Role: RoleUser, Content: "first message with some content"},
			{Role: RoleAssistant, Content: "response with details"},
			{Role: RoleUser, Content: "keep this one"},
		},
	}

	if err := engine.Compact(context.Background(), session, CompactPeriodic); err != nil {
		t.Fatalf("Compact() error = %v", err)
	}
	if received.Reason != CompactPeriodic {
		t.Fatalf("expected reason %q, got %q", CompactPeriodic, received.Reason)
	}
	if received.DiscardedCount != 2 {
		t.Fatalf("expected 2 discarded, got %d", received.DiscardedCount)
	}
	if received.PreTokens <= 0 {
		t.Fatalf("expected PreTokens > 0, got %d", received.PreTokens)
	}
	if received.Duration <= 0 {
		t.Fatal("expected Duration > 0")
	}
	if received.Session == nil {
		t.Fatal("expected Session to be populated in compact event")
	}
}

func TestCompact_NoHookNoEvent_StillWorks(t *testing.T) {
	t.Parallel()

	engine := NewSlidingWindowEngine(Config{
		CompactKeepLastN:    1,
		CompactSummaryChars: 500,
	}, nil)

	session := &Session{
		Messages: []Message{
			{Role: RoleUser, Content: "msg 1"},
			{Role: RoleAssistant, Content: "msg 2"},
			{Role: RoleUser, Content: "msg 3"},
		},
	}

	if err := engine.Compact(context.Background(), session, CompactManual); err != nil {
		t.Fatalf("Compact() error = %v", err)
	}
	if len(session.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(session.Messages))
	}
}

func TestCompact_PostCompactHookCalled(t *testing.T) {
	t.Parallel()

	var calls []string

	engine := NewSlidingWindowEngine(Config{
		CompactKeepLastN:    1,
		CompactSummaryChars: 500,
		OnCompact: func(evt CompactEvent) {
			calls = append(calls, "on")
			if evt.Session == nil {
				t.Fatal("expected session in compact event")
			}
		},
		PostCompactHook: func(_ context.Context, evt CompactEvent) error {
			calls = append(calls, "post")
			if evt.Session == nil {
				t.Fatal("expected session in compact event")
			}
			evt.Session.Summary = "hook adjusted summary"
			return nil
		},
	}, nil)

	session := &Session{
		Messages: []Message{
			{Role: RoleUser, Content: "msg 1"},
			{Role: RoleAssistant, Content: "msg 2"},
			{Role: RoleUser, Content: "msg 3"},
		},
	}

	if err := engine.Compact(context.Background(), session, CompactManual); err != nil {
		t.Fatalf("Compact() error = %v", err)
	}
	if len(calls) != 2 || calls[0] != "on" || calls[1] != "post" {
		t.Fatalf("expected hook order [on post], got %v", calls)
	}
	if session.Summary != "hook adjusted summary" {
		t.Fatalf("expected post hook to update summary, got %q", session.Summary)
	}
}

func TestCompact_PostCompactHookError(t *testing.T) {
	t.Parallel()

	engine := NewSlidingWindowEngine(Config{
		CompactKeepLastN:    1,
		CompactSummaryChars: 500,
		PostCompactHook: func(_ context.Context, evt CompactEvent) error {
			if evt.Session == nil {
				t.Fatal("expected session in compact event")
			}
			evt.Session.Summary = "should not commit"
			return context.Canceled
		},
	}, nil)

	session := &Session{
		Messages: []Message{
			{Role: RoleUser, Content: "msg 1"},
			{Role: RoleAssistant, Content: "msg 2"},
			{Role: RoleUser, Content: "msg 3"},
		},
	}

	err := engine.Compact(context.Background(), session, CompactManual)
	if err == nil {
		t.Fatal("expected error from post compact hook")
	}
	if !strings.Contains(err.Error(), "post-compact hook") {
		t.Fatalf("expected wrapped post-compact hook error, got %v", err)
	}
	if len(session.Messages) != 3 {
		t.Fatalf("expected session messages to remain unchanged, got %d", len(session.Messages))
	}
	if session.Summary != "" {
		t.Fatalf("expected summary to remain unchanged, got %q", session.Summary)
	}
}

func TestCompact_PostCompactHookNil(t *testing.T) {
	t.Parallel()

	engine := NewSlidingWindowEngine(Config{
		CompactKeepLastN:    1,
		CompactSummaryChars: 500,
	}, nil)

	session := &Session{
		Messages: []Message{
			{Role: RoleUser, Content: "msg 1"},
			{Role: RoleAssistant, Content: "msg 2"},
			{Role: RoleUser, Content: "msg 3"},
		},
	}

	if err := engine.Compact(context.Background(), session, CompactManual); err != nil {
		t.Fatalf("Compact() error = %v", err)
	}
	if len(session.Messages) != 1 {
		t.Fatalf("expected 1 message after compact, got %d", len(session.Messages))
	}
	if session.Summary == "" {
		t.Fatal("expected summary to be generated")
	}
}

func TestExtractAnnotatedSignals(t *testing.T) {
	t.Parallel()

	msg := Message{
		Metadata: map[string]any{
			MetadataKeySignalDecisions:   []any{"Use Redis.", "Use Redis."},
			MetadataKeySignalTODOs:       "Write tests.",
			MetadataKeySignalConstraints: []string{"Keep audit logs append-only."},
		},
	}

	decisions, todos, constraints := extractAnnotatedSignals(msg)
	if len(decisions) != 1 || decisions[0] != "Use Redis." {
		t.Fatalf("decisions = %v, want [Use Redis.]", decisions)
	}
	if len(todos) != 1 || todos[0] != "Write tests." {
		t.Fatalf("todos = %v, want [Write tests.]", todos)
	}
	if len(constraints) != 1 || constraints[0] != "Keep audit logs append-only." {
		t.Fatalf("constraints = %v, want [Keep audit logs append-only.]", constraints)
	}
}

func TestCountFlushableSignals(t *testing.T) {
	t.Parallel()

	messages := []Message{
		{
			Role:    RoleUser,
			Content: "Use Redis.",
			Metadata: map[string]any{
				MetadataKeySignalDecisions: []string{"Use Redis."},
			},
		},
		{Role: RoleTool, Content: "commit abc12345def6"},
		{
			Role:    RoleUser,
			Content: "ok",
			Metadata: map[string]any{
				"pinned_fact": "API is at /v2",
			},
		},
	}

	count := countFlushableSignals(messages)
	if count != 3 {
		t.Fatalf("expected 3 flushable signals, got %d", count)
	}
}
