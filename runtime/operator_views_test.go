package runtime

import (
	"context"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/planner"
)

func TestBuildRunViewsIncludesExecutionGraphOnDemand(t *testing.T) {
	t.Parallel()

	svc := NewService(nil, agent.NewInMemorySessionStore(), agent.NewInMemoryRunStore(), approval.NewInMemoryStore(), eventbus.NewInMemoryBus(), nil)
	run := &agent.Run{
		ID:        "run-exec-graph",
		SessionID: "sess-exec-graph",
		Status:    agent.RunRunning,
		ExecutionGraph: &agent.ExecutionGraph{
			RunID:          "run-exec-graph",
			SessionID:      "sess-exec-graph",
			Scope:          "single_session",
			SingleSession:  true,
			SessionLocking: true,
			MergeStrategy:  agent.MergeStrategyTaskOrder,
			Tasks: []agent.ExecutionTask{{
				ID:              "write-brief",
				Status:          planner.TaskRunning,
				ResourceKeys:    []string{"output:brief.md"},
				SideEffectScope: agent.SideEffectScopeWorkspace,
				MergeStrategy:   agent.MergeStrategyTaskOrder,
				IdempotencyKey:  "task:brief",
			}},
		},
	}

	withoutGraph := svc.BuildRunViews(context.Background(), []*agent.Run{run}, RunListViewOptions{})
	if len(withoutGraph) != 1 || withoutGraph[0] == nil {
		t.Fatalf("BuildRunViews() = %#v", withoutGraph)
	}
	if withoutGraph[0].ExecutionGraph != nil {
		t.Fatalf("ExecutionGraph = %#v, want nil when diagnostics are not requested", withoutGraph[0].ExecutionGraph)
	}

	withGraph := svc.BuildRunViews(context.Background(), []*agent.Run{run}, RunListViewOptions{IncludeExecutionGraph: true})
	if len(withGraph) != 1 || withGraph[0] == nil || withGraph[0].ExecutionGraph == nil {
		t.Fatalf("ExecutionGraph view = %#v", withGraph)
	}
	if !withGraph[0].ExecutionGraph.SingleSession || !withGraph[0].ExecutionGraph.SessionLocking {
		t.Fatalf("ExecutionGraph session contract = %#v", withGraph[0].ExecutionGraph)
	}
	if len(withGraph[0].ExecutionGraph.Tasks) != 1 {
		t.Fatalf("ExecutionGraph.Tasks = %#v", withGraph[0].ExecutionGraph.Tasks)
	}
	task := withGraph[0].ExecutionGraph.Tasks[0]
	if task.IdempotencyKey != "task:brief" {
		t.Fatalf("task.IdempotencyKey = %q, want task:brief", task.IdempotencyKey)
	}
	if task.SideEffectScope != agent.SideEffectScopeWorkspace {
		t.Fatalf("task.SideEffectScope = %q, want %q", task.SideEffectScope, agent.SideEffectScopeWorkspace)
	}
	if len(task.ResourceKeys) != 1 || task.ResourceKeys[0] != "output:brief.md" {
		t.Fatalf("task.ResourceKeys = %#v, want [output:brief.md]", task.ResourceKeys)
	}
}

func TestBuildRunViewsIncludesSemanticSignalDiagnostics(t *testing.T) {
	t.Parallel()

	svc := NewService(nil, agent.NewInMemorySessionStore(), agent.NewInMemoryRunStore(), approval.NewInMemoryStore(), eventbus.NewInMemoryBus(), nil)
	run := &agent.Run{
		ID:        "run-semantic",
		SessionID: "sess-semantic",
		Status:    agent.RunRunning,
		SemanticSignal: &agent.SemanticSignal{
			Language: agent.LanguageProfile{
				Family:           "es",
				Script:           "Latn",
				MainSemanticPath: true,
			},
			ExecutionMode:       agent.ExecutionModeDirect,
			RequiresCurrentInfo: true,
			SuggestedDomains:    []string{"browser", "fs"},
			JobType:             "report",
			TargetSummary:       "docs/tmp/resumen.md",
			TriageReady:         true,
			TaskContractReady:   true,
			Reason:              "fresh_page_state",
		},
	}

	items := svc.BuildRunViews(context.Background(), []*agent.Run{run}, RunListViewOptions{})
	if len(items) != 1 || items[0] == nil {
		t.Fatalf("BuildRunViews() = %#v", items)
	}
	if items[0].SemanticSignal == nil {
		t.Fatal("expected semantic signal diagnostics on run view")
	}
	if items[0].SemanticSignal.Language.Family != "es" || items[0].SemanticSignal.Language.Script != "Latn" {
		t.Fatalf("SemanticSignal.Language = %#v", items[0].SemanticSignal.Language)
	}
	if !items[0].SemanticSignal.RequiresCurrentInfo || !items[0].SemanticSignal.TriageReady || !items[0].SemanticSignal.TaskContractReady {
		t.Fatalf("SemanticSignal readiness = %#v", items[0].SemanticSignal)
	}
	if items[0].SemanticSignal.TargetSummary != "docs/tmp/resumen.md" {
		t.Fatalf("SemanticSignal.TargetSummary = %q, want docs/tmp/resumen.md", items[0].SemanticSignal.TargetSummary)
	}
}
