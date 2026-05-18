package agent

import (
	"testing"

	"github.com/fulcrus/hopclaw/contextengine"
)

func TestToolProgressTrackerDetectsUnchangedOutcomes(t *testing.T) {
	t.Parallel()

	tracker := toolProgressTracker{}
	callsA := []ToolCall{{Name: "desktop.list_windows", Input: map[string]any{"app": "notes"}}}
	callsB := []ToolCall{{Name: "desktop.list_windows", Input: map[string]any{"app": "safari"}}}
	results := []contextengine.ToolResult{{ToolName: "desktop.list_windows", Content: "[]"}}

	if got := tracker.Observe(callsA, results); got != 0 {
		t.Fatalf("first Observe() = %d, want 0", got)
	}
	if got := tracker.Observe(callsB, results); got != 1 {
		t.Fatalf("second Observe() = %d, want 1", got)
	}
	if got := tracker.Observe(callsA, results); got != 2 {
		t.Fatalf("third Observe() = %d, want 2", got)
	}
}

func TestToolResultSignatureNormalizesEquivalentJSON(t *testing.T) {
	t.Parallel()

	first := toolResultSignature([]contextengine.ToolResult{{
		ToolName: "net.fetch",
		Content:  "{\"ok\":true,\"items\":[1,2]}",
	}})
	second := toolResultSignature([]contextengine.ToolResult{{
		ToolName: "net.fetch",
		Content:  "{ \"items\" : [1,2], \"ok\" : true }",
	}})
	if first != second {
		t.Fatalf("toolResultSignature mismatch:\nfirst=%q\nsecond=%q", first, second)
	}
}

func TestToolResultSignatureIgnoresBrowserSnapshotArtifactURIChurn(t *testing.T) {
	t.Parallel()

	first := toolResultSignature([]contextengine.ToolResult{{
		ToolName:    "browser.snapshot",
		Content:     `{"url":"https://www.bing.com/search?q=openai","title":"openai - 搜索","content":"<html><body>same search page</body></html>"}`,
		ArtifactURI: "artifact://local/one",
	}})
	second := toolResultSignature([]contextengine.ToolResult{{
		ToolName:    "browser.snapshot",
		Content:     `{"title":"openai - 搜索","content":"<html><body>same search page</body></html>","url":"https://www.bing.com/search?q=openai"}`,
		ArtifactURI: "artifact://local/two",
	}})
	if first != second {
		t.Fatalf("browser snapshot signature mismatch:\nfirst=%q\nsecond=%q", first, second)
	}
}
