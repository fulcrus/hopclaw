package verify

import "strings"

type browserVerifier struct{}

func (browserVerifier) Name() string { return "browser.result" }
func (browserVerifier) Applies(input Input) bool {
	if input.HasToolPrefix("browser.") {
		return true
	}
	for _, ref := range input.Deliverables {
		if browserDeliverablePresent(ref) {
			return true
		}
	}
	return false
}

func (browserVerifier) Verify(input Input) Check {
	check := Check{Name: "browser.result", Domain: "browser", Requirement: RequirementAdvisory, Tools: matchingTools(input.ToolNames, "browser.")}
	if !runCompleted(input.Status) {
		check.Status = StatusSkipped
		check.Summary = "browser verification skipped because the run did not complete"
		return check
	}
	evidence := collectBrowserEvidence(input)
	if len(evidence) > 0 {
		check.Status = StatusPassed
		check.Summary = "browser run produced page, navigation, or artifact evidence"
		check.Evidence = evidence
		return check
	}
	check.Status = StatusWarning
	check.Summary = "browser run completed without clear page or artifact evidence"
	check.Issues = []Issue{{
		Code:     IssueCodeBrowserEvidenceMissing,
		Severity: SeverityWarning,
		Message:  "expected navigation, page snapshot, screenshot, or browser deliverable evidence",
	}}
	return check
}

func collectBrowserEvidence(input Input) []string {
	out := make([]string, 0, 4)
	for _, ref := range input.Deliverables {
		if browserDeliverablePresent(ref) {
			out = append(out, summarizeDeliverableEvidence(ref))
		}
	}
	for _, raw := range input.ToolOutputs {
		payload := parseJSONObject(raw)
		if len(payload) == 0 {
			continue
		}
		if hasAnyKey(payload, "url", "title", "html", "content", "dom", "snapshot", "tabs", "selector", "artifact_uri") {
			out = append(out, summarizeStructuredEvidence(payload, "url", "title", "selector", "artifact_uri"))
		}
	}
	return dedupeEvidence(out)
}

func browserDeliverablePresent(ref Deliverable) bool {
	if strings.HasPrefix(strings.TrimSpace(ref.ToolName), "browser.") {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(ref.Kind), contractDeliverableBrowserEvidence)
}
