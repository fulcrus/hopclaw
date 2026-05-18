package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/eventbus"
	domainscope "github.com/fulcrus/hopclaw/internal/domain/scope"
	"github.com/fulcrus/hopclaw/internal/meta"
	"github.com/fulcrus/hopclaw/modelrouter"
	"github.com/fulcrus/hopclaw/policy"
	"github.com/fulcrus/hopclaw/resultmodel"
	"github.com/fulcrus/hopclaw/skill"
	"github.com/fulcrus/hopclaw/toolspec"
)

func TestSubmitDedupesExternalEvent(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
		DedupeWindow: time.Minute,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil)

	first, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-1",
		ExternalEventID: "evt-1",
		Content:         "hello",
	})
	if err != nil {
		t.Fatalf("Submit(first) error = %v", err)
	}
	second, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-1",
		ExternalEventID: "evt-1",
		Content:         "hello again",
	})
	if err != nil {
		t.Fatalf("Submit(second) error = %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("expected deduped run, got %s and %s", first.ID, second.ID)
	}

	session, err := sessions.GetOrCreate(context.Background(), "chat-1", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	if len(session.Messages) != 1 {
		t.Fatalf("len(Messages) = %d", len(session.Messages))
	}
}

func TestSubmitRecordsSessionRevisionOnRun(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey: "chat-revision",
		Content:    "hello",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.LastSessionRevision != 3 {
		t.Fatalf("run.LastSessionRevision = %d, want 3", run.LastSessionRevision)
	}

	session, err := sessions.GetOrCreate(context.Background(), "chat-revision", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	if session.Revision != 3 {
		t.Fatalf("session.Revision = %d, want 3", session.Revision)
	}
}

func TestToContextRunPropagatesJobType(t *testing.T) {
	t.Parallel()

	run := &Run{
		ID:    "run-job-type",
		Model: "test-model",
		TaskContract: &TaskContract{
			JobType: taskContractJobDevelopment,
		},
	}

	contextRun := toContextRun(run, "system prompt")
	if contextRun == nil {
		t.Fatal("toContextRun() = nil")
	}
	if contextRun.JobType != taskContractJobDevelopment {
		t.Fatalf("JobType = %q, want %q", contextRun.JobType, taskContractJobDevelopment)
	}
}

func TestToContextRunWithPromptPropagatesJobType(t *testing.T) {
	t.Parallel()

	run := &Run{
		ID:    "run-task-prompt",
		Model: "test-model",
		TaskContract: &TaskContract{
			JobType: taskContractJobResearch,
		},
	}

	contextRun := toContextRunWithPrompt(run, "task prompt")
	if contextRun == nil {
		t.Fatal("toContextRunWithPrompt() = nil")
	}
	if contextRun.JobType != taskContractJobResearch {
		t.Fatalf("JobType = %q, want %q", contextRun.JobType, taskContractJobResearch)
	}
}

func TestToContextRunPropagatesGoalAndTargetSummary(t *testing.T) {
	t.Parallel()

	run := &Run{
		ID:    "run-goal-target",
		Model: "test-model",
		TaskContract: &TaskContract{
			Goal:          "Inspect the current deployment issue",
			TargetSummary: "staging web-api",
		},
	}

	contextRun := toContextRun(run, "system prompt")
	if contextRun == nil {
		t.Fatal("toContextRun() = nil")
	}
	if contextRun.Goal != "Inspect the current deployment issue" {
		t.Fatalf("Goal = %q", contextRun.Goal)
	}
	if contextRun.TargetSummary != "staging web-api" {
		t.Fatalf("TargetSummary = %q", contextRun.TargetSummary)
	}
}

func TestSubmitStartsNewEpisodeAfterGroupIdleTimeout(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil)

	first, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey: "slack:group-episode",
		Content:    "first",
		Metadata: map[string]any{
			meta.KeyChannel: "slack",
			"chat_type":     "group",
		},
	})
	if err != nil {
		t.Fatalf("Submit(first) error = %v", err)
	}
	session, err := sessions.Get(context.Background(), first.SessionID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	session.Messages[len(session.Messages)-1].CreatedAt = time.Now().UTC().Add(-5 * time.Hour)
	if err := sessions.Save(context.Background(), session); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if _, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey: "slack:group-episode",
		Content:    "second",
		Metadata: map[string]any{
			meta.KeyChannel: "slack",
			"chat_type":     "group",
		},
	}); err != nil {
		t.Fatalf("Submit(second) error = %v", err)
	}

	episodes, err := sessions.ListEpisodes(context.Background(), first.SessionID)
	if err != nil {
		t.Fatalf("ListEpisodes() error = %v", err)
	}
	if len(episodes) != 2 {
		t.Fatalf("episode count = %d, want 2", len(episodes))
	}
	if episodes[0].Status != "sealed" || episodes[1].Status != "active" {
		t.Fatalf("episodes = %#v", episodes)
	}
}

func TestSubmitStartsNewEpisodeOnAgentProfileSwitch(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil)

	if _, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey: "channel:profile-switch",
		Content:    "hello",
		Metadata: map[string]any{
			MetadataKeyAgentProfileName:  "sales",
			MetadataKeyAgentProfileModel: "gpt-4.1",
		},
	}); err != nil {
		t.Fatalf("Submit(first) error = %v", err)
	}
	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey: "channel:profile-switch",
		Content:    "hello again",
		Metadata: map[string]any{
			MetadataKeyAgentProfileName:  "support",
			MetadataKeyAgentProfileModel: "gpt-4.1",
		},
	})
	if err != nil {
		t.Fatalf("Submit(second) error = %v", err)
	}

	episodes, err := sessions.ListEpisodes(context.Background(), run.SessionID)
	if err != nil {
		t.Fatalf("ListEpisodes() error = %v", err)
	}
	if len(episodes) != 2 {
		t.Fatalf("episode count = %d, want 2", len(episodes))
	}
	if episodes[0].Status != "sealed" || episodes[1].Status != "active" {
		t.Fatalf("episodes = %#v", episodes)
	}
}

func TestSubmitStartsNewEpisodeOnExplicitFreshStartRequest(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil)

	first, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey: "cli-fresh-start",
		Content:    "first",
		Metadata: map[string]any{
			meta.KeyChannel: "cli",
			"chat_type":     "direct",
		},
	})
	if err != nil {
		t.Fatalf("Submit(first) error = %v", err)
	}
	if _, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey: "cli-fresh-start",
		Content:    "help me rewrite this note",
		Metadata: map[string]any{
			meta.KeyChannel:                  "cli",
			"chat_type":                      "direct",
			MetadataKeyEpisodeBoundaryReason: "explicit_request",
		},
	}); err != nil {
		t.Fatalf("Submit(second) error = %v", err)
	}

	episodes, err := sessions.ListEpisodes(context.Background(), first.SessionID)
	if err != nil {
		t.Fatalf("ListEpisodes() error = %v", err)
	}
	if len(episodes) != 2 {
		t.Fatalf("episode count = %d, want 2", len(episodes))
	}
	if episodes[0].Status != "sealed" || episodes[1].Status != "active" {
		t.Fatalf("episodes = %#v", episodes)
	}
}

func TestExecuteRunCompletesTextResponse(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	component := NewComponent(AgentConfig{
		SystemPrompt:  "Be precise.",
		DefaultModel:  "test-model",
		MaxToolRounds: 3,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{
		responses: []*ModelResponse{{
			Message: contextengine.Message{
				Role:    contextengine.RoleAssistant,
				Content: "final answer",
			},
		}},
	}, nil, nil)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-2",
		ExternalEventID: "evt-2",
		Content:         "inspect repo",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if err := component.ExecuteRun(context.Background(), run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}
	run = mustReloadRun(t, runs, run)
	if run.Status != RunCompleted {
		t.Fatalf("run.Status = %q", run.Status)
	}
	if run.LastSessionRevision != 4 {
		t.Fatalf("run.LastSessionRevision = %d, want 4", run.LastSessionRevision)
	}

	session, err := sessions.GetOrCreate(context.Background(), "chat-2", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	if len(session.Messages) != 2 {
		t.Fatalf("len(Messages) = %d", len(session.Messages))
	}
	if session.Revision != 4 {
		t.Fatalf("session.Revision = %d, want 4", session.Revision)
	}
	if session.Messages[1].Content != "final answer" {
		t.Fatalf("assistant message = %#v", session.Messages[1])
	}
}

func TestExecuteRunAppendsProjectRulesToSystemPrompt(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("# Local Rules\n- Keep output terse.\n- Verify changes with targeted tests."), 0o644); err != nil {
		t.Fatalf("WriteFile(AGENTS.md) error = %v", err)
	}

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &stubModelClient{
		responses: []*ModelResponse{{
			Message: contextengine.Message{
				Role:    contextengine.RoleAssistant,
				Content: "final answer",
			},
		}},
	}
	component := NewComponent(AgentConfig{
		SystemPrompt:  "Be precise.",
		DefaultModel:  "test-model",
		MaxToolRounds: 3,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, nil, StaticRuntimeContextProvider{
		RuntimeContext: skill.RuntimeContext{
			Workspace: skill.WorkspaceContext{Root: root},
		},
	})

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-project-rules",
		ExternalEventID: "evt-project-rules",
		Content:         "inspect repo",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if err := component.ExecuteRun(context.Background(), run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}
	if !strings.Contains(model.lastRequest.SystemPrompt, "<project_rules>") {
		t.Fatalf("system prompt missing project rules: %s", model.lastRequest.SystemPrompt)
	}
	if !strings.Contains(model.lastRequest.SystemPrompt, "Keep output terse.") {
		t.Fatalf("system prompt missing repo instruction: %s", model.lastRequest.SystemPrompt)
	}
}

func TestExecuteRunInjectsRecalledMemoriesIntoSystemPrompt(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	memories := NewGovernedMemoryStore(NewInMemoryKVStore())
	if _, err := memories.UpsertRecord(ctx, MemoryRecord{
		Key:        "project.deploy.server.primary",
		Namespace:  "project",
		ScopeKey:   "deploy",
		Field:      "server",
		Label:      "Deploy Server",
		Value:      "198.51.100.42",
		Source:     MemorySourceUser,
		SessionKey: "chat-memory-prompt",
		ProjectID:  ProjectID(root),
	}); err != nil {
		t.Fatalf("UpsertRecord() error = %v", err)
	}
	if _, err := memories.UpsertRecord(ctx, MemoryRecord{
		Key:        "project.deploy.server.other",
		Namespace:  "project",
		ScopeKey:   "deploy",
		Field:      "server",
		Label:      "Other Project Server",
		Value:      "10.0.0.2",
		Source:     MemorySourceUser,
		SessionKey: "chat-memory-prompt",
		ProjectID:  ProjectID(t.TempDir()),
	}); err != nil {
		t.Fatalf("UpsertRecord(other project) error = %v", err)
	}
	entries, err := memories.List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	recalled := RecallForContext(entries, "chat-memory-prompt", ProjectID(root))
	if len(recalled.Memories) != 1 {
		t.Fatalf("len(recalled.Memories) = %d, want 1 (entries=%#v project_id=%q)", len(recalled.Memories), entries, ProjectID(root))
	}
	if recalled.Memories[0].Value != "198.51.100.42" {
		t.Fatalf("recalled memory = %#v", recalled.Memories[0])
	}
	if facts := InjectMemoryFacts(recalled.Memories); len(facts) != 2 {
		t.Fatalf("len(InjectMemoryFacts(...)) = %d, want 2", len(facts))
	}

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &stubModelClient{
		responses: []*ModelResponse{{
			Message: contextengine.Message{
				Role:    contextengine.RoleAssistant,
				Content: "final answer",
			},
		}},
	}
	component := NewComponent(AgentConfig{
		SystemPrompt:  "Be precise.",
		DefaultModel:  "test-model",
		MaxToolRounds: 3,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, nil, StaticRuntimeContextProvider{
		RuntimeContext: skill.RuntimeContext{
			Workspace: skill.WorkspaceContext{Root: root},
		},
	}).WithMemoryStore(memories)
	probe := &Session{Key: "chat-memory-prompt"}
	component.injectPromptMemoryFacts(ctx, probe, skill.RuntimeContext{
		Workspace: skill.WorkspaceContext{Root: root},
	})
	if len(probe.PinnedFacts) != 2 {
		t.Fatalf("len(probe.PinnedFacts) = %d, want 2", len(probe.PinnedFacts))
	}

	run, err := component.Submit(ctx, IncomingMessage{
		SessionKey:      "chat-memory-prompt",
		ExternalEventID: "evt-memory-prompt",
		Content:         "deploy the service",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if err := component.ExecuteRun(ctx, run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}
	if !strings.Contains(model.lastRequest.SystemPrompt, "[_memory_guide]") {
		t.Fatalf("system prompt missing memory guide: %s", model.lastRequest.SystemPrompt)
	}
	if !strings.Contains(model.lastRequest.SystemPrompt, "198.51.100.42") {
		t.Fatalf("system prompt missing recalled memory: %s", model.lastRequest.SystemPrompt)
	}
	if !strings.Contains(model.lastRequest.SystemPrompt, "DO NOT overwrite") {
		t.Fatalf("system prompt missing overwrite guard: %s", model.lastRequest.SystemPrompt)
	}
	if strings.Contains(model.lastRequest.SystemPrompt, "10.0.0.2") {
		t.Fatalf("system prompt included memory from another project: %s", model.lastRequest.SystemPrompt)
	}
}

func TestExecuteRunAppendsDelegationContractToSystemPrompt(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &stubModelClient{
		responses: []*ModelResponse{{
			Message: contextengine.Message{
				Role:    contextengine.RoleAssistant,
				Content: "delegated answer",
			},
		}},
	}
	tools := stubRuntimeToolExecutor{
		definitions: []ToolDefinition{
			{Name: "agent.spawn", SideEffectClass: "local_write"},
			{Name: "agent.yield", SideEffectClass: "read"},
			{Name: "fs.read", SideEffectClass: "read"},
			{Name: "fs.write", SideEffectClass: "local_write"},
		},
	}
	component := NewComponent(AgentConfig{
		SystemPrompt:  "Be precise.",
		DefaultModel:  "test-model",
		MaxToolRounds: 3,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, tools, nil)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey: "chat-delegation-prompt",
		Content:    "并行检查当前仓库并总结明显问题",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	run.Preflight = nil
	run.Status = RunQueued
	run.Phase = PhasePreparing
	run.Delegation = &DelegationContract{
		Goal:                "Analyze and fix failures",
		AllowedDomains:      []string{string(DomainFS), string(DomainText)},
		SideEffectClass:     "local_write",
		MaxTurns:            4,
		MaxBudgetTokens:     4000,
		VerificationPlanRef: "task_contract:visible_result",
	}
	if err := runs.Update(context.Background(), run); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if err := component.ExecuteRun(context.Background(), run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}
	if !strings.Contains(model.lastRequest.SystemPrompt, "<delegation_contract>") {
		t.Fatalf("system prompt missing delegation contract: %s", model.lastRequest.SystemPrompt)
	}
	if !strings.Contains(model.lastRequest.SystemPrompt, "agent.spawn") {
		t.Fatalf("system prompt missing delegation tool guidance: %s", model.lastRequest.SystemPrompt)
	}
	if !strings.Contains(model.lastRequest.SystemPrompt, "allowed child tools:") {
		t.Fatalf("system prompt missing allowed child tool list: %s", model.lastRequest.SystemPrompt)
	}
}

func TestExecuteRunLifecycleEventsIncludeChannel(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	bus := eventbus.NewInMemoryBus()
	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 3,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{
		responses: []*ModelResponse{{
			Message: contextengine.Message{
				Role:    contextengine.RoleAssistant,
				Content: "done",
			},
		}},
	}, nil, nil).WithEventBus(bus)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey: "slack:C123",
		Content:    "inspect repo",
		Metadata: map[string]any{
			meta.KeyChannel: "slack",
		},
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if err := component.ExecuteRun(context.Background(), run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}

	var started, completed bool
	for _, event := range bus.Snapshot() {
		switch event.Type {
		case eventbus.EventRunStarted:
			if event.Attrs["channel"] != "slack" {
				t.Fatalf("run.started channel = %#v", event.Attrs["channel"])
			}
			started = true
		case eventbus.EventRunCompleted:
			if event.Attrs["channel"] != "slack" {
				t.Fatalf("run.completed channel = %#v", event.Attrs["channel"])
			}
			completed = true
		}
	}
	if !started || !completed {
		t.Fatalf("started=%v completed=%v", started, completed)
	}
}

func TestExecuteRunHandlesToolRound(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 3,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{
		responses: []*ModelResponse{
			{
				ToolCalls: []ToolCall{{
					ID:   "call-1",
					Name: "fs.read",
				}},
			},
			{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "done",
				},
			},
		},
	}, stubToolExecutor{
		results: []contextengine.ToolResult{{
			ToolName:    "fs.read",
			ToolCallID:  "call-1",
			Content:     "file contents",
			ArtifactURI: "artifact://fs.read/1",
		}},
	}, nil)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-3",
		ExternalEventID: "evt-3",
		Content:         "read file",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if err := component.ExecuteRun(context.Background(), run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}
	run = mustReloadRun(t, runs, run)
	if run.ToolRounds != 1 {
		t.Fatalf("run.ToolRounds = %d", run.ToolRounds)
	}

	session, err := sessions.GetOrCreate(context.Background(), "chat-3", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	// Messages: user + assistant(tool_calls) + tool_result + assistant("done")
	if len(session.Messages) != 4 {
		t.Fatalf("len(Messages) = %d", len(session.Messages))
	}
	if session.Messages[1].Role != contextengine.RoleAssistant || len(session.Messages[1].ToolCalls) == 0 {
		t.Fatalf("expected assistant tool_call message, got %#v", session.Messages[1])
	}
	if session.Messages[2].Role != contextengine.RoleTool {
		t.Fatalf("tool message = %#v", session.Messages[2])
	}
	if session.Messages[3].Content != "done" {
		t.Fatalf("assistant message = %#v", session.Messages[3])
	}
}

func TestExecuteRunEmitsProgressPhaseEvents(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	bus := eventbus.NewInMemoryBus()
	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 3,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{
		responses: []*ModelResponse{
			{
				ToolCalls: []ToolCall{{
					ID:   "call-1",
					Name: "fs.read",
				}},
			},
			{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "done",
				},
			},
		},
	}, stubToolExecutor{
		results: []contextengine.ToolResult{{
			ToolName:   "fs.read",
			ToolCallID: "call-1",
			Content:    "file contents",
		}},
	}, nil).WithEventBus(bus)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey: "chat-progress-phase",
		Content:    "read config",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if err := component.ExecuteRun(context.Background(), run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}

	var phases []string
	for _, event := range bus.Snapshot() {
		if event.Type != eventbus.EventRunPhaseChanged {
			continue
		}
		if phase, _ := event.Attrs["phase"].(string); phase != "" {
			phases = append(phases, phase)
		}
	}
	for _, want := range []string{"thinking", "executing_tools", "processing_results"} {
		found := false
		for _, phase := range phases {
			if phase == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("phase %q missing from %#v", want, phases)
		}
	}
}

func TestExecuteRunRecoversFromToolExecutionError(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	bus := eventbus.NewInMemoryBus()
	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 3,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{
		responses: []*ModelResponse{
			{
				ToolCalls: []ToolCall{{
					ID:   "call-1",
					Name: "fs.read",
				}},
			},
			{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "I could not read the file, so here is a partial answer with the limitation called out.",
				},
			},
		},
	}, stubToolExecutor{
		err: errors.New("permission denied"),
	}, nil).WithEventBus(bus)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-tool-recover",
		ExternalEventID: "evt-tool-recover",
		Content:         "read the file and summarize it",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if err := component.ExecuteRun(context.Background(), run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}
	run = mustReloadRun(t, runs, run)
	if run.Status != RunCompleted {
		t.Fatalf("run.Status = %q", run.Status)
	}

	session, err := sessions.GetOrCreate(context.Background(), "chat-tool-recover", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	if len(session.Messages) != 4 {
		t.Fatalf("len(Messages) = %d", len(session.Messages))
	}
	if session.Messages[2].Role != contextengine.RoleTool {
		t.Fatalf("tool message = %#v", session.Messages[2])
	}
	failureResult, ok := resultmodel.DecodeToolResultMetadata(session.Messages[2].Metadata)
	if !ok {
		t.Fatalf("tool metadata missing: %#v", session.Messages[2].Metadata)
	}
	if !strings.Contains(session.Messages[2].Content, "permission denied") || failureResult.Error == nil || failureResult.Error.Message != "permission denied" {
		t.Fatalf("tool failure result = %#v", failureResult)
	}
	if flagged, _ := failureResult.Structured["tool_execution_error"].(bool); !flagged {
		t.Fatalf("tool failure structured payload = %#v", failureResult.Structured)
	}
	if session.Messages[3].Content == "" {
		t.Fatal("expected final assistant response after tool failure recovery")
	}

	foundRecoveredEvent := false
	for _, event := range bus.Snapshot() {
		if event.Type != eventbus.EventToolExecuted {
			continue
		}
		if got, _ := event.Attrs["execution_error"].(string); got != "permission denied" {
			continue
		}
		if recovered, _ := event.Attrs["recovered"].(bool); !recovered {
			t.Fatalf("recovered = %v, want true", event.Attrs["recovered"])
		}
		foundRecoveredEvent = true
	}
	if !foundRecoveredEvent {
		t.Fatal("expected recovered tool execution event")
	}
}

func TestExecuteRunRetriesOnRevisionConflictAfterToolPhase(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &stubModelClient{
		responses: []*ModelResponse{
			{
				ToolCalls: []ToolCall{{
					ID:   "call-1",
					Name: "fs.read",
				}},
			},
			{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "used the latest user update",
				},
			},
		},
	}
	tools := &blockingToolExecutor{
		results: []contextengine.ToolResult{{
			ToolName:   "fs.read",
			ToolCallID: "call-1",
			Content:    "stale result",
		}},
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 4,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, tools, nil)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey: "chat-conflict-retry",
		Content:    "read the file",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- component.ExecuteRun(context.Background(), run)
	}()

	<-tools.started
	if err := sessions.AppendUserMessage(context.Background(), run.SessionID, IncomingMessage{
		Content: "actually use this new info",
	}); err != nil {
		t.Fatalf("AppendUserMessage() error = %v", err)
	}
	close(tools.release)

	if err := <-errCh; err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}

	session, err := sessions.Get(context.Background(), run.SessionID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if session.Messages[len(session.Messages)-1].Content != "used the latest user update" {
		t.Fatalf("final assistant message = %#v", session.Messages[len(session.Messages)-1])
	}
	for _, msg := range session.Messages {
		if msg.Role == contextengine.RoleTool {
			t.Fatalf("unexpected tool result committed after revision conflict: %#v", msg)
		}
	}
}

func TestExecuteRunRetriesOnRevisionConflictDuringModelPhase(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &gatedScriptedModelClient{
		responses: []*ModelResponse{
			{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "stale draft",
				},
			},
			{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "used the latest user update",
				},
			},
		},
		blockIndex: 0,
		started:    make(chan struct{}),
		release:    make(chan struct{}),
	}
	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 3,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, nil, nil)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey: "chat-model-conflict-retry",
		Content:    "draft answer",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- component.ExecuteRun(context.Background(), run)
	}()

	select {
	case <-model.started:
	case <-time.After(2 * time.Second):
		t.Fatal("ExecuteRun did not reach model call")
	}

	appendDone := make(chan error, 1)
	go func() {
		appendDone <- sessions.AppendUserMessage(context.Background(), run.SessionID, IncomingMessage{
			Content: "actually include this latest info",
		})
	}()

	select {
	case err := <-appendDone:
		if err != nil {
			t.Fatalf("AppendUserMessage() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("AppendUserMessage() blocked during model phase")
	}

	close(model.release)

	if err := <-errCh; err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}

	session, err := sessions.Get(context.Background(), run.SessionID)
	if err != nil {
		t.Fatalf("sessions.Get() error = %v", err)
	}
	if session.Messages[len(session.Messages)-1].Content != "used the latest user update" {
		t.Fatalf("final assistant message = %#v", session.Messages[len(session.Messages)-1])
	}
	for _, msg := range session.Messages {
		if msg.Role == contextengine.RoleAssistant && msg.Content == "stale draft" {
			t.Fatalf("stale model response committed unexpectedly: %#v", msg)
		}
	}
}

func TestExecuteRunRetriesOnRevisionConflictDuringPreparePhase(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	runtimeCtx := &blockingRuntimeContextProvider{
		blockCalls: 1,
		started:    make(chan struct{}),
		release:    make(chan struct{}),
	}
	model := &stubModelClient{
		responses: []*ModelResponse{
			{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "used the latest user update",
				},
			},
		},
	}
	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 3,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, nil, runtimeCtx)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey: "chat-prepare-conflict-retry",
		Content:    "draft answer",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- component.ExecuteRun(context.Background(), run)
	}()

	select {
	case <-runtimeCtx.started:
	case <-time.After(2 * time.Second):
		t.Fatal("ExecuteRun did not reach prepare compute")
	}

	appendDone := make(chan error, 1)
	go func() {
		appendDone <- sessions.AppendUserMessage(context.Background(), run.SessionID, IncomingMessage{
			Content: "actually include this latest info",
		})
	}()

	select {
	case err := <-appendDone:
		if err != nil {
			t.Fatalf("AppendUserMessage() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("AppendUserMessage() blocked during prepare phase")
	}

	close(runtimeCtx.release)

	if err := <-errCh; err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}

	if model.index != 1 {
		t.Fatalf("model.Chat call count = %d, want 1", model.index)
	}
	session, err := sessions.Get(context.Background(), run.SessionID)
	if err != nil {
		t.Fatalf("sessions.Get() error = %v", err)
	}
	if session.Messages[len(session.Messages)-1].Content != "used the latest user update" {
		t.Fatalf("final assistant message = %#v", session.Messages[len(session.Messages)-1])
	}
}

func TestPrepareConflictDoesNotEmitDuplicateModelRouteEvents(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	bus := eventbus.NewInMemoryBus()
	runtimeCtx := &blockingRuntimeContextProvider{
		blockCalls: 1,
		started:    make(chan struct{}),
		release:    make(chan struct{}),
	}
	router := modelrouter.NewInMemoryRouter([]modelrouter.ModelProfile{
		{
			ID:              "test-model",
			Provider:        "openai",
			Priority:        1,
			ContextWindow:   128000,
			MaxOutputTokens: 4000,
			Enabled:         true,
			Supports: map[modelrouter.Capability]bool{
				modelrouter.CapabilityChat: true,
			},
		},
	}, time.Minute)
	model := &stubModelClient{
		responses: []*ModelResponse{
			{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "final after prepare retry",
				},
			},
		},
	}
	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 3,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, nil, runtimeCtx).
		WithRouter(router).
		WithEventBus(bus)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey: "chat-prepare-route-events",
		Content:    "draft answer",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- component.ExecuteRun(context.Background(), run)
	}()

	select {
	case <-runtimeCtx.started:
	case <-time.After(2 * time.Second):
		t.Fatal("ExecuteRun did not reach prepare compute")
	}

	if err := sessions.AppendUserMessage(context.Background(), run.SessionID, IncomingMessage{
		Content: "new context before route commit",
	}); err != nil {
		t.Fatalf("AppendUserMessage() error = %v", err)
	}
	close(runtimeCtx.release)

	if err := <-errCh; err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}

	var routedCount int
	var failoverCount int
	for _, event := range bus.Snapshot() {
		switch event.Type {
		case eventbus.EventModelRouted:
			routedCount++
		case eventbus.EventModelFailover:
			failoverCount++
		}
	}
	if routedCount != 1 {
		t.Fatalf("EventModelRouted count = %d, want 1", routedCount)
	}
	if failoverCount != 0 {
		t.Fatalf("EventModelFailover count = %d, want 0", failoverCount)
	}
}

func TestCancelRunDuringToolExecutionDropsUncommittedResults(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &stubModelClient{
		responses: []*ModelResponse{{
			ToolCalls: []ToolCall{{
				ID:   "call-1",
				Name: "fs.read",
			}},
		}},
	}
	tools := &blockingToolExecutor{
		results: []contextengine.ToolResult{{
			ToolName:   "fs.read",
			ToolCallID: "call-1",
			Content:    "should not land",
		}},
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 2,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, tools, nil)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey: "chat-tool-cancel",
		Content:    "read the file",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- component.ExecuteRun(context.Background(), run)
	}()

	<-tools.started
	if _, err := component.CancelRun(context.Background(), run.ID); err != nil {
		t.Fatalf("CancelRun() error = %v", err)
	}
	close(tools.release)

	if err := <-errCh; err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}

	got, err := runs.Get(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Status != RunCancelled {
		t.Fatalf("run.Status = %q, want %q", got.Status, RunCancelled)
	}
	session, err := sessions.Get(context.Background(), run.SessionID)
	if err != nil {
		t.Fatalf("Get(session) error = %v", err)
	}
	for _, msg := range session.Messages {
		if msg.Role == contextengine.RoleTool {
			t.Fatalf("unexpected tool result after cancel: %#v", msg)
		}
	}
}

func TestExecuteRunEnforcesEffectiveAgentProfile(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &stubModelClient{
		responses: []*ModelResponse{{
			Message: contextengine.Message{
				Role:    contextengine.RoleAssistant,
				Content: "profile respected",
			},
		}},
	}
	engine := contextengine.NewSlidingWindowEngine(contextengine.Config{
		BaseSystemPrompt:     "You are a test agent.",
		IncludeSkillCatalog:  true,
		DefaultContextWindow: 512,
		DefaultOutputTokens:  64,
	}, nil)
	tools := stubRuntimeToolExecutor{
		definitions: []ToolDefinition{
			{Name: "allowed.tool", Description: "allowed"},
			{Name: "blocked.tool", Description: "blocked"},
		},
	}
	component := NewComponent(AgentConfig{
		DefaultModel:  "fallback-model",
		MaxToolRounds: 2,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), engine, model, tools, nil)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey: "chat-profile",
		Content:    "do the thing",
		Metadata: map[string]any{
			MetadataKeyAgentProfileName:         "builder",
			MetadataKeyAgentProfileModel:        "profile-model",
			MetadataKeyAgentProfileSystemPrompt: "Profile prompt",
			MetadataKeyAgentProfileTools:        []string{"allowed.tool"},
			MetadataKeyAgentProfileSkills:       []string{"allowed_skill"},
			MetadataKeyAgentProfileMaxTokens:    32,
			MetadataKeyAgentProfileSource:       "router",
		},
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	session, unlock, err := sessions.LoadForExecution(context.Background(), run.SessionID)
	if err != nil {
		t.Fatalf("LoadForExecution() error = %v", err)
	}
	allowed := skill.BoundSkill{
		Package: &skill.SkillPackage{
			Prompt: skill.PromptSkill{Name: "allowed_skill", Description: "Allowed skill"},
		},
		Eligibility: skill.EligibilityResult{Eligible: true},
	}
	blocked := skill.BoundSkill{
		Package: &skill.SkillPackage{
			Prompt: skill.PromptSkill{Name: "blocked_skill", Description: "Blocked skill"},
		},
		Eligibility: skill.EligibilityResult{Eligible: true},
	}
	session.SkillSnapshot = skill.SessionSkillSnapshot{
		Fingerprint: "skills-test",
		Skills: map[string]skill.BoundSkill{
			"allowed_skill": allowed,
			"blocked_skill": blocked,
		},
		Ordered: []skill.BoundSkill{allowed, blocked},
		PromptCatalog: []skill.PromptCatalogEntry{
			{Name: "allowed_skill", Description: "Allowed skill"},
			{Name: "blocked_skill", Description: "Blocked skill"},
		},
	}
	session.UpdatedAt = time.Now().UTC()
	if err := sessions.Save(context.Background(), session); err != nil {
		unlock()
		t.Fatalf("Save() error = %v", err)
	}
	unlock()

	if err := component.ExecuteRun(context.Background(), run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}

	if run.EffectiveProfile == nil || run.EffectiveProfile.Name != "builder" {
		t.Fatalf("run.EffectiveProfile = %#v", run.EffectiveProfile)
	}
	if model.lastRequest.Model != "profile-model" {
		t.Fatalf("model.lastRequest.Model = %q, want profile-model", model.lastRequest.Model)
	}
	if !strings.Contains(model.lastRequest.SystemPrompt, "Profile prompt") {
		t.Fatalf("system prompt missing profile prompt: %s", model.lastRequest.SystemPrompt)
	}
	if !strings.Contains(model.lastRequest.SystemPrompt, "allowed_skill") {
		t.Fatalf("system prompt missing allowed skill: %s", model.lastRequest.SystemPrompt)
	}
	if strings.Contains(model.lastRequest.SystemPrompt, "blocked_skill") {
		t.Fatalf("system prompt leaked blocked skill: %s", model.lastRequest.SystemPrompt)
	}
	if model.lastRequest.Budget.ReservedOutput != 32 {
		t.Fatalf("reserved output = %d, want 32", model.lastRequest.Budget.ReservedOutput)
	}
	if len(model.lastRequest.Tools) != 1 || model.lastRequest.Tools[0].Name != "allowed.tool" {
		t.Fatalf("model tools = %#v", model.lastRequest.Tools)
	}
}

func TestParseEffectiveAgentProfileDecodesStructuredMetadata(t *testing.T) {
	t.Parallel()

	profile := parseEffectiveAgentProfile(map[string]any{
		MetadataKeyAgentProfileName:         "builder",
		MetadataKeyAgentProfileModel:        "profile-model",
		MetadataKeyAgentProfileSystemPrompt: "Profile prompt",
		MetadataKeyAgentProfileTools:        []any{"allowed.tool", "allowed.tool", 7},
		MetadataKeyAgentProfileSkills:       []any{"allowed_skill", "allowed_skill"},
		MetadataKeyAgentProfileMaxTokens:    json.Number("48"),
		MetadataKeyAgentProfileSource:       "router",
	})
	if profile == nil {
		t.Fatal("expected profile")
	}
	if profile.Name != "builder" || profile.Model != "profile-model" || profile.MaxTokens != 48 || profile.Source != "router" {
		t.Fatalf("profile scalar fields = %#v", profile)
	}
	if !reflect.DeepEqual(profile.AllowedTools, []string{"allowed.tool", "7"}) {
		t.Fatalf("profile.AllowedTools = %#v", profile.AllowedTools)
	}
	if !reflect.DeepEqual(profile.AllowedSkills, []string{"allowed_skill"}) {
		t.Fatalf("profile.AllowedSkills = %#v", profile.AllowedSkills)
	}
}

func TestExplicitModelOverridesProfileModel(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &stubModelClient{
		responses: []*ModelResponse{{
			Message: contextengine.Message{
				Role:    contextengine.RoleAssistant,
				Content: "done",
			},
		}},
	}
	component := NewComponent(AgentConfig{
		DefaultModel: "fallback-model",
		QueueMode:    QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, nil, nil)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey: "chat-explicit-model",
		Content:    "hi",
		Model:      "user-model",
		Metadata: map[string]any{
			MetadataKeyAgentProfileModel: "profile-model",
		},
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if err := component.ExecuteRun(context.Background(), run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}
	if model.lastRequest.Model != "user-model" {
		t.Fatalf("model.lastRequest.Model = %q, want user-model", model.lastRequest.Model)
	}
}

func TestExecuteRunRecoversFromRepeatedToolLoop(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 6,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{
		responses: []*ModelResponse{
			{ToolCalls: []ToolCall{{ID: "call-1", Name: "fs.read", Input: map[string]any{"path": "README.md"}}}},
			{ToolCalls: []ToolCall{{ID: "call-2", Name: "fs.read", Input: map[string]any{"path": "README.md"}}}},
			{ToolCalls: []ToolCall{{ID: "call-3", Name: "fs.read", Input: map[string]any{"path": "README.md"}}}},
			{ToolCalls: []ToolCall{{ID: "call-4", Name: "fs.read", Input: map[string]any{"path": "README.md"}}}},
			{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "I already gathered enough evidence and here is the concise answer.",
				},
			},
		},
	}, stubToolExecutor{
		results: []contextengine.ToolResult{{
			ToolName:    "fs.read",
			Content:     "readme contents",
			ArtifactURI: "artifact://fs.read/readme",
		}},
	}, nil)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-tool-loop-recover",
		ExternalEventID: "evt-tool-loop-recover",
		Content:         "read the readme and summarize it",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if err := component.ExecuteRun(context.Background(), run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}
	run = mustReloadRun(t, runs, run)
	if run.Status != RunCompleted {
		t.Fatalf("run.Status = %q", run.Status)
	}

	session, err := sessions.GetOrCreate(context.Background(), "chat-tool-loop-recover", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	foundDirective := false
	for _, msg := range session.Messages {
		if msg.Role != contextengine.RoleUser {
			continue
		}
		if flag, _ := msg.Metadata["auto_recovery"].(bool); flag && strings.Contains(msg.Content, "Do not repeat the same tool call") {
			foundDirective = true
			break
		}
	}
	if !foundDirective {
		t.Fatal("expected auto recovery directive after repeated tool loop")
	}
}

func TestExecuteRunRecoversFromToolNoProgress(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 6,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{
		responses: []*ModelResponse{
			{ToolCalls: []ToolCall{{ID: "call-1", Name: "fs.read", Input: map[string]any{"path": "a.txt"}}}},
			{ToolCalls: []ToolCall{{ID: "call-2", Name: "fs.read", Input: map[string]any{"path": "b.txt"}}}},
			{ToolCalls: []ToolCall{{ID: "call-3", Name: "fs.read", Input: map[string]any{"path": "c.txt"}}}},
			{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "Those reads produced the same blocker, so here is the best answer with the limitation noted.",
				},
			},
		},
	}, stubToolExecutor{
		results: []contextengine.ToolResult{{
			ToolName: "fs.read",
			Content:  "permission denied",
		}},
	}, nil)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-tool-no-progress",
		ExternalEventID: "evt-tool-no-progress",
		Content:         "read candidate files and summarize what is available",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if err := component.ExecuteRun(context.Background(), run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}
	run = mustReloadRun(t, runs, run)
	if run.Status != RunCompleted {
		t.Fatalf("run.Status = %q", run.Status)
	}

	session, err := sessions.GetOrCreate(context.Background(), "chat-tool-no-progress", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	foundDirective := false
	for _, msg := range session.Messages {
		if msg.Role != contextengine.RoleUser {
			continue
		}
		if flag, _ := msg.Metadata["auto_recovery"].(bool); flag && strings.Contains(msg.Content, "not producing new evidence") {
			foundDirective = true
			break
		}
	}
	if !foundDirective {
		t.Fatal("expected auto recovery directive after stalled tool progress")
	}
}

func TestExecuteRunAllowsMultipleToolRecoveryAttempts(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	component := NewComponent(AgentConfig{
		DefaultModel:            "test-model",
		MaxToolRounds:           5,
		MaxToolRecoveryAttempts: 2,
		QueueMode:               QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{
		responses: []*ModelResponse{
			{ToolCalls: []ToolCall{{ID: "call-1", Name: "fs.read"}}},
			{ToolCalls: []ToolCall{{ID: "call-2", Name: "fs.stat"}}},
			{Message: contextengine.Message{Role: contextengine.RoleAssistant, Content: "I could not access the file system, so this is a partial answer."}},
		},
	}, stubToolExecutor{
		err: errors.New("permission denied"),
	}, nil)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-tool-recover-multi",
		ExternalEventID: "evt-tool-recover-multi",
		Content:         "inspect the file system",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if err := component.ExecuteRun(context.Background(), run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}

	session, err := sessions.GetOrCreate(context.Background(), "chat-tool-recover-multi", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	var toolMessages []contextengine.Message
	for _, msg := range session.Messages {
		if msg.Role == contextengine.RoleTool {
			toolMessages = append(toolMessages, msg)
		}
	}
	if len(toolMessages) != 2 {
		t.Fatalf("tool messages = %d, want 2", len(toolMessages))
	}
	firstResult, ok := resultmodel.DecodeToolResultMetadata(toolMessages[0].Metadata)
	if !ok {
		t.Fatalf("first tool metadata = %#v", toolMessages[0].Metadata)
	}
	if got, _ := firstResult.Structured["recovery_attempt"].(float64); got != 1 {
		t.Fatalf("first recovery_attempt = %v", got)
	}
	if got, _ := firstResult.Structured["recovery_attempts_remaining"].(float64); got != 1 {
		t.Fatalf("first recovery_attempts_remaining = %v", got)
	}
	secondResult, ok := resultmodel.DecodeToolResultMetadata(toolMessages[1].Metadata)
	if !ok {
		t.Fatalf("second tool metadata = %#v", toolMessages[1].Metadata)
	}
	if got, _ := secondResult.Structured["recovery_attempt"].(float64); got != 2 {
		t.Fatalf("second recovery_attempt = %v", got)
	}
	if got, _ := secondResult.Structured["recovery_attempts_remaining"].(float64); got != 0 {
		t.Fatalf("second recovery_attempts_remaining = %v", got)
	}
}

func TestExecuteRunFailsAfterToolRecoveryBudgetExhausted(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	component := NewComponent(AgentConfig{
		DefaultModel:            "test-model",
		MaxToolRounds:           5,
		MaxToolRecoveryAttempts: 2,
		QueueMode:               QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{
		responses: []*ModelResponse{
			{ToolCalls: []ToolCall{{ID: "call-1", Name: "fs.read"}}},
			{ToolCalls: []ToolCall{{ID: "call-2", Name: "fs.stat"}}},
			{ToolCalls: []ToolCall{{ID: "call-3", Name: "exec.run"}}},
		},
	}, stubToolExecutor{
		err: errors.New("permission denied"),
	}, nil)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-tool-recover-budget",
		ExternalEventID: "evt-tool-recover-budget",
		Content:         "inspect the file system",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	err = component.ExecuteRun(context.Background(), run)
	if err == nil {
		t.Fatal("expected ExecuteRun() to fail after recovery budget exhaustion")
	}
	if !strings.Contains(err.Error(), "tool execution failed after 2 recovery attempt(s)") {
		t.Fatalf("ExecuteRun() error = %v", err)
	}
	run = mustReloadRun(t, runs, run)
	if run.Status != RunFailed {
		t.Fatalf("run.Status = %q", run.Status)
	}
}

func TestExecuteRunAppliesSteeringDirectiveBeforeNextModelRound(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	bus := eventbus.NewInMemoryBus()
	directives := NewInMemorySessionDirectiveStore()
	session, err := sessions.GetOrCreate(ctx, "chat-steer", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}

	model := &steeringDirectiveModelClient{
		sessionID:  session.ID,
		directives: directives,
		responses: []*ModelResponse{
			{
				ToolCalls: []ToolCall{{
					ID:   "call-1",
					Name: "fs.read",
				}},
			},
			{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "done after steering",
				},
			},
		},
	}
	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 3,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, stubToolExecutor{
		results: []contextengine.ToolResult{{
			ToolName:   "fs.read",
			ToolCallID: "call-1",
			Content:    "file contents",
		}},
	}, nil).WithSessionDirectives(directives).WithEventBus(bus)

	run, err := component.Submit(ctx, IncomingMessage{
		SessionKey:      "chat-steer",
		ExternalEventID: "evt-steer",
		Content:         "read file",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if err := component.ExecuteRun(ctx, run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}
	if !model.sawSteering {
		t.Fatal("expected steering directive to appear in the next model request")
	}

	updated, err := sessions.Get(ctx, session.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	found := false
	for _, msg := range updated.Messages {
		if msg.Role != contextengine.RoleUser || msg.Content != "只保留标题列" {
			continue
		}
		if flag, _ := msg.Metadata["synthetic_msg"].(bool); !flag {
			t.Fatalf("steering message metadata = %#v", msg.Metadata)
		}
		found = true
		break
	}
	if !found {
		t.Fatalf("steering message not found in session: %#v", updated.Messages)
	}

	steered := false
	for _, event := range bus.Snapshot() {
		if event.Type == eventbus.EventRunSteered && event.RunID == run.ID {
			steered = true
			break
		}
	}
	if !steered {
		t.Fatal("expected run.steered event")
	}
}

func TestExecuteRunBuildsPlanBeforeExecutionWhenPlannerEnabled(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	bus := eventbus.NewInMemoryBus()
	model := &stubModelClient{
		responses: []*ModelResponse{
			{
				Message: contextengine.Message{
					Role: contextengine.RoleAssistant,
					Content: `{
						"goal":"search and summarize",
						"strategy":"serial",
						"tasks":[
							{"id":"research","kind":"research","goal":"search the topic","required_capabilities":["search.web"]},
							{"id":"deliver","kind":"deliver","goal":"write the final answer","depends_on":["research"]}
						],
						"final_task":"deliver"
					}`,
				},
			},
			{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "research summary",
				},
			},
			{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "final answer",
				},
			},
		},
	}

	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 3,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, nil, nil).
		WithPlanner(NewModelPlanner(model, 0)).
		WithEventBus(bus)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-plan",
		ExternalEventID: "evt-plan",
		Content:         "search and summarize",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if err := component.ExecuteRun(context.Background(), run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}
	run = mustReloadRun(t, runs, run)
	if run.Plan == nil {
		t.Fatal("expected run plan to be populated")
	}
	if len(run.Plan.Tasks) != 2 {
		t.Fatalf("len(run.Plan.Tasks) = %d, want 2", len(run.Plan.Tasks))
	}
	if run.Plan.FinalTask != "deliver" {
		t.Fatalf("run.Plan.FinalTask = %q", run.Plan.FinalTask)
	}

	planned := false
	for _, event := range bus.Snapshot() {
		if event.Type == eventbus.EventRunPlanned && event.RunID == run.ID {
			planned = true
			break
		}
	}
	if !planned {
		t.Fatal("expected run.planned event")
	}
}

func TestExecuteRunEmitsArtifactRefsOnToolExecuted(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	bus := eventbus.NewInMemoryBus()
	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 3,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{
		responses: []*ModelResponse{
			{
				ToolCalls: []ToolCall{{
					ID:   "call-1",
					Name: "fs.read",
				}},
			},
			{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "done",
				},
			},
		},
	}, stubToolExecutor{
		results: []contextengine.ToolResult{{
			ToolName:    "fs.read",
			ToolCallID:  "call-1",
			Content:     "preview",
			ArtifactURI: "artifact://local/blob-1",
		}},
	}, nil).WithEventBus(bus)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-artifacts",
		ExternalEventID: "evt-artifacts",
		Content:         "read file",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if err := component.ExecuteRun(context.Background(), run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}

	var toolEvent eventbus.Event
	found := false
	for _, event := range bus.Snapshot() {
		if event.Type == eventbus.EventToolExecuted {
			toolEvent = event
			found = true
		}
	}
	if !found {
		t.Fatal("expected tool.executed event")
	}
	if got := toolEvent.Attrs["artifact_count"]; got != 1 {
		t.Fatalf("artifact_count = %#v", got)
	}
	uris, ok := toolEvent.Attrs["artifact_uris"].([]string)
	if !ok || len(uris) != 1 || uris[0] != "artifact://local/blob-1" {
		t.Fatalf("artifact_uris = %#v", toolEvent.Attrs["artifact_uris"])
	}
}

func TestExecuteRunFailsWithoutToolExecutor(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 2,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{
		responses: []*ModelResponse{{
			ToolCalls: []ToolCall{{ID: "call-1", Name: "fs.read"}},
		}},
	}, nil, nil)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-4",
		ExternalEventID: "evt-4",
		Content:         "read file",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	err = component.ExecuteRun(context.Background(), run)
	if !errors.Is(err, ErrToolExecutorNil) {
		t.Fatalf("ExecuteRun() error = %v", err)
	}
	run = mustReloadRun(t, runs, run)
	if run.Status != RunFailed {
		t.Fatalf("run.Status = %q", run.Status)
	}
}

func TestExecuteRunUsesRouterSelection(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &stubModelClient{
		responses: []*ModelResponse{{
			Message: contextengine.Message{
				Role:    contextengine.RoleAssistant,
				Content: "routed",
			},
		}},
	}
	router := modelrouter.NewInMemoryRouter([]modelrouter.ModelProfile{
		{
			ID:              "primary",
			Provider:        "provider-a",
			Priority:        100,
			ContextWindow:   128000,
			MaxOutputTokens: 8000,
			Enabled:         true,
			Supports: map[modelrouter.Capability]bool{
				modelrouter.CapabilityTools: true,
			},
			CooldownUntil: time.Now().UTC().Add(time.Minute),
		},
		{
			ID:              "fallback",
			Provider:        "provider-b",
			Priority:        90,
			ContextWindow:   128000,
			MaxOutputTokens: 8000,
			Enabled:         true,
		},
	}, time.Minute)

	component := NewComponent(AgentConfig{
		DefaultModel:  "primary",
		MaxToolRounds: 2,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, nil, nil).WithRouter(router)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-router",
		ExternalEventID: "evt-router",
		Content:         "route me",
		Model:           "primary",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if err := component.ExecuteRun(context.Background(), run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}
	if got := model.lastRequest.Model; got != "fallback" {
		t.Fatalf("last routed model = %q", got)
	}
}

func TestExecuteRunFallsBackToRequestedModelWhenRouterMisses(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &stubModelClient{
		responses: []*ModelResponse{{
			Message: contextengine.Message{
				Role:    contextengine.RoleAssistant,
				Content: "explicit model still works",
			},
		}},
	}
	router := modelrouter.NewInMemoryRouter(nil, time.Minute)

	component := NewComponent(AgentConfig{
		DefaultModel:  "fallback-default",
		MaxToolRounds: 2,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, nil, nil).WithRouter(router)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-router-miss",
		ExternalEventID: "evt-router-miss",
		Content:         "use explicit model",
		Model:           "custom-explicit-model",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if err := component.ExecuteRun(context.Background(), run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}
	if got := model.lastRequest.Model; got != "custom-explicit-model" {
		t.Fatalf("last model = %q, want custom-explicit-model", got)
	}
}

func TestExecuteRunPrefersThinkingModelForRSSStyleRequests(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &stubModelClient{
		responses: []*ModelResponse{{
			Message: contextengine.Message{
				Role:    contextengine.RoleAssistant,
				Content: "rss answer",
			},
		}},
	}
	router := modelrouter.NewInMemoryRouter([]modelrouter.ModelProfile{
		{
			ID:              "fast-no-thinking",
			Provider:        "provider-a",
			Priority:        100,
			ContextWindow:   128000,
			MaxOutputTokens: 8000,
			Enabled:         true,
			Supports: map[modelrouter.Capability]bool{
				modelrouter.CapabilityChat:  true,
				modelrouter.CapabilityTools: true,
			},
		},
		{
			ID:              "thinking-tools",
			Provider:        "provider-b",
			Priority:        90,
			ContextWindow:   128000,
			MaxOutputTokens: 8000,
			Enabled:         true,
			Supports: map[modelrouter.Capability]bool{
				modelrouter.CapabilityChat:     true,
				modelrouter.CapabilityTools:    true,
				modelrouter.CapabilityThinking: true,
			},
		},
	}, time.Minute)

	component := NewComponent(AgentConfig{
		DefaultModel:  "fast-no-thinking",
		MaxToolRounds: 2,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, stubRuntimeToolExecutor{
		definitions: []ToolDefinition{
			{Name: "skill.ensure", SideEffectClass: "read"},
			{Name: "fs.read", SideEffectClass: "read"},
			{Name: "exec.run", SideEffectClass: "local_write"},
		},
	}, nil).WithRouter(router)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey: "chat-router-thinking",
		Content:    "读取这个 RSS feed 并总结最近三条：https://example.com/feed.xml",
		Model:      "fast-no-thinking",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if err := component.ExecuteRun(context.Background(), run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}
	if got := model.lastRequest.Model; got != "thinking-tools" {
		t.Fatalf("last routed model = %q, want thinking-tools", got)
	}
	if got := model.lastRequest.ThinkingMode; got != ThinkingExtended {
		t.Fatalf("lastRequest.ThinkingMode = %q, want %q", got, ThinkingExtended)
	}
}

func TestExecuteRunIncludesRuntimeToolDefinitions(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &stubModelClient{
		responses: []*ModelResponse{{
			Message: contextengine.Message{
				Role:    contextengine.RoleAssistant,
				Content: "runtime tools ready",
			},
		}},
	}
	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 2,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, stubRuntimeToolExecutor{
		definitions: []ToolDefinition{{
			Name:        "fs.read",
			Description: "builtin read",
			InputSchema: map[string]any{"type": "object"},
		}},
	}, nil)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-runtime-tools",
		ExternalEventID: "evt-runtime-tools",
		Content:         "list tools",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if err := component.ExecuteRun(context.Background(), run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}
	if len(model.lastRequest.Tools) != 1 {
		t.Fatalf("len(lastRequest.Tools) = %d", len(model.lastRequest.Tools))
	}
	if model.lastRequest.Tools[0].Name != "fs.read" {
		t.Fatalf("lastRequest.Tools[0] = %#v", model.lastRequest.Tools[0])
	}
}

func TestExecuteRunWaitsApprovalWhenPolicyRequires(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	approvals := approval.NewInMemoryStore()
	bus := eventbus.NewInMemoryBus()
	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 2,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{
		responses: []*ModelResponse{{
			ToolCalls: []ToolCall{{
				ID:   "call-1",
				Name: "fs.write",
			}},
		}},
	}, stubToolExecutor{}, nil).WithPolicy(policy.NewDefaultEngine(policy.Config{
		RequireApprovalForWrite:  true,
		RequireApprovalCommunity: true,
	})).WithApprovals(approvals).WithEventBus(bus)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-policy",
		ExternalEventID: "evt-policy",
		Content:         "write file",
		AutomationID:    "auto-policy",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	session, unlock, err := sessions.LoadForExecution(context.Background(), run.SessionID)
	if err != nil {
		t.Fatalf("LoadForExecution() error = %v", err)
	}
	bound := skill.BoundSkill{
		Package: &skill.SkillPackage{
			Prompt: skill.PromptSkill{Name: "writer"},
			Trust:  skill.TrustCommunity,
			ToolManifests: []skill.ToolManifest{{
				Name:            "fs.write",
				SideEffectClass: "local_write",
			}},
		},
		Eligibility: skill.EligibilityResult{Eligible: true},
	}
	session.SkillSnapshot = skill.SessionSkillSnapshot{
		Fingerprint: "skills-1",
		Skills: map[string]skill.BoundSkill{
			"writer": bound,
		},
		Ordered: []skill.BoundSkill{bound},
	}
	if err := sessions.Save(context.Background(), session); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	unlock()

	if err := component.ExecuteRun(context.Background(), run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}
	run = mustReloadRun(t, runs, run)
	if run.Status != RunWaitingApproval {
		t.Fatalf("run.Status = %q", run.Status)
	}
	if run.ApprovalID == "" {
		t.Fatal("expected approval ticket id on run")
	}
	ticket, err := approvals.GetByRun(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("GetByRun() error = %v", err)
	}
	if ticket.Status != approval.StatusPending {
		t.Fatalf("ticket.Status = %q", ticket.Status)
	}
	if run.Governance == nil {
		t.Fatal("expected governance evaluation on run")
	}
	if run.Governance.Decision.PolicySource == "" {
		t.Fatal("expected policy source on governance decision")
	}
	if run.Governance.Decision.Summary == "" {
		t.Fatal("expected policy summary on governance decision")
	}
	if scopeValue, ok := ticket.Metadata["scope"].(domainscope.Ref); !ok {
		t.Fatalf("ticket.Metadata[scope] = %#v", ticket.Metadata["scope"])
	} else if scopeValue.AutomationID != "auto-policy" {
		t.Fatalf("scope = %#v", scopeValue)
	}
	if ticket.Metadata["policy_source"] == "" || ticket.Metadata["policy_summary"] == "" {
		t.Fatalf("ticket.Metadata = %#v", ticket.Metadata)
	}
	if run.Error == "" {
		t.Fatal("expected approval reason to be recorded")
	}
	events := bus.Snapshot()
	if len(events) == 0 || events[len(events)-1].Type != eventbus.EventRunWaitingApproval {
		t.Fatalf("events = %#v", events)
	}
}

func TestExecuteRunWaitsApprovalBeforeUnavailableToolFiltering(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	approvals := approval.NewInMemoryStore()
	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 2,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{
		responses: []*ModelResponse{{
			ToolCalls: []ToolCall{{
				ID:   "call-1",
				Name: "fs.write",
			}},
		}},
	}, stubRuntimeToolExecutor{}, nil).WithPolicy(alwaysRequireApprovalPolicyEngine{}).WithApprovals(approvals)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-approval-before-visibility",
		ExternalEventID: "evt-approval-before-visibility",
		Content:         "write file",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if err := component.ExecuteRun(context.Background(), run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}
	run = mustReloadRun(t, runs, run)
	if run.Status != RunWaitingApproval {
		t.Fatalf("run.Status = %q, want %q", run.Status, RunWaitingApproval)
	}
	if run.ApprovalID == "" {
		t.Fatal("expected approval ticket id on run")
	}
	ticket, err := approvals.GetByRun(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("GetByRun() error = %v", err)
	}
	if ticket.Status != approval.StatusPending {
		t.Fatalf("ticket.Status = %q, want %q", ticket.Status, approval.StatusPending)
	}
}

func TestResolveApprovalResumesRun(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	approvals := approval.NewInMemoryStore()
	bus := eventbus.NewInMemoryBus()
	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 3,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{
		responses: []*ModelResponse{
			{
				ToolCalls: []ToolCall{{
					ID:   "call-1",
					Name: "fs.write",
				}},
			},
			{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "after approval",
				},
			},
		},
	}, stubToolExecutor{
		results: []contextengine.ToolResult{{
			ToolName:   "fs.write",
			ToolCallID: "call-1",
			Content:    "written",
		}},
	}, nil).WithPolicy(policy.NewDefaultEngine(policy.Config{
		RequireApprovalForWrite:  true,
		RequireApprovalCommunity: true,
	})).WithApprovals(approvals).WithEventBus(bus)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-resume",
		ExternalEventID: "evt-resume",
		Content:         "write file",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	session, unlock, err := sessions.LoadForExecution(context.Background(), run.SessionID)
	if err != nil {
		t.Fatalf("LoadForExecution() error = %v", err)
	}
	bound := skill.BoundSkill{
		Package: &skill.SkillPackage{
			Prompt: skill.PromptSkill{Name: "writer"},
			Trust:  skill.TrustCommunity,
			ToolManifests: []skill.ToolManifest{{
				Name:            "fs.write",
				SideEffectClass: "local_write",
			}},
		},
		Eligibility: skill.EligibilityResult{Eligible: true},
	}
	session.SkillSnapshot = skill.SessionSkillSnapshot{
		Fingerprint: "skills-1",
		Skills: map[string]skill.BoundSkill{
			"writer": bound,
		},
		Ordered: []skill.BoundSkill{bound},
	}
	if err := sessions.Save(context.Background(), session); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	unlock()

	if err := component.ExecuteRun(context.Background(), run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}
	ticket, err := approvals.GetByRun(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("GetByRun() error = %v", err)
	}

	if _, err := component.ResolveApproval(context.Background(), ticket.ID, approval.Resolution{
		Status:     approval.StatusApproved,
		ResolvedBy: "tester",
	}); err != nil {
		t.Fatalf("ResolveApproval() error = %v", err)
	}
	run, err = runs.Get(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("RunStore.Get() error = %v", err)
	}
	if run.Status != RunQueued {
		t.Fatalf("run.Status = %q", run.Status)
	}
	if err := component.ResumeRun(context.Background(), run.ID); err != nil {
		t.Fatalf("ResumeRun() error = %v", err)
	}
	run, err = runs.Get(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("RunStore.Get() error = %v", err)
	}
	if run.Status != RunCompleted {
		t.Fatalf("run.Status = %q", run.Status)
	}
	if run.ApprovalID != "" {
		t.Fatalf("run.ApprovalID = %q", run.ApprovalID)
	}
	session, err = sessions.GetOrCreate(context.Background(), "chat-resume", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	// Messages: user + assistant(tool_calls) + tool_result + assistant("done")
	if len(session.Messages) != 4 {
		t.Fatalf("len(Messages) = %d", len(session.Messages))
	}
	events := bus.Snapshot()
	if len(events) == 0 || events[len(events)-1].Type != eventbus.EventRunCompleted {
		t.Fatalf("events = %#v", events)
	}
}

func TestResumeRunSkipsPendingToolsOnRevisionConflict(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	approvals := approval.NewInMemoryStore()
	model := &stubModelClient{
		responses: []*ModelResponse{
			{
				ToolCalls: []ToolCall{{
					ID:   "call-1",
					Name: "fs.write",
				}},
			},
			{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "used the latest clarification after approval",
				},
			},
		},
	}
	tools := &countingRuntimeToolExecutor{
		countingToolExecutor: countingToolExecutor{
			results: []contextengine.ToolResult{{
				ToolName:   "fs.write",
				ToolCallID: "call-1",
				Content:    "stale tool result",
			}},
		},
		boundTools: map[string]skill.BoundTool{
			"fs.write": {
				Package: &skill.SkillPackage{
					Prompt: skill.PromptSkill{Name: "writer"},
					Trust:  skill.TrustBundled,
				},
				Manifest: skill.ToolManifest{
					Name:            "fs.write",
					SideEffectClass: "local_write",
				},
				Eligibility: skill.EligibilityResult{Eligible: true},
			},
		},
	}
	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 4,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, tools, nil).WithPolicy(policy.NewDefaultEngine(policy.Config{
		RequireApprovalForWrite: true,
	})).WithApprovals(approvals)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-resume-conflict",
		ExternalEventID: "evt-resume-conflict",
		Content:         "write file",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if err := component.ExecuteRun(context.Background(), run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}
	run = mustReloadRun(t, runs, run)

	ticket, err := approvals.GetByRun(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("GetByRun() error = %v", err)
	}
	if err := sessions.AppendUserMessage(context.Background(), run.SessionID, IncomingMessage{
		Content: "actually apply this new clarification first",
	}); err != nil {
		t.Fatalf("AppendUserMessage() error = %v", err)
	}
	if _, err := component.ResolveApproval(context.Background(), ticket.ID, approval.Resolution{
		Status:     approval.StatusApproved,
		ResolvedBy: "tester",
	}); err != nil {
		t.Fatalf("ResolveApproval() error = %v", err)
	}
	if err := component.ResumeRun(context.Background(), run.ID); err != nil {
		t.Fatalf("ResumeRun() error = %v", err)
	}

	gotRun, err := runs.Get(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("RunStore.Get() error = %v", err)
	}
	if gotRun.Status != RunCompleted {
		t.Fatalf("run.Status = %q, want completed", gotRun.Status)
	}
	if got := tools.calls.Load(); got != 0 {
		t.Fatalf("tool execute count = %d, want 0", got)
	}
	if len(gotRun.PendingTools) != 0 {
		t.Fatalf("run.PendingTools = %#v, want empty", gotRun.PendingTools)
	}

	session, err := sessions.Get(context.Background(), run.SessionID)
	if err != nil {
		t.Fatalf("sessions.Get() error = %v", err)
	}
	if session.Messages[len(session.Messages)-1].Content != "used the latest clarification after approval" {
		t.Fatalf("final assistant message = %#v", session.Messages[len(session.Messages)-1])
	}
	for _, msg := range session.Messages {
		if msg.Role == contextengine.RoleTool {
			t.Fatalf("unexpected tool result committed after approval resume conflict: %#v", msg)
		}
	}
}

func TestResumeRunReevaluatesPendingToolsPolicyBeforeExecuting(t *testing.T) {
	t.Parallel()

	// Verify that after approval, the approved ticket satisfies the
	// require_approval policy on re-evaluation so the tools execute.
	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	approvals := approval.NewInMemoryStore()
	model := &stubModelClient{
		responses: []*ModelResponse{
			{
				ToolCalls: []ToolCall{{
					ID:   "call-1",
					Name: "fs.write",
				}},
			},
			{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "done writing",
				},
			},
		},
	}
	tools := &countingRuntimeToolExecutor{
		countingToolExecutor: countingToolExecutor{
			results: []contextengine.ToolResult{{
				ToolName:   "fs.write",
				ToolCallID: "call-1",
				Content:    "written",
			}},
		},
		boundTools: map[string]skill.BoundTool{
			"fs.write": {
				Package: &skill.SkillPackage{
					Prompt: skill.PromptSkill{Name: "writer"},
					Trust:  skill.TrustBundled,
				},
				Manifest: skill.ToolManifest{
					Name:            "fs.write",
					SideEffectClass: "local_write",
				},
				Eligibility: skill.EligibilityResult{Eligible: true},
			},
		},
	}
	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 4,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, tools, nil).
		WithPolicy(alwaysRequireApprovalPolicyEngine{}).
		WithApprovals(approvals)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-resume-reeval",
		ExternalEventID: "evt-resume-reeval",
		Content:         "write file",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if err := component.ExecuteRun(context.Background(), run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}
	run = mustReloadRun(t, runs, run)
	if run.Status != RunWaitingApproval {
		t.Fatalf("run.Status = %q, want %q", run.Status, RunWaitingApproval)
	}

	ticket, err := approvals.GetByRun(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("GetByRun() error = %v", err)
	}
	if _, err := component.ResolveApproval(context.Background(), ticket.ID, approval.Resolution{
		Status:     approval.StatusApproved,
		ResolvedBy: "tester",
	}); err != nil {
		t.Fatalf("ResolveApproval() error = %v", err)
	}
	if err := component.ResumeRun(context.Background(), run.ID); err != nil {
		t.Fatalf("ResumeRun() error = %v", err)
	}

	gotRun, err := runs.Get(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("RunStore.Get() error = %v", err)
	}
	// Approved ticket satisfies the re-evaluation — tools should execute.
	if gotRun.Status != RunCompleted {
		t.Fatalf("run.Status = %q, want %q", gotRun.Status, RunCompleted)
	}
	if got := tools.calls.Load(); got != 1 {
		t.Fatalf("tool execute count = %d, want 1", got)
	}
}

func TestResumeRunPersistsPreparedSkillSnapshotForApprovalReevaluation(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	approvals := approval.NewInMemoryStore()
	model := &stubModelClient{
		responses: []*ModelResponse{
			{
				ToolCalls: []ToolCall{{
					ID:   "call-1",
					Name: "fs.write",
				}},
			},
			{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "done writing",
				},
			},
		},
	}
	tools := &countingToolExecutor{
		results: []contextengine.ToolResult{{
			ToolName:   "fs.write",
			ToolCallID: "call-1",
			Content:    "written",
		}},
	}
	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 4,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), contextengine.NewSlidingWindowEngine(contextengine.Config{
		BaseSystemPrompt:     "You are a test agent.",
		IncludeSkillCatalog:  false,
		DefaultContextWindow: 512,
		DefaultOutputTokens:  64,
	}, newSkillServiceForAgentTest(t, "fs.write", "local_write")), model, tools, nil).
		WithPolicy(policy.NewDefaultEngine(policy.Config{
			RequireApprovalForWrite: true,
		})).
		WithApprovals(approvals)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-resume-skill-snapshot",
		ExternalEventID: "evt-resume-skill-snapshot",
		Content:         "write file",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if err := component.ExecuteRun(context.Background(), run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}

	run = mustReloadRun(t, runs, run)
	if run.Status != RunWaitingApproval {
		t.Fatalf("run.Status = %q, want %q", run.Status, RunWaitingApproval)
	}
	session, err := sessions.Get(context.Background(), run.SessionID)
	if err != nil {
		t.Fatalf("SessionStore.Get() error = %v", err)
	}
	if session.SkillSnapshot.Fingerprint == "" {
		t.Fatal("session.SkillSnapshot.Fingerprint = empty, want persisted prepared snapshot")
	}
	if _, ok := session.SkillSnapshot.ResolveTool("fs.write"); !ok {
		t.Fatalf("session.SkillSnapshot missing fs.write: %#v", session.SkillSnapshot)
	}

	ticket, err := approvals.GetByRun(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("GetByRun() error = %v", err)
	}
	if _, err := component.ResolveApproval(context.Background(), ticket.ID, approval.Resolution{
		Status:     approval.StatusApproved,
		ResolvedBy: "tester",
	}); err != nil {
		t.Fatalf("ResolveApproval() error = %v", err)
	}
	if err := component.ResumeRun(context.Background(), run.ID); err != nil {
		t.Fatalf("ResumeRun() error = %v", err)
	}

	gotRun, err := runs.Get(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("RunStore.Get() error = %v", err)
	}
	if gotRun.Status != RunCompleted {
		t.Fatalf("run.Status = %q, want %q", gotRun.Status, RunCompleted)
	}
	if got := tools.calls.Load(); got != 1 {
		t.Fatalf("tool execute count = %d, want 1", got)
	}
}

func TestCancelRunCancelsPendingApprovalAndPreventsResolve(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	approvals := approval.NewInMemoryStore()
	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 3,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{
		responses: []*ModelResponse{{
			ToolCalls: []ToolCall{{
				ID:   "call-1",
				Name: "fs.write",
			}},
		}},
	}, stubRuntimeToolExecutor{
		results: []contextengine.ToolResult{{
			ToolName:   "fs.write",
			ToolCallID: "call-1",
			Content:    "written",
		}},
		boundTools: map[string]skill.BoundTool{
			"fs.write": {
				Package: &skill.SkillPackage{
					Prompt: skill.PromptSkill{Name: "writer"},
					Trust:  skill.TrustBundled,
				},
				Manifest: skill.ToolManifest{
					Name:            "fs.write",
					SideEffectClass: "local_write",
				},
				Eligibility: skill.EligibilityResult{Eligible: true},
			},
		},
	}, nil).WithPolicy(policy.NewDefaultEngine(policy.Config{
		RequireApprovalForWrite: true,
	})).WithApprovals(approvals)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-cancel-approval",
		ExternalEventID: "evt-cancel-approval",
		Content:         "write file",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if err := component.ExecuteRun(context.Background(), run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}
	run = mustReloadRun(t, runs, run)
	if run.Status != RunWaitingApproval {
		t.Fatalf("run.Status = %q", run.Status)
	}

	ticket, err := approvals.GetByRun(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("GetByRun() error = %v", err)
	}
	if _, err := component.CancelRun(context.Background(), run.ID); err != nil {
		t.Fatalf("CancelRun() error = %v", err)
	}

	run, err = runs.Get(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("RunStore.Get() error = %v", err)
	}
	if run.Status != RunCancelled {
		t.Fatalf("run.Status = %q", run.Status)
	}

	ticket, err = approvals.Get(context.Background(), ticket.ID)
	if err != nil {
		t.Fatalf("approvals.Get() error = %v", err)
	}
	if ticket.Status != approval.StatusCancelled {
		t.Fatalf("ticket.Status = %q", ticket.Status)
	}

	resolved, err := component.ResolveApproval(context.Background(), ticket.ID, approval.Resolution{
		Status:     approval.StatusApproved,
		ResolvedBy: "tester",
	})
	if !errors.Is(err, ErrRunCancelled) {
		t.Fatalf("ResolveApproval() error = %v, want ErrRunCancelled", err)
	}
	if resolved == nil || resolved.Status != approval.StatusCancelled {
		t.Fatalf("resolved ticket = %#v", resolved)
	}

	run, err = runs.Get(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("RunStore.Get() error = %v", err)
	}
	if run.Status != RunCancelled {
		t.Fatalf("run.Status after resolve = %q", run.Status)
	}
}

func TestCancelRunDoesNotAllowLateCompletion(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &blockingModelClient{
		response: &ModelResponse{
			Message: contextengine.Message{
				Role:    contextengine.RoleAssistant,
				Content: "late answer",
			},
		},
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 3,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, nil, nil)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-cancel-running",
		ExternalEventID: "evt-cancel-running",
		Content:         "answer later",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- component.ExecuteRun(context.Background(), run)
	}()

	select {
	case <-model.started:
	case <-time.After(2 * time.Second):
		t.Fatal("model.Chat was not called")
	}

	if _, err := component.CancelRun(context.Background(), run.ID); err != nil {
		t.Fatalf("CancelRun() error = %v", err)
	}
	close(model.release)

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("ExecuteRun() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ExecuteRun() did not return")
	}

	got, err := runs.Get(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("RunStore.Get() error = %v", err)
	}
	if got.Status != RunCancelled {
		t.Fatalf("run.Status = %q", got.Status)
	}

	session, err := sessions.GetOrCreate(context.Background(), "chat-cancel-running", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	if len(session.Messages) != 1 {
		t.Fatalf("len(Messages) = %d", len(session.Messages))
	}
}

func TestExecuteRunDoesNotStartAfterCancellationWhileWaitingForSessionLock(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &blockingModelClient{
		response: &ModelResponse{
			Message: contextengine.Message{
				Role:    contextengine.RoleAssistant,
				Content: "should never run",
			},
		},
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 3,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, nil, nil)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-lock-cancel",
		ExternalEventID: "evt-lock-cancel",
		Content:         "blocked start",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	session, err := sessions.GetOrCreate(context.Background(), "chat-lock-cancel", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	_, unlock, err := sessions.LoadForExecution(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("LoadForExecution() error = %v", err)
	}

	runID := run.ID // capture before goroutine to avoid race
	errCh := make(chan error, 1)
	go func() {
		errCh <- component.ExecuteRun(context.Background(), run)
	}()

	time.Sleep(50 * time.Millisecond)
	if _, err := component.CancelRun(context.Background(), runID); err != nil {
		unlock()
		t.Fatalf("CancelRun() error = %v", err)
	}
	unlock()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("ExecuteRun() error = %v", err)
		}
	case <-time.After(300 * time.Millisecond):
		close(model.release)
		t.Fatal("ExecuteRun() did not stop after cancellation")
	}

	select {
	case <-model.started:
		t.Fatal("model.Chat should not have been called")
	default:
	}

	got, err := runs.Get(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("RunStore.Get() error = %v", err)
	}
	if got.Status != RunCancelled {
		t.Fatalf("run.Status = %q", got.Status)
	}
}

func TestExecuteRunCancelsTimedOutRunAndPublishesTimeoutEvent(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &scriptedBlockingModelClient{
		responses: []*ModelResponse{{
			Message: contextengine.Message{
				Role:    contextengine.RoleAssistant,
				Content: "too late",
			},
		}},
		blockIndex: 0,
		started:    make(chan struct{}),
	}
	bus := eventbus.NewInMemoryBus()
	component := NewComponent(AgentConfig{
		DefaultModel:   "test-model",
		MaxRunDuration: 50 * time.Millisecond,
		MaxToolRounds:  2,
		QueueMode:      QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, nil, nil).WithEventBus(bus)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-timeout",
		ExternalEventID: "evt-timeout",
		Content:         "take your time",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	if err := component.ExecuteRun(context.Background(), run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}

	got, err := runs.Get(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("RunStore.Get() error = %v", err)
	}
	if got.Status != RunCancelled {
		t.Fatalf("run.Status = %q, want %q", got.Status, RunCancelled)
	}
	if got.Error != "run timed out after 50ms" {
		t.Fatalf("run.Error = %q", got.Error)
	}

	foundTimeout := false
	for _, event := range bus.Snapshot() {
		if event.Type != eventbus.EventRunTimeout {
			continue
		}
		foundTimeout = true
		if event.RunID != run.ID {
			t.Fatalf("timeout event run_id = %q, want %q", event.RunID, run.ID)
		}
		if event.Attrs["reason"] != "run_timeout" {
			t.Fatalf("timeout event reason = %#v", event.Attrs["reason"])
		}
		if event.Attrs["max_run_duration"] != "50ms" {
			t.Fatalf("timeout event max_run_duration = %#v", event.Attrs["max_run_duration"])
		}
	}
	if !foundTimeout {
		t.Fatal("expected run timeout event")
	}
}

func TestCancelRunInterruptsResumedExecution(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	approvals := approval.NewInMemoryStore()
	model := &scriptedBlockingModelClient{
		responses: []*ModelResponse{
			{
				ToolCalls: []ToolCall{{
					ID:   "call-1",
					Name: "fs.write",
				}},
			},
			{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "after approval",
				},
			},
		},
		blockIndex: 1,
		started:    make(chan struct{}),
	}
	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 3,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, stubRuntimeToolExecutor{
		results: []contextengine.ToolResult{{
			ToolName:   "fs.write",
			ToolCallID: "call-1",
			Content:    "written",
		}},
		boundTools: map[string]skill.BoundTool{
			"fs.write": {
				Package: &skill.SkillPackage{
					Prompt: skill.PromptSkill{Name: "writer"},
					Trust:  skill.TrustBundled,
				},
				Manifest: skill.ToolManifest{
					Name:            "fs.write",
					SideEffectClass: "local_write",
				},
				Eligibility: skill.EligibilityResult{Eligible: true},
			},
		},
	}, nil).WithPolicy(policy.NewDefaultEngine(policy.Config{
		RequireApprovalForWrite: true,
	})).WithApprovals(approvals)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-resume-cancel",
		ExternalEventID: "evt-resume-cancel",
		Content:         "write file",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if err := component.ExecuteRun(context.Background(), run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}

	ticket, err := approvals.GetByRun(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("GetByRun() error = %v", err)
	}
	if _, err := component.ResolveApproval(context.Background(), ticket.ID, approval.Resolution{
		Status:     approval.StatusApproved,
		ResolvedBy: "tester",
	}); err != nil {
		t.Fatalf("ResolveApproval() error = %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- component.ResumeRun(context.Background(), run.ID)
	}()

	select {
	case <-model.started:
	case <-time.After(2 * time.Second):
		t.Fatal("resumed model.Chat was not called")
	}

	if _, err := component.CancelRun(context.Background(), run.ID); err != nil {
		t.Fatalf("CancelRun() error = %v", err)
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("ResumeRun() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ResumeRun() did not return after cancellation")
	}

	got, err := runs.Get(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("RunStore.Get() error = %v", err)
	}
	if got.Status != RunCancelled {
		t.Fatalf("run.Status = %q", got.Status)
	}
}

func TestExecuteRunDedupesConcurrentExecution(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	queue := &blockingTestCoordinator{
		started: make(chan struct{}, 2),
		release: make(chan struct{}),
	}
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
	}, sessions, runs, queue, NewSlidingWindowEngineForTest(), &stubModelClient{
		responses: []*ModelResponse{{
			Message: contextengine.Message{
				Role:    contextengine.RoleAssistant,
				Content: "done",
			},
		}},
	}, nil, nil)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-exec-dedupe",
		ExternalEventID: "evt-exec-dedupe",
		Content:         "hello",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- component.ExecuteRun(context.Background(), run)
	}()

	select {
	case <-queue.started:
	case <-time.After(2 * time.Second):
		t.Fatal("first ExecuteRun did not reach StartRun")
	}

	runCopy, err := runs.Get(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("RunStore.Get() error = %v", err)
	}
	if err := component.ExecuteRun(context.Background(), runCopy); err != nil {
		t.Fatalf("second ExecuteRun() error = %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if got := queue.startCount.Load(); got != 1 {
		t.Fatalf("StartRun count = %d, want 1", got)
	}

	close(queue.release)

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("first ExecuteRun() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first ExecuteRun() did not return")
	}
}

func TestResumeRunDedupesConcurrentExecution(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	approvals := approval.NewInMemoryStore()
	queue := &blockingTestCoordinator{
		started: make(chan struct{}, 2),
		release: make(chan struct{}),
	}
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
	}, sessions, runs, queue, NewSlidingWindowEngineForTest(), &stubModelClient{
		responses: []*ModelResponse{{
			Message: contextengine.Message{
				Role:    contextengine.RoleAssistant,
				Content: "done after resume",
			},
		}},
	}, nil, nil).WithApprovals(approvals)

	session, err := sessions.GetOrCreate(context.Background(), "chat-resume-dedupe", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	run, err := runs.Create(context.Background(), session.ID, IncomingMessage{
		SessionKey: "chat-resume-dedupe",
		Content:    "resume me",
	}, AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
	})
	if err != nil {
		t.Fatalf("runs.Create() error = %v", err)
	}
	ticket, err := approvals.Create(context.Background(), approval.Ticket{
		RunID:     run.ID,
		SessionID: session.ID,
	})
	if err != nil {
		t.Fatalf("approvals.Create() error = %v", err)
	}
	if _, err := approvals.Resolve(context.Background(), ticket.ID, approval.Resolution{
		Status:     approval.StatusApproved,
		ResolvedBy: "tester",
	}); err != nil {
		t.Fatalf("approvals.Resolve() error = %v", err)
	}
	run.Status = RunQueued
	if err := runs.Update(context.Background(), run); err != nil {
		t.Fatalf("runs.Update() error = %v", err)
	}

	errCh := make(chan error, 2)
	for range 2 {
		go func() {
			errCh <- component.ResumeRun(context.Background(), run.ID)
		}()
	}

	select {
	case <-queue.started:
	case <-time.After(2 * time.Second):
		t.Fatal("first ResumeRun did not reach StartRun")
	}

	time.Sleep(100 * time.Millisecond)
	if got := queue.startCount.Load(); got != 1 {
		t.Fatalf("StartRun count = %d, want 1", got)
	}

	close(queue.release)

	for range 2 {
		select {
		case err := <-errCh:
			if err != nil {
				t.Fatalf("ResumeRun() error = %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("ResumeRun() did not return")
		}
	}
}

func TestSubmitQueueInterruptCancelsSessionRunsBeforeEnqueue(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &stubModelClient{
		responses: []*ModelResponse{{
			ToolCalls: []ToolCall{{
				ID:   "call-1",
				Name: "fs.read",
			}},
		}},
	}
	tools := &cancellableBlockingToolExecutor{
		started: make(chan struct{}),
	}
	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 2,
		QueueMode:     QueueInterrupt,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, tools, nil)

	first, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-interrupt",
		ExternalEventID: "evt-interrupt-1",
		Content:         "first",
	})
	if err != nil {
		t.Fatalf("Submit(first) error = %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- component.ExecuteRun(context.Background(), first)
	}()

	select {
	case <-tools.started:
	case <-time.After(2 * time.Second):
		t.Fatal("first ExecuteRun did not reach tool execution")
	}

	second, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-interrupt",
		ExternalEventID: "evt-interrupt-2",
		Content:         "second",
	})
	if err != nil {
		t.Fatalf("Submit(second) error = %v", err)
	}

	first, err = runs.Get(context.Background(), first.ID)
	if err != nil {
		t.Fatalf("runs.Get(first) error = %v", err)
	}
	if first.Status != RunCancelled {
		t.Fatalf("first.Status = %q, want %q", first.Status, RunCancelled)
	}

	second, err = runs.Get(context.Background(), second.ID)
	if err != nil {
		t.Fatalf("runs.Get(second) error = %v", err)
	}
	if second.Status != RunQueued {
		t.Fatalf("second.Status = %q, want %q", second.Status, RunQueued)
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("first ExecuteRun() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first ExecuteRun() did not return")
	}
}

func TestFailedRunClearsPendingToolState(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 2,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{
		responses: []*ModelResponse{{
			ToolCalls: []ToolCall{{
				ID:   "call-1",
				Name: "fs.write",
			}},
		}},
	}, stubToolExecutor{}, nil).WithPolicy(denyPolicyEngine{})

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-fail-pending-tools",
		ExternalEventID: "evt-fail-pending-tools",
		Content:         "write file",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	err = component.ExecuteRun(context.Background(), run)
	if !errors.Is(err, ErrToolDenied) {
		t.Fatalf("ExecuteRun() error = %v, want ErrToolDenied", err)
	}

	run, err = runs.Get(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("RunStore.Get() error = %v", err)
	}
	if run.Status != RunFailed {
		t.Fatalf("run.Status = %q, want failed", run.Status)
	}
	if len(run.PendingTools) != 0 {
		t.Fatalf("run.PendingTools = %#v, want empty", run.PendingTools)
	}
	if run.ApprovalID != "" {
		t.Fatalf("run.ApprovalID = %q, want empty", run.ApprovalID)
	}
}

func TestExecuteRunUsesRuntimeResolverForPolicy(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	approvals := approval.NewInMemoryStore()
	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 2,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{
		responses: []*ModelResponse{{
			ToolCalls: []ToolCall{{
				ID:   "call-1",
				Name: "fs.write",
			}},
		}},
	}, stubRuntimeToolExecutor{
		boundTools: map[string]skill.BoundTool{
			"fs.write": {
				Package: &skill.SkillPackage{
					Prompt: skill.PromptSkill{Name: "builtin-core"},
					Trust:  skill.TrustBundled,
				},
				Manifest: skill.ToolManifest{
					Name:            "fs.write",
					SideEffectClass: "local_write",
				},
				Eligibility: skill.EligibilityResult{Eligible: true},
			},
		},
	}, nil).WithPolicy(policy.NewDefaultEngine(policy.Config{
		RequireApprovalForWrite: true,
	})).WithApprovals(approvals)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-runtime-policy",
		ExternalEventID: "evt-runtime-policy",
		Content:         "write file",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if err := component.ExecuteRun(context.Background(), run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}
	run = mustReloadRun(t, runs, run)
	if run.Status != RunWaitingApproval {
		t.Fatalf("run.Status = %q", run.Status)
	}
	ticket, err := approvals.GetByRun(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("GetByRun() error = %v", err)
	}
	if ticket.Status != approval.StatusPending {
		t.Fatalf("ticket.Status = %q", ticket.Status)
	}
}

func NewSlidingWindowEngineForTest() contextengine.ContextEngine {
	return contextengine.NewSlidingWindowEngine(contextengine.Config{
		BaseSystemPrompt:     "You are a test agent.",
		IncludeSkillCatalog:  false,
		DefaultContextWindow: 512,
		DefaultOutputTokens:  64,
	}, nil)
}

func newSkillServiceForAgentTest(t *testing.T, toolName, sideEffect string) *skill.Service {
	t.Helper()

	root := t.TempDir()
	dir := filepath.Join(root, "writer")
	if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(`---
name: writer
description: write files
---
# writer
`), 0o644); err != nil {
		t.Fatalf("WriteFile(SKILL.md) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scripts", "run.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(run.sh) error = %v", err)
	}
	manifest := map[string]any{
		"version": "1",
		"tool": map[string]any{
			"name": toolName,
			"input_schema": map[string]any{
				"type": "object",
			},
			"output_schema": map[string]any{
				"type": "object",
			},
			"side_effect_class": sideEffect,
			"execution_key":     "session:{id}",
		},
		"runtime": map[string]any{
			"entry": "scripts/run.sh",
			"shell": "bash",
		},
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("Marshal(manifest) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skill.manifest.json"), data, 0o644); err != nil {
		t.Fatalf("WriteFile(skill.manifest.json) error = %v", err)
	}

	svc := skill.NewService(skill.ServiceConfig{
		Roots: []skill.DiscoveryRoot{{Kind: skill.SourceWorkspace, Path: root}},
	})
	if _, err := svc.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	return svc
}

func TestExecuteRunInjectsBrowserReferenceSummaryIntoModelTurn(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &stubModelClient{
		responses: []*ModelResponse{{
			Message: contextengine.Message{
				Role:    contextengine.RoleAssistant,
				Content: "done",
			},
		}},
	}
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
		DedupeWindow: time.Minute,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, nil, nil)

	session, err := sessions.GetOrCreate(ctx, "chat-browser-turn-summary", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	session.Messages = append(session.Messages, contextengine.Message{
		Role:      contextengine.RoleUser,
		Content:   "旧任务：打开表单页",
		CreatedAt: time.Now().UTC().Add(-2 * time.Minute),
		Metadata:  map[string]any{"run_id": "run-old"},
	}, contextengine.Message{
		Role:      contextengine.RoleTool,
		Name:      "browser.snapshot",
		Content:   `{"url":"https://httpbin.org/forms/post","title":"HTTPBin Form"}`,
		CreatedAt: time.Now().UTC().Add(-time.Minute),
		Metadata:  map[string]any{"run_id": "run-old"},
	})
	if err := sessions.Save(ctx, session); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	run, err := component.Submit(ctx, IncomingMessage{
		SessionKey:      "chat-browser-turn-summary",
		ExternalEventID: "evt-browser-turn-summary",
		Content:         "抓取页面信息，写到 docs/tmp/example-brief.md",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.Status == RunWaitingInput {
		t.Fatalf("run.Status = %q, expected browser context to satisfy reference", run.Status)
	}

	if err := component.ExecuteRun(ctx, run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}

	foundSummary := false
	for _, msg := range model.lastRequest.Messages {
		if msg.Role != contextengine.RoleSystem || msg.Name != "session-summary" {
			continue
		}
		if strings.Contains(msg.Content, "https://httpbin.org/forms/post") {
			foundSummary = true
			break
		}
	}
	if !foundSummary {
		t.Fatalf("model.lastRequest.Messages = %#v, want session-summary with browser reference", model.lastRequest.Messages)
	}
	if !strings.Contains(model.lastRequest.SystemPrompt, "reuse the current page instead of asking the user for the URL again") {
		t.Fatalf("system prompt missing browser reuse guidance: %s", model.lastRequest.SystemPrompt)
	}
}

func TestExecuteRunCurrentInfoGuidanceRequiresModelSignal(t *testing.T) {
	t.Parallel()

	// Keyword-based RequiresCurrentInfo detection is removed.
	// Without a model/triage decision signaling RequiresCurrentInfo,
	// the system prompt should NOT include "Current Information Rule."
	ctx := context.Background()
	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &stubModelClient{
		responses: []*ModelResponse{{
			Message: contextengine.Message{
				Role:    contextengine.RoleAssistant,
				Content: "The latest Go version is verified.",
			},
		}},
	}
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
		DedupeWindow: time.Minute,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, nil, nil)

	run, err := component.Submit(ctx, IncomingMessage{
		SessionKey:      "chat-current-info-guidance",
		ExternalEventID: "evt-current-info-guidance",
		Content:         "What's the latest Go version today?",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	session, unlock, err := sessions.LoadForExecution(ctx, run.SessionID)
	if err != nil {
		t.Fatalf("LoadForExecution() error = %v", err)
	}
	session.Messages = append(session.Messages, contextengine.Message{
		Role:      contextengine.RoleUser,
		Content:   "请整理成表格",
		CreatedAt: time.Now().UTC(),
		Metadata:  map[string]any{"run_id": "run-other"},
	})
	if err := sessions.Save(ctx, session); err != nil {
		unlock()
		t.Fatalf("Save() error = %v", err)
	}
	unlock()

	if err := component.ExecuteRun(ctx, run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}
	if strings.Contains(model.lastRequest.SystemPrompt, "Current Information Rule") {
		t.Fatalf("system prompt should not include current-info guidance without model signal (keyword heuristic removed)")
	}
}

type stubModelClient struct {
	responses   []*ModelResponse
	index       int
	lastRequest ChatRequest
}

func (s *stubModelClient) Chat(_ context.Context, req ChatRequest) (*ModelResponse, error) {
	s.lastRequest = req
	if s.index >= len(s.responses) {
		return &ModelResponse{}, nil
	}
	resp := s.responses[s.index]
	s.index++
	return resp, nil
}

type steeringDirectiveModelClient struct {
	sessionID   string
	directives  SessionDirectiveStore
	responses   []*ModelResponse
	index       int
	sawSteering bool
}

func (s *steeringDirectiveModelClient) Chat(ctx context.Context, req ChatRequest) (*ModelResponse, error) {
	if s.index == 1 {
		for _, msg := range req.Messages {
			if msg.Role != contextengine.RoleUser || msg.Content != "只保留标题列" {
				continue
			}
			if flag, _ := msg.Metadata["synthetic_msg"].(bool); flag {
				s.sawSteering = true
				break
			}
		}
	}
	if s.index == 0 && s.directives != nil && s.sessionID != "" {
		if err := s.directives.Push(ctx, s.sessionID, SessionDirective{
			Kind:    SessionDirectiveSteer,
			Content: "只保留标题列",
		}); err != nil {
			return nil, err
		}
	}
	if s.index >= len(s.responses) {
		return &ModelResponse{}, nil
	}
	resp := s.responses[s.index]
	s.index++
	return resp, nil
}

type blockingModelClient struct {
	response *ModelResponse
	started  chan struct{}
	release  chan struct{}
}

func (b *blockingModelClient) Chat(_ context.Context, _ ChatRequest) (*ModelResponse, error) {
	select {
	case <-b.started:
	default:
		close(b.started)
	}
	<-b.release
	return b.response, nil
}

type gatedScriptedModelClient struct {
	mu         sync.Mutex
	responses  []*ModelResponse
	blockIndex int
	started    chan struct{}
	release    chan struct{}
	index      int
}

func (g *gatedScriptedModelClient) Chat(_ context.Context, _ ChatRequest) (*ModelResponse, error) {
	g.mu.Lock()
	if g.index >= len(g.responses) {
		g.mu.Unlock()
		return &ModelResponse{}, nil
	}
	resp := g.responses[g.index]
	current := g.index
	g.index++
	g.mu.Unlock()
	if current == g.blockIndex {
		select {
		case <-g.started:
		default:
			close(g.started)
		}
		<-g.release
	}
	return resp, nil
}

type scriptedBlockingModelClient struct {
	responses  []*ModelResponse
	blockIndex int
	started    chan struct{}
	index      int
}

func (s *scriptedBlockingModelClient) Chat(ctx context.Context, _ ChatRequest) (*ModelResponse, error) {
	if s.index >= len(s.responses) {
		return &ModelResponse{}, nil
	}
	resp := s.responses[s.index]
	current := s.index
	s.index++
	if current == s.blockIndex {
		select {
		case <-s.started:
		default:
			close(s.started)
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return resp, nil
}

type stubToolExecutor struct {
	results []contextengine.ToolResult
	err     error
}

func (s stubToolExecutor) ExecuteBatch(context.Context, *Run, *Session, []ToolCall) ([]contextengine.ToolResult, error) {
	return append([]contextengine.ToolResult(nil), s.results...), s.err
}

type blockingToolExecutor struct {
	results []contextengine.ToolResult
	err     error
	started chan struct{}
	release chan struct{}
}

func (b *blockingToolExecutor) ExecuteBatch(_ context.Context, _ *Run, _ *Session, _ []ToolCall) ([]contextengine.ToolResult, error) {
	select {
	case <-b.started:
	default:
		close(b.started)
	}
	<-b.release
	return append([]contextengine.ToolResult(nil), b.results...), b.err
}

type blockingRuntimeContextProvider struct {
	blockCalls int32
	calls      atomic.Int32
	started    chan struct{}
	release    chan struct{}
}

func (p *blockingRuntimeContextProvider) Current(_ context.Context, _ *Session, _ *Run) (skill.RuntimeContext, error) {
	if p.calls.Add(1) <= p.blockCalls {
		select {
		case <-p.started:
		default:
			close(p.started)
		}
		<-p.release
	}
	return skill.RuntimeContext{}, nil
}

type cancellableBlockingToolExecutor struct {
	started chan struct{}
}

func (c *cancellableBlockingToolExecutor) ExecuteBatch(ctx context.Context, _ *Run, _ *Session, _ []ToolCall) ([]contextengine.ToolResult, error) {
	select {
	case <-c.started:
	default:
		close(c.started)
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

type countingToolExecutor struct {
	results []contextengine.ToolResult
	err     error
	calls   atomic.Int32
}

func (c *countingToolExecutor) ExecuteBatch(context.Context, *Run, *Session, []ToolCall) ([]contextengine.ToolResult, error) {
	c.calls.Add(1)
	return append([]contextengine.ToolResult(nil), c.results...), c.err
}

type countingRuntimeToolExecutor struct {
	countingToolExecutor
	boundTools map[string]skill.BoundTool
}

func (c *countingRuntimeToolExecutor) ToolDefinitions(*Session) []ToolDefinition {
	out := make([]ToolDefinition, 0, len(c.boundTools))
	for _, bound := range c.boundTools {
		out = append(out, ToolDefinition{
			Name:               bound.Manifest.Name,
			SideEffectClass:    bound.Manifest.SideEffectClass,
			Eligible:           bound.Eligibility.Eligible,
			EligibilityReasons: append([]string(nil), bound.Eligibility.Reasons...),
		})
	}
	return out
}

func (c *countingRuntimeToolExecutor) ResolveTool(_ *Session, name string) (*ResolvedTool, bool) {
	bound, ok := c.boundTools[name]
	if !ok {
		return nil, false
	}
	copied := bound
	trust := ""
	sourceRef := ""
	if copied.Package != nil {
		trust = string(copied.Package.Trust)
		sourceRef = copied.Package.Source.Dir
	}
	return toolspec.ResolvedFromSkillBinding(&copied, ToolDefinition{
		Name:               copied.Manifest.Name,
		Description:        copied.Manifest.Description,
		InputSchema:        cloneMap(copied.Manifest.InputSchema),
		OutputSchema:       cloneMap(copied.Manifest.OutputSchema),
		SideEffectClass:    copied.Manifest.SideEffectClass,
		Idempotent:         copied.Manifest.Idempotent,
		RequiresApproval:   copied.Manifest.RequiresApproval,
		ExecutionKey:       copied.Manifest.ExecutionKey,
		Source:             "test",
		SourceRef:          sourceRef,
		Trust:              trust,
		Eligible:           copied.Eligibility.Eligible,
		EligibilityReasons: append([]string(nil), copied.Eligibility.Reasons...),
	}, "component-test"), true
}

type stubRuntimeToolExecutor struct {
	results     []contextengine.ToolResult
	definitions []ToolDefinition
	boundTools  map[string]skill.BoundTool
	err         error
}

func (s stubRuntimeToolExecutor) ExecuteBatch(context.Context, *Run, *Session, []ToolCall) ([]contextengine.ToolResult, error) {
	return append([]contextengine.ToolResult(nil), s.results...), s.err
}

func (s stubRuntimeToolExecutor) ToolDefinitions(*Session) []ToolDefinition {
	out := make([]ToolDefinition, 0, len(s.definitions))
	for _, definition := range s.definitions {
		out = append(out, ToolDefinition{
			Name:             definition.Name,
			Description:      definition.Description,
			InputSchema:      cloneMap(definition.InputSchema),
			OutputSchema:     cloneMap(definition.OutputSchema),
			SideEffectClass:  definition.SideEffectClass,
			Idempotent:       definition.Idempotent,
			RequiresApproval: definition.RequiresApproval,
			ExecutionKey:     definition.ExecutionKey,
		})
	}
	return out
}

func (s stubRuntimeToolExecutor) ResolveTool(_ *Session, name string) (*ResolvedTool, bool) {
	bound, ok := s.boundTools[name]
	if !ok {
		return nil, false
	}
	copied := bound
	trust := ""
	sourceRef := ""
	if copied.Package != nil {
		trust = string(copied.Package.Trust)
		sourceRef = copied.Package.Source.Dir
	}
	return toolspec.ResolvedFromSkillBinding(&copied, ToolDefinition{
		Name:               copied.Manifest.Name,
		Description:        copied.Manifest.Description,
		InputSchema:        cloneMap(copied.Manifest.InputSchema),
		OutputSchema:       cloneMap(copied.Manifest.OutputSchema),
		SideEffectClass:    copied.Manifest.SideEffectClass,
		Idempotent:         copied.Manifest.Idempotent,
		RequiresApproval:   copied.Manifest.RequiresApproval,
		ExecutionKey:       copied.Manifest.ExecutionKey,
		Source:             "test",
		SourceRef:          sourceRef,
		Trust:              trust,
		Eligible:           copied.Eligibility.Eligible,
		EligibilityReasons: append([]string(nil), copied.Eligibility.Reasons...),
	}, "component-test"), true
}

type denyPolicyEngine struct{}

func (denyPolicyEngine) EvaluateTool(context.Context, policy.ToolContext) (policy.Decision, error) {
	return policy.Decision{
		Action:  policy.ActionDeny,
		Reasons: []string{"denied in test"},
	}, nil
}

type alwaysRequireApprovalPolicyEngine struct{}

func (alwaysRequireApprovalPolicyEngine) EvaluateTool(context.Context, policy.ToolContext) (policy.Decision, error) {
	return policy.Decision{
		Action:       policy.ActionRequireApproval,
		Reasons:      []string{"approval required in test"},
		PolicySource: "test.policy/always-require-approval",
		Summary:      "approval required in test",
	}, nil
}

type blockingTestCoordinator struct {
	startCount atomic.Int32
	started    chan struct{}
	release    chan struct{}
	once       sync.Once
}

func (c *blockingTestCoordinator) EnqueueSessionRun(context.Context, string, string, QueueMode) error {
	return nil
}

func (c *blockingTestCoordinator) NextQueuedRun(context.Context, string) (string, bool, error) {
	return "", false, nil
}

func (c *blockingTestCoordinator) StartRun(context.Context, string, string) error {
	c.startCount.Add(1)
	c.started <- struct{}{}
	c.once.Do(func() {
		<-c.release
	})
	return nil
}

func (c *blockingTestCoordinator) FinishRun(context.Context, string, string) error {
	return nil
}
