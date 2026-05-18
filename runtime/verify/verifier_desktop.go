package verify

import "strings"

type desktopVerifier struct{}

func (desktopVerifier) Name() string { return "desktop.result" }
func (desktopVerifier) Applies(input Input) bool {
	if input.HasToolPrefix("desktop.") {
		return true
	}
	for _, ref := range input.Deliverables {
		if strings.HasPrefix(strings.TrimSpace(ref.ToolName), "desktop.") && deliverableMatches(ref, ".png", ".jpg", ".jpeg", ".webp", ".json") {
			return true
		}
	}
	return false
}

func (desktopVerifier) Verify(input Input) Check {
	check := Check{Name: "desktop.result", Domain: "desktop", Requirement: RequirementAdvisory, Tools: matchingTools(input.ToolNames, "desktop.")}
	if !runCompleted(input.Status) {
		check.Status = StatusSkipped
		check.Summary = "desktop verification skipped because the run did not complete"
		return check
	}
	evidence := collectDesktopEvidence(input)
	if len(evidence) > 0 {
		check.Status = StatusPassed
		check.Summary = "desktop run produced app, window, UI, or artifact evidence"
		check.Evidence = evidence
		return check
	}
	check.Status = StatusWarning
	check.Summary = "desktop run completed without clear UI or artifact evidence"
	check.Issues = []Issue{{
		Code:     IssueCodeDesktopEvidenceMissing,
		Severity: SeverityWarning,
		Message:  "expected app state, window state, UI tree, screenshot, or desktop deliverable evidence",
	}}
	return check
}

func collectDesktopEvidence(input Input) []string {
	out := make([]string, 0, 4)
	for _, ref := range input.Deliverables {
		if deliverableMatches(ref, ".png", ".jpg", ".jpeg", ".webp", ".json") {
			out = append(out, summarizeDeliverableEvidence(ref))
		}
	}
	for _, raw := range input.ToolOutputs {
		payload := parseJSONObject(raw)
		if len(payload) == 0 {
			continue
		}
		if hasAnyKey(payload, "app", "apps", "window", "windows", "elements", "tree", "ocr", "text", "artifact_uri") {
			out = append(out, summarizeStructuredEvidence(payload, "app", "window", "text", "artifact_uri"))
		}
	}
	return dedupeEvidence(out)
}
