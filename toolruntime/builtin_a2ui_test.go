package toolruntime

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/canvas"
)

func TestHandleA2UIPush_NoCanvasHost(t *testing.T) {
	b := &Builtins{}
	call := agent.ToolCall{
		Name: "a2ui.push",
		Input: map[string]any{
			"session_id": "s1",
			"components": []any{
				map[string]any{"id": "c1", "type": "chart"},
			},
		},
	}
	result, err := handleA2UIPush(context.Background(), b, call)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Content != "canvas host not configured" {
		t.Errorf("unexpected result: %s", result.Content)
	}
}

func TestHandleA2UIPush_WithCanvasHost(t *testing.T) {
	host, err := canvas.NewHost(canvas.HostConfig{
		Port: 0,
		Root: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("NewHost: %v", err)
	}

	b := &Builtins{canvasHost: host}
	call := agent.ToolCall{
		Name: "a2ui.push",
		Input: map[string]any{
			"session_id": "s1",
			"components": []any{
				map[string]any{"id": "c1", "type": "chart", "props": map[string]any{"title": "Test"}},
			},
		},
	}
	result, err := handleA2UIPush(context.Background(), b, call)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var output map[string]any
	if err := json.Unmarshal([]byte(result.Content), &output); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if output["ok"] != true {
		t.Errorf("expected ok=true, got %v", output["ok"])
	}
	if output["component_count"] != float64(1) {
		t.Errorf("expected component_count=1, got %v", output["component_count"])
	}
}

func TestHandleA2UIReset_NoCanvasHost(t *testing.T) {
	b := &Builtins{}
	call := agent.ToolCall{
		Name: "a2ui.reset",
		Input: map[string]any{
			"session_id": "s1",
		},
	}
	result, err := handleA2UIReset(context.Background(), b, call)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Content != "canvas host not configured" {
		t.Errorf("unexpected result: %s", result.Content)
	}
}

func TestHandleA2UIReset_WithCanvasHost(t *testing.T) {
	host, err := canvas.NewHost(canvas.HostConfig{
		Port: 0,
		Root: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("NewHost: %v", err)
	}

	b := &Builtins{canvasHost: host}

	// Push first, then reset.
	pushCall := agent.ToolCall{
		Name: "a2ui.push",
		Input: map[string]any{
			"session_id": "s1",
			"components": []any{
				map[string]any{"id": "c1", "type": "chart"},
			},
		},
	}
	_, _ = handleA2UIPush(context.Background(), b, pushCall)

	resetCall := agent.ToolCall{
		Name: "a2ui.reset",
		Input: map[string]any{
			"session_id": "s1",
		},
	}
	result, err := handleA2UIReset(context.Background(), b, resetCall)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var output map[string]any
	if err := json.Unmarshal([]byte(result.Content), &output); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if output["ok"] != true {
		t.Errorf("expected ok=true, got %v", output["ok"])
	}
}

func TestHandleA2UIPush_MissingSessionID(t *testing.T) {
	host, err := canvas.NewHost(canvas.HostConfig{
		Port: 0,
		Root: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("NewHost: %v", err)
	}

	b := &Builtins{canvasHost: host}
	call := agent.ToolCall{
		Name: "a2ui.push",
		Input: map[string]any{
			"components": []any{
				map[string]any{"id": "c1", "type": "chart"},
			},
		},
	}
	result, err := handleA2UIPush(context.Background(), b, call)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Content != "session_id is required" {
		t.Errorf("expected session_id validation error, got: %s", result.Content)
	}
}

func TestHandleA2UIPush_EmptyComponents(t *testing.T) {
	host, err := canvas.NewHost(canvas.HostConfig{
		Port: 0,
		Root: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("NewHost: %v", err)
	}

	b := &Builtins{canvasHost: host}
	call := agent.ToolCall{
		Name: "a2ui.push",
		Input: map[string]any{
			"session_id": "s1",
			"components": []any{},
		},
	}
	result, err := handleA2UIPush(context.Background(), b, call)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Content != "at least one component is required" {
		t.Errorf("expected empty components error, got: %s", result.Content)
	}
}
