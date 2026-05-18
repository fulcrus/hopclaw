package eventbus

import (
	"reflect"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/approval"
	domainscope "github.com/fulcrus/hopclaw/internal/domain/scope"
)

// ---------------------------------------------------------------------------
// RunPlannedAttrs
// ---------------------------------------------------------------------------

func TestRunPlannedAttrs_ToMap(t *testing.T) {
	t.Parallel()

	attrs := RunPlannedAttrs{
		TaskCount:        5,
		Strategy:         "parallel",
		Replanned:        true,
		CoverageWarnings: []string{"deliverable \"spreadsheet\" is not clearly covered by any plan task"},
	}
	m := attrs.ToMap()

	if m["task_count"] != 5 {
		t.Errorf("task_count = %v, want 5", m["task_count"])
	}
	if m["strategy"] != "parallel" {
		t.Errorf("strategy = %v, want parallel", m["strategy"])
	}
	if m["replanned"] != true {
		t.Errorf("replanned = %v, want true", m["replanned"])
	}
	if warnings, ok := m["plan_coverage_warnings"].([]string); !ok || len(warnings) != 1 {
		t.Fatalf("plan_coverage_warnings = %#v", m["plan_coverage_warnings"])
	}
}

// ---------------------------------------------------------------------------
// ModelRetryAttrs
// ---------------------------------------------------------------------------

func TestModelRetryAttrs_ToMap(t *testing.T) {
	t.Parallel()

	attrs := ModelRetryAttrs{
		Model:         "gpt-4o",
		Attempt:       2,
		MaxAttempts:   3,
		FailureReason: "rate_limit",
		DelayMs:       500,
		Error:         "429 Too Many Requests",
	}
	m := attrs.ToMap()

	if m["model"] != "gpt-4o" {
		t.Errorf("model = %v, want gpt-4o", m["model"])
	}
	if m["attempt"] != 2 {
		t.Errorf("attempt = %v, want 2", m["attempt"])
	}
	if m["max_attempts"] != 3 {
		t.Errorf("max_attempts = %v, want 3", m["max_attempts"])
	}
	if m["failure_reason"] != "rate_limit" {
		t.Errorf("failure_reason = %v, want rate_limit", m["failure_reason"])
	}
	if m["delay_ms"] != int64(500) {
		t.Errorf("delay_ms = %v, want 500", m["delay_ms"])
	}
	if m["error"] != "429 Too Many Requests" {
		t.Errorf("error = %v, want 429 Too Many Requests", m["error"])
	}
}

// ---------------------------------------------------------------------------
// ThinkingDegradedAttrs
// ---------------------------------------------------------------------------

func TestThinkingDegradedAttrs_ToMap(t *testing.T) {
	t.Parallel()

	attrs := ThinkingDegradedAttrs{
		Model:   "claude-opus-4-20250515",
		From:    "high",
		To:      "low",
		Reason:  "budget exceeded",
		Error:   "thinking budget exhausted",
		Attempt: 1,
	}
	m := attrs.ToMap()

	if m["model"] != "claude-opus-4-20250515" {
		t.Errorf("model = %v", m["model"])
	}
	if m["from"] != "high" {
		t.Errorf("from = %v, want high", m["from"])
	}
	if m["to"] != "low" {
		t.Errorf("to = %v, want low", m["to"])
	}
	if m["reason"] != "budget exceeded" {
		t.Errorf("reason = %v", m["reason"])
	}
	if m["error"] != "thinking budget exhausted" {
		t.Errorf("error = %v", m["error"])
	}
	if m["attempt"] != 1 {
		t.Errorf("attempt = %v, want 1", m["attempt"])
	}
}

// ---------------------------------------------------------------------------
// ModelRoutedAttrs
// ---------------------------------------------------------------------------

func TestModelRoutedAttrs_ToMap(t *testing.T) {
	t.Parallel()

	attrs := ModelRoutedAttrs{
		RequestedModel: "gpt-4o",
		SelectedModel:  "gpt-4o-mini",
		FailoverFrom:   "gpt-4o",
		Reason:         "rate limited",
	}
	m := attrs.ToMap()

	if m["requested_model"] != "gpt-4o" {
		t.Errorf("requested_model = %v", m["requested_model"])
	}
	if m["selected_model"] != "gpt-4o-mini" {
		t.Errorf("selected_model = %v", m["selected_model"])
	}
	if m["failover_from"] != "gpt-4o" {
		t.Errorf("failover_from = %v", m["failover_from"])
	}
	if m["reason"] != "rate limited" {
		t.Errorf("reason = %v", m["reason"])
	}
}

func TestModelRoutedAttrs_ToMap_OmitsEmpty(t *testing.T) {
	t.Parallel()

	attrs := ModelRoutedAttrs{
		RequestedModel: "gpt-4o",
		SelectedModel:  "gpt-4o",
	}
	m := attrs.ToMap()

	if _, ok := m["failover_from"]; ok {
		t.Error("expected failover_from to be omitted when empty")
	}
	if _, ok := m["reason"]; ok {
		t.Error("expected reason to be omitted when empty")
	}
}

// ---------------------------------------------------------------------------
// ModelFailoverAttrs
// ---------------------------------------------------------------------------

func TestModelFailoverAttrs_ToMap(t *testing.T) {
	t.Parallel()

	attrs := ModelFailoverAttrs{
		FromModel: "gpt-4o",
		ToModel:   "gpt-4o-mini",
		Reason:    "quota exceeded",
	}
	m := attrs.ToMap()

	if m["from_model"] != "gpt-4o" {
		t.Errorf("from_model = %v", m["from_model"])
	}
	if m["to_model"] != "gpt-4o-mini" {
		t.Errorf("to_model = %v", m["to_model"])
	}
	if m["reason"] != "quota exceeded" {
		t.Errorf("reason = %v", m["reason"])
	}
}

func TestModelFailoverAttrs_ToMap_OmitsEmptyReason(t *testing.T) {
	t.Parallel()

	attrs := ModelFailoverAttrs{
		FromModel: "gpt-4o",
		ToModel:   "gpt-4o-mini",
	}
	m := attrs.ToMap()

	if _, ok := m["reason"]; ok {
		t.Error("expected reason to be omitted when empty")
	}
}

// ---------------------------------------------------------------------------
// ToolExecutedAttrs
// ---------------------------------------------------------------------------

func TestToolExecutedAttrs_ToMap(t *testing.T) {
	t.Parallel()

	attrs := ToolExecutedAttrs{
		ToolCallID:   "call-123",
		ToolName:     "exec.run",
		Success:      true,
		ArtifactRefs: []string{"art-1", "art-2"},
	}
	m := attrs.ToMap()

	if m["tool_call_id"] != "call-123" {
		t.Errorf("tool_call_id = %v", m["tool_call_id"])
	}
	if m["tool_name"] != "exec.run" {
		t.Errorf("tool_name = %v", m["tool_name"])
	}
	if m["success"] != true {
		t.Errorf("success = %v, want true", m["success"])
	}
	refs, ok := m["artifact_refs"].([]string)
	if !ok {
		t.Fatalf("artifact_refs type = %T, want []string", m["artifact_refs"])
	}
	if len(refs) != 2 {
		t.Errorf("artifact_refs len = %d, want 2", len(refs))
	}
}

func TestApprovalEventAttrs_ToMap(t *testing.T) {
	t.Parallel()

	attrs := ApprovalEventAttrs{
		ApprovalID:    "appr-1",
		ApprovalKind:  "tool_calls",
		Status:        "pending",
		ResolvedBy:    "operator",
		ToolCount:     2,
		Reasons:       []string{"needs review", "needs review"},
		PolicySource:  "policy.overlay/test",
		PolicySummary: "approval required",
		ExternalRefs: []ApprovalExternalRefAttrs{{
			Provider:   "jira",
			ExternalID: "JIRA-123",
			URL:        "https://jira.example.com/browse/JIRA-123",
			Status:     "pending_remote",
			SyncedAt:   "2026-03-19T10:00:00Z",
		}},
	}
	m := attrs.ToMap()

	if m["approval_id"] != "appr-1" {
		t.Fatalf("approval_id = %v", m["approval_id"])
	}
	if m["approval_kind"] != "tool_calls" {
		t.Fatalf("approval_kind = %v", m["approval_kind"])
	}
	if m["status"] != "pending" {
		t.Fatalf("status = %v", m["status"])
	}
	if m["resolved_by"] != "operator" {
		t.Fatalf("resolved_by = %v", m["resolved_by"])
	}
	if m["tool_count"] != 2 {
		t.Fatalf("tool_count = %v", m["tool_count"])
	}
	if !reflect.DeepEqual(m["reasons"], []string{"needs review"}) {
		t.Fatalf("reasons = %#v", m["reasons"])
	}
	if m["policy_source"] != "policy.overlay/test" {
		t.Fatalf("policy_source = %v", m["policy_source"])
	}
	if m["policy_summary"] != "approval required" {
		t.Fatalf("policy_summary = %v", m["policy_summary"])
	}
	refs, ok := m["approval_external_refs"].([]map[string]any)
	if !ok || len(refs) != 1 {
		t.Fatalf("approval_external_refs = %#v", m["approval_external_refs"])
	}
	if refs[0]["provider"] != "jira" || refs[0]["external_id"] != "JIRA-123" || refs[0]["status"] != "pending_remote" {
		t.Fatalf("approval_external_refs[0] = %#v", refs[0])
	}
	if !reflect.DeepEqual(m["approval_external_providers"], []string{"jira"}) {
		t.Fatalf("approval_external_providers = %#v", m["approval_external_providers"])
	}
	if !reflect.DeepEqual(m["approval_external_statuses"], []string{"jira:pending_remote"}) {
		t.Fatalf("approval_external_statuses = %#v", m["approval_external_statuses"])
	}
}

func TestApprovalExternalReferencesFromValue(t *testing.T) {
	t.Parallel()

	got := ApprovalExternalReferencesFromValue([]any{
		map[string]any{
			"provider":    "jira",
			"external_id": "JIRA-123",
			"url":         "https://jira.example.com/browse/JIRA-123",
			"status":      "approved",
			"synced_at":   "2026-03-19T10:00:00Z",
		},
		ApprovalExternalRefAttrs{
			Provider:   "bpm",
			ExternalID: "FLOW-9",
			Status:     "pending_remote",
			SyncedAt:   "2026-03-19T10:05:00Z",
		},
	})
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	if got[0].Provider != "jira" || got[0].ExternalID != "JIRA-123" || got[0].Status != "approved" {
		t.Fatalf("got[0] = %#v", got[0])
	}
	if got[0].SyncedAt.IsZero() || got[0].SyncedAt.UTC().Format(time.RFC3339) != "2026-03-19T10:00:00Z" {
		t.Fatalf("got[0].SyncedAt = %v", got[0].SyncedAt)
	}
	if got[1].Provider != "bpm" || got[1].ExternalID != "FLOW-9" || got[1].Status != "pending_remote" {
		t.Fatalf("got[1] = %#v", got[1])
	}
}

func TestApprovalExternalReferencesFromValue_ClonesApprovalRefs(t *testing.T) {
	t.Parallel()

	source := []approval.ExternalReference{{
		Provider:   "jira",
		ExternalID: "JIRA-123",
		Status:     "pending_remote",
		Metadata: map[string]any{
			"ticket_type": "change",
		},
	}}
	got := ApprovalExternalReferencesFromValue(source)
	if len(got) != 1 {
		t.Fatalf("len(got) = %d", len(got))
	}
	got[0].Metadata["ticket_type"] = "incident"
	if source[0].Metadata["ticket_type"] != "change" {
		t.Fatalf("source metadata mutated: %#v", source[0].Metadata)
	}
}

func TestGovernanceAttrs_ToMap(t *testing.T) {
	t.Parallel()

	attrs := GovernanceAttrs{
		Scope: domainscope.Ref{
			AutomationID: "auto-1",
		},
		EffectiveConfigSnapshotID:  "ecs-1",
		PolicyAction:               "require_approval",
		PolicySource:               "policy.test/source",
		PolicySummary:              "approval required by test policy",
		PolicyReasons:              []string{"reason-1", "reason-2"},
		PolicyReasonCodes:          []string{"tool_requires_approval"},
		PolicyLayers:               []string{"policy.base", "policy.overlay"},
		PolicyAuditLabels:          []string{"security_risk"},
		PolicyApprovalDefaultScope: "once",
		PolicyApprovalMaxScope:     "session",
		PolicyToolNames:            []string{"fs.write"},
	}
	m := attrs.ToMap()

	if m["effective_config_snapshot_id"] != "ecs-1" {
		t.Fatalf("effective_config_snapshot_id = %v", m["effective_config_snapshot_id"])
	}
	if m["policy_source"] != "policy.test/source" {
		t.Fatalf("policy_source = %v", m["policy_source"])
	}
	scope, ok := m["scope"].(domainscope.Ref)
	if !ok || scope.AutomationID != "auto-1" {
		t.Fatalf("scope = %#v", m["scope"])
	}
	layers, ok := m["policy_layers"].([]string)
	if !ok || !reflect.DeepEqual(layers, []string{"policy.base", "policy.overlay"}) {
		t.Fatalf("policy_layers = %#v", m["policy_layers"])
	}
	if codes, ok := m["policy_reason_codes"].([]string); !ok || !reflect.DeepEqual(codes, []string{"tool_requires_approval"}) {
		t.Fatalf("policy_reason_codes = %#v", m["policy_reason_codes"])
	}
	if labels, ok := m["policy_audit_labels"].([]string); !ok || !reflect.DeepEqual(labels, []string{"security_risk"}) {
		t.Fatalf("policy_audit_labels = %#v", m["policy_audit_labels"])
	}
	if m["policy_approval_default_scope"] != "once" {
		t.Fatalf("policy_approval_default_scope = %#v", m["policy_approval_default_scope"])
	}
	if m["policy_approval_max_scope"] != "session" {
		t.Fatalf("policy_approval_max_scope = %#v", m["policy_approval_max_scope"])
	}
}

func TestToolExecutedAttrs_ToMap_OmitsEmpty(t *testing.T) {
	t.Parallel()

	attrs := ToolExecutedAttrs{
		ToolCallID: "call-456",
		ToolName:   "fs.read",
		Success:    false,
	}
	m := attrs.ToMap()

	if _, ok := m["error"]; ok {
		t.Error("expected error to be omitted when empty")
	}
	if _, ok := m["artifact_refs"]; ok {
		t.Error("expected artifact_refs to be omitted when empty")
	}
}

func TestToolExecutedAttrs_ToMap_WithError(t *testing.T) {
	t.Parallel()

	attrs := ToolExecutedAttrs{
		ToolCallID: "call-789",
		ToolName:   "exec.run",
		Success:    false,
		Error:      "command timed out",
	}
	m := attrs.ToMap()

	if m["error"] != "command timed out" {
		t.Errorf("error = %v", m["error"])
	}
}

// ---------------------------------------------------------------------------
// RunStatusAttrs
// ---------------------------------------------------------------------------

func TestRunStatusAttrs_ToMap_WithError(t *testing.T) {
	t.Parallel()

	attrs := RunStatusAttrs{Error: "context deadline exceeded", Summary: "request timed out"}
	m := attrs.ToMap()

	if m == nil {
		t.Fatal("expected non-nil map when error is set")
	}
	if m["error"] != "context deadline exceeded" {
		t.Errorf("error = %v", m["error"])
	}
	if m["summary"] != "request timed out" {
		t.Errorf("summary = %v", m["summary"])
	}
}

func TestRunStatusAttrs_ToMap_NoError(t *testing.T) {
	t.Parallel()

	attrs := RunStatusAttrs{}
	m := attrs.ToMap()

	if m != nil {
		t.Errorf("expected nil map when no error, got %v", m)
	}
}

func TestRunStatusAttrs_ToMap_WithSummaryOnly(t *testing.T) {
	t.Parallel()

	attrs := RunStatusAttrs{Summary: "task completed"}
	m := attrs.ToMap()

	if m == nil {
		t.Fatal("expected non-nil map when summary is set")
	}
	if m["summary"] != "task completed" {
		t.Errorf("summary = %v", m["summary"])
	}
	if _, ok := m["error"]; ok {
		t.Errorf("expected error to be omitted when empty")
	}
}

func TestRunStatusAttrs_ToMap_WithChannelOnly(t *testing.T) {
	t.Parallel()

	attrs := RunStatusAttrs{Channel: "slack"}
	m := attrs.ToMap()

	if m == nil {
		t.Fatal("expected non-nil map when channel is set")
	}
	if m["channel"] != "slack" {
		t.Errorf("channel = %v", m["channel"])
	}
	if _, ok := m["error"]; ok {
		t.Errorf("expected error to be omitted when empty")
	}
	if _, ok := m["summary"]; ok {
		t.Errorf("expected summary to be omitted when empty")
	}
}

// ---------------------------------------------------------------------------
// PlanTaskAttrs
// ---------------------------------------------------------------------------

func TestPlanTaskAttrs_ToMap_Full(t *testing.T) {
	t.Parallel()

	attrs := PlanTaskAttrs{
		TaskID:         "task-1",
		Title:          "Research",
		Kind:           "research",
		Goal:           "find papers",
		Error:          "timeout",
		ResultSummary:  "found 5 papers",
		CompletedCount: 3,
		TotalTasks:     5,
		FinalTask:      "synthesize results",
	}
	m := attrs.ToMap()

	if m["task_id"] != "task-1" {
		t.Errorf("task_id = %v", m["task_id"])
	}
	if m["task_title"] != "Research" {
		t.Errorf("task_title = %v", m["task_title"])
	}
	if m["task_kind"] != "research" {
		t.Errorf("task_kind = %v", m["task_kind"])
	}
	if m["task_goal"] != "find papers" {
		t.Errorf("task_goal = %v", m["task_goal"])
	}
	if m["error"] != "timeout" {
		t.Errorf("error = %v", m["error"])
	}
	if m["result_summary"] != "found 5 papers" {
		t.Errorf("result_summary = %v", m["result_summary"])
	}
	if m["completed_count"] != 3 {
		t.Errorf("completed_count = %v", m["completed_count"])
	}
	if m["total_tasks"] != 5 {
		t.Errorf("total_tasks = %v", m["total_tasks"])
	}
	if m["final_task"] != "synthesize results" {
		t.Errorf("final_task = %v", m["final_task"])
	}
}

func TestPlanTaskAttrs_ToMap_Minimal(t *testing.T) {
	t.Parallel()

	attrs := PlanTaskAttrs{TaskID: "task-2"}
	m := attrs.ToMap()

	if m["task_id"] != "task-2" {
		t.Errorf("task_id = %v", m["task_id"])
	}
	// Optional fields should be omitted.
	for _, key := range []string{"task_title", "task_kind", "task_goal", "error", "result_summary", "final_task"} {
		if _, ok := m[key]; ok {
			t.Errorf("expected %s to be omitted in minimal attrs", key)
		}
	}
	// completed_count and total_tasks are omitted when both are zero.
	if _, ok := m["completed_count"]; ok {
		t.Error("expected completed_count to be omitted when zero")
	}
}
