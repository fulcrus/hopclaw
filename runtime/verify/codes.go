package verify

const (
	IssueCodeContractMissingInfoUnresolved = "contract_missing_info_unresolved"
	IssueCodeContractExternalEffectMissing = "contract_external_effect_missing"
	IssueCodeContractDeliverableMissing    = "contract_deliverable_missing"
	IssueCodeContractAcceptanceMissing     = "contract_acceptance_missing"
	IssueCodePlanCoverageGap               = "plan_coverage_gap"
	IssueCodeRunFailed                     = "run_failed"
	IssueCodeMissingResult                 = "missing_result"
	IssueCodeToolExecutionFailed           = "tool_execution_failed"
	IssueCodeBrowserEvidenceMissing        = "browser_evidence_missing"
	IssueCodeDesktopEvidenceMissing        = "desktop_evidence_missing"
	IssueCodeSpreadsheetEvidenceMissing    = "spreadsheet_evidence_missing"
	IssueCodeDocumentEvidenceMissing       = "document_evidence_missing"
	IssueCodePresentationEvidenceMissing   = "presentation_evidence_missing"
	IssueCodeEmailNotConfigured            = "email_not_configured"
	IssueCodeEmailActionFailed             = "email_action_failed"
	IssueCodeEmailEvidenceMissing          = "email_evidence_missing"
	IssueCodeWatchNotificationMissing      = "watch_notification_missing"
)

func AllIssueCodes() []string {
	return []string{
		IssueCodeContractMissingInfoUnresolved,
		IssueCodeContractExternalEffectMissing,
		IssueCodeContractDeliverableMissing,
		IssueCodeContractAcceptanceMissing,
		IssueCodePlanCoverageGap,
		IssueCodeRunFailed,
		IssueCodeMissingResult,
		IssueCodeToolExecutionFailed,
		IssueCodeBrowserEvidenceMissing,
		IssueCodeDesktopEvidenceMissing,
		IssueCodeSpreadsheetEvidenceMissing,
		IssueCodeDocumentEvidenceMissing,
		IssueCodePresentationEvidenceMissing,
		IssueCodeEmailNotConfigured,
		IssueCodeEmailActionFailed,
		IssueCodeEmailEvidenceMissing,
		IssueCodeWatchNotificationMissing,
	}
}
