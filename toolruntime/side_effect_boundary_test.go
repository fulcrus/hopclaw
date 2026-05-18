package toolruntime

import (
	"context"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/resultmodel"
	"github.com/fulcrus/hopclaw/skill"
)

type boundaryStubExecutor struct {
	definitions map[string]*agent.ResolvedTool
	callCount   int
	lastTool    string
}

func (s *boundaryStubExecutor) ExecuteBatch(_ context.Context, _ *agent.Run, _ *agent.Session, calls []agent.ToolCall) ([]contextengine.ToolResult, error) {
	s.callCount++
	results := make([]contextengine.ToolResult, len(calls))
	for i, call := range calls {
		s.lastTool = call.Name
		results[i] = contextengine.ToolResult{
			ToolName:   call.Name,
			ToolCallID: call.ID,
			Content:    "ok",
		}
	}
	return results, nil
}

func (s *boundaryStubExecutor) ResolveTool(_ *agent.Session, name string) (*agent.ResolvedTool, bool) {
	resolved, ok := s.definitions[name]
	return resolved, ok
}

func TestSideEffectBoundaryRejectsMislabeledFilesystemWrite(t *testing.T) {
	t.Parallel()

	inner := &boundaryStubExecutor{
		definitions: map[string]*agent.ResolvedTool{
			"fs.write": {
				Manifest:    skill.ToolManifest{Name: "fs.write", SideEffectClass: "read"},
				Descriptor:  agent.ToolDefinition{Name: "fs.write", SideEffectClass: "read"},
				ExecutorRef: "builtin",
			},
		},
	}
	wrapped := WithSideEffectBoundary()(inner)
	results, err := wrapped.ExecuteBatch(context.Background(), nil, nil, []agent.ToolCall{{
		ID:    "call-1",
		Name:  "fs.write",
		Input: map[string]any{"path": "README.md", "content": "hello"},
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch() error = %v", err)
	}
	if inner.callCount != 0 {
		t.Fatalf("inner.callCount = %d, want 0", inner.callCount)
	}
	if len(results) != 1 || results[0].Status != resultmodel.ToolResultError {
		t.Fatalf("results = %#v", results)
	}
	if results[0].Error == nil || results[0].Error.Message == "" {
		t.Fatalf("results[0].Error = %#v", results[0].Error)
	}
}

func TestSideEffectBoundaryRejectsMislabeledProcessExecution(t *testing.T) {
	t.Parallel()

	inner := &boundaryStubExecutor{
		definitions: map[string]*agent.ResolvedTool{
			"exec.run": {
				Manifest:    skill.ToolManifest{Name: "exec.run", SideEffectClass: "local_write"},
				Descriptor:  agent.ToolDefinition{Name: "exec.run", SideEffectClass: "local_write"},
				ExecutorRef: "builtin",
			},
		},
	}
	wrapped := WithSideEffectBoundary()(inner)
	results, err := wrapped.ExecuteBatch(context.Background(), nil, nil, []agent.ToolCall{{
		ID:    "call-1",
		Name:  "exec.run",
		Input: map[string]any{"command": "git", "args": []any{"status"}},
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch() error = %v", err)
	}
	if inner.callCount != 0 {
		t.Fatalf("inner.callCount = %d, want 0", inner.callCount)
	}
	if len(results) != 1 || results[0].Status != resultmodel.ToolResultError {
		t.Fatalf("results = %#v", results)
	}
}

func TestSideEffectBoundaryRejectsMislabeledNetworkWrite(t *testing.T) {
	t.Parallel()

	inner := &boundaryStubExecutor{
		definitions: map[string]*agent.ResolvedTool{
			"net.http": {
				Manifest:    skill.ToolManifest{Name: "net.http", SideEffectClass: "read"},
				Descriptor:  agent.ToolDefinition{Name: "net.http", SideEffectClass: "read"},
				ExecutorRef: "builtin",
			},
		},
	}
	wrapped := WithSideEffectBoundary()(inner)
	results, err := wrapped.ExecuteBatch(context.Background(), nil, nil, []agent.ToolCall{{
		ID:   "call-1",
		Name: "net.http",
		Input: map[string]any{
			"url":    "https://api.example.com/deploy",
			"method": "POST",
		},
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch() error = %v", err)
	}
	if inner.callCount != 0 {
		t.Fatalf("inner.callCount = %d, want 0", inner.callCount)
	}
	if len(results) != 1 || results[0].Status != resultmodel.ToolResultError {
		t.Fatalf("results = %#v", results)
	}
}

func TestSideEffectBoundaryAllowsCompatibleCalls(t *testing.T) {
	t.Parallel()

	inner := &boundaryStubExecutor{
		definitions: map[string]*agent.ResolvedTool{
			"net.http": {
				Manifest:    skill.ToolManifest{Name: "net.http", SideEffectClass: "read"},
				Descriptor:  agent.ToolDefinition{Name: "net.http", SideEffectClass: "read"},
				ExecutorRef: "builtin",
			},
			"channel.send": {
				Manifest:    skill.ToolManifest{Name: "channel.send", SideEffectClass: "external_write"},
				Descriptor:  agent.ToolDefinition{Name: "channel.send", SideEffectClass: "external_write"},
				ExecutorRef: "builtin",
			},
		},
	}
	wrapped := WithSideEffectBoundary()(inner)
	results, err := wrapped.ExecuteBatch(context.Background(), nil, nil, []agent.ToolCall{
		{
			ID:   "call-1",
			Name: "net.http",
			Input: map[string]any{
				"url":    "https://api.example.com/status",
				"method": "GET",
			},
		},
		{
			ID:   "call-2",
			Name: "channel.send",
			Input: map[string]any{
				"channel":   "slack",
				"target_id": "ops",
				"content":   "deploy done",
			},
		},
	})
	if err != nil {
		t.Fatalf("ExecuteBatch() error = %v", err)
	}
	if inner.callCount != 1 {
		t.Fatalf("inner.callCount = %d, want 1", inner.callCount)
	}
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
	for _, result := range results {
		if result.Status == resultmodel.ToolResultError {
			t.Fatalf("unexpected error result: %#v", result)
		}
	}
}

func TestSideEffectBoundaryAllowsSafeUnknownReadAction(t *testing.T) {
	t.Parallel()

	inner := &boundaryStubExecutor{
		definitions: map[string]*agent.ResolvedTool{
			"git.status": {
				Manifest:    skill.ToolManifest{Name: "git.status", SideEffectClass: "read"},
				Descriptor:  agent.ToolDefinition{Name: "git.status", SideEffectClass: "read"},
				ExecutorRef: "builtin",
			},
		},
	}
	wrapped := WithSideEffectBoundary()(inner)
	results, err := wrapped.ExecuteBatch(context.Background(), nil, nil, []agent.ToolCall{{
		ID:    "call-1",
		Name:  "git.status",
		Input: map[string]any{"dir": "."},
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch() error = %v", err)
	}
	if inner.callCount != 1 {
		t.Fatalf("inner.callCount = %d, want 1", inner.callCount)
	}
	if len(results) != 1 || results[0].Status == resultmodel.ToolResultError {
		t.Fatalf("results = %#v", results)
	}
}

func TestSideEffectBoundaryRejectsUnclassifiedReadTool(t *testing.T) {
	t.Parallel()

	inner := &boundaryStubExecutor{
		definitions: map[string]*agent.ResolvedTool{
			"deploy.apply": {
				Manifest:    skill.ToolManifest{Name: "deploy.apply", SideEffectClass: "read"},
				Descriptor:  agent.ToolDefinition{Name: "deploy.apply", SideEffectClass: "read"},
				ExecutorRef: "builtin",
			},
		},
	}
	wrapped := WithSideEffectBoundary()(inner)
	results, err := wrapped.ExecuteBatch(context.Background(), nil, nil, []agent.ToolCall{{
		ID:    "call-1",
		Name:  "deploy.apply",
		Input: map[string]any{"target": "prod"},
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch() error = %v", err)
	}
	if inner.callCount != 0 {
		t.Fatalf("inner.callCount = %d, want 0", inner.callCount)
	}
	if len(results) != 1 || results[0].Status != resultmodel.ToolResultError {
		t.Fatalf("results = %#v", results)
	}
	if results[0].Error == nil || results[0].Error.Message == "" {
		t.Fatalf("results[0].Error = %#v", results[0].Error)
	}
}

func TestSideEffectBoundaryRejectsUnresolvedToolMetadata(t *testing.T) {
	t.Parallel()

	inner := &boundaryStubExecutor{
		definitions: map[string]*agent.ResolvedTool{},
	}
	wrapped := WithSideEffectBoundary()(inner)
	results, err := wrapped.ExecuteBatch(context.Background(), nil, nil, []agent.ToolCall{{
		ID:   "call-1",
		Name: "dynamic.skill_tool",
		Input: map[string]any{
			"path": "README.md",
		},
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch() error = %v", err)
	}
	if inner.callCount != 0 {
		t.Fatalf("inner.callCount = %d, want 0", inner.callCount)
	}
	if len(results) != 1 || results[0].Status != resultmodel.ToolResultError {
		t.Fatalf("results = %#v", results)
	}
	if results[0].Error == nil || results[0].Error.Message == "" {
		t.Fatalf("results[0].Error = %#v", results[0].Error)
	}
}
