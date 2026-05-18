package agent

import (
	"strings"
	"testing"
)

func TestUnavailableToolCallsFiltersToCurrentTurn(t *testing.T) {
	t.Parallel()

	calls := []ToolCall{
		{ID: "call-1", Name: "browser.open"},
		{ID: "call-2", Name: "skill.ensure"},
	}
	available := []ToolDefinition{
		{Name: "browser.open"},
		{Name: "browser.select"},
	}

	disallowed := unavailableToolCalls(calls, available)
	if len(disallowed) != 1 || disallowed[0].Name != "skill.ensure" {
		t.Fatalf("unavailableToolCalls = %#v, want only skill.ensure", disallowed)
	}
}

func TestUnavailableToolCallsSkipsWhenAvailabilityUnknown(t *testing.T) {
	t.Parallel()

	calls := []ToolCall{
		{ID: "call-1", Name: "fs.read"},
	}

	if disallowed := unavailableToolCalls(calls, nil); len(disallowed) != 0 {
		t.Fatalf("unavailableToolCalls(nil) = %#v, want nil", disallowed)
	}
	if disallowed := unavailableToolCalls(calls, []ToolDefinition{}); len(disallowed) != 0 {
		t.Fatalf("unavailableToolCalls(empty) = %#v, want nil", disallowed)
	}
	if disallowed := unavailableToolCalls(calls, []ToolDefinition{{Name: "  "}}); len(disallowed) != 0 {
		t.Fatalf("unavailableToolCalls(blank) = %#v, want nil", disallowed)
	}
}

func TestBuildUnavailableToolCallResultsIncludesAvailableTools(t *testing.T) {
	t.Parallel()

	results := buildUnavailableToolCallResults([]ToolCall{
		{ID: "call-2", Name: "skill.ensure"},
	}, []ToolDefinition{
		{Name: "browser.open"},
		{Name: "browser.select"},
		{Name: "browser.wait"},
	})
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].Error == nil || results[0].Error.Code != "tool_not_available" {
		t.Fatalf("result error = %#v, want tool_not_available", results[0].Error)
	}
	if !strings.Contains(results[0].TranscriptText, "browser.open") || !strings.Contains(results[0].TranscriptText, "browser.select") {
		t.Fatalf("transcript = %q, want available tools listed", results[0].TranscriptText)
	}
}

func TestBuildUnavailableToolCallResultsSkillEnsureUsesBrowserRecoveryHint(t *testing.T) {
	t.Parallel()

	results := buildUnavailableToolCallResults([]ToolCall{
		{ID: "call-2", Name: "skill.ensure"},
	}, []ToolDefinition{
		{Name: "browser.open"},
		{Name: "browser.snapshot"},
	})
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if !strings.Contains(results[0].Actions[0].Target, "available browser tools") {
		t.Fatalf("recovery hint = %q, want browser-tools guidance", results[0].Actions[0].Target)
	}
}
