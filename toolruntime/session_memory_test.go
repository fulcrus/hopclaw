package toolruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
)

type sessionMemoryEmbeddingStub struct{}

func (sessionMemoryEmbeddingStub) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, text := range texts {
		lower := strings.ToLower(text)
		switch {
		case strings.Contains(lower, "calendar"),
			strings.Contains(lower, "schedule"),
			strings.Contains(lower, "meeting"):
			out[i] = []float32{1, 0}
		default:
			out[i] = []float32{0, 1}
		}
	}
	return out, nil
}

func TestMemoryGetSetDelete(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root, MaxReadBytes: 64 * 1024})
	builtins.ApplyBindings(BuiltinsBindings{MemoryStore: agent.NewInMemoryKVStore()})

	ctx := context.Background()
	run := &agent.Run{ID: "run-1"}
	sess := &agent.Session{ID: "sess-1"}

	exec := func(name string, input map[string]any) map[string]any {
		t.Helper()
		results, err := builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
			ID: "call-" + name, Name: name, Input: input,
		}})
		if err != nil {
			t.Fatalf("%s error: %v", name, err)
		}
		var out map[string]any
		if err := json.Unmarshal([]byte(results[0].Content), &out); err != nil {
			t.Fatalf("%s unmarshal: %v", name, err)
		}
		return out
	}

	// Get non-existent key.
	result := exec("memory.get", map[string]any{"key": "hello"})
	if result["found"] != false {
		t.Fatal("expected found=false for non-existent key")
	}

	// Set a key.
	result = exec("memory.set", map[string]any{"key": "hello", "value": "world"})
	if result["success"] != true {
		t.Fatalf("expected success=true, got %v", result["success"])
	}

	// Get the key.
	result = exec("memory.get", map[string]any{"key": "hello"})
	if result["found"] != true {
		t.Fatal("expected found=true after set")
	}
	if result["value"] != "world" {
		t.Fatalf("expected value='world', got %v", result["value"])
	}

	// Delete the key.
	result = exec("memory.delete", map[string]any{"key": "hello"})
	if result["success"] != true {
		t.Fatal("expected success=true for delete")
	}

	// Get after delete.
	result = exec("memory.get", map[string]any{"key": "hello"})
	if result["found"] != false {
		t.Fatal("expected found=false after delete")
	}
}

func TestMemorySearch(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root, MaxReadBytes: 64 * 1024})
	builtins.ApplyBindings(BuiltinsBindings{MemoryStore: agent.NewInMemoryKVStore()})

	ctx := context.Background()
	run := &agent.Run{ID: "run-1"}
	sess := &agent.Session{ID: "sess-1"}

	exec := func(name string, input map[string]any) map[string]any {
		t.Helper()
		results, err := builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
			ID: "call-" + name, Name: name, Input: input,
		}})
		if err != nil {
			t.Fatalf("%s error: %v", name, err)
		}
		var out map[string]any
		if err := json.Unmarshal([]byte(results[0].Content), &out); err != nil {
			t.Fatalf("%s unmarshal: %v", name, err)
		}
		return out
	}

	// Set some entries.
	exec("memory.set", map[string]any{"key": "project.name", "value": "OpenClaw"})
	exec("memory.set", map[string]any{"key": "project.version", "value": "1.0.0"})
	exec("memory.set", map[string]any{"key": "user.name", "value": "Alice"})

	// Search by key.
	result := exec("memory.search", map[string]any{"query": "project"})
	count := result["count"].(float64)
	if count != 2 {
		t.Fatalf("expected 2 results for 'project', got %v", count)
	}

	// Search by value.
	result = exec("memory.search", map[string]any{"query": "OpenClaw"})
	count = result["count"].(float64)
	if count != 1 {
		t.Fatalf("expected 1 result for 'OpenClaw', got %v", count)
	}

	// Search no match.
	result = exec("memory.search", map[string]any{"query": "nonexistent"})
	count = result["count"].(float64)
	if count != 0 {
		t.Fatalf("expected 0 results for 'nonexistent', got %v", count)
	}
}

func TestMemorySearchKeywordAndHybridModesDifferWhenEmbeddingsAreAvailable(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store := agent.NewInMemoryKVStore()
	store.SetEmbedding(sessionMemoryEmbeddingStub{})

	builtins := NewBuiltins(BuiltinsConfig{Root: root, MaxReadBytes: 64 * 1024})
	builtins.ApplyBindings(BuiltinsBindings{MemoryStore: store})

	ctx := context.Background()
	run := &agent.Run{ID: "run-1"}
	sess := &agent.Session{ID: "sess-1"}

	exec := func(input map[string]any) map[string]any {
		t.Helper()
		results, err := builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
			ID: "call-memory.search", Name: "memory.search", Input: input,
		}})
		if err != nil {
			t.Fatalf("memory.search error: %v", err)
		}
		var out map[string]any
		if err := json.Unmarshal([]byte(results[0].Content), &out); err != nil {
			t.Fatalf("memory.search unmarshal: %v", err)
		}
		return out
	}

	if err := store.Set(ctx, "agenda", "meeting schedule for next week"); err != nil {
		t.Fatalf("seed agenda: %v", err)
	}
	if err := store.Set(ctx, "groceries", "buy oranges and milk"); err != nil {
		t.Fatalf("seed groceries: %v", err)
	}

	keyword := exec(map[string]any{"query": "calendar", "mode": "keyword"})
	if keyword["count"].(float64) != 0 {
		t.Fatalf("expected keyword mode to stay lexical-only, got %v", keyword)
	}

	hybrid := exec(map[string]any{"query": "calendar", "mode": "hybrid"})
	if hybrid["count"].(float64) == 0 {
		t.Fatalf("expected hybrid mode to return semantic match, got %v", hybrid)
	}
	results := hybrid["results"].([]any)
	first := results[0].(map[string]any)
	if first["key"] != "agenda" {
		t.Fatalf("expected semantic agenda match, got %v", first["key"])
	}
}

func TestMemorySetUsesAgentUpsertAndCapturesContext(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	governed := agent.NewGovernedMemoryStore(agent.NewInMemoryKVStore())
	mirrored, err := agent.NewMirroredMemoryStore(governed, root+"/memory.md")
	if err != nil {
		t.Fatalf("NewMirroredMemoryStore() error = %v", err)
	}
	builtins := NewBuiltins(BuiltinsConfig{Root: root, MaxReadBytes: 64 * 1024})
	builtins.ApplyBindings(BuiltinsBindings{MemoryStore: mirrored})

	ctx := context.Background()
	run := &agent.Run{ID: "run-1"}
	sess := &agent.Session{ID: "sess-1", Key: "session-alpha"}

	results, err := builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
		ID:    "call-memory.set",
		Name:  "memory.set",
		Input: map[string]any{"key": "project.name", "value": "HopClaw"},
	}})
	if err != nil {
		t.Fatalf("memory.set error: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal([]byte(results[0].Content), &out); err != nil {
		t.Fatalf("memory.set unmarshal: %v", err)
	}
	if out["success"] != true {
		t.Fatalf("expected success=true, got %v", out["success"])
	}
	if out["session_key"] != sess.Key {
		t.Fatalf("session_key = %v, want %q", out["session_key"], sess.Key)
	}
	wantProjectID := agent.ProjectID(root)
	if out["project_id"] != wantProjectID {
		t.Fatalf("project_id = %v, want %q", out["project_id"], wantProjectID)
	}

	entry, err := mirrored.Get(ctx, "project.name")
	if err != nil {
		t.Fatalf("Get(project.name) error: %v", err)
	}
	if entry == nil {
		t.Fatal("expected stored entry")
	}
	if entry.Source != agent.MemorySourceAgent {
		t.Fatalf("entry.Source = %q, want %q", entry.Source, agent.MemorySourceAgent)
	}
	if entry.SessionKey != sess.Key {
		t.Fatalf("entry.SessionKey = %q, want %q", entry.SessionKey, sess.Key)
	}
	if entry.ProjectID != wantProjectID {
		t.Fatalf("entry.ProjectID = %q, want %q", entry.ProjectID, wantProjectID)
	}
}

func TestMemorySetReturnsBlockedHintForUserMemory(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	governed := agent.NewGovernedMemoryStore(agent.NewInMemoryKVStore())
	if err := governed.Set(context.Background(), "owner", "Alice"); err != nil {
		t.Fatalf("seed user memory: %v", err)
	}
	mirrored, err := agent.NewMirroredMemoryStore(governed, root+"/memory.md")
	if err != nil {
		t.Fatalf("NewMirroredMemoryStore() error = %v", err)
	}
	builtins := NewBuiltins(BuiltinsConfig{Root: root, MaxReadBytes: 64 * 1024})
	builtins.ApplyBindings(BuiltinsBindings{MemoryStore: mirrored})

	ctx := context.Background()
	run := &agent.Run{ID: "run-1"}
	sess := &agent.Session{ID: "sess-1", Key: "session-alpha"}

	results, err := builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
		ID:    "call-memory.set",
		Name:  "memory.set",
		Input: map[string]any{"key": "owner", "value": "Bob"},
	}})
	if err != nil {
		t.Fatalf("memory.set error: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal([]byte(results[0].Content), &out); err != nil {
		t.Fatalf("memory.set unmarshal: %v", err)
	}
	if blocked, _ := out["blocked"].(bool); !blocked {
		t.Fatalf("blocked = %v, want true", out["blocked"])
	}
	if out["reason"] != "cannot overwrite user memory" {
		t.Fatalf("reason = %v", out["reason"])
	}
	hint, _ := out["hint"].(string)
	if !strings.Contains(hint, "cannot be overwritten") {
		t.Fatalf("hint = %q", hint)
	}

	entry, err := mirrored.Get(ctx, "owner")
	if err != nil {
		t.Fatalf("Get(owner) error: %v", err)
	}
	if entry == nil || entry.Value != "Alice" {
		t.Fatalf("entry.Value = %v, want Alice", entry)
	}
}

func TestMemoryWithoutStore(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root, MaxReadBytes: 64 * 1024})
	// No memory store set.

	ctx := context.Background()
	run := &agent.Run{ID: "run-1"}
	sess := &agent.Session{ID: "sess-1"}

	results, err := builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
		ID: "call-1", Name: "memory.get", Input: map[string]any{"key": "test"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(results[0].Content), &out); err != nil {
		t.Fatal(err)
	}
	if out["found"] != false {
		t.Fatal("expected found=false without store")
	}
}

func TestSessionListWithStore(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root, MaxReadBytes: 64 * 1024})
	sessions := agent.NewInMemorySessionStore()
	builtins.ApplyBindings(BuiltinsBindings{Sessions: sessions})

	ctx := context.Background()
	run := &agent.Run{ID: "run-1"}
	sess := &agent.Session{ID: "sess-1"}

	// Create some sessions.
	sessions.GetOrCreate(ctx, "key-1", "gpt-4")
	sessions.GetOrCreate(ctx, "key-2", "claude-3")

	results, err := builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
		ID: "call-1", Name: "session.list", Input: map[string]any{},
	}})
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(results[0].Content), &out); err != nil {
		t.Fatal(err)
	}
	count := out["count"].(float64)
	if count != 2 {
		t.Fatalf("expected 2 sessions, got %v", count)
	}
}

func TestSessionHistoryWithStore(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root, MaxReadBytes: 64 * 1024})
	sessions := agent.NewInMemorySessionStore()
	builtins.ApplyBindings(BuiltinsBindings{Sessions: sessions})

	ctx := context.Background()
	run := &agent.Run{ID: "run-1"}
	sess := &agent.Session{ID: "sess-1"}

	// Create a session and add a message.
	created, _ := sessions.GetOrCreate(ctx, "test-key", "gpt-4")
	sessions.AppendUserMessage(ctx, created.ID, agent.IncomingMessage{
		Content: "Hello, world!",
	})

	results, err := builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
		ID: "call-1", Name: "session.history", Input: map[string]any{
			"session_id": created.ID,
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(results[0].Content), &out); err != nil {
		t.Fatal(err)
	}
	total := out["total_messages"].(float64)
	if total != 1 {
		t.Fatalf("expected 1 message, got %v", total)
	}
	msgs := out["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message in list, got %d", len(msgs))
	}
	msg := msgs[0].(map[string]any)
	if msg["content"] != "Hello, world!" {
		t.Fatalf("expected content 'Hello, world!', got %v", msg["content"])
	}
}

func TestSessionListWithoutStore(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root, MaxReadBytes: 64 * 1024})
	// No session store set.

	ctx := context.Background()
	run := &agent.Run{ID: "run-1"}
	sess := &agent.Session{ID: "sess-1"}

	results, err := builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
		ID: "call-1", Name: "session.list", Input: map[string]any{},
	}})
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(results[0].Content), &out); err != nil {
		t.Fatal(err)
	}
	// Should return empty sessions without error.
	if _, ok := out["sessions"]; !ok {
		t.Fatal("expected 'sessions' key")
	}
}

type helperOnlySessionStore struct {
	sessions map[string]*agent.Session
}

func (s *helperOnlySessionStore) GetOrCreate(context.Context, string, string, ...string) (*agent.Session, error) {
	return nil, nil
}

func (s *helperOnlySessionStore) AppendUserMessage(context.Context, string, agent.IncomingMessage) error {
	return nil
}

func (s *helperOnlySessionStore) LoadForExecution(context.Context, string) (*agent.Session, func(), error) {
	return nil, func() {}, nil
}

func (s *helperOnlySessionStore) Save(context.Context, *agent.Session) error {
	return nil
}

func (s *helperOnlySessionStore) ListScoped(_ context.Context, filter agent.SessionListFilter) ([]*agent.Session, error) {
	items := make([]*agent.Session, 0, len(s.sessions))
	for _, session := range s.sessions {
		if session == nil {
			continue
		}
		if !filter.Scope.IsZero() && !filter.Scope.Matches(session.Scope) {
			continue
		}
		items = append(items, &agent.Session{
			ID:           session.ID,
			Key:          session.Key,
			Model:        session.Model,
			MessageCount: len(session.Messages),
			CreatedAt:    session.CreatedAt,
			UpdatedAt:    session.UpdatedAt,
		})
	}
	return items, nil
}

func (s *helperOnlySessionStore) GetMetadata(_ context.Context, sessionID string) (*agent.Session, error) {
	session, ok := s.sessions[sessionID]
	if !ok || session == nil {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}
	return &agent.Session{
		ID:           session.ID,
		Key:          session.Key,
		Model:        session.Model,
		MessageCount: len(session.Messages),
		CreatedAt:    session.CreatedAt,
		UpdatedAt:    session.UpdatedAt,
	}, nil
}

func (s *helperOnlySessionStore) RecentMessages(_ context.Context, sessionID string, limit int) ([]contextengine.Message, error) {
	session, ok := s.sessions[sessionID]
	if !ok || session == nil {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}
	messages := append([]contextengine.Message(nil), session.Messages...)
	if limit > 0 && len(messages) > limit {
		messages = messages[len(messages)-limit:]
	}
	return messages, nil
}

func TestSessionBuiltinsUseAgentCapabilityHelpers(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	builtins := NewBuiltins(BuiltinsConfig{Root: root, MaxReadBytes: 64 * 1024})

	now := time.Unix(1_710_000_000, 0).UTC()
	store := &helperOnlySessionStore{
		sessions: map[string]*agent.Session{
			"sess-helper": {
				ID:        "sess-helper",
				Key:       "key-helper",
				Model:     "gpt-4.1",
				CreatedAt: now,
				UpdatedAt: now.Add(2 * time.Minute),
				Session: contextengine.Session{
					Messages: []contextengine.Message{
						{Role: contextengine.RoleUser, Content: "first", CreatedAt: now},
						{Role: contextengine.RoleAssistant, Content: "second", CreatedAt: now.Add(time.Second)},
					},
				},
			},
		},
	}
	builtins.ApplyBindings(BuiltinsBindings{Sessions: store})

	ctx := context.Background()
	run := &agent.Run{ID: "run-1"}
	sess := &agent.Session{ID: "sess-1"}

	listResults, err := builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
		ID: "call-list", Name: "session.list", Input: map[string]any{},
	}})
	if err != nil {
		t.Fatalf("session.list error: %v", err)
	}
	var listOut map[string]any
	if err := json.Unmarshal([]byte(listResults[0].Content), &listOut); err != nil {
		t.Fatalf("session.list unmarshal: %v", err)
	}
	if count := listOut["count"].(float64); count != 1 {
		t.Fatalf("session.list count = %v, want 1", count)
	}

	historyResults, err := builtins.ExecuteBatch(ctx, run, sess, []agent.ToolCall{{
		ID: "call-history", Name: "session.history", Input: map[string]any{
			"session_id": "sess-helper",
			"limit":      float64(1),
		},
	}})
	if err != nil {
		t.Fatalf("session.history error: %v", err)
	}
	var historyOut map[string]any
	if err := json.Unmarshal([]byte(historyResults[0].Content), &historyOut); err != nil {
		t.Fatalf("session.history unmarshal: %v", err)
	}
	if total := historyOut["total_messages"].(float64); total != 2 {
		t.Fatalf("session.history total_messages = %v, want 2", total)
	}
	items := historyOut["messages"].([]any)
	if len(items) != 1 {
		t.Fatalf("session.history messages len = %d, want 1", len(items))
	}
	message := items[0].(map[string]any)
	if message["content"] != "second" {
		t.Fatalf("session.history content = %#v, want second", message["content"])
	}
}
