package runtime

import (
	"strings"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	capprofile "github.com/fulcrus/hopclaw/capability/profile"
	"github.com/fulcrus/hopclaw/resultmodel"
)

func TestMessageInRunWindowDoesNotOverlapAdjacentRuns(t *testing.T) {
	t.Parallel()

	start := time.Unix(100, 0).UTC()
	end := start.Add(500 * time.Millisecond)
	if !messageInRunWindow(start.Add(250*time.Millisecond), start, end) {
		t.Fatal("expected message inside run window to match")
	}
	if messageInRunWindow(end.Add(100*time.Millisecond), start, end) {
		t.Fatal("expected message outside exact run window to be excluded")
	}
}

func TestSummarizeRunResultPrefersFinalOutputOverToolFailure(t *testing.T) {
	t.Parallel()

	result := &RunResult{
		Output: "Example Domain",
	}
	toolResults := []resultmodel.ToolResult{{
		Summary:        "tool execution failed",
		TranscriptText: "error: navigation timeout",
	}}

	if got := summarizeRunResult(result, toolResults); got != "Example Domain" {
		t.Fatalf("summarizeRunResult() = %q, want %q", got, "Example Domain")
	}
}

func TestSummarizeRunResultDoesNotExposeToolOutputWhileRunIsRunning(t *testing.T) {
	t.Parallel()

	result := &RunResult{
		Status: agent.RunRunning,
	}
	toolResults := []resultmodel.ToolResult{{
		Summary:        `{ "ok": true }`,
		TranscriptText: `{ "ok": true }`,
	}}

	if got := summarizeRunResult(result, toolResults); got != "running" {
		t.Fatalf("summarizeRunResult() = %q, want %q", got, "running")
	}
}

func TestDeriveRunOutputDoesNotFallbackToRawToolJSONOnTerminalRun(t *testing.T) {
	t.Parallel()

	toolResults := []resultmodel.ToolResult{{
		Summary:        `{"command":"go test ./...","stdout":"FAIL","stderr":"","exit_code":1}`,
		TranscriptText: `{"command":"go test ./...","stdout":"FAIL","stderr":"","exit_code":1}`,
	}}

	if got := deriveRunOutput(nil, "run-1", toolResults, agent.RunCompleted); got != "" {
		t.Fatalf("deriveRunOutput() = %q, want empty string", got)
	}
}

func TestDeriveRunOutputAllowsNaturalLanguageToolSummaryFallback(t *testing.T) {
	t.Parallel()

	toolResults := []resultmodel.ToolResult{{
		Summary:        "Page title is Example Domain.",
		TranscriptText: "Page title is Example Domain.",
	}}

	if got := deriveRunOutput(nil, "run-1", toolResults, agent.RunCompleted); got != "Page title is Example Domain." {
		t.Fatalf("deriveRunOutput() = %q, want natural-language summary", got)
	}
}

func TestSummarizeRunResultDoesNotExposeRawToolOutputAfterCompletion(t *testing.T) {
	t.Parallel()

	result := &RunResult{
		Status: agent.RunCompleted,
	}
	toolResults := []resultmodel.ToolResult{{
		Summary:        `{"command":"go test ./...","stdout":"FAIL","stderr":"","exit_code":1}`,
		TranscriptText: `{"command":"go test ./...","stdout":"FAIL","stderr":"","exit_code":1}`,
	}}

	if got := summarizeRunResult(result, toolResults); got != "completed" {
		t.Fatalf("summarizeRunResult() = %q, want %q", got, "completed")
	}
}

func TestSummarizeRunResultHumanizesInternalError(t *testing.T) {
	t.Parallel()

	result := &RunResult{
		Status: agent.RunFailed,
		Error:  "context deadline exceeded",
	}

	if got := summarizeRunResultForLocale(result, nil, "en"); !strings.Contains(got, "timed out") {
		t.Fatalf("summarizeRunResultForLocale() = %q", got)
	}
}

func TestCollectExecutionTracesFromToolResultMetadata(t *testing.T) {
	t.Parallel()

	results := []resultmodel.ToolResult{{
		ToolName: "desktop.invoke_driver_action",
		Metadata: map[string]any{
			capprofile.MetadataKeyExecutionTrace: capprofile.ExecutionTrace{
				Surface:         "desktop",
				Capability:      "desktop",
				Operation:       "invoke_driver_action",
				ProfileID:       "douyin.desktop.macos",
				ChosenTransport: capprofile.TransportSemanticUIAction,
				ExecutionMode:   capprofile.ModeDeterministic,
			}.MetadataMap(),
		},
	}}

	traces := collectExecutionTraces(results)
	if len(traces) != 1 {
		t.Fatalf("len(traces) = %d", len(traces))
	}
	if traces[0].ProfileID != "douyin.desktop.macos" {
		t.Fatalf("traces[0].ProfileID = %q", traces[0].ProfileID)
	}
	if traces[0].ToolName != "desktop.invoke_driver_action" {
		t.Fatalf("traces[0].ToolName = %q", traces[0].ToolName)
	}
}

func TestBuildBundleStructuredDataIncludesExecutionTraceCounts(t *testing.T) {
	t.Parallel()

	data := buildBundleStructuredData(&RunResult{
		Output: "ok",
		ExecutionTraces: []capprofile.ExecutionTrace{
			{
				ProfileHit:      true,
				FallbackPath:    []string{capprofile.TransportSemanticUIAction},
				ExecutionMode:   capprofile.ModeVisualFallback,
				ChosenTransport: capprofile.TransportOCRAnchoredVisual,
			},
		},
	})

	if data["execution_trace_count"] != 1 {
		t.Fatalf("execution_trace_count = %#v", data["execution_trace_count"])
	}
	if data["execution_profile_hit_count"] != 1 {
		t.Fatalf("execution_profile_hit_count = %#v", data["execution_profile_hit_count"])
	}
	if data["execution_fallback_count"] != 1 {
		t.Fatalf("execution_fallback_count = %#v", data["execution_fallback_count"])
	}
	if data["execution_visual_fallback_count"] != 1 {
		t.Fatalf("execution_visual_fallback_count = %#v", data["execution_visual_fallback_count"])
	}
}
