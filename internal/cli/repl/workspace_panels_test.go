package repl

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/acp"
	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/internal/cli/richedit"
)

func TestCompactCommandUsesInteractiveConfirmPanel(t *testing.T) {
	registry := NewCommandRegistry()
	prompter := &panelAwarePrompter{}
	service := &fakeService{
		detail: &SessionDetail{
			Summary: SessionSummary{ID: "sess-1", Key: "ops", Model: "gpt-5.4"},
			Messages: []SessionMessage{
				{Role: "user", Content: "summarize the delivery status"},
				{Role: "assistant", Content: "working on it"},
			},
		},
	}
	repl := &REPL{
		renderer:  NewRenderer(io.Discard, true),
		service:   service,
		commands:  registry,
		prompter:  prompter,
		sessionID: "sess-1",
	}

	if _, err := registry.Execute(context.Background(), repl, "/compact"); err != nil {
		t.Fatalf("Execute(/compact) error = %v", err)
	}
	panel, ok := repl.panelController.(*confirmPanel)
	if !ok || panel == nil {
		t.Fatalf("panelController = %#v, want *confirmPanel", repl.panelController)
	}
	if panel.title != "Compact Conversation" {
		t.Fatalf("panel.title = %q, want %q", panel.title, "Compact Conversation")
	}
	if service.compactCalled {
		t.Fatal("compactCalled = true before confirmation")
	}

	result, err := panel.HandleOverlayKey(richedit.KeyEvent{Action: richedit.ActionInsertRune, Rune: 'y'})
	if err != nil {
		t.Fatalf("HandleOverlayKey(confirm) error = %v", err)
	}
	if strings.TrimSpace(result.Submit) == "" {
		t.Fatalf("confirm result = %#v, want submit command", result)
	}
	if _, err := registry.Execute(context.Background(), repl, result.Submit); err != nil {
		t.Fatalf("Execute(%s) error = %v", result.Submit, err)
	}
	if !service.compactCalled {
		t.Fatal("compactCalled = false, want true after confirmation")
	}
	info, ok := repl.panelController.(*infoPanel)
	if !ok || info == nil {
		t.Fatalf("panelController = %#v, want *infoPanel after compaction", repl.panelController)
	}
	if info.title != "Conversation Compacted" {
		t.Fatalf("info.title = %q, want %q", info.title, "Conversation Compacted")
	}
}

func TestResetCommandUsesInteractiveConfirmPanel(t *testing.T) {
	client := newTestACPClient(t)
	t.Cleanup(func() { client.Close() })

	registry := NewCommandRegistry()
	prompter := &panelAwarePrompter{}
	service := &fakeService{
		detail: &SessionDetail{
			Summary: SessionSummary{ID: "sess-1", Key: "ops", Model: "gpt-5.4"},
		},
	}
	repl := &REPL{
		client:     client,
		renderer:   NewRenderer(io.Discard, true),
		service:    service,
		commands:   registry,
		prompter:   prompter,
		sessionID:  "sess-1",
		sessionKey: "ops",
		prompt:     &DynamicPrompt{},
	}

	if _, err := registry.Execute(context.Background(), repl, "/reset"); err != nil {
		t.Fatalf("Execute(/reset) error = %v", err)
	}
	panel, ok := repl.panelController.(*confirmPanel)
	if !ok || panel == nil {
		t.Fatalf("panelController = %#v, want *confirmPanel", repl.panelController)
	}
	if panel.title != "Reset Conversation" {
		t.Fatalf("panel.title = %q, want %q", panel.title, "Reset Conversation")
	}
	if service.resetCalled {
		t.Fatal("resetCalled = true before confirmation")
	}

	result, err := panel.HandleOverlayKey(richedit.KeyEvent{Action: richedit.ActionInsertRune, Rune: 'y'})
	if err != nil {
		t.Fatalf("HandleOverlayKey(confirm) error = %v", err)
	}
	if strings.TrimSpace(result.Submit) == "" {
		t.Fatalf("confirm result = %#v, want submit command", result)
	}
	if _, err := registry.Execute(context.Background(), repl, result.Submit); err != nil {
		t.Fatalf("Execute(%s) error = %v", result.Submit, err)
	}
	if !service.resetCalled {
		t.Fatal("resetCalled = false, want true after confirmation")
	}
	if repl.sessionKey != "ops" {
		t.Fatalf("sessionKey = %q, want %q after reset", repl.sessionKey, "ops")
	}
}

func TestRenderContextPanelShowsPressureBreakdown(t *testing.T) {
	service := &fakeService{
		detail: &SessionDetail{
			Summary: SessionSummary{ID: "sess-1", Key: "default", Model: "gpt-5.4"},
			Messages: []SessionMessage{
				{Role: "user", Content: "check delivery health"},
				{Role: "assistant", Content: "working on it"},
			},
		},
		contextPressure: &ContextPressureInfo{
			WindowSize:     128000,
			UsedTokens:     96000,
			UsedPercent:    75,
			KeptItems:      9,
			TrimmedItems:   3,
			Recommendation: "consider /compact soon",
		},
		memoryUsage: []MemoryUsageItem{
			{Scope: "saved", Reason: "pinned"},
			{Scope: "project", Reason: "project context"},
			{Scope: "project", Reason: "recalled"},
		},
		memoryItems: []agent.MemoryEntry{{Key: "project.root", Value: "/tmp/hopclaw", ProjectID: "proj-1"}},
		memories:    []agent.MemoryEntry{{Key: "project.root", Value: "/tmp/hopclaw", ProjectID: "proj-1"}},
		project:     &agent.Project{ID: "proj-1", Name: "hopclaw"},
	}
	var output strings.Builder
	repl := &REPL{
		renderer:       NewRenderer(&output, false),
		service:        service,
		sessionID:      "sess-1",
		sessionKey:     "default",
		sessionModel:   "gpt-5.4",
		currentProject: &agent.Project{ID: "proj-1", Name: "hopclaw"},
	}

	if err := repl.renderContextPanel(context.Background()); err != nil {
		t.Fatalf("renderContextPanel() error = %v", err)
	}

	text := output.String()
	for _, want := range []string{"[panel] Context Window", "[CTX 75%] memory 1 · kept 9 · trimmed 3", "Kept items    9", "Trimmed items 3", "Recommendation compact recommended", "Using         using: pinned 1 · project 2 · recalled 1"} {
		if !strings.Contains(text, want) {
			t.Fatalf("context panel missing %q: %q", want, text)
		}
	}
}

func TestRenderContextPanelResolvesBackendSessionByKey(t *testing.T) {
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
		renderer:     NewRenderer(&output, false),
		service:      service,
		sessionID:    "acp-1",
		sessionKey:   "cli-1",
		sessionModel: "gpt-4o",
		modelCache:   []ModelInfo{{ID: "gpt-4o", ContextWindow: 128000}},
	}

	if err := repl.renderContextPanel(context.Background()); err != nil {
		t.Fatalf("renderContextPanel() error = %v", err)
	}

	text := output.String()
	for _, want := range []string{"[panel] Context Window", "Messages      2", "Recommendation no action needed"} {
		if !strings.Contains(text, want) {
			t.Fatalf("renderContextPanel() missing %q: %q", want, text)
		}
	}
	if repl.serviceSessionID != "sess-real" {
		t.Fatalf("serviceSessionID = %q, want %q", repl.serviceSessionID, "sess-real")
	}
}

func TestREPLRunOneShotSessionPanelsResolveBackendSessionByKey(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	tests := []struct {
		name    string
		initial string
		want    string
	}{
		{name: "history", initial: "/history", want: "[panel] Recent History"},
		{name: "context", initial: "/context", want: "[panel] Context Window"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := acp.NewServer(fakeGateway{}, acp.ServerConfig{DefaultSessionKey: "cli-1"})
			client, err := acp.NewInProcessClient(context.Background(), server)
			if err != nil {
				t.Fatalf("NewInProcessClient() error = %v", err)
			}
			t.Cleanup(func() { client.Close() })

			service := &fakeService{
				models:   []ModelInfo{{ID: "gpt-4o", ContextWindow: 128000}},
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
			var status strings.Builder
			var assistant strings.Builder
			repl, err := New(Config{
				Client:         client,
				Service:        service,
				Prompter:       &scriptedPrompter{},
				Renderer:       NewSplitRenderer(&status, &assistant, false),
				History:        NewHistory("", 10),
				SessionKey:     "cli-1",
				InitialMessage: tt.initial,
				OneShot:        true,
				Model:          "gpt-4o",
			})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			repl.sessionID = "acp-1"

			if err := repl.Run(context.Background()); err != nil {
				t.Fatalf("Run() error = %v", err)
			}

			if !strings.Contains(status.String(), tt.want) {
				t.Fatalf("status output = %q, want %q", status.String(), tt.want)
			}
			if repl.serviceSessionID != "sess-real" {
				t.Fatalf("serviceSessionID = %q, want %q", repl.serviceSessionID, "sess-real")
			}
			if assistant.Len() != 0 {
				t.Fatalf("assistant output = %q, want no model submission", assistant.String())
			}
		})
	}
}
