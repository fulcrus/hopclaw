package agent

import (
	"strings"
	"testing"
)

func TestBuildToolGuidancePrompt_CurrentInfoWithAvailableTools(t *testing.T) {
	t.Parallel()

	prompt := BuildToolGuidancePrompt(ToolIntent{
		MessageType:         MessageTypeKnowledge,
		RequiresCurrentInfo: true,
	}, []ToolDefinition{
		{Name: "search.web"},
		{Name: "search.news"},
		{Name: "news.digest"},
		{Name: "net.fetch"},
	})

	if !strings.Contains(prompt, "Current Information Rule") {
		t.Fatalf("prompt missing current information rule:\n%s", prompt)
	}
	if !strings.Contains(prompt, "MUST verify") {
		t.Fatalf("prompt missing MUST verify guidance:\n%s", prompt)
	}
	if !strings.Contains(prompt, "`news.digest`") {
		t.Fatalf("prompt missing news.digest preference:\n%s", prompt)
	}
	if !strings.Contains(prompt, "`search.web` first") {
		t.Fatalf("prompt missing search.web-first guidance:\n%s", prompt)
	}
	if !strings.Contains(prompt, "`net.fetch` only when you already have a trustworthy public URL") {
		t.Fatalf("prompt missing constrained net.fetch guidance:\n%s", prompt)
	}
}

func TestBuildToolGuidancePrompt_CurrentInfoWithoutAvailableTools(t *testing.T) {
	t.Parallel()

	prompt := BuildToolGuidancePrompt(ToolIntent{
		MessageType:         MessageTypeKnowledge,
		RequiresCurrentInfo: true,
	}, []ToolDefinition{{Name: "fs.read"}})

	if !strings.Contains(prompt, "may be outdated") {
		t.Fatalf("prompt missing outdated disclaimer guidance:\n%s", prompt)
	}
	if !strings.Contains(prompt, "exact calendar dates") {
		t.Fatalf("prompt missing exact date guidance:\n%s", prompt)
	}
}

func TestToolGuidanceIntentForRunUsesPersistedCurrentInfo(t *testing.T) {
	t.Parallel()

	intent := toolGuidanceIntentForRun(&Run{
		Triage: &RunTriageTrace{RequiresCurrentInfo: true},
	}, "整理成表格")
	if !intent.RequiresCurrentInfo {
		t.Fatalf("intent = %#v, want requires_current_info=true", intent)
	}
}

func TestToolGuidanceIntentForRunFallsBackToCurrentInfoDomains(t *testing.T) {
	t.Parallel()

	intent := toolGuidanceIntentForRun(&Run{
		Preflight: &RunPreflightReport{
			DetectedDomains: []string{"news"},
		},
	}, "整理成表格")
	if !intent.RequiresCurrentInfo {
		t.Fatalf("intent = %#v, want requires_current_info=true", intent)
	}
}
