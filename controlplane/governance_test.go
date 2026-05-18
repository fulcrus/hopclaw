package controlplane

import (
	"testing"

	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/eventbus"
)

func TestScopeRefFromValueAndSummary(t *testing.T) {
	t.Parallel()

	ref := ScopeRefFromValue(map[string]any{
		"automation_id": "  auto-1  ",
	})
	if ref.AutomationID != "auto-1" {
		t.Fatalf("AutomationID = %q, want auto-1", ref.AutomationID)
	}
	if summary := ScopeSummary(ref); summary != "automation=auto-1" {
		t.Fatalf("ScopeSummary() = %q, want automation=auto-1", summary)
	}
}

func TestGovernanceDecisionNormalized(t *testing.T) {
	t.Parallel()

	decision := GovernanceDecision{
		Action:       GovernanceDecisionAction("invalid"),
		ReasonCodes:  []string{"dup", "dup", "  unique  ", ""},
		PolicySource: "  policy-pack  ",
		Summary:      "  requires approval  ",
		PolicyLayers: []string{"base", "base", "overlay"},
		AuditLabels:  []string{"ops", "ops", "high-risk"},
		ApprovalPolicy: &GovernanceDecisionApprovalPolicy{
			DefaultScope: approval.ScopeSession,
			MaxScope:     approval.ScopeOnce,
		},
	}

	normalized := decision.Normalized()
	if normalized.Action != GovernanceDecisionAllow {
		t.Fatalf("Action = %q, want %q", normalized.Action, GovernanceDecisionAllow)
	}
	if len(normalized.ReasonCodes) != 2 || normalized.ReasonCodes[0] != "dup" || normalized.ReasonCodes[1] != "unique" {
		t.Fatalf("ReasonCodes = %#v", normalized.ReasonCodes)
	}
	if normalized.ApprovalPolicy == nil || normalized.ApprovalPolicy.DefaultScope != approval.ScopeOnce || normalized.ApprovalPolicy.MaxScope != approval.ScopeOnce {
		t.Fatalf("ApprovalPolicy = %#v", normalized.ApprovalPolicy)
	}
}

func TestGovernanceDeliveryEntryNormalized(t *testing.T) {
	t.Parallel()

	entry := GovernanceDeliveryEntry{
		ID:          "  gdel-1  ",
		AdapterName: "  audit-hub  ",
		Record: GovernanceDeliveryRecord{
			Kind:      GovernanceKindSecurityEvent,
			EventID:   "  evt-1  ",
			EventType: eventbus.EventSecurityRiskDetected,
			RunID:     "  run-1  ",
			SessionID: "  sess-1  ",
			Scope:     ScopeRef{AutomationID: "  auto-1  "},
			ToolNames: []string{"scan", "scan", " notify "},
		},
	}

	normalized := entry.Normalized()
	if normalized.Status != GovernanceDeliveryStatusPending {
		t.Fatalf("Status = %q, want pending", normalized.Status)
	}
	if normalized.ID != "gdel-1" || normalized.AdapterName != "audit-hub" {
		t.Fatalf("entry identifiers = %#v", normalized)
	}
	if normalized.Record.Scope.AutomationID != "auto-1" {
		t.Fatalf("Scope = %#v, want auto-1", normalized.Record.Scope)
	}
	if len(normalized.Record.ToolNames) != 2 || normalized.Record.ToolNames[1] != "notify" {
		t.Fatalf("ToolNames = %#v", normalized.Record.ToolNames)
	}
}
