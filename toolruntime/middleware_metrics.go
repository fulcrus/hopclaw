package toolruntime

import (
	"context"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/internal/metrics"
	"github.com/fulcrus/hopclaw/resultmodel"
)

// MetricsMiddleware returns a ToolMiddleware that records per-tool latency and errors.
func MetricsMiddleware() ToolMiddleware {
	return func(next agent.ToolExecutor) agent.ToolExecutor {
		return &metricsExecutor{next: next}
	}
}

type metricsExecutor struct {
	next agent.ToolExecutor
}

func (m *metricsExecutor) ExecuteBatch(ctx context.Context, run *agent.Run, session *agent.Session, calls []agent.ToolCall) ([]contextengine.ToolResult, error) {
	if m.next == nil {
		return nil, agent.ErrToolExecutorNil
	}

	start := time.Now()
	results, err := m.next.ExecuteBatch(ctx, run, session, calls)
	durationSeconds := time.Since(start).Seconds()

	if len(calls) == 0 {
		return results, err
	}

	if err != nil {
		for _, call := range calls {
			metrics.ToolCallDuration.WithLabelValues(call.Name).Observe(durationSeconds)
			metrics.ToolCallErrors.WithLabelValues(call.Name).Inc()
		}
		return results, err
	}

	perToolDuration := durationSeconds / float64(len(calls))
	for index, call := range calls {
		metrics.ToolCallDuration.WithLabelValues(call.Name).Observe(perToolDuration)
		if index < len(results) && results[index].Normalized().Status == resultmodel.ToolResultError {
			metrics.ToolCallErrors.WithLabelValues(call.Name).Inc()
		}
	}
	return results, nil
}

func (m *metricsExecutor) ToolDefinitions(session *agent.Session) []agent.ToolDefinition {
	provider, ok := m.next.(agent.ToolDefinitionProvider)
	if !ok {
		return nil
	}
	return provider.ToolDefinitions(session)
}

func (m *metricsExecutor) ResolveTool(session *agent.Session, name string) (*agent.ResolvedTool, bool) {
	resolver, ok := m.next.(agent.ToolResolver)
	if !ok {
		return nil, false
	}
	return resolver.ResolveTool(session, name)
}
