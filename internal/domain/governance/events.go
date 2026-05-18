package governance

import (
	"strings"

	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/eventbus"
	domainscope "github.com/fulcrus/hopclaw/internal/domain/scope"
	supportmaps "github.com/fulcrus/hopclaw/internal/support/maps"
	"github.com/fulcrus/hopclaw/internal/support/normalize"
)

type EventApproval struct {
	ID       string                       `json:"id,omitempty"`
	Kind     approval.Kind                `json:"kind,omitempty"`
	Status   approval.Status              `json:"status,omitempty"`
	External []approval.ExternalReference `json:"external,omitempty"`
}

func (a EventApproval) Normalized() EventApproval {
	out := a
	out.ID = strings.TrimSpace(out.ID)
	out.Kind = approval.Kind(strings.TrimSpace(string(out.Kind)))
	out.Status = approval.Status(strings.TrimSpace(string(out.Status)))
	out.External = approval.CloneExternalReferences(out.External)
	return out
}

func (a EventApproval) Empty() bool {
	normalized := a.Normalized()
	return normalized.ID == "" &&
		normalized.Kind == "" &&
		normalized.Status == "" &&
		len(normalized.External) == 0
}

func CloneEventApproval(in *EventApproval) *EventApproval {
	if in == nil {
		return nil
	}
	out := in.Normalized()
	if out.Empty() {
		return nil
	}
	return &out
}

type EventContext struct {
	Scope                     domainscope.Ref `json:"scope,omitempty"`
	EffectiveConfigSnapshotID string          `json:"effective_config_snapshot_id,omitempty"`
	Policy                    *Decision       `json:"policy,omitempty"`
	Approval                  *EventApproval  `json:"approval,omitempty"`
	ToolNames                 []string        `json:"tool_names,omitempty"`
	Summary                   string          `json:"summary,omitempty"`
}

func (c EventContext) Normalized() EventContext {
	out := c
	out.Scope = out.Scope.Normalize()
	out.EffectiveConfigSnapshotID = strings.TrimSpace(out.EffectiveConfigSnapshotID)
	if out.Policy != nil {
		decision := out.Policy.Normalized()
		if decision.Empty() {
			out.Policy = nil
		} else {
			out.Policy = &decision
		}
	}
	out.Approval = CloneEventApproval(out.Approval)
	out.ToolNames = normalize.DedupeStrings(out.ToolNames)
	out.Summary = strings.TrimSpace(out.Summary)
	if out.Summary == "" {
		out.Summary = governanceSummary(out)
	}
	return out
}

func (c EventContext) Empty() bool {
	normalized := c.Normalized()
	return normalized.Scope.IsZero() &&
		normalized.EffectiveConfigSnapshotID == "" &&
		normalized.Policy == nil &&
		normalized.Approval == nil &&
		len(normalized.ToolNames) == 0 &&
		normalized.Summary == ""
}

func EventAttrs(scope domainscope.Ref, evaluation *Evaluation) map[string]any {
	attrs := eventbus.GovernanceAttrs{
		Scope: scope.Normalize(),
	}
	if evaluation != nil {
		normalized := evaluation.Normalized()
		attrs.EffectiveConfigSnapshotID = normalized.EffectiveConfigSnapshotID
		attrs.PolicyAction = strings.TrimSpace(string(normalized.Decision.Action))
		attrs.PolicySource = normalized.Decision.PolicySource
		attrs.PolicySummary = normalized.Decision.Summary
		attrs.PolicyReasons = append([]string(nil), normalized.Decision.Reasons...)
		attrs.PolicyReasonCodes = append([]string(nil), normalized.Decision.ReasonCodes...)
		attrs.PolicyLayers = append([]string(nil), normalized.Decision.PolicyLayers...)
		attrs.PolicyAuditLabels = append([]string(nil), normalized.Decision.AuditLabels...)
		if normalized.Decision.ApprovalPolicy != nil {
			attrs.PolicyApprovalDefaultScope = strings.TrimSpace(string(normalized.Decision.ApprovalPolicy.DefaultScope))
			attrs.PolicyApprovalMaxScope = strings.TrimSpace(string(normalized.Decision.ApprovalPolicy.MaxScope))
		}
		attrs.PolicyToolNames = append([]string(nil), normalized.ToolNames...)
	}
	return attrs.ToMap()
}

func MetadataAttrs(metadata map[string]any) map[string]any {
	if len(metadata) == 0 {
		return nil
	}
	attrs := eventbus.GovernanceAttrs{
		Scope:                      domainscope.FromValue(metadata["scope"]),
		EffectiveConfigSnapshotID:  normalize.String(metadata["effective_config_snapshot_id"]),
		PolicyAction:               normalize.String(metadata["policy_action"]),
		PolicySource:               normalize.String(metadata["policy_source"]),
		PolicySummary:              normalize.String(metadata["policy_summary"]),
		PolicyReasons:              stringSliceValue(metadata["policy_reasons"]),
		PolicyReasonCodes:          stringSliceValue(metadata["policy_reason_codes"]),
		PolicyLayers:               stringSliceValue(metadata["policy_layers"]),
		PolicyAuditLabels:          stringSliceValue(metadata["policy_audit_labels"]),
		PolicyApprovalDefaultScope: normalize.String(metadata["policy_approval_default_scope"]),
		PolicyApprovalMaxScope:     normalize.String(metadata["policy_approval_max_scope"]),
		PolicyToolNames: firstNonEmptyStrings(
			stringSliceValue(metadata["policy_tool_names"]),
			stringSliceValue(metadata["tool_names"]),
		),
	}
	return attrs.ToMap()
}

func DecisionFromMetadata(metadata map[string]any) *Decision {
	if len(metadata) == 0 {
		return nil
	}
	decision := Decision{
		Action:       DecisionAction(normalize.String(metadata["policy_action"])),
		PolicySource: normalize.String(metadata["policy_source"]),
		Summary:      normalize.String(metadata["policy_summary"]),
		Reasons:      stringSliceValue(metadata["policy_reasons"]),
		ReasonCodes:  stringSliceValue(metadata["policy_reason_codes"]),
		PolicyLayers: stringSliceValue(metadata["policy_layers"]),
		AuditLabels:  stringSliceValue(metadata["policy_audit_labels"]),
		ApprovalPolicy: approvalPolicyFromValues(
			metadata["policy_approval_default_scope"],
			metadata["policy_approval_max_scope"],
		),
	}
	if decision.Empty() {
		return nil
	}
	normalized := decision.Normalized()
	return &normalized
}

func EventContextFromEvent(eventType eventbus.EventType, attrs map[string]any) EventContext {
	if len(attrs) == 0 {
		return EventContext{}
	}
	event := eventbus.Event{Attrs: attrs}
	governancePayload, _ := event.GovernancePayload()
	approvalPayload, _ := event.ApprovalPayload()
	context := EventContext{
		Scope:                     governancePayload.Scope.Normalize(),
		EffectiveConfigSnapshotID: governancePayload.EffectiveConfigSnapshotID,
		ToolNames: firstNonEmptyStrings(
			governancePayload.PolicyToolNames,
			stringSliceValue(attrs["tool_names"]),
		),
		Policy: DecisionFromMetadata(attrs),
	}

	approvalState := EventApproval{
		ID:       approvalPayload.ApprovalID,
		Kind:     approval.Kind(approvalPayload.ApprovalKind),
		Status:   approval.Status(approvalPayload.Status),
		External: eventbus.ApprovalExternalReferencesFromValue(attrs["approval_external_refs"]),
	}
	switch eventType {
	case eventbus.EventApprovalRequested, eventbus.EventRunWaitingApproval:
		if approvalState.Status == "" {
			approvalState.Status = approval.StatusPending
		}
	case eventbus.EventApprovalTimedOut:
		if approvalState.Status == "" {
			approvalState.Status = approval.StatusCancelled
		}
	}
	context.Approval = CloneEventApproval(&approvalState)
	context.Summary = governanceSummary(context)
	return context.Normalized()
}

func EventSummary(event eventbus.Event, context EventContext) string {
	if payload, ok := event.RunStatusPayload(); ok && strings.TrimSpace(payload.Summary) != "" {
		return payload.Summary
	}
	if payload, ok := event.ApprovalPayload(); ok && strings.TrimSpace(payload.PolicySummary) != "" {
		return payload.PolicySummary
	}
	if payload, ok := event.RunFailedPayload(); ok && strings.TrimSpace(payload.Error) != "" {
		return payload.Error
	}
	if payload, ok := event.RunTimeoutPayload(); ok && strings.TrimSpace(payload.Error) != "" {
		return payload.Error
	}
	if payload, ok := event.SecurityFindingPayload(); ok && strings.TrimSpace(payload.Detail) != "" {
		return payload.Detail
	}
	return normalize.FirstNonEmpty(
		context.Normalized().Summary,
	)
}

func ApprovalFromTicket(ticket *approval.Ticket) *EventApproval {
	if ticket == nil {
		return nil
	}
	return CloneEventApproval(&EventApproval{
		ID:       strings.TrimSpace(ticket.ID),
		Kind:     ticket.Kind,
		Status:   ticket.Status,
		External: approval.CloneExternalReferences(ticket.External),
	})
}

func MergeEventAttrs(base map[string]any, extras ...map[string]any) map[string]any {
	var out map[string]any
	if len(base) > 0 {
		out = supportmaps.Clone(base)
	}
	for _, extra := range extras {
		if len(extra) == 0 {
			continue
		}
		if out == nil {
			out = make(map[string]any, len(extra))
		}
		for key, value := range extra {
			out[key] = value
		}
	}
	return out
}

func governanceSummary(context EventContext) string {
	parts := make([]string, 0, 4)
	if context.Policy != nil {
		if summary := strings.TrimSpace(context.Policy.Summary); summary != "" {
			parts = append(parts, summary)
		} else if action := strings.TrimSpace(string(context.Policy.Action)); action != "" {
			parts = append(parts, "policy="+action)
		}
	}
	if context.Approval != nil {
		if status := strings.TrimSpace(string(context.Approval.Status)); status != "" {
			parts = append(parts, "approval="+status)
		}
		if providers := approvalExternalProviders(context.Approval.External); len(providers) > 0 {
			parts = append(parts, "providers="+strings.Join(providers, ","))
		}
	}
	if summary := domainscope.Summary(context.Scope); summary != "" {
		parts = append(parts, summary)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " | ")
}

func approvalExternalProviders(items []approval.ExternalReference) []string {
	if len(items) == 0 {
		return nil
	}
	providers := make([]string, 0, len(items))
	for _, item := range items {
		if provider := strings.TrimSpace(item.Provider); provider != "" {
			providers = append(providers, provider)
		}
	}
	return normalize.DedupeStrings(providers)
}

func approvalPolicyFromValues(defaultScopeValue, maxScopeValue any) *ApprovalPolicy {
	defaultScope, _ := approval.NormalizeScope(approval.Scope(normalize.String(defaultScopeValue)))
	maxScope, _ := approval.NormalizeScope(approval.Scope(normalize.String(maxScopeValue)))
	policy := ApprovalPolicy{
		DefaultScope: defaultScope,
		MaxScope:     maxScope,
	}.Normalized()
	if policy.Empty() {
		return nil
	}
	return &policy
}

func firstNonEmptyStrings(items ...[]string) []string {
	for _, item := range items {
		if normalized := normalize.DedupeStrings(item); len(normalized) > 0 {
			return normalized
		}
	}
	return nil
}

func stringSliceValue(value any) []string {
	return normalize.StringSlice(value)
}
