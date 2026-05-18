package toolruntime

import (
	"context"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
)

type benchmarkToolExecutor struct {
	results []contextengine.ToolResult
}

func (e benchmarkToolExecutor) ExecuteBatch(_ context.Context, _ *agent.Run, _ *agent.Session, calls []agent.ToolCall) ([]contextengine.ToolResult, error) {
	return e.results[:len(calls)], nil
}

type benchmarkMiddlewareExecutor struct {
	next agent.ToolExecutor
}

func (e benchmarkMiddlewareExecutor) ExecuteBatch(ctx context.Context, run *agent.Run, session *agent.Session, calls []agent.ToolCall) ([]contextengine.ToolResult, error) {
	return e.next.ExecuteBatch(ctx, run, session, calls)
}

func benchmarkPassthroughMiddleware() ToolMiddleware {
	return func(next agent.ToolExecutor) agent.ToolExecutor {
		return benchmarkMiddlewareExecutor{next: next}
	}
}

var benchmarkChainResultsSink []contextengine.ToolResult

func BenchmarkChainExecuteBatchNoMiddleware(b *testing.B) {
	benchmarkChainExecuteBatch(b)
}

func BenchmarkChainExecuteBatchThreeMiddleware(b *testing.B) {
	benchmarkChainExecuteBatch(
		b,
		benchmarkPassthroughMiddleware(),
		benchmarkPassthroughMiddleware(),
		benchmarkPassthroughMiddleware(),
	)
}

func benchmarkChainExecuteBatch(b *testing.B, mws ...ToolMiddleware) {
	executor := Chain(
		benchmarkToolExecutor{results: make([]contextengine.ToolResult, 1)},
		mws...,
	)
	calls := []agent.ToolCall{{Name: "bench.tool"}}
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		results, err := executor.ExecuteBatch(ctx, nil, nil, calls)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkChainResultsSink = results
	}
}
