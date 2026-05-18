package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/policy"
	"github.com/fulcrus/hopclaw/skill"
	"github.com/fulcrus/hopclaw/toolspec"
)

func TestToolCallMessagePersistsBeforeWaitingApproval(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "state")
	sessions, err := NewJSONLSessionStore(root)
	if err != nil {
		t.Fatalf("NewJSONLSessionStore() error = %v", err)
	}
	runs, err := NewJSONLRunStore(root)
	if err != nil {
		t.Fatalf("NewJSONLRunStore() error = %v", err)
	}
	approvals := approval.NewInMemoryStore()

	component := agent.NewComponent(agent.AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 3,
		QueueMode:     agent.QueueEnqueue,
	}, sessions, runs, agent.NewInMemoryCoordinator(), contextengine.NewSlidingWindowEngine(contextengine.Config{}, nil), persistenceTestModel{}, persistenceToolExecutor{}, nil).
		WithPolicy(policy.NewDefaultEngine(policy.Config{RequireApprovalForWrite: true})).
		WithApprovals(approvals)

	run, err := component.Submit(context.Background(), agent.IncomingMessage{
		SessionKey:      "chat-persist-tool-call",
		ExternalEventID: "evt-persist-tool-call",
		Content:         "write file",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if err := component.ExecuteRun(context.Background(), run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}
	run, err = runs.Get(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("runs.Get() error = %v", err)
	}
	if run.Status != agent.RunWaitingApproval {
		t.Fatalf("run.Status = %q", run.Status)
	}

	reloaded, err := NewJSONLSessionStore(root)
	if err != nil {
		t.Fatalf("NewJSONLSessionStore(reload) error = %v", err)
	}
	session, err := reloaded.GetOrCreate(context.Background(), "chat-persist-tool-call", "ignored")
	if err != nil {
		t.Fatalf("GetOrCreate(reload) error = %v", err)
	}
	if len(session.Messages) != 2 {
		t.Fatalf("len(session.Messages) = %d", len(session.Messages))
	}
	toolCallMsg := session.Messages[1]
	if toolCallMsg.Role != contextengine.RoleAssistant {
		t.Fatalf("toolCallMsg.Role = %q", toolCallMsg.Role)
	}
	if len(toolCallMsg.ToolCalls) != 1 {
		t.Fatalf("len(toolCallMsg.ToolCalls) = %d", len(toolCallMsg.ToolCalls))
	}
	if toolCallMsg.ToolCalls[0].Name != "fs.write" {
		t.Fatalf("toolCallMsg.ToolCalls[0].Name = %q", toolCallMsg.ToolCalls[0].Name)
	}
	if got, _ := toolCallMsg.Metadata["run_id"].(string); got != run.ID {
		t.Fatalf("toolCallMsg.Metadata[run_id] = %q", got)
	}
}

type persistenceTestModel struct{}

func (persistenceTestModel) Chat(context.Context, agent.ChatRequest) (*agent.ModelResponse, error) {
	return &agent.ModelResponse{
		ToolCalls: []agent.ToolCall{{
			ID:   "call-1",
			Name: "fs.write",
			Input: map[string]any{
				"path": "README.md",
			},
		}},
	}, nil
}

type persistenceToolExecutor struct{}

func (persistenceToolExecutor) ExecuteBatch(context.Context, *agent.Run, *agent.Session, []agent.ToolCall) ([]contextengine.ToolResult, error) {
	return nil, nil
}

func (persistenceToolExecutor) ResolveTool(_ *agent.Session, name string) (*agent.ResolvedTool, bool) {
	if name != "fs.write" {
		return nil, false
	}
	bound := &skill.BoundTool{
		Manifest: skill.ToolManifest{
			Name:            "fs.write",
			SideEffectClass: "local_write",
		},
		Package: &skill.SkillPackage{
			Prompt: skill.PromptSkill{Name: "writer"},
			Trust:  skill.TrustBundled,
		},
		Eligibility: skill.EligibilityResult{Eligible: true},
	}
	return toolspec.ResolvedFromSkillBinding(bound, agent.ToolDefinition{
		Name:            "fs.write",
		SideEffectClass: "local_write",
		Source:          "test",
		Trust:           string(skill.TrustBundled),
		Eligible:        true,
		Availability:    agent.ToolAvailability{Status: agent.AvailabilityReady},
	}, "store-test"), true
}
