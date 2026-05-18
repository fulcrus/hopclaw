package repl

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/acp"
)

func TestTerminalScenarioControlCommands(t *testing.T) {
	registry := NewCommandRegistry()
	service := &fakeService{
		models: []ModelInfo{
			{ID: "gpt-4o", ContextWindow: 128000, SupportsThinking: true},
			{ID: "gpt-4o-mini", ContextWindow: 128000},
		},
	}
	var output strings.Builder
	repl := &REPL{
		renderer:     NewRenderer(&output, false),
		service:      service,
		targetName:   "local",
		sessionKey:   "default",
		sessionModel: "gpt-4o",
		layoutMode:   LayoutAuto,
		commands:     registry,
		prompt:       &DynamicPrompt{},
	}

	if _, err := registry.Execute(context.Background(), repl, "/status"); err != nil {
		t.Fatalf("Execute(/status) error = %v", err)
	}
	if _, err := registry.Execute(context.Background(), repl, "/view compact"); err != nil {
		t.Fatalf("Execute(/view compact) error = %v", err)
	}
	if repl.layoutMode != LayoutCompact {
		t.Fatalf("layoutMode = %q, want %q", repl.layoutMode, LayoutCompact)
	}
	if _, err := registry.Execute(context.Background(), repl, "/view auto"); err != nil {
		t.Fatalf("Execute(/view auto) error = %v", err)
	}
	if repl.layoutMode != LayoutAuto {
		t.Fatalf("layoutMode = %q, want %q", repl.layoutMode, LayoutAuto)
	}
	if _, err := registry.Execute(context.Background(), repl, "/model"); err != nil {
		t.Fatalf("Execute(/model) error = %v", err)
	}
	if _, err := registry.Execute(context.Background(), repl, "/model gpt-4o-mini"); err != nil {
		t.Fatalf("Execute(/model gpt-4o-mini) error = %v", err)
	}
	if repl.selectedModel != "gpt-4o-mini" {
		t.Fatalf("selectedModel = %q, want %q", repl.selectedModel, "gpt-4o-mini")
	}
	if _, err := registry.Execute(context.Background(), repl, "/think on"); err != nil {
		t.Fatalf("Execute(/think on) error = %v", err)
	}
	if !repl.thinking {
		t.Fatal("thinking = false, want true after /think on")
	}
	if _, err := registry.Execute(context.Background(), repl, "/think"); err != nil {
		t.Fatalf("Execute(/think) error = %v", err)
	}
	if _, err := registry.Execute(context.Background(), repl, "/view sideways"); err == nil {
		t.Fatal("Execute(/view sideways) error = nil, want usage error")
	}

	exitResult, err := registry.Execute(context.Background(), repl, "/exit")
	if err != nil {
		t.Fatalf("Execute(/exit) error = %v", err)
	}
	if !exitResult.Exit {
		t.Fatal("Execute(/exit) did not request exit")
	}
	quitResult, err := registry.Execute(context.Background(), repl, "/quit")
	if err != nil {
		t.Fatalf("Execute(/quit) error = %v", err)
	}
	if !quitResult.Exit {
		t.Fatal("Execute(/quit) did not request exit")
	}

	text := output.String()
	for _, want := range []string{
		"[system] local · conversation default · model gpt-4o · status ready · phase idle",
		"[system] View set to compact.",
		"[system] View set to auto.",
		"[panel] Models",
		"gpt-4o",
		"gpt-4o-mini",
		"[system] Model set to gpt-4o-mini",
		"Thinking mode enabled",
		"[system] Thinking mode: on",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("control command output missing %q: %q", want, text)
		}
	}
}

func TestTerminalScenarioPickerCommandsRenderFallbackPanels(t *testing.T) {
	registry := NewCommandRegistry()
	service := &fakeService{
		sessions: []SessionSummary{
			{ID: "sess-default", Key: "default", Model: "gpt-4o", MessageCount: 12},
			{ID: "sess-ops", Key: "ops-incident", Model: "gpt-5.4", MessageCount: 40},
		},
		models: []ModelInfo{
			{ID: "gpt-4o", ContextWindow: 128000, SupportsThinking: true},
			{ID: "gpt-5.4", ContextWindow: 200000, SupportsThinking: true},
		},
	}
	var output strings.Builder
	repl := &REPL{
		renderer:     NewRenderer(&output, false),
		service:      service,
		sessionID:    "sess-default",
		sessionKey:   "default",
		sessionModel: "gpt-4o",
		commands:     registry,
		prompt:       &DynamicPrompt{},
	}

	if _, err := registry.Execute(context.Background(), repl, "/session"); err != nil {
		t.Fatalf("Execute(/session) error = %v", err)
	}
	if _, err := registry.Execute(context.Background(), repl, "/model"); err != nil {
		t.Fatalf("Execute(/model) error = %v", err)
	}

	text := output.String()
	for _, want := range []string{
		"[panel] Conversations",
		"default",
		"ops-incident",
		"12 turns",
		"Actions: Enter switch conversation",
		"[panel] Models",
		"128k ctx",
		"thinking yes",
		"Actions: Enter choose model  t toggle think  Esc back",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("picker output missing %q: %q", want, text)
		}
	}
}

func TestTerminalScenarioBackgroundAndForegroundCommands(t *testing.T) {
	registry := NewCommandRegistry()
	service := &fakeService{}
	var output strings.Builder
	repl := &REPL{
		renderer:     NewRenderer(&output, false),
		service:      service,
		sessionID:    "sess-1",
		sessionKey:   "default",
		targetName:   "local",
		currentRunID: "run-1",
		running:      true,
		phase:        PhaseDelivering,
		commands:     registry,
		prompt:       &DynamicPrompt{},
	}

	if _, err := registry.Execute(context.Background(), repl, "/background"); err != nil {
		t.Fatalf("Execute(/background) error = %v", err)
	}
	if repl.running {
		t.Fatal("running = true, want false after /background")
	}
	if repl.currentRunID != "" {
		t.Fatalf("currentRunID = %q, want cleared after /background", repl.currentRunID)
	}
	if !repl.isBackgroundRun("run-1") {
		t.Fatalf("backgroundRuns = %#v, want run-1 tracked", repl.backgroundRuns)
	}

	if _, err := registry.Execute(context.Background(), repl, "/foreground run-1"); err != nil {
		t.Fatalf("Execute(/foreground run-1) error = %v", err)
	}
	if repl.currentRunID != "run-1" || repl.foregroundRunID != "run-1" {
		t.Fatalf("foreground state = (%q, %q), want run-1 foreground", repl.currentRunID, repl.foregroundRunID)
	}
	if repl.isBackgroundRun("run-1") {
		t.Fatalf("backgroundRuns = %#v, want run-1 removed after foreground", repl.backgroundRuns)
	}

	text := output.String()
	for _, want := range []string{
		"[task] Background Run · run-1",
		"[system] Backgrounded run run-1.",
		"[system] Foreground focus moved to run run-1.",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("run control output missing %q: %q", want, text)
		}
	}
}

func TestTerminalScenarioHistoryContextPauseAndClearCommands(t *testing.T) {
	registry := NewCommandRegistry()
	service := &fakeService{
		detail: &SessionDetail{
			Summary: SessionSummary{ID: "sess-1", Key: "default", Model: "gpt-4o"},
			Messages: []SessionMessage{
				{Role: "user", Content: "show me the plan"},
				{Role: "assistant", Content: "working on it"},
			},
		},
	}
	var output strings.Builder
	repl := &REPL{
		renderer:     NewRenderer(&output, false),
		service:      service,
		sessionID:    "sess-1",
		sessionKey:   "default",
		sessionModel: "gpt-4o",
		modelCache:   []ModelInfo{{ID: "gpt-4o", ContextWindow: 128000}},
		commands:     registry,
		prompt:       &DynamicPrompt{},
	}

	for _, input := range []string{"/history", "/context", "/pause", "/clear"} {
		if _, err := registry.Execute(context.Background(), repl, input); err != nil {
			t.Fatalf("Execute(%s) error = %v", input, err)
		}
	}

	text := output.String()
	for _, want := range []string{
		"[panel] Recent History",
		"[user]",
		"[panel] Context Window",
		"Recommendation",
		"[system] No active task.",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("session surface output missing %q: %q", want, text)
		}
	}
	if strings.Contains(text, "HopClaw") {
		t.Fatalf("/clear should not re-render banner text in fallback mode: %q", text)
	}
}

func TestTerminalScenarioCompletionAndErrorSnapshots(t *testing.T) {
	t.Run("completed", func(t *testing.T) {
		var output strings.Builder
		repl := &REPL{
			renderer:        NewRenderer(&output, false),
			sessionID:       "sess-1",
			sessionKey:      "default",
			targetName:      "local",
			currentRunID:    "run-1",
			runStartedAt:    time.Now().Add(-41 * time.Second),
			snapshotTracker: snapshotTracker{},
			prompt:          &DynamicPrompt{},
		}

		if err := repl.handleUpdate(acp.SessionUpdateNotification{
			Status: acp.SessionCompleted,
			Usage:  &acp.UsageInfo{PromptTokens: 16, CompletionTokens: 2},
		}); err != nil {
			t.Fatalf("handleUpdate(completed) error = %v", err)
		}

		text := output.String()
		for _, want := range []string{
			"[task] Task Completed · run-1",
			"label=Exec content=Primary result is ready status=completed",
			"[card] Task Completed",
			"Duration  00:41",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("completion output missing %q: %q", want, text)
			}
		}
	})

	t.Run("recovery", func(t *testing.T) {
		var output strings.Builder
		repl := &REPL{
			renderer:        NewRenderer(&output, false),
			sessionID:       "sess-2",
			sessionKey:      "ops",
			targetName:      "prod-eu",
			currentRunID:    "run-2",
			runStartedAt:    time.Now().Add(-5 * time.Second),
			snapshotTracker: snapshotTracker{},
			prompt:          &DynamicPrompt{},
		}

		if err := repl.handleUpdate(acp.SessionUpdateNotification{
			Status: acp.SessionError,
			Error:  "gateway unreachable",
		}); err != nil {
			t.Fatalf("handleUpdate(error recovery) error = %v", err)
		}

		text := output.String()
		for _, want := range []string{
			"[task] Task Failed · run-2",
			"[error] gateway unreachable",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("recovery output missing %q: %q", want, text)
			}
		}
		if strings.Contains(text, "label=Next content=(none)") {
			t.Fatalf("recovery output unexpectedly used failed snapshot: %q", text)
		}
	})

	t.Run("failed", func(t *testing.T) {
		var output strings.Builder
		repl := &REPL{
			renderer:        NewRenderer(&output, false),
			sessionID:       "sess-3",
			sessionKey:      "default",
			targetName:      "local",
			currentRunID:    "run-3",
			runStartedAt:    time.Now().Add(-5 * time.Second),
			snapshotTracker: snapshotTracker{},
			prompt:          &DynamicPrompt{},
		}

		if err := repl.handleUpdate(acp.SessionUpdateNotification{
			Status: acp.SessionError,
			Error:  "compiler panicked",
		}); err != nil {
			t.Fatalf("handleUpdate(error failed) error = %v", err)
		}

		text := output.String()
		for _, want := range []string{
			"[task] Task Failed · run-3",
			"[error] compiler panicked",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("failed output missing %q: %q", want, text)
			}
		}
		if strings.Contains(text, "switch remote or keep working local") {
			t.Fatalf("failed output unexpectedly used recovery snapshot: %q", text)
		}
	})
}

func TestTerminalScenarioAutomationPromotionSnapshot(t *testing.T) {
	var output strings.Builder
	repl := &REPL{
		renderer:        NewRenderer(&output, false),
		sessionID:       "sess-1",
		sessionKey:      "ops",
		targetName:      "local",
		lastRunID:       "run-9",
		snapshotTracker: snapshotTracker{},
	}

	repl.renderAutomationPromotionSnapshot("run-9", "Prepare weekday ops briefing", "slack:#ops")

	text := output.String()
	for _, want := range []string{
		"[task] Suggested Automation · run-9",
		"label=Recovery content=Prepare weekday ops briefing status=confirmed",
		"label=Delivery content=slack:#ops status=attention needed",
		"Actions: /promote",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("automation promotion snapshot missing %q: %q", want, text)
		}
	}
}
