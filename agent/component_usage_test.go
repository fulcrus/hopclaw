package agent

import (
	"context"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/usage"
)

func TestTrackModelUsageIncludesWorkflowFields(t *testing.T) {
	t.Parallel()

	usageStore := usage.NewInMemoryStore()
	component := (&AgentComponent{}).WithUsageTracker(usage.NewTracker(usageStore))

	run := &Run{
		ID:          "run-002",
		ParentRunID: "run-001",
		WorkflowState: &WorkflowState{
			OriginalRunID:     "run-root",
			ContinuationIndex: 2,
		},
	}
	run.WorkflowState.EnsureBudget(time.Now().UTC())
	session := &Session{ID: "sess-001"}
	resp := &ModelResponse{
		Usage: &ModelUsageInfo{
			PromptTokens:     120,
			CompletionTokens: 30,
			TotalTokens:      150,
		},
	}

	component.trackModelUsage(context.Background(), run, session, "gpt-4o", "openai", resp, 250*time.Millisecond)

	records, err := usageStore.Query(context.Background(), usage.QueryFilter{WorkflowID: "run-root"})
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("len(records) = %d, want 1", len(records))
	}
	if records[0].ParentRunID != "run-001" {
		t.Fatalf("records[0].ParentRunID = %q, want run-001", records[0].ParentRunID)
	}
	if records[0].ContinuationIndex != 2 {
		t.Fatalf("records[0].ContinuationIndex = %d, want 2", records[0].ContinuationIndex)
	}
	if records[0].RecordType != usage.RecordTypeModelCall {
		t.Fatalf("records[0].RecordType = %q, want %q", records[0].RecordType, usage.RecordTypeModelCall)
	}
	if run.WorkflowState.Budget == nil {
		t.Fatal("run.WorkflowState.Budget = nil, want hot cache")
	}
	if run.WorkflowState.Budget.Usage.ModelTotalTokens != 150 {
		t.Fatalf("run.WorkflowState.Budget.Usage.ModelTotalTokens = %d, want 150", run.WorkflowState.Budget.Usage.ModelTotalTokens)
	}
	if run.WorkflowState.Budget.Usage.ModelCallCount != 1 {
		t.Fatalf("run.WorkflowState.Budget.Usage.ModelCallCount = %d, want 1", run.WorkflowState.Budget.Usage.ModelCallCount)
	}
	if run.WorkflowState.Budget.Usage.LastUpdatedAt.IsZero() {
		t.Fatal("run.WorkflowState.Budget.Usage.LastUpdatedAt = zero, want updated timestamp")
	}
}

func TestTrackToolBatchUsageUpdatesWorkflowBudgetHotCache(t *testing.T) {
	t.Parallel()

	component := (&AgentComponent{})
	run := &Run{
		ID: "run-usage-tools",
		WorkflowState: &WorkflowState{
			OriginalRunID: "run-root",
		},
	}
	run.WorkflowState.EnsureBudget(time.Now().UTC())

	component.trackToolBatchUsage(context.Background(), run, &Session{ID: "sess-001"}, []ToolCall{
		{ID: "call-1", Name: "exec.run"},
		{ID: "call-2", Name: "fs.write"},
	}, 2*time.Second)

	if got := run.WorkflowState.Budget.Usage.ToolExecutionCount; got != 2 {
		t.Fatalf("run.WorkflowState.Budget.Usage.ToolExecutionCount = %d, want 2", got)
	}
	if got := run.WorkflowState.Budget.Usage.ToolExecutionDuration; got != 2*time.Second {
		t.Fatalf("run.WorkflowState.Budget.Usage.ToolExecutionDuration = %s, want %s", got, 2*time.Second)
	}
}
