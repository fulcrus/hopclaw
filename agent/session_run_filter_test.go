package agent

import (
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/internal/meta"
)

func TestFilterSessionMessagesForRunKeepsOnlyCurrentRunMessages(t *testing.T) {
	now := time.Now().UTC()
	session := &Session{
		Session: contextengine.Session{
			Messages: []contextengine.Message{
				{Role: contextengine.RoleUser, Content: "old user", CreatedAt: now, Metadata: map[string]any{meta.KeyRunID: "run-1"}},
				{Role: contextengine.RoleAssistant, Content: "old answer", CreatedAt: now, Metadata: map[string]any{meta.KeyRunID: "run-1"}},
				{Role: contextengine.RoleUser, Content: "current request", CreatedAt: now, Metadata: map[string]any{meta.KeyRunID: "run-2"}},
				{Role: contextengine.RoleAssistant, Content: "current intermediate", CreatedAt: now, Metadata: map[string]any{meta.KeyRunID: "run-2"}},
				{Role: contextengine.RoleUser, Content: "future request", CreatedAt: now, Metadata: map[string]any{meta.KeyRunID: "run-3"}},
				{Role: contextengine.RoleAssistant, Content: "future answer", CreatedAt: now, Metadata: map[string]any{meta.KeyRunID: "run-3"}},
				{Role: contextengine.RoleTool, Content: "current tool", CreatedAt: now, Metadata: map[string]any{meta.KeyRunID: "run-2"}},
			},
		},
		MessageCount: 7,
	}

	filterSessionMessagesForRun(session, "run-2")

	if len(session.Messages) != 3 {
		t.Fatalf("message count = %d, want 3", len(session.Messages))
	}
	for _, msg := range session.Messages {
		if msg.Content == "old user" || msg.Content == "old answer" {
			t.Fatalf("previous run message leaked into filtered session: %+v", msg)
		}
		if msg.Content == "future request" || msg.Content == "future answer" {
			t.Fatalf("future run message leaked into filtered session: %+v", msg)
		}
		if runID := msg.Metadata[meta.KeyRunID]; runID != "run-2" {
			t.Fatalf("message run id = %v, want run-2", runID)
		}
	}
}
