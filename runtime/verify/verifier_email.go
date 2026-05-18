package verify

import (
	"strings"

	"github.com/fulcrus/hopclaw/internal/support/normalize"
)

type emailVerifier struct{}

func (emailVerifier) Name() string             { return "email.result" }
func (emailVerifier) Applies(input Input) bool { return input.HasToolPrefix("email.") }

func (emailVerifier) Verify(input Input) Check {
	check := Check{Name: "email.result", Domain: "email", Requirement: RequirementRequired, Tools: matchingTools(input.ToolNames, "email.")}
	for _, raw := range input.ToolOutputs {
		payload := parseJSONObject(raw)
		if len(payload) == 0 {
			continue
		}
		if strings.EqualFold(normalize.String(payload["status"]), "not_configured") {
			check.Status = StatusFailed
			check.Summary = "email capability was invoked without a valid configuration"
			check.Issues = []Issue{{
				Code:     IssueCodeEmailNotConfigured,
				Severity: SeverityError,
				Message:  firstNonEmptyTrimmed(normalize.String(payload["message"]), "email tool is not configured"),
			}}
			return check
		}
		if hasBool(payload, "success", true) || hasKey(payload, "count") || hasKey(payload, "item") || hasKey(payload, "items") {
			check.Status = StatusPassed
			check.Summary = "email run returned delivery or inbox evidence"
			check.Evidence = collectEmailEvidence(payload)
			return check
		}
		if hasBool(payload, "success", false) {
			check.Status = StatusFailed
			check.Summary = "email action reported a failure"
			check.Issues = []Issue{{
				Code:     IssueCodeEmailActionFailed,
				Severity: SeverityError,
				Message:  firstNonEmptyTrimmed(normalize.String(payload["error"]), "email tool reported success=false"),
			}}
			return check
		}
	}
	if !runCompleted(input.Status) {
		check.Status = StatusSkipped
		check.Summary = "email verification skipped because the run did not complete"
		return check
	}
	check.Status = StatusWarning
	check.Summary = "email run completed without clear send/read/search evidence"
	check.Issues = []Issue{{
		Code:     IssueCodeEmailEvidenceMissing,
		Severity: SeverityWarning,
		Message:  "expected success, count, item, or items fields from email tool output",
	}}
	return check
}

func collectEmailEvidence(payload map[string]any) []string {
	return dedupeEvidence([]string{
		summarizeStructuredEvidence(payload, "message_id", "to", "subject", "count"),
	})
}
