package policy

const (
	ReasonCodeUnknownToolAllowed           = "unknown_tool_allowed"
	ReasonCodeUnknownTool                  = "unknown_tool"
	ReasonCodeToolUnavailable              = "tool_unavailable"
	ReasonCodeToolIneligible               = "tool_ineligible"
	ReasonCodeSessionDeniedGrant           = "session_denied_grant"
	ReasonCodeApprovalGrant                = "approval_grant"
	ReasonCodeBlockedCommand               = "blocked_command"
	ReasonCodeSafeExecAllowlist            = "safe_exec_allowlist"
	ReasonCodeDestructiveToolBlocked       = "destructive_tool_blocked"
	ReasonCodeSkillInstallAutoAllowed      = "skill_install_auto_allowed"
	ReasonCodeSkillInstallDenied           = "skill_install_denied"
	ReasonCodeSkillInstallRequiresApproval = "skill_install_requires_approval"
	ReasonCodeHighSecurityRisk             = "high_security_risk"
)

func AllReasonCodes() []string {
	return []string{
		ReasonCodeUnknownToolAllowed,
		ReasonCodeUnknownTool,
		ReasonCodeToolUnavailable,
		ReasonCodeToolIneligible,
		ReasonCodeSessionDeniedGrant,
		ReasonCodeApprovalGrant,
		ReasonCodeBlockedCommand,
		ReasonCodeSafeExecAllowlist,
		ReasonCodeDestructiveToolBlocked,
		ReasonCodeSkillInstallAutoAllowed,
		ReasonCodeSkillInstallDenied,
		ReasonCodeSkillInstallRequiresApproval,
		ReasonCodeHighSecurityRisk,
	}
}
