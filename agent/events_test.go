package agent

import (
	"reflect"
	"testing"

	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/eventbus"
	domaingov "github.com/fulcrus/hopclaw/internal/domain/governance"
	domainscope "github.com/fulcrus/hopclaw/internal/domain/scope"
)

func TestBuildToolExecutedPayloadBasic(t *testing.T) {
	t.Parallel()

	calls := []ToolCall{
		{Name: "fs.read"},
		{Name: "exec.run"},
	}
	results := []contextengine.ToolResult{
		{ToolName: "fs.read", ToolCallID: "call-1"},
		{ToolName: "exec.run", ToolCallID: "call-2"},
	}

	payload := buildToolExecutedPayload(nil, calls, results, "", 3)
	if payload.ToolCount != 2 {
		t.Fatalf("payload.ToolCount = %d, want 2", payload.ToolCount)
	}
	if payload.ToolRound != 3 {
		t.Fatalf("payload.ToolRound = %d, want 3", payload.ToolRound)
	}
	if len(payload.ToolNames) != 2 {
		t.Fatalf("payload.ToolNames = %#v", payload.ToolNames)
	}
}

func TestBuildToolExecutedPayloadSkipsEmptyNames(t *testing.T) {
	t.Parallel()

	calls := []ToolCall{
		{Name: "fs.read"},
		{Name: ""},
		{Name: "exec.run"},
	}
	payload := buildToolExecutedPayload(nil, calls, nil, "", 1)
	if len(payload.ToolNames) != 2 {
		t.Fatalf("expected 2 non-empty tool names, got %d", len(payload.ToolNames))
	}
}

func TestBuildToolExecutedPayloadWithApprovalID(t *testing.T) {
	t.Parallel()

	payload := buildToolExecutedPayload(nil, nil, nil, "approval-123", 1)
	if payload.ApprovalID != "approval-123" {
		t.Fatalf("payload.ApprovalID = %q", payload.ApprovalID)
	}
}

func TestBuildToolExecutedPayloadWithArtifacts(t *testing.T) {
	t.Parallel()

	results := []contextengine.ToolResult{
		{ToolName: "fs.write", ToolCallID: "call-1", ArtifactURI: "artifact://abc"},
		{ToolName: "fs.read", ToolCallID: "call-2"},
	}

	payload := buildToolExecutedPayload(nil, nil, results, "", 1)
	if payload.ArtifactCount != 1 {
		t.Fatalf("payload.ArtifactCount = %d, want 1", payload.ArtifactCount)
	}
	if len(payload.ArtifactURIs) != 1 || payload.ArtifactURIs[0] != "artifact://abc" {
		t.Fatalf("payload.ArtifactURIs = %#v", payload.ArtifactURIs)
	}
}

func TestBuildToolExecutedEventIncludesGovernance(t *testing.T) {
	t.Parallel()

	run := &Run{
		Scope: domainscope.Ref{
			AutomationID: "auto-events",
		}.Normalize(),
		Governance: &domaingov.Evaluation{
			Decision: domaingov.Decision{
				Action:       domaingov.DecisionRequireApproval,
				PolicySource: "policy.test/events",
				Summary:      "approval required for file write",
				Reasons:      []string{"file write needs approval"},
				ReasonCodes:  []string{"tool_requires_approval"},
				PolicyLayers: []string{"policy.base", "policy.overlay"},
				AuditLabels:  []string{"write_tool"},
				ApprovalPolicy: &domaingov.ApprovalPolicy{
					DefaultScope: "once",
					MaxScope:     "session",
				},
			},
			ToolNames:                 []string{"fs.write"},
			EffectiveConfigSnapshotID: "ecs-events-1",
		},
	}

	event := eventbus.NewToolExecutedEvent(run.ID, run.SessionID, buildToolExecutedPayload(run, []ToolCall{{Name: "fs.write"}}, nil, "", 1), buildGovernanceEventAttrs(run))
	attrs := event.Attrs
	if attrs["effective_config_snapshot_id"] != "ecs-events-1" {
		t.Fatalf("effective_config_snapshot_id = %v", attrs["effective_config_snapshot_id"])
	}
	if attrs["policy_source"] != "policy.test/events" {
		t.Fatalf("policy_source = %v", attrs["policy_source"])
	}
	layers, ok := attrs["policy_layers"].([]string)
	if !ok || !reflect.DeepEqual(layers, []string{"policy.base", "policy.overlay"}) {
		t.Fatalf("policy_layers = %#v", attrs["policy_layers"])
	}
	if codes, ok := attrs["policy_reason_codes"].([]string); !ok || !reflect.DeepEqual(codes, []string{"tool_requires_approval"}) {
		t.Fatalf("policy_reason_codes = %#v", attrs["policy_reason_codes"])
	}
	if labels, ok := attrs["policy_audit_labels"].([]string); !ok || !reflect.DeepEqual(labels, []string{"write_tool"}) {
		t.Fatalf("policy_audit_labels = %#v", attrs["policy_audit_labels"])
	}
	if attrs["policy_approval_default_scope"] != "once" {
		t.Fatalf("policy_approval_default_scope = %#v", attrs["policy_approval_default_scope"])
	}
	if attrs["policy_approval_max_scope"] != "session" {
		t.Fatalf("policy_approval_max_scope = %#v", attrs["policy_approval_max_scope"])
	}
	scope, ok := attrs["scope"].(domainscope.Ref)
	if !ok || scope.AutomationID != "auto-events" {
		t.Fatalf("scope = %#v", attrs["scope"])
	}
}

func TestBuildPlanSnapshotAttrs(t *testing.T) {
	t.Parallel()

	snap := PlanExecutionSnapshot{
		Completed:        3,
		FailedCount:      1,
		SkippedCount:     0,
		Total:            5,
		FinalTask:        "summary",
		CoverageWarnings: []string{"deliverable \"document\" is not clearly covered by any plan task"},
		RunningTasks: []TaskProgressItem{
			{ID: "t-4", Title: "compile"},
		},
	}

	attrs := buildPlanSnapshotAttrs(snap)
	if attrs["completed_count"] != 3 {
		t.Fatalf("completed_count = %v", attrs["completed_count"])
	}
	if attrs["failed_count"] != 1 {
		t.Fatalf("failed_count = %v", attrs["failed_count"])
	}
	if attrs["total_tasks"] != 5 {
		t.Fatalf("total_tasks = %v", attrs["total_tasks"])
	}
	if attrs["final_task"] != "summary" {
		t.Fatalf("final_task = %v", attrs["final_task"])
	}
	ids, ok := attrs["running_task_ids"].([]string)
	if !ok || len(ids) != 1 || ids[0] != "t-4" {
		t.Fatalf("running_task_ids = %v", attrs["running_task_ids"])
	}
	if warnings, ok := attrs["plan_coverage_warnings"].([]string); !ok || len(warnings) != 1 {
		t.Fatalf("plan_coverage_warnings = %#v", attrs["plan_coverage_warnings"])
	}
}

func TestBuildPlanSnapshotAttrsNoRunning(t *testing.T) {
	t.Parallel()

	snap := PlanExecutionSnapshot{
		Completed: 5,
		Total:     5,
	}

	attrs := buildPlanSnapshotAttrs(snap)
	if _, ok := attrs["running_task_ids"]; ok {
		t.Fatal("running_task_ids should not be set when no running tasks")
	}
}
