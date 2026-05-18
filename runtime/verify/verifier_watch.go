package verify

import "strings"

type watchNotificationVerifier struct{}

func (watchNotificationVerifier) Name() string { return "watch.notification" }
func (watchNotificationVerifier) Applies(input Input) bool {
	return strings.HasPrefix(strings.TrimSpace(input.SessionKey), "watch:")
}

func (watchNotificationVerifier) Verify(input Input) Check {
	check := Check{Name: "watch.notification", Domain: "watch", Requirement: RequirementAdvisory}
	if !runCompleted(input.Status) {
		check.Status = StatusSkipped
		check.Summary = "watch verification skipped because the run did not complete"
		return check
	}
	if strings.TrimSpace(input.Output) != "" || len(input.Deliverables) > 0 {
		check.Status = StatusPassed
		check.Summary = "watch-triggered run produced a notification payload"
		check.Evidence = collectWatchEvidence(input)
		return check
	}
	check.Status = StatusWarning
	check.Summary = "watch-triggered run has no final notification payload"
	check.Issues = []Issue{{
		Code:     IssueCodeWatchNotificationMissing,
		Severity: SeverityWarning,
		Message:  "watch run completed without final output or deliverables",
	}}
	return check
}

func collectWatchEvidence(input Input) []string {
	out := make([]string, 0, 2)
	if text := strings.TrimSpace(input.Output); text != "" {
		out = append(out, compactEvidence(text))
	}
	for _, ref := range input.Deliverables {
		out = append(out, summarizeDeliverableEvidence(ref))
	}
	return dedupeEvidence(out)
}
