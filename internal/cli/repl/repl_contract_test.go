package repl

import (
	"strings"
	"testing"
)

func TestREPLViewStateAndPanelContract(t *testing.T) {

	var output strings.Builder
	repl := &REPL{
		service:        &fakeService{},
		renderer:       NewRenderer(&output, false),
		prompt:         &DynamicPrompt{},
		targetName:     "local",
		sessionKey:     "session-contract",
		selectedModel:  "gpt-5.4",
		phase:          PhaseExecutingTools,
		lastToolStatus: "browser.open\nloaded workspace docs",
		layoutMode:     LayoutAuto,
	}

	repl.refreshViewState()
	if repl.viewState.Model != "gpt-5.4" {
		t.Fatalf("viewState.Model = %q, want gpt-5.4", repl.viewState.Model)
	}
	if repl.viewState.Phase != PhaseExecutingTools.String() {
		t.Fatalf("viewState.Phase = %q, want %q", repl.viewState.Phase, PhaseExecutingTools.String())
	}
	if repl.viewState.LastTool != "browser.open" {
		t.Fatalf("viewState.LastTool = %q, want browser.open", repl.viewState.LastTool)
	}

	repl.showPanel("Quality Gate", []string{"All checks passed."}, "Esc back")
	if repl.activePanel != "Quality Gate" {
		t.Fatalf("activePanel = %q, want Quality Gate", repl.activePanel)
	}
	if repl.viewState.ActivePanel != "Quality Gate" {
		t.Fatalf("viewState.ActivePanel = %q, want Quality Gate", repl.viewState.ActivePanel)
	}
	if !strings.Contains(output.String(), "Quality Gate") {
		t.Fatalf("rendered output missing panel title: %q", output.String())
	}

	repl.clearPanel()
	if repl.activePanel != "" {
		t.Fatalf("activePanel = %q, want empty after clear", repl.activePanel)
	}
	if repl.viewState.ActivePanel != "" {
		t.Fatalf("viewState.ActivePanel = %q, want empty after clear", repl.viewState.ActivePanel)
	}
}
