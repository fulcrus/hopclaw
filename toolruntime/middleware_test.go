package toolruntime

import (
	"context"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
)

// stubToolExecutor records calls for testing.
type stubToolExecutor struct {
	callCount int
	lastTool  string
}

func (s *stubToolExecutor) ExecuteBatch(_ context.Context, _ *agent.Run, _ *agent.Session, calls []agent.ToolCall) ([]contextengine.ToolResult, error) {
	s.callCount++
	if len(calls) > 0 {
		s.lastTool = calls[0].Name
	}
	results := make([]contextengine.ToolResult, len(calls))
	for i, c := range calls {
		results[i] = contextengine.ToolResult{ToolName: c.Name, Content: "stub-result"}
	}
	return results, nil
}

func TestChainNoMiddleware(t *testing.T) {
	t.Parallel()

	core := &stubToolExecutor{}
	chained := Chain(core)

	calls := []agent.ToolCall{{Name: "test"}}
	results, err := chained.ExecuteBatch(context.Background(), nil, nil, calls)
	if err != nil {
		t.Fatalf("ExecuteBatch error: %v", err)
	}
	if len(results) != 1 || results[0].Content != "stub-result" {
		t.Fatalf("results = %v", results)
	}
	if core.callCount != 1 {
		t.Fatalf("callCount = %d, want 1", core.callCount)
	}
}

func TestChainSingleMiddleware(t *testing.T) {
	t.Parallel()

	mwCalled := false
	mw := ToolMiddleware(func(next agent.ToolExecutor) agent.ToolExecutor {
		return &middlewareWrapper{next: next, onCall: func() { mwCalled = true }}
	})

	core := &stubToolExecutor{}
	chained := Chain(core, mw)

	calls := []agent.ToolCall{{Name: "test"}}
	_, err := chained.ExecuteBatch(context.Background(), nil, nil, calls)
	if err != nil {
		t.Fatalf("ExecuteBatch error: %v", err)
	}
	if !mwCalled {
		t.Fatal("middleware was not called")
	}
	if core.callCount != 1 {
		t.Fatalf("core callCount = %d, want 1", core.callCount)
	}
}

func TestChainMultipleMiddleware(t *testing.T) {
	t.Parallel()

	var order []string
	makeMW := func(name string) ToolMiddleware {
		return ToolMiddleware(func(next agent.ToolExecutor) agent.ToolExecutor {
			return &middlewareWrapper{next: next, onCall: func() { order = append(order, name) }}
		})
	}

	core := &stubToolExecutor{}
	chained := Chain(core, makeMW("outer"), makeMW("middle"), makeMW("inner"))

	calls := []agent.ToolCall{{Name: "test"}}
	_, err := chained.ExecuteBatch(context.Background(), nil, nil, calls)
	if err != nil {
		t.Fatalf("ExecuteBatch error: %v", err)
	}

	if len(order) != 3 {
		t.Fatalf("order = %v, want 3 entries", order)
	}
	if order[0] != "outer" || order[1] != "middle" || order[2] != "inner" {
		t.Fatalf("order = %v, want [outer, middle, inner]", order)
	}
}

func TestChainNilMiddlewareSkipped(t *testing.T) {
	t.Parallel()

	mwCalled := false
	mw := ToolMiddleware(func(next agent.ToolExecutor) agent.ToolExecutor {
		return &middlewareWrapper{next: next, onCall: func() { mwCalled = true }}
	})

	core := &stubToolExecutor{}
	chained := Chain(core, nil, mw, nil)

	calls := []agent.ToolCall{{Name: "test"}}
	_, err := chained.ExecuteBatch(context.Background(), nil, nil, calls)
	if err != nil {
		t.Fatalf("ExecuteBatch error: %v", err)
	}
	if !mwCalled {
		t.Fatal("non-nil middleware should still be called")
	}
}

// middlewareWrapper is a test helper that wraps a ToolExecutor.
type middlewareWrapper struct {
	next   agent.ToolExecutor
	onCall func()
}

func (m *middlewareWrapper) ExecuteBatch(ctx context.Context, run *agent.Run, session *agent.Session, calls []agent.ToolCall) ([]contextengine.ToolResult, error) {
	if m.onCall != nil {
		m.onCall()
	}
	return m.next.ExecuteBatch(ctx, run, session, calls)
}
