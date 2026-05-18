package toolruntime

import (
	"context"
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/internal/metrics"
	"github.com/fulcrus/hopclaw/resultmodel"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

type metricsErrorExecutor struct{}

func (metricsErrorExecutor) ExecuteBatch(_ context.Context, _ *agent.Run, _ *agent.Session, calls []agent.ToolCall) ([]contextengine.ToolResult, error) {
	results := make([]contextengine.ToolResult, len(calls))
	for index, call := range calls {
		results[index] = contextengine.ToolResult{
			ToolName: call.Name,
			Status:   resultmodel.ToolResultError,
			Error: &resultmodel.ResultError{
				Message: "tool failed",
			},
		}
	}
	return results, nil
}

type metricsDefinitionExecutor struct {
	definitions []agent.ToolDefinition
	resolved    *agent.ResolvedTool
	lastName    string
}

func (m *metricsDefinitionExecutor) ExecuteBatch(_ context.Context, _ *agent.Run, _ *agent.Session, _ []agent.ToolCall) ([]contextengine.ToolResult, error) {
	return nil, nil
}

func (m *metricsDefinitionExecutor) ToolDefinitions(_ *agent.Session) []agent.ToolDefinition {
	return m.definitions
}

func (m *metricsDefinitionExecutor) ResolveTool(_ *agent.Session, name string) (*agent.ResolvedTool, bool) {
	m.lastName = name
	return m.resolved, m.resolved != nil
}

func TestMetricsMiddlewareRecords(t *testing.T) {
	core := &stubToolExecutor{}
	wrapped := MetricsMiddleware()(core)

	run := &agent.Run{ID: "run-1", SessionID: "session-1"}
	session := &agent.Session{ID: "session-1"}
	calls := []agent.ToolCall{{Name: "metrics.success"}}

	results, err := wrapped.ExecuteBatch(context.Background(), run, session, calls)
	if err != nil {
		t.Fatalf("ExecuteBatch() error = %v", err)
	}
	if core.callCount != 1 {
		t.Fatalf("callCount = %d, want 1", core.callCount)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
}

func TestMetricsMiddlewareCountsToolErrors(t *testing.T) {
	toolName := "tool_" + strings.ReplaceAll(t.Name(), "/", "_")
	before := testutil.ToFloat64(metrics.ToolCallErrors.WithLabelValues(toolName))

	wrapped := MetricsMiddleware()(metricsErrorExecutor{})
	results, err := wrapped.ExecuteBatch(context.Background(), nil, nil, []agent.ToolCall{{Name: toolName}})
	if err != nil {
		t.Fatalf("ExecuteBatch() error = %v", err)
	}
	if len(results) != 1 || results[0].Normalized().Status != resultmodel.ToolResultError {
		t.Fatalf("results = %#v", results)
	}

	after := testutil.ToFloat64(metrics.ToolCallErrors.WithLabelValues(toolName))
	if after <= before {
		t.Fatalf("error counter = %v, want > %v", after, before)
	}
}

func TestMetricsMiddlewareDelegatesDefinitionsAndResolution(t *testing.T) {
	definition := agent.ToolDefinition{Name: "metrics.delegate"}
	exec := &metricsDefinitionExecutor{
		definitions: []agent.ToolDefinition{definition},
		resolved: &agent.ResolvedTool{
			Descriptor: definition,
		},
	}
	wrapped := MetricsMiddleware()(exec)

	provider, ok := wrapped.(agent.ToolDefinitionProvider)
	if !ok {
		t.Fatal("wrapped executor does not implement ToolDefinitionProvider")
	}
	defs := provider.ToolDefinitions(nil)
	if len(defs) != 1 || defs[0].Name != definition.Name {
		t.Fatalf("defs = %#v", defs)
	}

	resolver, ok := wrapped.(agent.ToolResolver)
	if !ok {
		t.Fatal("wrapped executor does not implement ToolResolver")
	}
	resolved, found := resolver.ResolveTool(nil, definition.Name)
	if !found || resolved == nil || resolved.Descriptor.Name != definition.Name {
		t.Fatalf("resolved = %#v, found = %v", resolved, found)
	}
	if exec.lastName != definition.Name {
		t.Fatalf("lastName = %q, want %q", exec.lastName, definition.Name)
	}
}
