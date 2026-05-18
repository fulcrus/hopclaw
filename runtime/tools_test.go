package runtime

import (
	"context"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
)

// ---------------------------------------------------------------------------
// ListTools — nil agent
// ---------------------------------------------------------------------------

func TestListToolsNilAgentReturnsError(t *testing.T) {
	t.Parallel()
	runs := agent.NewInMemoryRunStore()
	svc := NewService(nil, agent.NewInMemorySessionStore(), runs, nil, nil, nil)
	_, err := svc.ListTools(context.Background(), "session-key")
	if err == nil {
		t.Fatal("expected error when agent is nil")
	}
}

// ---------------------------------------------------------------------------
// ListTools — with agent
// ---------------------------------------------------------------------------

func TestListToolsWithAgentReturnsToolDefinitions(t *testing.T) {
	t.Parallel()
	svc := newFullService()
	tools, err := svc.ListTools(context.Background(), "any-session")
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	// Without tool executors configured, should return empty list.
	if tools == nil {
		// nil is acceptable, but we verify no error occurred.
		return
	}
	// If any tools were returned, each should have a non-empty name.
	for _, tool := range tools {
		if tool.Name == "" {
			t.Fatal("tool name should not be empty")
		}
	}
}

// ---------------------------------------------------------------------------
// ListTools — different session keys
// ---------------------------------------------------------------------------

func TestListToolsDifferentSessionKeys(t *testing.T) {
	t.Parallel()
	svc := newFullService()

	tools1, err := svc.ListTools(context.Background(), "session-a")
	if err != nil {
		t.Fatalf("ListTools(session-a) error = %v", err)
	}

	tools2, err := svc.ListTools(context.Background(), "session-b")
	if err != nil {
		t.Fatalf("ListTools(session-b) error = %v", err)
	}

	// With no per-session configuration, both should return the same set.
	if len(tools1) != len(tools2) {
		t.Fatalf("expected same tool count, got %d vs %d", len(tools1), len(tools2))
	}
}

func TestListToolsEmptySessionKey(t *testing.T) {
	t.Parallel()
	svc := newFullService()
	tools, err := svc.ListTools(context.Background(), "")
	if err != nil {
		t.Fatalf("ListTools('') error = %v", err)
	}
	// Should not error even with empty session key.
	_ = tools
}
