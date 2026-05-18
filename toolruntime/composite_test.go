package toolruntime

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/skill"
	"github.com/fulcrus/hopclaw/toolspec"
)

func TestCompositeMergesDefinitionsAndPrefersFirstResolver(t *testing.T) {
	t.Parallel()

	first := &stubCompositeExecutor{
		definitions: []agent.ToolDefinition{{
			Name:        "fs.read",
			Description: "builtin read",
		}},
		tools: map[string]skill.BoundTool{
			"fs.read": boundTool("fs.read", "read", "fs:{path}", skill.TrustBundled),
		},
	}
	second := &stubCompositeExecutor{
		definitions: []agent.ToolDefinition{{
			Name:        "fs.read",
			Description: "shadowed read",
		}, {
			Name:        "exec.run",
			Description: "exec",
		}},
		tools: map[string]skill.BoundTool{
			"fs.read":  boundTool("fs.read", "read", "other:{path}", skill.TrustInternal),
			"exec.run": boundTool("exec.run", "destructive", "exec:{command}", skill.TrustInternal),
		},
	}

	composite := NewComposite(CompositeConfig{}, first, second)
	definitions := composite.ToolDefinitions(nil)
	if len(definitions) != 2 {
		t.Fatalf("len(ToolDefinitions) = %d", len(definitions))
	}
	if definitions[0].Description != "builtin read" {
		t.Fatalf("definitions[0] = %#v", definitions[0])
	}
	resolved, ok := composite.ResolveTool(nil, "fs.read")
	if !ok {
		t.Fatal("expected fs.read to resolve")
	}
	if resolved.Package.Trust != skill.TrustBundled {
		t.Fatalf("ResolveTool(fs.read).Package.Trust = %q", resolved.Package.Trust)
	}
}

func TestCompositeParallelizesDistinctReadCalls(t *testing.T) {
	t.Parallel()

	executor := &stubCompositeExecutor{
		delay: 80 * time.Millisecond,
		tools: map[string]skill.BoundTool{
			"read.one": boundTool("read.one", "read", "fs:{path}", skill.TrustInternal),
			"read.two": boundTool("read.two", "read", "fs:{path}", skill.TrustInternal),
		},
	}
	composite := NewComposite(CompositeConfig{MaxParallel: 2}, executor)

	if _, err := composite.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{
		{ID: "call-1", Name: "read.one", Input: map[string]any{"path": "a.txt"}},
		{ID: "call-2", Name: "read.two", Input: map[string]any{"path": "b.txt"}},
	}); err != nil {
		t.Fatalf("ExecuteBatch() error = %v", err)
	}

	events := executor.Events()
	if len(events) < 4 {
		t.Fatalf("events = %#v", events)
	}
	if events[0] == "done:call-1" || events[0] == "done:call-2" || events[1] == "done:call-1" || events[1] == "done:call-2" {
		t.Fatalf("expected both calls to start before completion, got %#v", events)
	}
}

func TestCompositeSerializesDuplicateExecutionKeysAndWrites(t *testing.T) {
	t.Parallel()

	executor := &stubCompositeExecutor{
		delay: 30 * time.Millisecond,
		tools: map[string]skill.BoundTool{
			"read.one":  boundTool("read.one", "read", "fs:{path}", skill.TrustInternal),
			"read.two":  boundTool("read.two", "read", "fs:{path}", skill.TrustInternal),
			"write.one": boundTool("write.one", "local_write", "fs:{path}", skill.TrustInternal),
		},
	}
	composite := NewComposite(CompositeConfig{MaxParallel: 3}, executor)

	if _, err := composite.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{
		{ID: "call-1", Name: "read.one", Input: map[string]any{"path": "same.txt"}},
		{ID: "call-2", Name: "read.two", Input: map[string]any{"path": "same.txt"}},
		{ID: "call-3", Name: "write.one", Input: map[string]any{"path": "same.txt"}},
	}); err != nil {
		t.Fatalf("ExecuteBatch() error = %v", err)
	}

	events := executor.Events()
	expected := []string{
		"start:call-1",
		"done:call-1",
		"start:call-2",
		"done:call-2",
		"start:call-3",
		"done:call-3",
	}
	if fmt.Sprintf("%v", events) != fmt.Sprintf("%v", expected) {
		t.Fatalf("events = %#v, want %#v", events, expected)
	}
}

type stubCompositeExecutor struct {
	delay       time.Duration
	definitions []agent.ToolDefinition
	tools       map[string]skill.BoundTool

	mu     sync.Mutex
	events []string
}

func (s *stubCompositeExecutor) ToolDefinitions(*agent.Session) []agent.ToolDefinition {
	out := make([]agent.ToolDefinition, 0, len(s.definitions))
	for _, definition := range s.definitions {
		out = append(out, agent.ToolDefinition{
			Name:             definition.Name,
			Description:      definition.Description,
			InputSchema:      cloneSchema(definition.InputSchema),
			OutputSchema:     cloneSchema(definition.OutputSchema),
			SideEffectClass:  definition.SideEffectClass,
			Idempotent:       definition.Idempotent,
			RequiresApproval: definition.RequiresApproval,
			ExecutionKey:     definition.ExecutionKey,
		})
	}
	return out
}

func (s *stubCompositeExecutor) ResolveTool(_ *agent.Session, name string) (*agent.ResolvedTool, bool) {
	bound, ok := s.tools[name]
	if !ok {
		return nil, false
	}
	copied := bound
	return toolspec.ResolvedFromSkillBinding(&copied, agent.ToolDefinition{
		Name:               copied.Manifest.Name,
		Description:        copied.Manifest.Description,
		InputSchema:        cloneSchema(copied.Manifest.InputSchema),
		OutputSchema:       cloneSchema(copied.Manifest.OutputSchema),
		SideEffectClass:    copied.Manifest.SideEffectClass,
		Idempotent:         copied.Manifest.Idempotent,
		RequiresApproval:   copied.Manifest.RequiresApproval,
		ExecutionKey:       copied.Manifest.ExecutionKey,
		Source:             "test",
		Trust:              string(copied.Package.Trust),
		Eligible:           copied.Eligibility.Eligible,
		EligibilityReasons: append([]string(nil), copied.Eligibility.Reasons...),
	}, "composite-test"), true
}

func (s *stubCompositeExecutor) ExecuteBatch(_ context.Context, _ *agent.Run, _ *agent.Session, calls []agent.ToolCall) ([]contextengine.ToolResult, error) {
	results := make([]contextengine.ToolResult, 0, len(calls))
	for _, call := range calls {
		s.record("start:" + call.ID)
		time.Sleep(s.delay)
		s.record("done:" + call.ID)
		results = append(results, contextengine.ToolResult{
			ToolName:   call.Name,
			ToolCallID: call.ID,
			Content:    call.ID,
		})
	}
	return results, nil
}

func (s *stubCompositeExecutor) Events() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.events...)
}

func (s *stubCompositeExecutor) record(event string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
}

func boundTool(name, sideEffect, executionKey string, trust skill.TrustClass) skill.BoundTool {
	return skill.BoundTool{
		Package: &skill.SkillPackage{
			Prompt: skill.PromptSkill{Name: "runtime"},
			Trust:  trust,
		},
		Manifest: skill.ToolManifest{
			Name:            name,
			SideEffectClass: sideEffect,
			ExecutionKey:    executionKey,
		},
		Eligibility: skill.EligibilityResult{Eligible: true},
	}
}
