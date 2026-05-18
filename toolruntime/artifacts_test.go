package toolruntime

import (
	"context"
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/artifact"
	"github.com/fulcrus/hopclaw/contextengine"
)

func TestArtifactingExecutorStoresLargeOutputs(t *testing.T) {
	t.Parallel()

	store := artifact.NewInMemoryStore()
	executor := NewArtifactingExecutor(stubArtifactExecutor{
		results: []contextengine.ToolResult{{
			ToolName:   "fs.read",
			ToolCallID: "call-1",
			Content:    strings.Repeat("x", 2048),
		}},
	}, store, ArtifactingConfig{
		InlineMaxBytes: 32,
		PreviewChars:   16,
	})

	results, err := executor.ExecuteBatch(context.Background(), &agent.Run{ID: "run-1"}, &agent.Session{ID: "sess-1"}, []agent.ToolCall{{
		ID:   "call-1",
		Name: "fs.read",
	}})
	if err != nil {
		t.Fatalf("ExecuteBatch() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d", len(results))
	}
	if results[0].ArtifactURI == "" {
		t.Fatal("expected artifact URI")
	}
	if !strings.Contains(results[0].Content, "[artifact stored]") {
		t.Fatalf("results[0].Content = %q", results[0].Content)
	}
	body, _, err := store.Read(context.Background(), results[0].ArtifactURI)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if len(body) != 2048 {
		t.Fatalf("len(body) = %d", len(body))
	}
}

type stubArtifactExecutor struct {
	results []contextengine.ToolResult
}

func (s stubArtifactExecutor) ExecuteBatch(context.Context, *agent.Run, *agent.Session, []agent.ToolCall) ([]contextengine.ToolResult, error) {
	return append([]contextengine.ToolResult(nil), s.results...), nil
}
