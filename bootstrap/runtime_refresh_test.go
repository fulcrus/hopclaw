package bootstrap

import (
	"context"
	"sync"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/artifact"
	"github.com/fulcrus/hopclaw/config"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/internal/modules"
	"github.com/fulcrus/hopclaw/mcp"
	toolregistry "github.com/fulcrus/hopclaw/toolruntime/registry"
)

type stubBootstrapModelClient struct {
	resp *agent.ModelResponse
	err  error
}

var testRefreshGlobalsMu sync.Mutex

type stubPluginMCPRuntime struct {
	tools []mcp.Tool
}

type stubBootstrapToolExecutor struct{}
type runtimeRefreshContextKey string

func (stubBootstrapToolExecutor) ExecuteBatch(context.Context, *agent.Run, *agent.Session, []agent.ToolCall) ([]contextengine.ToolResult, error) {
	return nil, nil
}

func (s stubBootstrapModelClient) Chat(context.Context, agent.ChatRequest) (*agent.ModelResponse, error) {
	return s.resp, s.err
}

func (*stubPluginMCPRuntime) Start(context.Context) error { return nil }

func (*stubPluginMCPRuntime) Stop() error { return nil }

func (s *stubPluginMCPRuntime) Tools() []mcp.Tool {
	if s == nil {
		return nil
	}
	out := make([]mcp.Tool, len(s.tools))
	copy(out, s.tools)
	return out
}

func (*stubPluginMCPRuntime) CallTool(context.Context, string, map[string]any) (*mcp.CallToolResult, error) {
	return &mcp.CallToolResult{}, nil
}

func TestEstimateChatRequestTokensIncludesToolDefinitions(t *testing.T) {
	t.Parallel()

	estimator := contextengine.CharRatioEstimator{
		CharsPerToken:        1.0,
		ToolCharsPerToken:    1.0,
		EmptyMessageOverhead: 1,
		SafetyMargin:         1.0,
	}
	req := agent.ChatRequest{
		Budget: contextengine.Budget{
			EstimatedInputTokens: 10,
		},
		Tools: []agent.ToolDefinition{{
			Name:        "fs.read",
			Description: "Read a file",
			InputSchema: map[string]any{"type": "object"},
		}},
	}

	got := estimateChatRequestTokens(estimator, req)
	if got <= 10 {
		t.Fatalf("estimateChatRequestTokens() = %d, want tool-schema tokens above base estimate", got)
	}
}

func TestDynamicModelClientRecordsObservedPromptUsage(t *testing.T) {
	t.Parallel()

	estimator := contextengine.NewCalibratedEstimator(contextengine.CharRatioEstimator{SafetyMargin: 1.0})
	client := newDynamicModelClient(stubBootstrapModelClient{
		resp: &agent.ModelResponse{
			Usage: &agent.ModelUsageInfo{
				PromptTokens: 50,
				TotalTokens:  50,
			},
		},
	}, estimator)

	before := estimator.CorrectionFactor()
	_, err := client.Chat(context.Background(), agent.ChatRequest{
		Budget: contextengine.Budget{
			EstimatedInputTokens: 25,
		},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if estimator.CorrectionFactor() <= before {
		t.Fatalf("CorrectionFactor() = %f, want > %f after recording actual usage", estimator.CorrectionFactor(), before)
	}
}

func TestComposeRuntimeToolsPropagatesContext(t *testing.T) {
	testRefreshGlobalsMu.Lock()
	original := buildRuntimeToolStack
	buildRuntimeToolStack = func(ctx context.Context, base agent.ToolExecutor, artifactStore artifact.Store, cfg config.Config, moduleCatalog *modules.Store, pluginMCP agent.ToolExecutor) (toolregistry.BuildResult, error) {
		if got := ctx.Value(runtimeRefreshContextKey("trace_id")); got != "compose-runtime-tools" {
			t.Fatalf("context value = %#v, want propagated trace id", got)
		}
		return toolregistry.BuildResult{Executor: stubBootstrapToolExecutor{}}, nil
	}
	defer func() {
		buildRuntimeToolStack = original
		testRefreshGlobalsMu.Unlock()
	}()

	ctx := context.WithValue(context.Background(), runtimeRefreshContextKey("trace_id"), "compose-runtime-tools")
	exec, err := composeRuntimeTools(ctx, nil, nil, config.Config{}, nil, nil)
	if err != nil {
		t.Fatalf("composeRuntimeTools() error = %v", err)
	}
	if exec == nil {
		t.Fatal("composeRuntimeTools() executor = nil")
	}
}
