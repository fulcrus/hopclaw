package toolruntime

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/isolation"
)

func TestAgentSpawnInjectsDelegationContractIntoChildMessage(t *testing.T) {
	t.Parallel()

	messageCh := make(chan string, 1)
	spawner := isolation.NewSpawner(func(_ context.Context, _, message string) (string, error) {
		messageCh <- message
		return "ok", nil
	})

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	builtins.ApplyBindings(BuiltinsBindings{Spawner: spawner})

	results, err := builtins.ExecuteBatch(context.Background(), &agent.Run{
		ID:        "run-1",
		SessionID: "sess-1",
		Delegation: &agent.DelegationContract{
			Goal:                "Split repo inspection",
			AllowedDomains:      []string{"fs", "text"},
			SideEffectClass:     "local_write",
			MaxTurns:            4,
			MaxBudgetTokens:     4000,
			VerificationPlanRef: "task_contract:visible_result",
		},
	}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-1",
		Name: "agent.spawn",
		Input: map[string]any{
			"agent_name": "researcher",
			"message":    "Inspect the failing tests",
			"session_id": "sess-1",
		},
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch(agent.spawn) error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}

	select {
	case childMessage := <-messageCh:
		if !strings.Contains(childMessage, "<delegation_contract>") {
			t.Fatalf("child message = %q, want delegation contract", childMessage)
		}
		if !strings.Contains(childMessage, "Allowed domains: fs, text") {
			t.Fatalf("child message = %q, want allowed domains", childMessage)
		}
		if !strings.Contains(childMessage, "Inspect the failing tests") {
			t.Fatalf("child message = %q, want original task content", childMessage)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for spawned child message")
	}

	var payload struct {
		DelegationApplied bool   `json:"delegation_applied"`
		DelegationScope   string `json:"delegation_scope"`
	}
	if err := json.Unmarshal([]byte(results[0].Content), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if !payload.DelegationApplied {
		t.Fatalf("payload = %#v, want delegation_applied=true", payload)
	}
	if !strings.Contains(payload.DelegationScope, "domains=fs,text") {
		t.Fatalf("payload = %#v, want delegation scope summary", payload)
	}
}

func TestAgentSpawnRejectsUnauthorizedDelegation(t *testing.T) {
	t.Parallel()

	builtins := NewBuiltins(BuiltinsConfig{Root: t.TempDir()})
	builtins.ApplyBindings(BuiltinsBindings{
		Spawner: isolation.NewSpawner(func(_ context.Context, _, _ string) (string, error) {
			return "ok", nil
		}),
	})

	_, err := builtins.ExecuteBatch(context.Background(), &agent.Run{
		ID:        "run-1",
		SessionID: "sess-1",
	}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-1",
		Name: "agent.spawn",
		Input: map[string]any{
			"agent_name": "researcher",
			"message":    "Inspect the failing tests",
			"session_id": "sess-1",
		},
	}})
	if err == nil || !strings.Contains(err.Error(), "delegation is not authorized") {
		t.Fatalf("ExecuteBatch(agent.spawn) error = %v, want delegation authorization failure", err)
	}
}
