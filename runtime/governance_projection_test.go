package runtime

import (
	"reflect"
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/eventbus"
)

func TestGovernanceDecisionFromApprovalIncludesPolicyLayers(t *testing.T) {
	t.Parallel()

	decision := governanceDecisionFromApproval(&approval.Ticket{
		Metadata: map[string]any{
			"policy_action":                 "require_approval",
			"policy_source":                 "policy.test/approval",
			"policy_summary":                "approval required by approval ticket",
			"policy_reasons":                []any{"needs review", "needs review", " "},
			"policy_reason_codes":           []any{"needs_review"},
			"policy_layers":                 []any{"policy.base", "policy.overlay", "policy.base"},
			"policy_audit_labels":           []any{"security_risk"},
			"policy_approval_default_scope": "once",
			"policy_approval_max_scope":     "session",
		},
	})
	if decision == nil {
		t.Fatal("expected governance decision")
	}
	if decision.PolicySource != "policy.test/approval" {
		t.Fatalf("decision.PolicySource = %q", decision.PolicySource)
	}
	wantLayers := []string{"policy.base", "policy.overlay"}
	if !reflect.DeepEqual(decision.PolicyLayers, wantLayers) {
		t.Fatalf("decision.PolicyLayers = %#v, want %#v", decision.PolicyLayers, wantLayers)
	}
	if !reflect.DeepEqual(decision.ReasonCodes, []string{"needs_review"}) {
		t.Fatalf("decision.ReasonCodes = %#v", decision.ReasonCodes)
	}
	if !reflect.DeepEqual(decision.AuditLabels, []string{"security_risk"}) {
		t.Fatalf("decision.AuditLabels = %#v", decision.AuditLabels)
	}
	if decision.ApprovalPolicy == nil || decision.ApprovalPolicy.DefaultScope != approval.ScopeOnce || decision.ApprovalPolicy.MaxScope != approval.ScopeSession {
		t.Fatalf("decision.ApprovalPolicy = %#v", decision.ApprovalPolicy)
	}
}

func TestGovernanceReceiptFromEventIncludesPolicyLayers(t *testing.T) {
	t.Parallel()

	receipt := governanceReceiptFromEvent(eventbus.Event{
		Type: eventbus.EventRunWaitingApproval,
		Attrs: map[string]any{
			"policy_action":                 "deny",
			"policy_source":                 "policy.test/event",
			"policy_summary":                "denied by overlay event policy",
			"policy_reason_codes":           []string{"policy_deny"},
			"policy_layers":                 []string{"policy.base", "policy.overlay"},
			"policy_audit_labels":           []string{"security_risk"},
			"policy_tool_names":             []string{"fs.write"},
			"policy_approval_default_scope": "once",
			"policy_approval_max_scope":     "session",
			"approval_id":                   "approval-1",
			"approval_kind":                 "tool_calls",
			"status":                        "pending",
		},
	})
	if receipt == nil {
		t.Fatal("expected governance receipt")
	}
	if receipt.Policy == nil {
		t.Fatalf("receipt.Policy = %#v", receipt.Policy)
	}
	wantLayers := []string{"policy.base", "policy.overlay"}
	if !reflect.DeepEqual(receipt.Policy.PolicyLayers, wantLayers) {
		t.Fatalf("receipt.Policy.PolicyLayers = %#v, want %#v", receipt.Policy.PolicyLayers, wantLayers)
	}
	if !reflect.DeepEqual(receipt.Policy.ReasonCodes, []string{"policy_deny"}) {
		t.Fatalf("receipt.Policy.ReasonCodes = %#v", receipt.Policy.ReasonCodes)
	}
	if !reflect.DeepEqual(receipt.Policy.AuditLabels, []string{"security_risk"}) {
		t.Fatalf("receipt.Policy.AuditLabels = %#v", receipt.Policy.AuditLabels)
	}
	if receipt.Approval == nil || receipt.Approval.ID != "approval-1" {
		t.Fatalf("receipt.Approval = %#v", receipt.Approval)
	}
	if receipt.Approval.Kind != approval.KindToolCalls {
		t.Fatalf("receipt.Approval.Kind = %q", receipt.Approval.Kind)
	}
	if !reflect.DeepEqual(receipt.ToolNames, []string{"fs.write"}) {
		t.Fatalf("receipt.ToolNames = %#v", receipt.ToolNames)
	}
}

func TestGovernanceReceiptFromEventIncludesApprovalExternalRefs(t *testing.T) {
	t.Parallel()

	receipt := governanceReceiptFromEvent(eventbus.Event{
		Type: eventbus.EventApprovalResolved,
		Attrs: map[string]any{
			"approval_id":   "approval-1",
			"approval_kind": "tool_calls",
			"status":        "approved",
			"approval_external_refs": []map[string]any{{
				"provider":    "jira",
				"external_id": "JIRA-123",
				"url":         "https://jira.example.com/browse/JIRA-123",
				"status":      "approved",
				"synced_at":   "2026-03-19T10:00:00Z",
			}},
		},
	})
	if receipt == nil || receipt.Approval == nil {
		t.Fatalf("receipt = %#v", receipt)
	}
	if len(receipt.Approval.External) != 1 {
		t.Fatalf("receipt.Approval.External = %#v", receipt.Approval.External)
	}
	if receipt.Approval.External[0].Provider != "jira" || receipt.Approval.External[0].ExternalID != "JIRA-123" {
		t.Fatalf("receipt.Approval.External[0] = %#v", receipt.Approval.External[0])
	}
	if !strings.Contains(receipt.Summary, "providers=jira") {
		t.Fatalf("receipt.Summary = %q", receipt.Summary)
	}
}

func TestGovernanceDecisionFromApprovalReturnsNilWhenPolicyMetadataMissing(t *testing.T) {
	t.Parallel()

	if decision := governanceDecisionFromApproval(&approval.Ticket{Metadata: map[string]any{}}); decision != nil {
		t.Fatalf("decision = %#v, want nil", decision)
	}
}
