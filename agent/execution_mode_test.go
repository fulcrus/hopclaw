package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/planner"
)

type staticExecutionModeSelector struct {
	decision ExecutionModeDecision
	err      error
}

func (s staticExecutionModeSelector) Select(context.Context, ExecutionModeRequest) (ExecutionModeDecision, error) {
	if s.err != nil {
		return ExecutionModeDecision{}, s.err
	}
	return s.decision, nil
}

func TestNormalizeExecutionModeDecision(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		decision         ExecutionModeDecision
		plannerAvailable bool
		want             ExecutionMode
	}{
		{name: "direct preserved", decision: ExecutionModeDecision{Mode: ExecutionModeDirect}, plannerAvailable: true, want: ExecutionModeDirect},
		{name: "watch preserved", decision: ExecutionModeDecision{Mode: ExecutionModeWatch}, plannerAvailable: true, want: ExecutionModeWatch},
		{name: "invalid falls back", decision: ExecutionModeDecision{Mode: ExecutionMode("bad")}, plannerAvailable: true, want: ExecutionModePlanned},
		{name: "planner unavailable degrades to direct", decision: ExecutionModeDecision{Mode: ExecutionModeWatch}, plannerAvailable: false, want: ExecutionModeDirect},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := normalizeExecutionModeDecision(tt.decision, tt.plannerAvailable)
			if got.Mode != tt.want {
				t.Fatalf("normalizeExecutionModeDecision(%+v) = %q, want %q", tt.decision, got.Mode, tt.want)
			}
		})
	}
}

func TestSubmitSetsExecutionMode(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil).
		WithPlanner(staticPlanner{plan: nil})

	cases := []struct {
		content string
		want    ExecutionMode
	}{
		{content: "read README.md", want: ExecutionModeDirect},
		{content: "search the market and summarize it into a table", want: ExecutionModePlanned},
		{content: "每周监控网站价格变化并通知我", want: ExecutionModeWatch},
	}
	for i, tc := range cases {
		component.WithExecutionModeSelector(staticExecutionModeSelector{decision: ExecutionModeDecision{Mode: tc.want}})
		run, err := component.Submit(context.Background(), IncomingMessage{
			SessionKey:      "chat-mode-submit",
			ExternalEventID: "evt-mode-submit-" + string(rune('a'+i)),
			Content:         tc.content,
		})
		if err != nil {
			t.Fatalf("Submit() error = %v", err)
		}
		if run.ExecutionMode != tc.want {
			t.Fatalf("run.ExecutionMode = %q, want %q for %q", run.ExecutionMode, tc.want, tc.content)
		}
	}
}

func TestExecuteRunSkipsPlannerForDirectMode(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &stubModelClient{
		responses: []*ModelResponse{{
			Message: contextengine.Message{
				Role:    contextengine.RoleAssistant,
				Content: "README contents loaded.",
			},
		}},
	}
	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 2,
		QueueMode:     QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, nil, nil).
		WithPlanner(staticPlanner{plan: nil}).
		WithExecutionModeSelector(staticExecutionModeSelector{decision: ExecutionModeDecision{Mode: ExecutionModeDirect}})

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-direct-mode",
		ExternalEventID: "evt-direct-mode",
		Content:         "read README.md",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.ExecutionMode != ExecutionModeDirect {
		t.Fatalf("run.ExecutionMode = %q, want direct", run.ExecutionMode)
	}
	if err := component.ExecuteRun(context.Background(), run); err != nil {
		t.Fatalf("ExecuteRun() error = %v", err)
	}
	run = mustReloadRun(t, runs, run)
	if run.Plan != nil {
		t.Fatalf("run.Plan = %#v, want nil for direct mode", run.Plan)
	}
}

func TestSelectExecutionModeFallsBackWhenSelectorFails(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil).
		WithPlanner(staticPlanner{plan: nil}).
		WithExecutionModeSelector(staticExecutionModeSelector{err: errors.New("selector unavailable")})

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-mode-fallback",
		ExternalEventID: "evt-mode-fallback",
		Content:         "anything",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.ExecutionMode != ExecutionModePlanned {
		t.Fatalf("run.ExecutionMode = %q, want planned fallback", run.ExecutionMode)
	}
}

func TestSelectExecutionModeFallsBackToDefaultWhenSelectorFailsForSimpleDesktopTask(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil).
		WithPlanner(staticPlanner{plan: nil}).
		WithExecutionModeSelector(staticExecutionModeSelector{err: errors.New("selector unavailable")})

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-mode-desktop-direct",
		ExternalEventID: "evt-mode-desktop-direct",
		Content:         "List the currently running desktop applications on this machine.",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.ExecutionMode != ExecutionModeDirect {
		t.Fatalf("run.ExecutionMode = %q, want direct default fallback for a simple desktop request", run.ExecutionMode)
	}
}

func TestSelectExecutionModeUsesHeuristicForMultiStepTask(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil).
		WithPlanner(staticPlanner{plan: nil})

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-mode-planned-heuristic",
		ExternalEventID: "evt-mode-planned-heuristic",
		Content:         "Open Calculator, then open TextEdit, type a note, and take a screenshot.",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.ExecutionMode != ExecutionModePlanned {
		t.Fatalf("run.ExecutionMode = %q, want planned", run.ExecutionMode)
	}
}

func TestSubmitRefinesExecutionModeToWatchFromTaskContractAnalyzer(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &stubModelClient{
		responses: []*ModelResponse{{
			Message: contextengine.Message{
				Role: contextengine.RoleAssistant,
				Content: `{
					"job_type":"monitor",
					"suggested_domains":["watch","browser"],
					"deliverable_kinds":["watch_alert"],
					"missing_info_ids":[],
					"requires_external_effect":true,
					"requires_approval":false,
					"confidence":0.91
				}`,
			},
		}},
	}
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, nil, nil).
		WithPlanner(staticPlanner{plan: nil}).
		WithTaskContractAnalyzer(NewModelTaskContractAnalyzer(model, 0))

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-mode-refine-watch",
		ExternalEventID: "evt-mode-refine-watch",
		Content:         "Vigila https://example.com cada hora y avísame aquí cuando cambie el título.",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.ExecutionMode != ExecutionModeWatch {
		t.Fatalf("run.ExecutionMode = %q, want watch", run.ExecutionMode)
	}
	if run.Preflight == nil || !containsTestString(run.Preflight.SuggestedDomains, "watch") {
		t.Fatalf("run.Preflight = %#v, want merged watch domain", run.Preflight)
	}
}

func TestSubmitRefinesExecutionModeToWatchFromStructuredTaskContract(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil).
		WithPlanner(staticPlanner{plan: nil}).
		WithExecutionModeSelector(staticExecutionModeSelector{decision: ExecutionModeDecision{Mode: ExecutionModeDirect}})

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-mode-refine-watch-heuristic",
		ExternalEventID: "evt-mode-refine-watch-heuristic",
		Content:         "Use watch.poll on https://example.com with cron 0 * * * * and report title changes.",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.ExecutionMode != ExecutionModeWatch {
		t.Fatalf("run.ExecutionMode = %q, want watch", run.ExecutionMode)
	}
}

func TestSubmitRefinesExecutionModeToWorkflowFromTaskContractAnalyzer(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &stubModelClient{
		responses: []*ModelResponse{{
			Message: contextengine.Message{
				Role: contextengine.RoleAssistant,
				Content: `{
					"job_type":"delivery",
					"suggested_domains":["email","document"],
					"deliverable_kinds":["message_delivery","document"],
					"missing_info_ids":[],
					"requires_external_effect":true,
					"requires_approval":true,
					"confidence":0.94
				}`,
			},
		}},
	}
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, nil, nil).
		WithPlanner(staticPlanner{plan: nil}).
		WithTaskContractAnalyzer(NewModelTaskContractAnalyzer(model, 0))

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-mode-refine-workflow",
		ExternalEventID: "evt-mode-refine-workflow",
		Content:         "Envía el informe semanal a ceo@example.com por correo electrónico.",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.ExecutionMode != ExecutionModeWorkflow {
		t.Fatalf("run.ExecutionMode = %q, want workflow", run.ExecutionMode)
	}
}

func TestSubmitRefinesExecutionModeToPlannedFromHeuristicTaskContract(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil).
		WithPlanner(staticPlanner{plan: nil}).
		WithExecutionModeSelector(staticExecutionModeSelector{decision: ExecutionModeDecision{Mode: ExecutionModeDirect}})

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-mode-refine-planned-heuristic",
		ExternalEventID: "evt-mode-refine-planned-heuristic",
		Content:         "Collect the market options into reports/comparison-table.xlsx",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.ExecutionMode != ExecutionModePlanned {
		t.Fatalf("run.ExecutionMode = %q, want planned", run.ExecutionMode)
	}
}

func TestSubmitRefinesExecutionModeToDirectFromTaskContractAnalyzer(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &stubModelClient{
		responses: []*ModelResponse{{
			Message: contextengine.Message{
				Role: contextengine.RoleAssistant,
				Content: `{
					"job_type":"general",
					"suggested_domains":["desktop"],
					"deliverable_kinds":["desktop_evidence"],
					"missing_info_ids":[],
					"requires_external_effect":false,
					"requires_approval":false,
					"confidence":0.88
				}`,
			},
		}},
	}
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, nil, nil).
		WithPlanner(staticPlanner{plan: nil}).
		WithTaskContractAnalyzer(NewModelTaskContractAnalyzer(model, 0))

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-mode-refine-direct",
		ExternalEventID: "evt-mode-refine-direct",
		Content:         "現在開いているアプリを一覧し、前面ウィンドウのスクリーンショットを見せてください。",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.ExecutionMode != ExecutionModeDirect {
		t.Fatalf("run.ExecutionMode = %q, want direct", run.ExecutionMode)
	}
}

func TestSubmitKeepsModelSourcedDirectModeWithoutSequentialHeuristicReplay(t *testing.T) {
	t.Parallel()

	sessions := NewInMemorySessionStore()
	runs := NewInMemoryRunStore()
	model := &stubModelClient{
		responses: []*ModelResponse{{
			Message: contextengine.Message{
				Role: contextengine.RoleAssistant,
				Content: `{
					"job_type":"general",
					"suggested_domains":["desktop"],
					"deliverable_kinds":["desktop_evidence"],
					"missing_info_ids":[],
					"requires_external_effect":false,
					"requires_approval":false,
					"confidence":0.9
				}`,
			},
		}},
	}
	component := NewComponent(AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    QueueEnqueue,
	}, sessions, runs, NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), model, nil, nil).
		WithPlanner(staticPlanner{plan: nil}).
		WithExecutionModeSelector(staticExecutionModeSelector{decision: ExecutionModeDecision{Mode: ExecutionModePlanned}}).
		WithTaskContractAnalyzer(NewModelTaskContractAnalyzer(model, 0))

	run, err := component.Submit(context.Background(), IncomingMessage{
		SessionKey:      "chat-mode-direct-structured-only",
		ExternalEventID: "evt-mode-direct-structured-only",
		Content:         "List the running apps, then show me the frontmost window screenshot.",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.TaskContract == nil || run.TaskContract.Source != taskContractSourceModel {
		t.Fatalf("run.TaskContract = %#v, want model-sourced contract", run.TaskContract)
	}
	if run.ExecutionMode != ExecutionModeDirect {
		t.Fatalf("run.ExecutionMode = %q, want direct from model-sourced structured contract", run.ExecutionMode)
	}
}

func TestToolRecoveryBudgetByExecutionMode(t *testing.T) {
	t.Parallel()

	component := NewComponent(AgentConfig{
		DefaultModel:            "test-model",
		MaxToolRecoveryAttempts: 3,
		QueueMode:               QueueEnqueue,
	}, NewInMemorySessionStore(), NewInMemoryRunStore(), NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil)

	tests := []struct {
		name string
		mode ExecutionMode
		want int
	}{
		{name: "direct keeps base budget", mode: ExecutionModeDirect, want: 3},
		{name: "planned gets extra budget", mode: ExecutionModePlanned, want: 5},
		{name: "workflow gets extra budget", mode: ExecutionModeWorkflow, want: 5},
		{name: "watch keeps base budget", mode: ExecutionModeWatch, want: 3},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			run := &Run{ExecutionMode: tt.mode}
			if got := component.toolRecoveryBudget(run); got != tt.want {
				t.Fatalf("toolRecoveryBudget(%q) = %d, want %d", tt.mode, got, tt.want)
			}
		})
	}
}

func TestToolRoundBudgetAddsDesktopAndPlannedHeadroom(t *testing.T) {
	t.Parallel()

	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 12,
		QueueMode:     QueueEnqueue,
	}, NewInMemorySessionStore(), NewInMemoryRunStore(), NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil)

	run := &Run{ExecutionMode: ExecutionModePlanned}
	task := &planner.Task{
		Kind:                 planner.TaskExecute,
		Goal:                 "Open Safari, navigate to a page, and take a screenshot.",
		RequiredCapabilities: []string{"desktop"},
	}

	if got := component.toolRoundBudget(run, task, task.Goal); got != 20 {
		t.Fatalf("toolRoundBudget() = %d, want 20", got)
	}
}

func TestToolRoundBudgetAddsResearchHeadroomForRSSRequests(t *testing.T) {
	t.Parallel()

	component := NewComponent(AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 12,
		QueueMode:     QueueEnqueue,
	}, NewInMemorySessionStore(), NewInMemoryRunStore(), NewInMemoryCoordinator(), NewSlidingWindowEngineForTest(), &stubModelClient{}, nil, nil)

	prompt := "读取这个 RSS feed 并总结最近三条：https://example.com/feed.xml"
	run := &Run{
		Preflight: &RunPreflightReport{
			SuggestedDomains: []string{string(DomainNews), string(DomainSearch)},
		},
		TaskContract: &TaskContract{
			Goal:             prompt,
			JobType:          taskContractJobResearch,
			SuggestedDomains: []string{string(DomainNews), string(DomainSearch)},
			CapabilityHints:  []string{"search.news"},
		},
	}
	if got := component.toolRoundBudget(run, nil, prompt); got != 17 {
		t.Fatalf("toolRoundBudget(%q) = %d, want 17", prompt, got)
	}
}
