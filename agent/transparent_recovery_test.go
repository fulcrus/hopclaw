package agent

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/resultmodel"
)

func TestInferTransparentRecoveryIntent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		goal     string
		hints    []string
		domains  []string
		tools    []ToolDefinition
		wantKey  string
		wantNone bool
	}{
		{
			name:    "rss recovery from structured domain",
			goal:    "collect recent feed updates",
			tools:   []ToolDefinition{{Name: "skill.ensure"}, {Name: "net.fetch"}},
			domains: []string{string(DomainNews)},
			wantKey: "rss",
		},
		{
			name:     "news service hint with direct tool already available",
			goal:     "collect recent feed updates",
			hints:    []string{"search.news"},
			tools:    []ToolDefinition{{Name: "skill.ensure"}, {Name: "search.news"}},
			wantNone: true,
		},
		{
			name:     "rss request with direct rss tool",
			goal:     "collect recent feed updates",
			tools:    []ToolDefinition{{Name: "skill.ensure"}, {Name: "rss.fetch"}},
			domains:  []string{string(DomainNews)},
			wantNone: true,
		},
		{
			name:    "news service hint falls back to rss recovery",
			goal:    "provide a current news update",
			hints:   []string{"search.news"},
			tools:   []ToolDefinition{{Name: "skill.ensure"}},
			wantKey: "rss",
		},
		{
			name:    "email recovery from structured domain",
			goal:    "find the recent invoice correspondence",
			domains: []string{string(DomainEmail)},
			tools:   []ToolDefinition{{Name: "skill.ensure"}},
			wantKey: "email",
		},
		{
			name:    "translation recovery from capability hint",
			goal:    "translate the provided text",
			hints:   []string{"translate.run"},
			tools:   []ToolDefinition{{Name: "skill.ensure"}},
			wantKey: "translate",
		},
		{
			name:    "calculator recovery from capability hint",
			goal:    "calculate the requested percentage",
			hints:   []string{"calculator.eval"},
			tools:   []ToolDefinition{{Name: "skill.ensure"}},
			wantKey: "calculator",
		},
		{
			name:     "translation request with direct tool already available",
			goal:     "translate this paragraph into Spanish",
			hints:    []string{"translate.run"},
			tools:    []ToolDefinition{{Name: "skill.ensure"}, {Name: "translate.run"}},
			wantNone: true,
		},
		{
			name:     "no ensure tool available",
			goal:     "provide a current news update",
			hints:    []string{"search.news"},
			tools:    []ToolDefinition{{Name: "net.fetch"}},
			wantNone: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			intent := inferTransparentRecoveryIntentWithHints(tt.goal, tt.hints, tt.tools)
			if len(tt.domains) > 0 {
				intent = inferTransparentRecoveryIntentWithDomains(tt.goal, tt.domains, tt.tools)
			}
			if tt.wantNone {
				if intent != nil {
					t.Fatalf("intent = %+v, want nil", intent)
				}
				return
			}
			if intent == nil {
				t.Fatal("intent = nil")
			}
			if intent.Key != tt.wantKey {
				t.Fatalf("intent.Key = %q, want %q", intent.Key, tt.wantKey)
			}
		})
	}
}

func TestExecuteRunAutomaticallyRecoversMissingCapabilityBeforeModelCall(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &stubModelClient{
		responses: []*ModelResponse{{
			Message: contextengine.Message{
				Role:    contextengine.RoleAssistant,
				Content: "rss summary ready",
			},
		}},
	}
	tools := &transparentRecoveryToolExecutor{
		enableToolName: "rss.fetch",
		ensureResult: contextengine.ToolResult{
			ToolName:       "skill.ensure",
			ToolCallID:     "auto-recover-rss",
			Status:         resultmodel.ToolResultOK,
			TranscriptText: `{"success":true,"resolved":true,"installed":true,"message":"required capability is ready via package \"rss\""}`,
			Content:        `{"success":true,"resolved":true,"installed":true,"message":"required capability is ready via package \"rss\""}`,
		},
	}
	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 3,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, tools, nil)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey: "transparent-recovery-rss",
		Content:    "读取这个 RSS feed 并总结最近三条：https://example.com/feed.xml",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	run.TaskContract = &TaskContract{
		Goal:             "读取这个 RSS feed 并总结最近三条：https://example.com/feed.xml",
		SuggestedDomains: []string{string(DomainNews)},
		CapabilityHints:  []string{"rss.fetch"},
	}
	if err := runs.Update(context.Background(), run); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if err := component.ExecuteRun(context.Background(), run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}

	if got := tools.ensureCalls.Load(); got != 1 {
		t.Fatalf("ensureCalls = %d, want 1, model tools=%#v messages=%#v", got, model.lastRequest.Tools, model.lastRequest.Messages)
	}
	if !containsToolName(model.lastRequest.Tools, "rss.fetch") {
		t.Fatalf("model tools = %#v, want rss.fetch after automatic recovery", model.lastRequest.Tools)
	}
	if !hasToolMessage(model.lastRequest.Messages, "skill.ensure") {
		t.Fatalf("model messages = %#v, want prior skill.ensure tool result", model.lastRequest.Messages)
	}
}

func TestExecuteRunAutomaticallyRecoversTranslationCapabilityBeforeModelCall(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &stubModelClient{
		responses: []*ModelResponse{{
			Message: contextengine.Message{
				Role:    contextengine.RoleAssistant,
				Content: "translation ready",
			},
		}},
	}
	tools := &transparentRecoveryToolExecutor{
		enableToolName: "translate.run",
		ensureResult: contextengine.ToolResult{
			ToolName:       "skill.ensure",
			ToolCallID:     "auto-recover-translate",
			Status:         resultmodel.ToolResultOK,
			TranscriptText: `{"success":true,"resolved":true,"installed":true,"message":"required capability is ready via package \"translate\""}`,
			Content:        `{"success":true,"resolved":true,"installed":true,"message":"required capability is ready via package \"translate\""}`,
		},
	}
	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 3,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, tools, nil)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey: "transparent-recovery-translate",
		Content:    "把这段话翻译成英文：你好，世界",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	run.TaskContract = &TaskContract{
		Goal:            "把这段话翻译成英文：你好，世界",
		CapabilityHints: []string{"translate.run"},
	}
	if err := runs.Update(context.Background(), run); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if err := component.ExecuteRun(context.Background(), run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}

	if got := tools.ensureCalls.Load(); got != 1 {
		t.Fatalf("ensureCalls = %d, want 1, model tools=%#v messages=%#v", got, model.lastRequest.Tools, model.lastRequest.Messages)
	}
	if !containsToolName(model.lastRequest.Tools, "translate.run") {
		t.Fatalf("model tools = %#v, want translate.run after automatic recovery", model.lastRequest.Tools)
	}
	if !hasToolMessage(model.lastRequest.Messages, "skill.ensure") {
		t.Fatalf("model messages = %#v, want prior skill.ensure tool result", model.lastRequest.Messages)
	}
}

func TestExecuteRunUsesAnalyzerCapabilityHintsForTransparentRecovery(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &stubModelClient{
		responses: []*ModelResponse{
			{
				Message: contextengine.Message{
					Role: contextengine.RoleAssistant,
					Content: `{
						"job_type":"general",
						"suggested_domains":["text"],
						"capability_hints":["translate.run"],
						"deliverable_kinds":["summary"],
						"missing_info_ids":[],
						"requires_external_effect":false,
						"requires_approval":false,
						"confidence":0.93
					}`,
				},
			},
			{
				Message: contextengine.Message{
					Role:    contextengine.RoleAssistant,
					Content: "translation ready",
				},
			},
		},
	}
	tools := &transparentRecoveryToolExecutor{
		enableToolName: "translate.run",
		ensureResult: contextengine.ToolResult{
			ToolName:       "skill.ensure",
			ToolCallID:     "auto-recover-translate",
			Status:         resultmodel.ToolResultOK,
			TranscriptText: `{"success":true,"resolved":true,"installed":true,"message":"required capability is ready via package \"translate\""}`,
			Content:        `{"success":true,"resolved":true,"installed":true,"message":"required capability is ready via package \"translate\""}`,
		},
	}
	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 3,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, tools, nil).
		WithTaskContractAnalyzer(NewModelTaskContractAnalyzer(model, 0))

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey: "transparent-recovery-translate-analyzer",
		Content:    "Traduce este párrafo al inglés.",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.TaskContract == nil || len(run.TaskContract.CapabilityHints) == 0 {
		t.Fatalf("run.TaskContract = %#v, want analyzer capability hints", run.TaskContract)
	}
	if err := component.ExecuteRun(context.Background(), run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}

	if got := tools.ensureCalls.Load(); got != 1 {
		t.Fatalf("ensureCalls = %d, want 1, contract=%#v tools=%#v", got, run.TaskContract, model.lastRequest.Tools)
	}
	if !containsToolName(model.lastRequest.Tools, "translate.run") {
		t.Fatalf("model tools = %#v, want translate.run after analyzer-driven recovery", model.lastRequest.Tools)
	}
}

func TestExecuteRunAutomaticallyRecoversMissingCalendarCapabilityBeforeModelCall(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &stubModelClient{
		responses: []*ModelResponse{{
			Message: contextengine.Message{
				Role:    contextengine.RoleAssistant,
				Content: "calendar capability ready",
			},
		}},
	}
	tools := &transparentRecoveryToolExecutor{
		enableToolName: "calendar.list_events",
		ensureResult: contextengine.ToolResult{
			ToolName:       "skill.ensure",
			ToolCallID:     "auto-recover-missing-domain-calendar",
			Status:         resultmodel.ToolResultOK,
			TranscriptText: `{"success":true,"resolved":true,"installed":true,"message":"required capability is ready via package \"calendar\""}`,
			Content:        `{"success":true,"resolved":true,"installed":true,"message":"required capability is ready via package \"calendar\""}`,
		},
	}
	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 3,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, tools, nil)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey: "transparent-recovery-calendar",
		Content:    "安排下周的领导周会",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	run.TaskContract = &TaskContract{
		Goal:             "安排下周的领导周会",
		SuggestedDomains: []string{string(DomainCalendar)},
	}
	if err := runs.Update(context.Background(), run); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if err := component.ExecuteRun(context.Background(), run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}

	if got := tools.ensureCalls.Load(); got != 1 {
		t.Fatalf("ensureCalls = %d, want 1", got)
	}
	if !containsToolName(model.lastRequest.Tools, "calendar.list_events") {
		t.Fatalf("model tools = %#v, want calendar.list_events after automatic recovery", model.lastRequest.Tools)
	}
	if !hasToolMessage(model.lastRequest.Messages, "skill.ensure") {
		t.Fatalf("model messages = %#v, want prior skill.ensure tool result", model.lastRequest.Messages)
	}
}

func TestMaybeAttemptTransparentCapabilityRecovery(t *testing.T) {
	t.Parallel()

	tools := &transparentRecoveryToolExecutor{
		ensureResult: contextengine.ToolResult{
			ToolName:       "skill.ensure",
			ToolCallID:     "auto-recover-email",
			Status:         resultmodel.ToolResultOK,
			TranscriptText: `{"success":true,"resolved":true}`,
			Content:        `{"success":true,"resolved":true}`,
		},
	}
	component := NewComponent(AgentConfig{DefaultModel: "test-model"}, NewInMemorySessionStore(), NewInMemoryRunStore(), NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, tools, nil)
	session := &Session{
		ID: "sess-1",
		Session: contextengine.Session{
			Messages: []contextengine.Message{{
				Role:    contextengine.RoleUser,
				Content: "find the recent invoice correspondence",
			}},
		},
	}
	run := &Run{
		ID: "run-1",
		Preflight: &RunPreflightReport{
			SuggestedDomains: []string{string(DomainEmail)},
		},
		TaskContract: &TaskContract{
			Goal:             "find the recent invoice correspondence",
			SuggestedDomains: []string{string(DomainEmail)},
			CapabilityHints:  []string{"email.search"},
		},
	}
	available := []ToolDefinition{
		{Name: "skill.ensure", SideEffectClass: "read"},
		{Name: "fs.read", SideEffectClass: "read"},
		{Name: "exec.run", SideEffectClass: "local_write"},
	}

	recovered, err := component.maybeAttemptTransparentCapabilityRecovery(context.Background(), run, session, session.Messages[0].Content, available)
	if err != nil {
		t.Fatalf("maybeAttemptTransparentCapabilityRecovery() error = %v", err)
	}
	if !recovered {
		t.Fatal("expected transparent recovery attempt")
	}
	if got := tools.ensureCalls.Load(); got != 1 {
		t.Fatalf("ensureCalls = %d, want 1", got)
	}
	if !hasToolMessage(session.Messages, "skill.ensure") {
		t.Fatalf("session messages = %#v, want skill.ensure result", session.Messages)
	}
}

func TestExecuteRunRecordsTransparentRecoveryFailureForModel(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &stubModelClient{
		responses: []*ModelResponse{{
			Message: contextengine.Message{
				Role:    contextengine.RoleAssistant,
				Content: "I could not fetch that feed automatically.",
			},
		}},
	}
	tools := &transparentRecoveryToolExecutor{
		ensureResult: contextengine.ToolResult{
			ToolName:       "skill.ensure",
			ToolCallID:     "auto-recover-rss",
			Status:         resultmodel.ToolResultError,
			TranscriptText: `{"success":false,"resolved":false,"message":"no matching capability package found in the catalog"}`,
			Content:        `{"success":false,"resolved":false,"message":"no matching capability package found in the catalog"}`,
		},
	}
	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 3,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, tools, nil)

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey: "transparent-recovery-failure",
		Content:    "读取这个 RSS feed 并总结最近三条：https://example.com/feed.xml",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if err := component.ExecuteRun(context.Background(), run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}

	if got := tools.ensureCalls.Load(); got != 1 {
		t.Fatalf("ensureCalls = %d, want 1, model tools=%#v messages=%#v", got, model.lastRequest.Tools, model.lastRequest.Messages)
	}
	if containsToolName(model.lastRequest.Tools, "rss.fetch") {
		t.Fatalf("model tools = %#v, did not expect rss.fetch after failed recovery", model.lastRequest.Tools)
	}
	if !hasToolMessage(model.lastRequest.Messages, "skill.ensure") {
		t.Fatalf("model messages = %#v, want prior skill.ensure tool result", model.lastRequest.Messages)
	}
	if !messagesContain(model.lastRequest.Messages, "no matching capability package found in the catalog") {
		t.Fatalf("model messages = %#v, want recovery failure details", model.lastRequest.Messages)
	}
}

type transparentRecoveryToolExecutor struct {
	ensureResult   contextengine.ToolResult
	enableToolName string
	toolEnabled    atomic.Bool
	ensureCalls    atomic.Int32
}

func (t *transparentRecoveryToolExecutor) ExecuteBatch(_ context.Context, _ *Run, _ *Session, calls []ToolCall) ([]contextengine.ToolResult, error) {
	if len(calls) == 1 && calls[0].Name == "skill.ensure" {
		t.ensureCalls.Add(1)
		if strings.TrimSpace(t.enableToolName) != "" {
			t.toolEnabled.Store(true)
		}
		result := t.ensureResult
		result.ToolCallID = calls[0].ID
		return []contextengine.ToolResult{result}, nil
	}
	return nil, nil
}

func (t *transparentRecoveryToolExecutor) ToolDefinitions(*Session) []ToolDefinition {
	defs := []ToolDefinition{
		{Name: "skill.ensure", SideEffectClass: "read"},
		{Name: "fs.read", SideEffectClass: "read"},
		{Name: "exec.run", SideEffectClass: "local_write"},
	}
	if t.toolEnabled.Load() && strings.TrimSpace(t.enableToolName) != "" {
		defs = append(defs, ToolDefinition{Name: t.enableToolName, SideEffectClass: "read"})
	}
	return defs
}

func containsToolName(tools []ToolDefinition, want string) bool {
	for _, tool := range tools {
		if strings.TrimSpace(tool.Name) == want {
			return true
		}
	}
	return false
}

func hasToolMessage(messages []contextengine.Message, name string) bool {
	for _, msg := range messages {
		if msg.Role == contextengine.RoleTool && strings.TrimSpace(msg.Name) == name {
			return true
		}
	}
	return false
}

func messagesContain(messages []contextengine.Message, want string) bool {
	for _, msg := range messages {
		if strings.Contains(msg.Content, want) {
			return true
		}
	}
	return false
}
