package runtime

import (
	"strings"

	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/controlplane"
	domaingov "github.com/fulcrus/hopclaw/internal/domain/governance"
	domainscope "github.com/fulcrus/hopclaw/internal/domain/scope"
)

func publicScopeRef(scope domainscope.Ref) controlplane.ScopeRef {
	return controlplane.ScopeRef{AutomationID: strings.TrimSpace(scope.AutomationID)}.Normalize()
}

func internalScopeRef(scope controlplane.ScopeRef) domainscope.Ref {
	return domainscope.Ref{AutomationID: strings.TrimSpace(scope.AutomationID)}.Normalize()
}

func publicGovernanceApproval(in *domaingov.EventApproval) *controlplane.GovernanceApproval {
	if in == nil {
		return nil
	}
	out := controlplane.GovernanceApproval{
		ID:       strings.TrimSpace(in.ID),
		Kind:     in.Kind,
		Status:   in.Status,
		External: append([]approval.ExternalReference(nil), in.External...),
	}
	return controlplane.CloneGovernanceApproval(&out)
}

func internalGovernanceApproval(in *controlplane.GovernanceApproval) *domaingov.EventApproval {
	if in == nil {
		return nil
	}
	normalized := in.Normalized()
	out := &domaingov.EventApproval{
		ID:       strings.TrimSpace(normalized.ID),
		Kind:     normalized.Kind,
		Status:   normalized.Status,
		External: append([]approval.ExternalReference(nil), normalized.External...),
	}
	return out
}

func publicGovernanceDecision(in *domaingov.Decision) *controlplane.GovernanceDecision {
	if in == nil {
		return nil
	}
	out := controlplane.GovernanceDecision{
		Action:       controlplane.GovernanceDecisionAction(in.Action),
		Reasons:      append([]string(nil), in.Reasons...),
		ReasonCodes:  append([]string(nil), in.ReasonCodes...),
		PolicySource: strings.TrimSpace(in.PolicySource),
		Summary:      strings.TrimSpace(in.Summary),
		PolicyLayers: append([]string(nil), in.PolicyLayers...),
		AuditLabels:  append([]string(nil), in.AuditLabels...),
	}
	if in.ApprovalPolicy != nil {
		out.ApprovalPolicy = &controlplane.GovernanceDecisionApprovalPolicy{
			DefaultScope: in.ApprovalPolicy.DefaultScope,
			MaxScope:     in.ApprovalPolicy.MaxScope,
		}
	}
	normalized := out.Normalized()
	return &normalized
}

func internalGovernanceDecision(in *controlplane.GovernanceDecision) *domaingov.Decision {
	if in == nil {
		return nil
	}
	normalized := in.Normalized()
	out := &domaingov.Decision{
		Action:       domaingov.DecisionAction(normalized.Action),
		Reasons:      append([]string(nil), normalized.Reasons...),
		ReasonCodes:  append([]string(nil), normalized.ReasonCodes...),
		PolicySource: strings.TrimSpace(normalized.PolicySource),
		Summary:      strings.TrimSpace(normalized.Summary),
		PolicyLayers: append([]string(nil), normalized.PolicyLayers...),
		AuditLabels:  append([]string(nil), normalized.AuditLabels...),
	}
	if normalized.ApprovalPolicy != nil {
		out.ApprovalPolicy = &domaingov.ApprovalPolicy{
			DefaultScope: normalized.ApprovalPolicy.DefaultScope,
			MaxScope:     normalized.ApprovalPolicy.MaxScope,
		}
	}
	return out
}

func publicGovernanceEvaluation(in *domaingov.Evaluation) *controlplane.GovernanceEvaluation {
	if in == nil {
		return nil
	}
	out := controlplane.GovernanceEvaluation{
		Decision:                  *publicGovernanceDecision(&in.Decision),
		ToolNames:                 append([]string(nil), in.ToolNames...),
		EffectiveConfigSnapshotID: strings.TrimSpace(in.EffectiveConfigSnapshotID),
		UpdatedAt:                 in.UpdatedAt,
	}
	normalized := out.Normalized()
	return &normalized
}
