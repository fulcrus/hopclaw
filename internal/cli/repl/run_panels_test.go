package repl

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestRunCommandRendersRunDetail(t *testing.T) {
	var output bytes.Buffer
	service := &fakeService{
		runDetails: map[string]*RunDetail{
			"run-graph": {
				Run: RunSummary{
					ID:         "run-graph",
					SessionID:  "sess-1",
					SessionKey: "ops-incident",
					Status:     "waiting_approval",
					Target:     "local",
					Attention:  "approval",
				},
				Tool:   "exec.shell",
				Scope:  "workspace_write",
				Output: "cleanup cache and redrive dead-letter",
				Semantic: &RunSemanticDetails{
					Language:            "es-Latn",
					RequiresCurrentInfo: true,
					SuggestedDomains:    []string{"browser", "fs"},
					JobType:             "report",
					TargetSummary:       "docs/tmp/resumen.md",
					TriageReady:         true,
					TaskContractReady:   true,
					Reason:              "fresh_page_state",
				},
			},
		},
	}
	repl := &REPL{
		renderer:  NewRenderer(&output, false),
		service:   service,
		sessionID: "sess-1",
		commands:  NewCommandRegistry(),
	}

	if _, err := repl.commands.Execute(context.Background(), repl, "/run run-graph"); err != nil {
		t.Fatalf("Execute(/run) error = %v", err)
	}

	text := output.String()
	for _, want := range []string{"[panel] Run run-graph", "status: waiting_approval", "Semantic Signal", "language: es-Latn", "Output", "cleanup cache and redrive dead-letter"} {
		if !strings.Contains(text, want) {
			t.Fatalf("run command output missing %q: %q", want, text)
		}
	}
}

func TestRenderHistoryResolvesBackendSessionByKey(t *testing.T) {
	service := &fakeService{
		sessions: []SessionSummary{{ID: "sess-real", Key: "cli-1", Model: "gpt-4o", MessageCount: 2}},
		sessionByID: map[string]*SessionDetail{
			"sess-real": {
				Summary: SessionSummary{ID: "sess-real", Key: "cli-1", Model: "gpt-4o", MessageCount: 2},
				Messages: []SessionMessage{
					{Role: "user", Content: "hello"},
					{Role: "assistant", Content: "world"},
				},
			},
		},
	}
	var output strings.Builder
	repl := &REPL{
		renderer:   NewRenderer(&output, false),
		service:    service,
		sessionID:  "acp-1",
		sessionKey: "cli-1",
	}

	if err := repl.renderHistory(context.Background()); err != nil {
		t.Fatalf("renderHistory() error = %v", err)
	}

	text := output.String()
	for _, want := range []string{"[panel] Recent History", "[user]", "hello", "[assistant]", "world"} {
		if !strings.Contains(text, want) {
			t.Fatalf("renderHistory() missing %q: %q", want, text)
		}
	}
	if repl.serviceSessionID != "sess-real" {
		t.Fatalf("serviceSessionID = %q, want %q", repl.serviceSessionID, "sess-real")
	}
}

func TestRunsCommandInteractivePickerSupportsForegroundAndBackground(t *testing.T) {
	registry := NewCommandRegistry()
	prompter := &panelAwarePrompter{}
	service := &fakeService{
		runs: []RunSummary{
			{
				ID:         "run-bg",
				SessionID:  "sess-1",
				SessionKey: "ops",
				Status:     "running",
				Phase:      "executing_tools",
				ToolName:   "exec.shell",
				Target:     "prod-eu",
			},
			{
				ID:         "run-done",
				SessionID:  "sess-1",
				SessionKey: "ops",
				Status:     "completed",
				Phase:      "completed",
				ToolName:   "fs.read",
				Target:     "prod-eu",
			},
		},
	}
	repl := &REPL{
		renderer:   NewRenderer(io.Discard, true),
		service:    service,
		commands:   registry,
		prompter:   prompter,
		sessionID:  "sess-1",
		sessionKey: "ops",
		targetName: "prod-eu",
		running:    true,
	}

	if _, err := registry.Execute(context.Background(), repl, "/runs recent"); err != nil {
		t.Fatalf("Execute(/runs recent) error = %v", err)
	}
	panel, ok := repl.panelController.(*selectionPanel)
	if !ok || panel == nil {
		t.Fatalf("panelController = %#v, want *selectionPanel", repl.panelController)
	}
	if !strings.Contains(panel.actions, "f foreground") || !strings.Contains(panel.actions, "b background") {
		t.Fatalf("panel.actions = %q, want run control actions", panel.actions)
	}

	if _, err := panel.hotkeys['b'](panel, firstPanelItem(panel.items)); err != nil {
		t.Fatalf("background hotkey error = %v", err)
	}
	if !repl.isBackgroundRun("run-bg") {
		t.Fatalf("backgroundRuns = %#v, want run-bg added", repl.backgroundRuns)
	}
	if !strings.Contains(panel.status, "Backgrounded run-bg.") {
		t.Fatalf("panel.status = %q, want background status", panel.status)
	}

	if _, err := panel.hotkeys['f'](panel, firstPanelItem(panel.items)); err != nil {
		t.Fatalf("foreground hotkey error = %v", err)
	}
	if repl.currentRunID != "run-bg" || repl.foregroundRunID != "run-bg" {
		t.Fatalf("foreground state = (%q, %q), want run-bg", repl.currentRunID, repl.foregroundRunID)
	}
}

func TestRunPanelRowProjectsNamedLocalRuntimeAsLocal(t *testing.T) {
	row := runPanelRow(RunSummary{ID: "run-1", Status: "running"}, nil, "", "", "local-dev", "local")
	if !strings.Contains(row, "local:local") {
		t.Fatalf("runPanelRow() = %q, want local target label", row)
	}
	if strings.Contains(row, "remote") {
		t.Fatalf("runPanelRow() = %q, want no remote label for named local runtime", row)
	}
}

func TestHandleRunKeyBackgroundsCurrentRun(t *testing.T) {
	var output strings.Builder
	service := &fakeService{
		supervisor: &SupervisorSnapshot{
			ActiveRunCount: 1,
			Items: []RunSummary{{
				ID:        "run-1",
				SessionID: "sess-1",
				Status:    "running",
				Phase:     "executing_tools",
			}},
		},
		runDetails: map[string]*RunDetail{
			"run-1": {Run: RunSummary{ID: "run-1", SessionID: "sess-1", Status: "running", Phase: "executing_tools"}},
		},
	}
	repl := &REPL{
		renderer:     NewRenderer(&output, false),
		service:      service,
		sessionID:    "sess-1",
		sessionKey:   "ops",
		currentRunID: "run-1",
		running:      true,
		phase:        PhaseDelivering,
	}

	err := repl.handleRunKey(context.Background(), 'b')
	if !errors.Is(err, errREPLBackgrounded) {
		t.Fatalf("handleRunKey('b') error = %v, want %v", err, errREPLBackgrounded)
	}
	if repl.running {
		t.Fatal("run should no longer be foreground-running after backgrounding")
	}
	if repl.currentRunID != "" {
		t.Fatalf("currentRunID = %q, want cleared after backgrounding", repl.currentRunID)
	}
	if !repl.isBackgroundRun("run-1") {
		t.Fatalf("backgroundRuns = %#v, want run-1 tracked", repl.backgroundRuns)
	}
	text := output.String()
	for _, want := range []string{"[task] Background Run · run-1", "Backgrounded run run-1."} {
		if !strings.Contains(text, want) {
			t.Fatalf("background output missing %q: %q", want, text)
		}
	}
}

func TestRefreshViewStateUsesBackgroundProjection(t *testing.T) {
	repl := &REPL{
		renderer: NewRenderer(&strings.Builder{}, false),
		supervisorSnapshot: &SupervisorSnapshot{
			ActiveRunCount:     1,
			BackgroundRunCount: 1,
			PausedRunCount:     1,
			AttentionCount:     1,
			Items: []RunSummary{{
				ID:        "run-1",
				SessionID: "sess-1",
				Status:    "running",
				Phase:     "executing_tools",
				Attention: "approval",
			}},
		},
		backgroundRuns: []string{"run-1"},
		lastRunID:      "run-1",
	}

	repl.refreshViewState()

	if repl.viewState.ForegroundRunCount != 0 {
		t.Fatalf("ForegroundRunCount = %d, want 0", repl.viewState.ForegroundRunCount)
	}
	if repl.viewState.BackgroundRunCount != 1 {
		t.Fatalf("BackgroundRunCount = %d, want 1", repl.viewState.BackgroundRunCount)
	}
	if repl.viewState.QueueDepth != 1 {
		t.Fatalf("QueueDepth = %d, want 1", repl.viewState.QueueDepth)
	}
	if repl.viewState.AttentionPrimary != "approval" {
		t.Fatalf("AttentionPrimary = %q, want %q", repl.viewState.AttentionPrimary, "approval")
	}
}

func TestRenderLastRunShowsToolTimeline(t *testing.T) {
	var output strings.Builder
	service := &fakeService{
		runsBySession: map[string][]RunSummary{"sess-1": {{
			ID:        "run-1",
			SessionID: "sess-1",
			Status:    "completed",
			Phase:     "completed",
		}}},
		runDetails: map[string]*RunDetail{
			"run-1": {
				Run: RunSummary{
					ID:        "run-1",
					SessionID: "sess-1",
					Status:    "completed",
					Phase:     "completed",
				},
				Output: "final answer",
			},
		},
	}
	repl := &REPL{
		renderer:  NewRenderer(&output, false),
		service:   service,
		sessionID: "sess-1",
		lastRunID: "run-1",
		lastTimeline: []ToolTimelineEntry{
			{Name: "fs.read", Status: "ok", Summary: "read plan", Duration: 1500 * time.Millisecond},
			{Name: "exec.shell", Status: "error", Summary: "permission denied", Duration: 250 * time.Millisecond},
		},
	}

	if err := repl.renderLastRun(context.Background()); err != nil {
		t.Fatalf("renderLastRun() error = %v", err)
	}

	text := output.String()
	for _, want := range []string{"Timeline", "1. fs.read · ok · 1.5s", "read plan", "2. exec.shell · error · 250ms"} {
		if !strings.Contains(text, want) {
			t.Fatalf("renderLastRun() missing %q: %q", want, text)
		}
	}
}

func TestRenderLastRunUsesResolvedBackendSessionID(t *testing.T) {
	var output strings.Builder
	service := &fakeService{
		sessions: []SessionSummary{{ID: "sess-real", Key: "cli-1", Model: "gpt-4o"}},
		sessionByID: map[string]*SessionDetail{
			"sess-real": {
				Summary:  SessionSummary{ID: "sess-real", Key: "cli-1", Model: "gpt-4o"},
				Messages: []SessionMessage{{Role: "user", Content: "show last run"}},
			},
		},
		runsBySession: map[string][]RunSummary{
			"sess-real": {{
				ID:         "run-1",
				SessionID:  "sess-real",
				SessionKey: "cli-1",
				Status:     "completed",
				Phase:      "completed",
			}},
		},
		runDetails: map[string]*RunDetail{
			"run-1": {
				Run: RunSummary{
					ID:         "run-1",
					SessionID:  "sess-real",
					SessionKey: "cli-1",
					Status:     "completed",
					Phase:      "completed",
				},
				Output: "final answer",
			},
		},
	}
	repl := &REPL{
		renderer:   NewRenderer(&output, false),
		service:    service,
		sessionID:  "acp-1",
		sessionKey: "cli-1",
	}

	if err := repl.renderLastRun(context.Background()); err != nil {
		t.Fatalf("renderLastRun() error = %v", err)
	}
	if len(service.listRunsRequests) == 0 || service.listRunsRequests[0] != "sess-real" {
		t.Fatalf("listRunsRequests = %#v, want first request for sess-real", service.listRunsRequests)
	}
	if !strings.Contains(output.String(), "run-1") {
		t.Fatalf("output = %q, want resolved run details", output.String())
	}
}

func TestRenderRunDetailShowsSupervisorBlocks(t *testing.T) {
	var output strings.Builder
	service := &fakeService{
		runDetails: map[string]*RunDetail{
			"run-graph": {
				Run: RunSummary{
					ID:        "run-graph",
					SessionID: "sess-1",
					Status:    "running",
					Phase:     "executing_tools",
				},
				Scope: "automation=weekday-briefing",
				ScopeDetails: &RunScopeDetails{
					SideEffectScope: "workspace_write",
					Destructive:     true,
					Resources:       []string{"file:plan.md", "webhook:ops"},
					Summary:         "requires approval for remote write",
				},
				Workflow: &RunWorkflowDetails{
					Mode:              "workflow",
					ContinuationIndex: 2,
					TotalRoundsUsed:   7,
					Yielded:           false,
				},
				Delegation: &RunDelegationDetails{
					Enabled:         true,
					ParallelTasks:   3,
					SerialFallback:  1,
					SideEffectClass: "workspace_write",
				},
				ExecutionGraph: &RunExecutionGraphDetails{
					SingleSession:  true,
					SessionLocking: true,
					Tasks: []RunExecutionTask{{
						ID:              "compare-deliveries",
						Title:           "compare deliveries",
						Status:          "running",
						AttemptCount:    2,
						MergeStrategy:   "task_order",
						SideEffectScope: "workspace_write",
						ResourceKeys:    []string{"file:plan.md"},
						Summary:         "diffing receipts",
					}},
				},
			},
		},
	}
	repl := &REPL{
		renderer:  NewRenderer(&output, false),
		service:   service,
		sessionID: "sess-1",
	}

	if err := repl.renderRunDetail(context.Background(), "run-graph"); err != nil {
		t.Fatalf("renderRunDetail() error = %v", err)
	}

	text := output.String()
	for _, want := range []string{"Scope", "destructive: yes", "Workflow", "continuation: 2 / total rounds used: 7", "Delegation", "parallel tasks: 3", "Execution Graph", "[running] compare deliveries"} {
		if !strings.Contains(text, want) {
			t.Fatalf("run detail missing %q: %q", want, text)
		}
	}
}

func TestResumeCommandSwitchesToPastRunSession(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	registry := NewCommandRegistry()
	client := newTestACPClient(t)
	defer client.Close()

	service := &fakeService{
		detail: &SessionDetail{Summary: SessionSummary{ID: "sess-2", Key: "ops-session", Model: "gpt-4o"}},
		runDetails: map[string]*RunDetail{
			"run-2": {
				Run: RunSummary{
					ID:        "run-2",
					SessionID: "sess-2",
					Status:    "failed",
					Phase:     "error",
				},
				Output: "fix the deploy",
			},
		},
	}
	var output strings.Builder
	repl, err := New(Config{
		Client:     client,
		Service:    service,
		Prompter:   &scriptedPrompter{},
		Renderer:   NewRenderer(&output, false),
		History:    NewHistory("", 10),
		SessionKey: "default",
		Version:    "test",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if _, err := registry.Execute(context.Background(), repl, "/continue run-2"); err != nil {
		t.Fatalf("Execute(/continue run-2) error = %v", err)
	}
	if repl.sessionKey != "ops-session" {
		t.Fatalf("sessionKey = %q, want %q", repl.sessionKey, "ops-session")
	}
	if !strings.Contains(output.String(), "Resumed work from task run-2 in conversation ops-session.") {
		t.Fatalf("output = %q", output.String())
	}
}
