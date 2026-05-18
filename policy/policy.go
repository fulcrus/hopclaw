package policy

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/audit"
	domaingov "github.com/fulcrus/hopclaw/internal/domain/governance"
	"github.com/fulcrus/hopclaw/skill"
	"github.com/fulcrus/hopclaw/toolspec"
)

type Action = domaingov.DecisionAction

const (
	ActionAllow           Action = domaingov.DecisionAllow
	ActionRequireApproval Action = domaingov.DecisionRequireApproval
	ActionDeny            Action = domaingov.DecisionDeny
)

type Decision = domaingov.Decision

type ToolContext struct {
	RunID     string
	SessionID string
	ToolName  string
	Input     map[string]any
	Tool      any
}

type Engine interface {
	EvaluateTool(ctx context.Context, call ToolContext) (Decision, error)
}

type Config struct {
	AllowUnknownTools              bool
	RequireApprovalForWrite        bool
	AllowLocalWriteWithoutApproval bool
	RequireApprovalCommunity       bool
	SkipManifestApproval           bool // trusted_desktop: ignore RequiresApproval in tool manifests
	DenyDestructive                bool
	SkillInstallPolicy             string
	DefaultApprovalScope           approval.Scope
	MaxApprovalScope               approval.Scope
	SkillInstallDefaultScope       approval.Scope
	SkillInstallMaxScope           approval.Scope
	SafePatterns                   []string
	BlockedCommands                []string
	DangerousTools                 []string
}

type DefaultEngine struct {
	config         Config
	safeMatcher    *SafeCommandMatcher
	blockedMatcher *BlockedCommandMatcher
	grants         *approval.GrantStore
	auditor        *audit.SecurityAuditor
	dangerousTools map[string]struct{}
}

func NewDefaultEngine(cfg Config) *DefaultEngine {
	cfg = cfg.Normalized()
	patterns := cfg.SafePatterns
	if len(patterns) == 0 {
		patterns = DefaultSafePatterns()
	}
	dt := make(map[string]struct{}, len(cfg.DangerousTools))
	for _, t := range cfg.DangerousTools {
		name := strings.TrimSpace(strings.ToLower(t))
		if name != "" {
			dt[name] = struct{}{}
		}
	}
	return &DefaultEngine{
		config:         cfg,
		safeMatcher:    NewSafeCommandMatcher(patterns),
		blockedMatcher: NewBlockedCommandMatcher(cfg.BlockedCommands),
		dangerousTools: dt,
	}
}

func (c Config) Normalized() Config {
	out := c
	out.DefaultApprovalScope = normalizeApprovalScopeWithFallback(out.DefaultApprovalScope, approval.ScopeOnce)
	out.MaxApprovalScope = normalizeApprovalScopeWithFallback(out.MaxApprovalScope, approval.ScopeSession)
	if approval.IsScopeBroader(out.DefaultApprovalScope, out.MaxApprovalScope) {
		out.DefaultApprovalScope = out.MaxApprovalScope
	}
	out.SkillInstallDefaultScope = normalizeApprovalScopeWithFallback(out.SkillInstallDefaultScope, approval.ScopeOnce)
	out.SkillInstallMaxScope = normalizeApprovalScopeWithFallback(out.SkillInstallMaxScope, approval.ScopeOnce)
	if approval.IsScopeBroader(out.SkillInstallDefaultScope, out.SkillInstallMaxScope) {
		out.SkillInstallDefaultScope = out.SkillInstallMaxScope
	}
	return out
}

func (c Config) ApprovalDefaults() domaingov.ApprovalPolicy {
	cfg := c.Normalized()
	return domaingov.ApprovalPolicy{
		DefaultScope: cfg.DefaultApprovalScope,
		MaxScope:     cfg.MaxApprovalScope,
	}.Normalized()
}

func (c Config) SkillInstallApprovalDefaults() domaingov.ApprovalPolicy {
	cfg := c.Normalized()
	return domaingov.ApprovalPolicy{
		DefaultScope: cfg.SkillInstallDefaultScope,
		MaxScope:     cfg.SkillInstallMaxScope,
	}.Normalized()
}

func normalizeApprovalScopeWithFallback(raw, fallback approval.Scope) approval.Scope {
	scope, err := approval.NormalizeScope(raw)
	if err != nil || scope == "" {
		return fallback
	}
	return scope
}

// SetGrantStore wires the approval grant store for session-scoped grants.
func (e *DefaultEngine) SetGrantStore(gs *approval.GrantStore) {
	e.grants = gs
}

// SetSecurityAuditor wires the optional security auditor for tool call
// inspection. When set, high-severity risks escalate to require approval or
// deny.
func (e *DefaultEngine) SetSecurityAuditor(a *audit.SecurityAuditor) {
	e.auditor = a
}

func (e *DefaultEngine) EvaluateTool(ctx context.Context, call ToolContext) (Decision, error) {
	tool := normalizeTool(call.Tool)
	if tool == nil {
		if e.config.AllowUnknownTools {
			return finalizeDecision(Decision{
				Action:       ActionAllow,
				ReasonCodes:  []string{ReasonCodeUnknownToolAllowed},
				PolicySource: policySourceUnknownTool,
				Summary:      "allowed because unknown tools are permitted by policy",
			}), nil
		}
		return finalizeDecision(Decision{
			Action:       ActionDeny,
			Reasons:      []string{"unknown tool"},
			ReasonCodes:  []string{ReasonCodeUnknownTool},
			AuditLabels:  []string{"tool_unknown"},
			PolicySource: policySourceUnknownTool,
			Summary:      "denied because the tool is unknown",
		}), nil
	}

	manifest := tool.Manifest
	decision := Decision{Action: ActionAllow}
	reasons := make([]string, 0, 3)
	reasonCodes := make([]string, 0, 3)
	auditLabels := make([]string, 0, 3)

	switch tool.Descriptor.Availability.Status {
	case toolspec.AvailabilityBlocked, toolspec.AvailabilityDiscoverable:
		availabilityReasons := append([]string(nil), tool.Descriptor.Availability.Reasons...)
		if len(availabilityReasons) == 0 {
			availabilityReasons = []string{"tool is unavailable"}
		}
		return finalizeDecision(Decision{
			Action:       ActionDeny,
			Reasons:      availabilityReasons,
			ReasonCodes:  []string{ReasonCodeToolUnavailable},
			AuditLabels:  []string{"tool_unavailable"},
			PolicySource: policySourceAvailability,
			Summary:      "denied because the tool is unavailable",
		}), nil
	}

	if !tool.Eligibility.Eligible {
		return finalizeDecision(Decision{
			Action:       ActionDeny,
			Reasons:      append([]string(nil), tool.Eligibility.Reasons...),
			ReasonCodes:  []string{ReasonCodeToolIneligible},
			AuditLabels:  []string{"tool_ineligible"},
			PolicySource: policySourceEligibility,
			Summary:      "denied because the tool is not eligible in the current runtime",
		}), nil
	}

	// Check session-level grants.
	if e.grants != nil && call.SessionID != "" {
		grantDecision := e.grants.Evaluate(call.SessionID, call.ToolName, call.Input)
		if grantDecision.Denied {
			return finalizeDecision(Decision{
				Action:       ActionDeny,
				Reasons:      []string{"tool denied by session grant"},
				ReasonCodes:  []string{ReasonCodeSessionDeniedGrant},
				AuditLabels:  []string{"approval_grant"},
				PolicySource: policySourceGrant,
				Summary:      "denied by session grant policy",
			}), nil
		}
		if grantDecision.Granted {
			return finalizeDecision(Decision{
				Action:       ActionAllow,
				ReasonCodes:  []string{ReasonCodeApprovalGrant},
				PolicySource: policySourceGrant,
				Summary:      "allowed by approval grant",
			}), nil
		}
	}

	// Blocked command denylist for exec tools.
	if isExecTool(call.ToolName) {
		if cmd := extractExecCommand(call); cmd != "" && e.blockedMatcher != nil {
			if blockedBy, ok := e.blockedMatcher.Match(cmd); ok {
				return finalizeDecision(Decision{
					Action:       ActionDeny,
					Reasons:      []string{fmt.Sprintf("command %q is blocked by policy", blockedBy)},
					ReasonCodes:  []string{ReasonCodeBlockedCommand},
					AuditLabels:  []string{"blocked_command"},
					PolicySource: policySourceBlockedCommand,
					Summary:      "denied because the exec command is blocked by policy",
				}), nil
			}
		}
	}

	// Safe command whitelist for exec tools.
	if e.safeMatcher != nil && isExecTool(call.ToolName) {
		if cmd := extractExecCommand(call); cmd != "" && e.safeMatcher.IsSafe(cmd) {
			return finalizeDecision(Decision{
				Action:       ActionAllow,
				ReasonCodes:  []string{ReasonCodeSafeExecAllowlist},
				PolicySource: policySourceSafeExec,
				Summary:      "allowed by safe exec allowlist",
			}), nil
		}
	}

	sideEffect := skill.NormalizeSideEffectClass(manifest.SideEffectClass)
	if e.config.DenyDestructive && sideEffect == "destructive" {
		return finalizeDecision(Decision{
			Action:       ActionDeny,
			Reasons:      []string{"destructive tools are denied by policy"},
			ReasonCodes:  []string{ReasonCodeDestructiveToolBlocked},
			AuditLabels:  []string{"destructive_tool"},
			PolicySource: policySourceDestructive,
			Summary:      "denied because destructive tools are blocked",
		}), nil
	}
	if isSkillInstallTool(call.ToolName) {
		switch normalizeSkillInstallPolicy(e.config.SkillInstallPolicy) {
		case skillInstallPolicyAuto:
			return finalizeDecision(Decision{
				Action:       ActionAllow,
				ReasonCodes:  []string{ReasonCodeSkillInstallAutoAllowed},
				AuditLabels:  []string{"skill_install"},
				PolicySource: policySourceSkillInstall,
				Summary:      "allowed because skill installation is auto-approved by policy",
			}), nil
		case skillInstallPolicyDeny:
			return finalizeDecision(Decision{
				Action:       ActionDeny,
				Reasons:      []string{describeSkillInstallDenial(call)},
				ReasonCodes:  []string{ReasonCodeSkillInstallDenied},
				AuditLabels:  []string{"skill_install"},
				PolicySource: policySourceSkillInstall,
				Summary:      "denied by skill installation policy",
			}), nil
		default:
			return finalizeDecision(Decision{
				Action:         ActionRequireApproval,
				Reasons:        []string{describeSkillInstallApproval(call)},
				ReasonCodes:    []string{ReasonCodeSkillInstallRequiresApproval},
				AuditLabels:    []string{"skill_install"},
				PolicySource:   policySourceSkillInstall,
				Summary:        "approval is required by skill installation policy",
				ApprovalPolicy: ptrDecisionApprovalPolicy(e.config.SkillInstallApprovalDefaults()),
			}), nil
		}
	}
	if manifest.RequiresApproval {
		if e.config.SkipManifestApproval {
			reasons = append(reasons, "tool manifest requires approval, but policy is configured to bypass manifest approval")
			log.Warn("manifest approval bypassed by policy configuration",
				slog.String("tool_name", call.ToolName),
				slog.String("session_id", call.SessionID),
				slog.String("run_id", call.RunID))
		} else {
			decision.Action = ActionRequireApproval
			reasons = append(reasons, "tool manifest requires approval")
			reasonCodes = append(reasonCodes, "manifest_requires_approval")
		}
	}
	if e.config.RequireApprovalForWrite && sideEffect != "" && sideEffect != "read" {
		if sideEffect == "local_write" && e.config.AllowLocalWriteWithoutApproval {
			reasons = append(reasons, "local write tools are auto-approved by policy")
			reasonCodes = append(reasonCodes, "local_write_auto_allowed")
		} else {
			decision.Action = ActionRequireApproval
			reasons = append(reasons, fmt.Sprintf("side effect class %q requires approval", sideEffect))
			reasonCodes = append(reasonCodes, "side_effect_requires_approval")
			auditLabels = append(auditLabels, "write_tool")
		}
	}
	if e.config.RequireApprovalCommunity && tool.Package != nil && tool.Package.Trust == skill.TrustCommunity {
		decision.Action = ActionRequireApproval
		reasons = append(reasons, "community tools require approval")
		reasonCodes = append(reasonCodes, "community_tool_requires_approval")
	}

	// Dangerous tool check: before security audit.
	if len(e.dangerousTools) > 0 {
		normalized := strings.TrimSpace(strings.ToLower(call.ToolName))
		if _, isDangerous := e.dangerousTools[normalized]; isDangerous {
			decision.Action = ActionRequireApproval
			reasons = append(reasons, "tool is marked as dangerous")
			reasonCodes = append(reasonCodes, "dangerous_tool_requires_approval")
			auditLabels = append(auditLabels, "dangerous_tool")
		}
	}

	// Security audit: run checks before final allow decision.
	if e.auditor != nil {
		risks := e.auditor.AuditToolCall(ctx, call.ToolName, call.Input)
		if audit.HasHighSeverity(risks) {
			auditReasons := make([]string, 0, len(risks))
			for _, risk := range risks {
				if risk.Severity == "high" {
					auditReasons = append(auditReasons, fmt.Sprintf("security: %s — %s", risk.Type, risk.Detail))
				}
			}
			if e.config.DenyDestructive {
				return finalizeDecision(Decision{
					Action:       ActionDeny,
					Reasons:      dedupeReasons(auditReasons),
					ReasonCodes:  []string{ReasonCodeHighSecurityRisk},
					AuditLabels:  []string{"security_risk"},
					PolicySource: policySourceSecurityAudit,
					Summary:      "denied because high-severity security risks were detected",
				}), nil
			}
			decision.Action = ActionRequireApproval
			reasons = append(reasons, auditReasons...)
			reasonCodes = append(reasonCodes, ReasonCodeHighSecurityRisk)
			auditLabels = append(auditLabels, "security_risk")
		}
	}

	decision.Reasons = dedupeReasons(reasons)
	decision.ReasonCodes = append([]string(nil), reasonCodes...)
	decision.AuditLabels = append([]string(nil), auditLabels...)
	if decision.PolicySource == "" {
		decision.PolicySource = policySourceRules
	}
	if decision.Action == ActionRequireApproval && decision.ApprovalPolicy == nil {
		decision.ApprovalPolicy = ptrDecisionApprovalPolicy(e.config.ApprovalDefaults())
	}
	return finalizeDecision(decision), nil
}

func ptrDecisionApprovalPolicy(policy domaingov.ApprovalPolicy) *domaingov.ApprovalPolicy {
	if policy.Empty() {
		return nil
	}
	copy := policy.Normalized()
	return &copy
}

func normalizeTool(raw any) *toolspec.ResolvedTool {
	switch tool := raw.(type) {
	case nil:
		return nil
	case *toolspec.ResolvedTool:
		return toolspec.NormalizeResolvedTool(tool)
	case *skill.BoundTool:
		return toolspec.ResolvedFromSkillBinding(tool, toolspec.ToolDefinition{
			Name:               tool.Manifest.Name,
			Description:        tool.Manifest.Description,
			InputSchema:        cloneSchema(tool.Manifest.InputSchema),
			OutputSchema:       cloneSchema(tool.Manifest.OutputSchema),
			SideEffectClass:    tool.Manifest.SideEffectClass,
			Idempotent:         tool.Manifest.Idempotent,
			RequiresApproval:   tool.Manifest.RequiresApproval,
			ExecutionKey:       tool.Manifest.ExecutionKey,
			Source:             "skill",
			SourceRef:          sourceDir(tool.Package),
			Trust:              trustString(tool.Package),
			Eligible:           tool.Eligibility.Eligible,
			EligibilityReasons: append([]string(nil), tool.Eligibility.Reasons...),
		}, "policy-legacy")
	default:
		return nil
	}
}

func cloneSchema(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func sourceDir(pkg *skill.SkillPackage) string {
	if pkg == nil {
		return ""
	}
	return strings.TrimSpace(pkg.Source.Dir)
}

func trustString(pkg *skill.SkillPackage) string {
	if pkg == nil {
		return ""
	}
	return string(pkg.Trust)
}

const (
	skillInstallPolicyAsk  = "ask"
	skillInstallPolicyAuto = "auto"
	skillInstallPolicyDeny = "deny"
)

func normalizeSkillInstallPolicy(raw string) string {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "", skillInstallPolicyAsk:
		return skillInstallPolicyAsk
	case skillInstallPolicyAuto:
		return skillInstallPolicyAuto
	case skillInstallPolicyDeny:
		return skillInstallPolicyDeny
	default:
		return strings.TrimSpace(strings.ToLower(raw))
	}
}

func isSkillInstallTool(name string) bool {
	switch strings.TrimSpace(strings.ToLower(name)) {
	case "skill.install", "skill.ensure":
		return true
	default:
		return false
	}
}

func describeSkillInstallApproval(call ToolContext) string {
	target := describeSkillInstallTarget(call)
	if target == "" {
		return "skill installation requires approval by policy"
	}
	return fmt.Sprintf("install skill %q requires approval by policy", target)
}

func describeSkillInstallDenial(call ToolContext) string {
	target := describeSkillInstallTarget(call)
	if target == "" {
		return "skill installation is denied by policy"
	}
	return fmt.Sprintf("install skill %q is denied by policy", target)
}

func describeSkillInstallTarget(call ToolContext) string {
	if call.Input == nil {
		return ""
	}
	for _, key := range []string{"name", "skill_id", "query", "goal"} {
		if raw, ok := call.Input[key]; ok {
			if text := strings.TrimSpace(fmt.Sprintf("%v", raw)); text != "" && text != "<nil>" {
				return text
			}
		}
	}
	return ""
}

func isExecTool(name string) bool {
	switch strings.TrimSpace(strings.ToLower(name)) {
	case "exec.run", "exec.shell", "exec.script":
		return true
	default:
		return false
	}
}

func extractExecCommand(call ToolContext) string {
	if call.Input == nil {
		return ""
	}
	// exec.run uses "command" field, exec.shell uses "command" field too
	if raw, ok := call.Input["command"]; ok {
		if cmd, ok := raw.(string); ok {
			return strings.TrimSpace(cmd)
		}
	}
	return ""
}

func dedupeReasons(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, item := range in {
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

const (
	policySourceUnknownTool    = "policy.default_engine/unknown_tool"
	policySourceAvailability   = "policy.default_engine/availability"
	policySourceEligibility    = "policy.default_engine/eligibility"
	policySourceGrant          = "policy.default_engine/grant"
	policySourceBlockedCommand = "policy.default_engine/blocked_command"
	policySourceSafeExec       = "policy.default_engine/safe_exec"
	policySourceDestructive    = "policy.default_engine/destructive"
	policySourceSkillInstall   = "policy.default_engine/skill_install"
	policySourceSecurityAudit  = "policy.default_engine/security_audit"
	policySourceRules          = "policy.default_engine/rules"
)

func finalizeDecision(decision Decision) Decision {
	decision = decision.Normalized()
	if decision.PolicySource == "" {
		decision.PolicySource = policySourceRules
	}
	if decision.Summary == "" {
		decision.Summary = summarizeDecision(decision)
	}
	return decision
}

func summarizeDecision(decision Decision) string {
	reasonSummary := summarizeReasons(decision.Reasons)
	switch decision.Action {
	case ActionDeny:
		if reasonSummary != "" {
			return "denied by policy: " + reasonSummary
		}
		return "denied by policy"
	case ActionRequireApproval:
		if reasonSummary != "" {
			return "approval required by policy: " + reasonSummary
		}
		return "approval required by policy"
	default:
		if reasonSummary != "" {
			return "allowed by policy: " + reasonSummary
		}
		return "allowed by policy"
	}
}

func summarizeReasons(reasons []string) string {
	reasons = dedupeReasons(reasons)
	if len(reasons) == 0 {
		return ""
	}
	if len(reasons) == 1 {
		return reasons[0]
	}
	return reasons[0] + "; " + reasons[1]
}
