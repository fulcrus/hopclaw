package contextengine

import (
	"context"
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/skill"
)

// mockMemoryReader returns canned search results.
type mockMemoryReader struct {
	results []MemorySearchResult
	err     error
}

func (m *mockMemoryReader) Search(_ context.Context, _ string) ([]MemorySearchResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.results, nil
}

func TestSyncPinnedFactsToMemory(t *testing.T) {
	t.Parallel()
	writer := newMockMemoryWriter()

	facts := []PinnedFact{
		{Key: "api_endpoint", Content: "/v2/users"},
		{Key: "db_choice", Content: "PostgreSQL"},
		{Key: "", Content: "no key, should be skipped"},
		{Key: "empty_content", Content: ""},
	}

	count := SyncPinnedFactsToMemory(context.Background(), facts, writer)
	if count != 2 {
		t.Fatalf("expected 2 synced facts, got %d", count)
	}
	if !writer.hasValueContaining("/v2/users") {
		t.Fatal("expected api_endpoint to be synced")
	}
	if !writer.hasValueContaining("PostgreSQL") {
		t.Fatal("expected db_choice to be synced")
	}
}

func TestSyncPinnedFactsToMemory_NilWriter(t *testing.T) {
	t.Parallel()
	count := SyncPinnedFactsToMemory(context.Background(), []PinnedFact{{Key: "k", Content: "v"}}, nil)
	if count != 0 {
		t.Fatalf("expected 0 for nil writer, got %d", count)
	}
}

func TestLoadPinnedFactsFromMemory(t *testing.T) {
	t.Parallel()

	reader := &mockMemoryReader{
		results: []MemorySearchResult{
			{Key: "pinned:api_endpoint", Value: "/v2/users"},
			{Key: "pinned:db_choice", Value: "PostgreSQL"},
			{Key: "unrelated:other", Value: "should be filtered"},
		},
	}

	facts := LoadPinnedFactsFromMemory(context.Background(), reader)
	if len(facts) != 2 {
		t.Fatalf("expected 2 facts, got %d", len(facts))
	}
	if facts[0].Key != "api_endpoint" || facts[0].Content != "/v2/users" {
		t.Fatalf("fact[0] = %+v", facts[0])
	}
	if facts[1].Key != "db_choice" || facts[1].Content != "PostgreSQL" {
		t.Fatalf("fact[1] = %+v", facts[1])
	}
	for _, f := range facts {
		if f.Source != "memory" {
			t.Fatalf("expected source 'memory', got %q", f.Source)
		}
	}
}

func TestLoadPinnedFactsFromMemory_NilReader(t *testing.T) {
	t.Parallel()
	facts := LoadPinnedFactsFromMemory(context.Background(), nil)
	if facts != nil {
		t.Fatalf("expected nil for nil reader, got %v", facts)
	}
}

func TestLoadPinnedFactsFromMemory_ErrorReturnsNil(t *testing.T) {
	t.Parallel()

	reader := &mockMemoryReader{err: context.DeadlineExceeded}
	facts := LoadPinnedFactsFromMemory(context.Background(), reader)
	if facts != nil {
		t.Fatalf("expected nil on error, got %v", facts)
	}
}

func TestPinnedFactRoundTrip_SyncThenLoad(t *testing.T) {
	t.Parallel()

	// Simulate: sync facts to memory, then load them back.
	writer := newMockMemoryWriter()
	facts := []PinnedFact{
		{Key: "db", Content: "PostgreSQL"},
		{Key: "cache", Content: "Redis"},
	}
	SyncPinnedFactsToMemory(context.Background(), facts, writer)

	// Build a reader from what was written.
	var results []MemorySearchResult
	for k, v := range writer.entries {
		if strings.HasPrefix(k, "pinned:") {
			results = append(results, MemorySearchResult{Key: k, Value: v})
		}
	}
	reader := &mockMemoryReader{results: results}

	loaded := LoadPinnedFactsFromMemory(context.Background(), reader)
	if len(loaded) != 2 {
		t.Fatalf("expected 2 round-tripped facts, got %d", len(loaded))
	}
}

func TestPinnedFactSurvivesCompaction(t *testing.T) {
	t.Parallel()

	writer := newMockMemoryWriter()

	engine := NewSlidingWindowEngine(Config{
		CompactKeepLastN:    1,
		CompactSummaryChars: 500,
		MemoryWriter:        writer,
	}, nil)

	session := &Session{
		Messages: []Message{
			{
				Role:    RoleUser,
				Content: "use Redis",
				Metadata: map[string]any{
					"pinned_fact": map[string]any{
						"key":     "cache",
						"content": "Use Redis for caching",
					},
				},
			},
			{Role: RoleAssistant, Content: "noted"},
			{Role: RoleUser, Content: "what was our cache decision?"},
		},
	}

	if err := engine.Compact(context.Background(), session, CompactManual); err != nil {
		t.Fatalf("Compact() error = %v", err)
	}

	// The pinned fact should survive in session.PinnedFacts.
	found := false
	for _, f := range session.PinnedFacts {
		if f.Key == "cache" && strings.Contains(f.Content, "Redis") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("pinned fact 'cache' not found in session after compaction: %+v", session.PinnedFacts)
	}

	// And it should be synced to the memory writer.
	if !writer.hasValueContaining("Redis") {
		t.Fatal("expected pinned fact to be synced to memory writer")
	}
}

func TestPreparePrefersSessionPinnedFactsOverDurableMemory(t *testing.T) {
	t.Parallel()

	engine := NewSlidingWindowEngine(Config{
		BaseSystemPrompt:    "You are a production coding agent.",
		PinnedFactsMaxChars: 400,
		MemoryReader: &mockMemoryReader{
			results: []MemorySearchResult{
				{Key: "pinned:cache", Value: "Use Memcached for caching"},
			},
		},
	}, nil)

	session := &Session{
		Messages: []Message{
			{
				Role:    RoleUser,
				Content: "Switch cache to Redis.",
				Metadata: map[string]any{
					MetadataKeyPinnedFact: map[string]any{
						"key":     "cache",
						"content": "Use Redis for caching",
					},
				},
			},
		},
	}

	prepared, _, err := engine.Prepare(context.Background(), session, nil, skill.RuntimeContext{})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if !strings.Contains(prepared.SystemPrompt, "Redis") {
		t.Fatalf("SystemPrompt = %q, want Redis fact", prepared.SystemPrompt)
	}
	if strings.Contains(prepared.SystemPrompt, "Memcached") {
		t.Fatalf("SystemPrompt = %q, durable memory incorrectly overrode session fact", prepared.SystemPrompt)
	}
	if len(session.PinnedFacts) != 1 || session.PinnedFacts[0].Content != "Use Redis for caching" {
		t.Fatalf("PinnedFacts = %#v", session.PinnedFacts)
	}
}
