package controlplane

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/approval"
)

type ScopeRef struct {
	AutomationID string `json:"automation_id,omitempty"`
}

func (r ScopeRef) Normalize() ScopeRef {
	r.AutomationID = strings.TrimSpace(r.AutomationID)
	return r
}

func (r ScopeRef) IsZero() bool {
	return strings.TrimSpace(r.AutomationID) == ""
}

func ScopeRefFromValue(value any) ScopeRef {
	switch typed := value.(type) {
	case nil:
		return ScopeRef{}
	case ScopeRef:
		return typed.Normalize()
	case *ScopeRef:
		if typed == nil {
			return ScopeRef{}
		}
		return typed.Normalize()
	}
	data, err := json.Marshal(value)
	if err != nil || len(data) == 0 || string(data) == "null" {
		return ScopeRef{}
	}
	var ref ScopeRef
	if err := json.Unmarshal(data, &ref); err != nil {
		return ScopeRef{}
	}
	return ref.Normalize()
}

func ScopeSummary(ref ScopeRef) string {
	ref = ref.Normalize()
	parts := make([]string, 0, 1)
	if ref.AutomationID != "" {
		parts = append(parts, "automation="+ref.AutomationID)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " | ")
}

type GovernanceApproval struct {
	ID       string                       `json:"id,omitempty"`
	Kind     approval.Kind                `json:"kind,omitempty"`
	Status   approval.Status              `json:"status,omitempty"`
	External []approval.ExternalReference `json:"external,omitempty"`
}

func (a GovernanceApproval) Normalized() GovernanceApproval {
	out := a
	out.ID = strings.TrimSpace(out.ID)
	out.Kind = approval.Kind(strings.TrimSpace(string(out.Kind)))
	out.Status = approval.Status(strings.TrimSpace(string(out.Status)))
	out.External = approval.CloneExternalReferences(out.External)
	return out
}

func (a GovernanceApproval) Empty() bool {
	normalized := a.Normalized()
	return normalized.ID == "" &&
		normalized.Kind == "" &&
		normalized.Status == "" &&
		len(normalized.External) == 0
}

func CloneGovernanceApproval(in *GovernanceApproval) *GovernanceApproval {
	if in == nil {
		return nil
	}
	out := in.Normalized()
	if out.Empty() {
		return nil
	}
	return &out
}

type GovernanceDecisionAction string

const (
	GovernanceDecisionAllow           GovernanceDecisionAction = "allow"
	GovernanceDecisionRequireApproval GovernanceDecisionAction = "require_approval"
	GovernanceDecisionDeny            GovernanceDecisionAction = "deny"
)

type GovernanceDecisionApprovalPolicy struct {
	DefaultScope approval.Scope `json:"default_scope,omitempty"`
	MaxScope     approval.Scope `json:"max_scope,omitempty"`
}

func (p GovernanceDecisionApprovalPolicy) Normalized() GovernanceDecisionApprovalPolicy {
	out := p
	if scope, err := approval.NormalizeScope(out.DefaultScope); err == nil {
		out.DefaultScope = scope
	} else {
		out.DefaultScope = approval.ScopeOnce
	}
	if scope, err := approval.NormalizeScope(out.MaxScope); err == nil {
		out.MaxScope = scope
	} else {
		out.MaxScope = approval.ScopeOnce
	}
	if out.MaxScope == "" && out.DefaultScope != "" {
		out.MaxScope = out.DefaultScope
	}
	if approval.IsScopeBroader(out.DefaultScope, out.MaxScope) {
		out.DefaultScope = out.MaxScope
	}
	return out
}

func (p GovernanceDecisionApprovalPolicy) Empty() bool {
	return p.DefaultScope == "" && p.MaxScope == ""
}

type GovernanceDecision struct {
	Action         GovernanceDecisionAction          `json:"action"`
	Reasons        []string                          `json:"reasons,omitempty"`
	ReasonCodes    []string                          `json:"reason_codes,omitempty"`
	PolicySource   string                            `json:"policy_source,omitempty"`
	Summary        string                            `json:"summary,omitempty"`
	PolicyLayers   []string                          `json:"policy_layers,omitempty"`
	AuditLabels    []string                          `json:"audit_labels,omitempty"`
	ApprovalPolicy *GovernanceDecisionApprovalPolicy `json:"approval_policy,omitempty"`
}

func (d GovernanceDecision) Normalized() GovernanceDecision {
	out := d
	switch GovernanceDecisionAction(strings.TrimSpace(string(out.Action))) {
	case GovernanceDecisionRequireApproval, GovernanceDecisionDeny:
	default:
		out.Action = GovernanceDecisionAllow
	}
	if len(out.Reasons) == 0 {
		out.Reasons = nil
	}
	out.ReasonCodes = dedupeNonEmptyStrings(out.ReasonCodes)
	out.PolicySource = strings.TrimSpace(out.PolicySource)
	out.Summary = strings.TrimSpace(out.Summary)
	out.PolicyLayers = dedupeNonEmptyStrings(out.PolicyLayers)
	out.AuditLabels = dedupeNonEmptyStrings(out.AuditLabels)
	if out.ApprovalPolicy != nil {
		policy := out.ApprovalPolicy.Normalized()
		if policy.Empty() {
			out.ApprovalPolicy = nil
		} else {
			out.ApprovalPolicy = &policy
		}
	}
	return out
}

func (d GovernanceDecision) RequiresApproval() bool {
	return d.Action == GovernanceDecisionRequireApproval
}

func (d GovernanceDecision) Denied() bool {
	return d.Action == GovernanceDecisionDeny
}

func (d GovernanceDecision) Empty() bool {
	return strings.TrimSpace(string(d.Action)) == "" &&
		len(d.Reasons) == 0 &&
		len(d.ReasonCodes) == 0 &&
		strings.TrimSpace(d.PolicySource) == "" &&
		strings.TrimSpace(d.Summary) == "" &&
		len(d.PolicyLayers) == 0 &&
		len(d.AuditLabels) == 0 &&
		(d.ApprovalPolicy == nil || d.ApprovalPolicy.Empty())
}

type GovernanceEvaluation struct {
	Decision                  GovernanceDecision `json:"decision"`
	ToolNames                 []string           `json:"tool_names,omitempty"`
	EffectiveConfigSnapshotID string             `json:"effective_config_snapshot_id,omitempty"`
	UpdatedAt                 time.Time          `json:"updated_at,omitempty"`
}

func (e GovernanceEvaluation) Normalized() GovernanceEvaluation {
	out := e
	out.Decision = out.Decision.Normalized()
	out.ToolNames = dedupeNonEmptyStrings(out.ToolNames)
	out.EffectiveConfigSnapshotID = strings.TrimSpace(out.EffectiveConfigSnapshotID)
	if len(out.ToolNames) == 0 {
		out.ToolNames = nil
	}
	return out
}

const (
	GovernanceKindApprovalRequested    GovernanceKind = "approval_requested"
	GovernanceKindApprovalResolved     GovernanceKind = "approval_resolved"
	GovernanceKindApprovalTimedOut     GovernanceKind = "approval_timed_out"
	GovernanceKindApprovalGraceWarning GovernanceKind = "approval_grace_warning"
	GovernanceKindSecurityEvent        GovernanceKind = "security_event"
)

func dedupeNonEmptyStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, item := range in {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
