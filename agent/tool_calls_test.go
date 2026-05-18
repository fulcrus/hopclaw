package agent

import "testing"

func TestNormalizeToolCallsFillsIDsAndDropsBlankNames(t *testing.T) {
	t.Parallel()

	calls := normalizeToolCalls([]ToolCall{
		{Name: " fs.read ", Input: map[string]any{"path": "README.md"}},
		{Name: "   "},
		{Name: "exec.run"},
	})
	if len(calls) != 2 {
		t.Fatalf("len(calls) = %d, want 2", len(calls))
	}
	if calls[0].ID == "" || calls[1].ID == "" {
		t.Fatalf("expected generated IDs, got %#v", calls)
	}
	if calls[0].Name != "fs.read" || calls[1].Name != "exec.run" {
		t.Fatalf("normalized names = %#v", calls)
	}
	if calls[1].Input == nil {
		t.Fatalf("expected non-nil input map, got %#v", calls[1])
	}
}
