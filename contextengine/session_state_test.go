package contextengine

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/skill"
)

type stubStateStore struct {
	states map[string]map[string]StateEntry
}

func newStubStateStore() *stubStateStore {
	return &stubStateStore{
		states: make(map[string]map[string]StateEntry),
	}
}

func (s *stubStateStore) UpsertState(_ context.Context, sessionID string, entries []StateEntry) error {
	if _, ok := s.states[sessionID]; !ok {
		s.states[sessionID] = make(map[string]StateEntry)
	}
	for _, entry := range entries {
		if existing, ok := s.states[sessionID][entry.Key]; ok && entry.CreatedAt.IsZero() {
			entry.CreatedAt = existing.CreatedAt
		}
		s.states[sessionID][entry.Key] = entry
	}
	return nil
}

func (s *stubStateStore) ActiveStates(_ context.Context, sessionID string) ([]StateEntry, error) {
	sessionStates := s.states[sessionID]
	if len(sessionStates) == 0 {
		return nil, nil
	}
	out := make([]StateEntry, 0, len(sessionStates))
	for _, entry := range sessionStates {
		if entry.Status == "" || entry.Status == "active" {
			out = append(out, entry)
		}
	}
	return out, nil
}

func TestCompactWritesSessionState(t *testing.T) {
	t.Parallel()

	store := newStubStateStore()
	engine := NewSlidingWindowEngine(Config{
		CompactKeepLastN: 1,
		StateWriter:      store,
	}, nil)

	session := &Session{
		ID: "sess-state-compact",
		Messages: []Message{
			{
				Role:      RoleUser,
				Content:   "State policy captured.",
				CreatedAt: time.Now().UTC().Add(-2 * time.Minute),
				Metadata: map[string]any{
					MetadataKeySignalDecisions:   []string{"Use SQLite state."},
					MetadataKeySignalConstraints: []string{"Preserve audit trail."},
					MetadataKeySignalTODOs:       []string{"Add prepare coverage."},
				},
			},
			{
				Role:      RoleAssistant,
				Content:   "Newest reply stays in the hot window.",
				CreatedAt: time.Now().UTC().Add(-time.Minute),
			},
		},
	}

	if err := engine.Compact(context.Background(), session, CompactManual); err != nil {
		t.Fatalf("Compact() error = %v", err)
	}

	active, err := store.ActiveStates(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("ActiveStates() error = %v", err)
	}
	if len(active) != 3 {
		t.Fatalf("active state count = %d, want 3", len(active))
	}

	var seenDecision, seenConstraint, seenTodo bool
	for _, entry := range active {
		if entry.Key == "" || entry.CreatedAt.IsZero() || entry.UpdatedAt.IsZero() {
			t.Fatalf("invalid state entry = %#v", entry)
		}
		switch entry.Category {
		case "decision":
			seenDecision = entry.Value == "Use SQLite state."
		case "constraint":
			seenConstraint = entry.Value == "Preserve audit trail."
		case "todo":
			seenTodo = entry.Value == "Add prepare coverage."
		}
	}
	if !seenDecision || !seenConstraint || !seenTodo {
		t.Fatalf("missing extracted states: %#v", active)
	}
}

func TestPrepareInjectsSessionStatePrompt(t *testing.T) {
	t.Parallel()

	store := newStubStateStore()
	now := time.Now().UTC()
	if err := store.UpsertState(context.Background(), "sess-state-prepare", []StateEntry{
		{
			Key:        "decision:1",
			Category:   "decision",
			Value:      "Use session state as durable truth.",
			Status:     "active",
			Confidence: 1.0,
			CreatedAt:  now,
			UpdatedAt:  now,
		},
		{
			Key:        "todo:1",
			Category:   "todo",
			Value:      "Add follow-up verification.",
			Status:     "active",
			Confidence: 0.8,
			CreatedAt:  now,
			UpdatedAt:  now,
		},
	}); err != nil {
		t.Fatalf("UpsertState() error = %v", err)
	}

	engine := NewSlidingWindowEngine(Config{
		StateReader: store,
	}, nil)

	session := &Session{
		ID: "sess-state-prepare",
		PinnedFacts: []PinnedFact{
			{Key: "tenant", Content: "workspace-a"},
		},
		Messages: []Message{
			{Role: RoleUser, Content: "What's still active?"},
		},
	}

	prepared, _, err := engine.Prepare(context.Background(), session, nil, skill.RuntimeContext{})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if !strings.Contains(prepared.SystemPrompt, "<session_state>") {
		t.Fatalf("system prompt missing session_state block: %q", prepared.SystemPrompt)
	}
	if !strings.Contains(prepared.SystemPrompt, "- decision: Use session state as durable truth.") {
		t.Fatalf("system prompt missing decision state: %q", prepared.SystemPrompt)
	}
	if strings.Index(prepared.SystemPrompt, "Pinned facts:") > strings.Index(prepared.SystemPrompt, "<session_state>") {
		t.Fatalf("session_state block should follow pinned facts: %q", prepared.SystemPrompt)
	}
	var hasSessionStateSegment bool
	for _, segment := range prepared.Segments {
		if segment.Kind == SegmentSessionState {
			hasSessionStateSegment = true
			break
		}
	}
	if !hasSessionStateSegment {
		t.Fatalf("segments missing session_state entry: %#v", prepared.Segments)
	}
}
