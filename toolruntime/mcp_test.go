package toolruntime

import (
	"context"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/mcp"
)

type stubMCPRuntime struct {
	tools  []mcp.Tool
	result *mcp.CallToolResult
	err    error
}

func (s stubMCPRuntime) Tools() []mcp.Tool {
	return append([]mcp.Tool(nil), s.tools...)
}

func (s stubMCPRuntime) CallTool(_ context.Context, _ string, _ map[string]any) (*mcp.CallToolResult, error) {
	return s.result, s.err
}

func TestMCPExecutorExposesToolsAndResults(t *testing.T) {
	t.Parallel()

	exec := NewMCPExecutor(stubMCPRuntime{
		tools: []mcp.Tool{{
			Name:        "demo.server__search",
			Description: "Search docs",
			InputSchema: map[string]any{"type": "object"},
		}},
		result: &mcp.CallToolResult{
			Content: []mcp.ContentBlock{
				{Type: "text", Text: "found 3 results"},
				{Type: "resource", URI: "https://example.com/result"},
			},
		},
	})
	if exec == nil {
		t.Fatal("expected executor")
	}

	defs := exec.ToolDefinitions(nil)
	if len(defs) != 1 || defs[0].Name != "demo.server__search" {
		t.Fatalf("ToolDefinitions() = %#v", defs)
	}

	resolved, ok := exec.ResolveTool(nil, "demo.server__search")
	if !ok || resolved == nil {
		t.Fatal("ResolveTool() = false")
	}

	results, err := exec.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:    "call-1",
		Name:  "demo.server__search",
		Input: map[string]any{"q": "hopclaw"},
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results len = %d", len(results))
	}
	if results[0].TranscriptText == "" {
		t.Fatal("expected transcript text")
	}
	if len(results[0].Artifacts) != 1 {
		t.Fatalf("artifacts len = %d", len(results[0].Artifacts))
	}
}

func TestMCPExecutorPreservesBatchSlotsOnPerCallFailure(t *testing.T) {
	t.Parallel()

	exec := NewMCPExecutor(stubMCPRuntime{
		tools: []mcp.Tool{{
			Name:        "demo.server__search",
			Description: "Search docs",
			InputSchema: map[string]any{"type": "object"},
		}},
		err: context.DeadlineExceeded,
	})

	results, err := exec.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:    "call-1",
		Name:  "demo.server__search",
		Input: map[string]any{"q": "hopclaw"},
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results len = %d", len(results))
	}
	if results[0].Error == nil || results[0].Error.Message == "" {
		t.Fatalf("expected error result, got %#v", results[0])
	}
}

func TestNewMCPExecutorAllowsEmptyLiveRuntime(t *testing.T) {
	t.Parallel()

	exec := NewMCPExecutor(stubMCPRuntime{})
	if exec == nil {
		t.Fatal("expected executor for live runtime")
	}
	if defs := exec.ToolDefinitions(nil); len(defs) != 0 {
		t.Fatalf("ToolDefinitions() len = %d, want 0", len(defs))
	}
}
