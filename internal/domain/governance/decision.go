package governance

import (
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/internal/support/normalize"
)

type DecisionAction string

const (
	DecisionAllow           DecisionAction = "allow"
	DecisionRequireApproval DecisionAction = "require_approval"
	DecisionDeny            DecisionAction = "deny"
)

type Decision struct {
	Action         DecisionAction  `json:"action"`
	Reasons        []string        `json:"reasons,omitempty"`
	ReasonCodes    []string        `json:"reason_codes,omitempty"`
	PolicySource   string          `json:"policy_source,omitempty"`
	Summary        string          `json:"summary,omitempty"`
	PolicyLayers   []string        `json:"policy_layers,omitempty"`
	AuditLabels    []string        `json:"audit_labels,omitempty"`
	ApprovalPolicy *ApprovalPolicy `json:"approval_policy,omitempty"`
}

type ApprovalPolicy struct {
	DefaultScope approval.Scope `json:"default_scope,omitempty"`
	MaxScope     approval.Scope `json:"max_scope,omitempty"`
}

func (d Decision) Normalized() Decision {
	out := d
	switch DecisionAction(strings.TrimSpace(string(out.Action))) {
	case DecisionRequireApproval, DecisionDeny:
	default:
		out.Action = DecisionAllow
	}
	if len(out.Reasons) == 0 {
		out.Reasons = nil
	}
	out.ReasonCodes = normalize.DedupeStrings(out.ReasonCodes)
	out.PolicySource = strings.TrimSpace(out.PolicySource)
	out.Summary = strings.TrimSpace(out.Summary)
	out.PolicyLayers = normalize.DedupeStrings(out.PolicyLayers)
	out.AuditLabels = normalize.DedupeStrings(out.AuditLabels)
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

func (d Decision) RequiresApproval() bool {
	return d.Action == DecisionRequireApproval
}

func (d Decision) Denied() bool {
	return d.Action == DecisionDeny
}

func (d Decision) Empty() bool {
	return strings.TrimSpace(string(d.Action)) == "" &&
		len(d.Reasons) == 0 &&
		len(d.ReasonCodes) == 0 &&
		strings.TrimSpace(d.PolicySource) == "" &&
		strings.TrimSpace(d.Summary) == "" &&
		len(d.PolicyLayers) == 0 &&
		len(d.AuditLabels) == 0 &&
		(d.ApprovalPolicy == nil || d.ApprovalPolicy.Empty())
}

func (p ApprovalPolicy) Normalized() ApprovalPolicy {
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

func (p ApprovalPolicy) Empty() bool {
	return p.DefaultScope == "" && p.MaxScope == ""
}

type Evaluation struct {
	Decision                  Decision  `json:"decision"`
	ToolNames                 []string  `json:"tool_names,omitempty"`
	EffectiveConfigSnapshotID string    `json:"effective_config_snapshot_id,omitempty"`
	UpdatedAt                 time.Time `json:"updated_at,omitempty"`
}

func (e Evaluation) Normalized() Evaluation {
	out := e
	out.Decision = out.Decision.Normalized()
	out.ToolNames = normalize.DedupeStrings(out.ToolNames)
	out.EffectiveConfigSnapshotID = strings.TrimSpace(out.EffectiveConfigSnapshotID)
	if len(out.ToolNames) == 0 {
		out.ToolNames = nil
	}
	return out
}
