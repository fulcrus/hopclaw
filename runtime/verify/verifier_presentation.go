package verify

type presentationVerifier struct{}

func (presentationVerifier) Name() string { return "presentation.result" }
func (presentationVerifier) Applies(input Input) bool {
	if input.HasToolPrefix("presentation.") {
		return true
	}
	for _, ref := range input.Deliverables {
		if deliverableMatches(ref, ".pptx", ".ppt", ".key") {
			return true
		}
	}
	return false
}

func (presentationVerifier) Verify(input Input) Check {
	check := Check{Name: "presentation.result", Domain: "presentation", Requirement: RequirementAdvisory, Tools: matchingTools(input.ToolNames, "presentation.")}
	if !runCompleted(input.Status) {
		check.Status = StatusSkipped
		check.Summary = "presentation verification skipped because the run did not complete"
		return check
	}
	if hasPresentationEvidence(input) {
		check.Status = StatusPassed
		check.Summary = "presentation run produced slide metadata or file evidence"
		check.Evidence = collectPresentationEvidence(input)
		return check
	}
	check.Status = StatusWarning
	check.Summary = "presentation run completed without clear slide evidence"
	check.Issues = []Issue{{
		Code:     IssueCodePresentationEvidenceMissing,
		Severity: SeverityWarning,
		Message:  "expected slide_count, slides, or a presentation deliverable",
	}}
	return check
}

func hasPresentationEvidence(input Input) bool {
	return len(collectPresentationEvidence(input)) > 0
}

func collectPresentationEvidence(input Input) []string {
	out := make([]string, 0, 4)
	for _, ref := range input.Deliverables {
		if deliverableMatches(ref, ".pptx", ".ppt", ".key") {
			out = append(out, summarizeDeliverableEvidence(ref))
		}
	}
	for _, raw := range input.ToolOutputs {
		payload := parseJSONObject(raw)
		if len(payload) == 0 {
			continue
		}
		if hasKey(payload, "slide_count") || hasKey(payload, "slides") {
			out = append(out, summarizeStructuredEvidence(payload, "path", "slide_count", "slides"))
		}
		if hasKey(payload, "path") && (hasKey(payload, "bytes") || hasKey(payload, "title") || hasKey(payload, "author")) {
			out = append(out, summarizeStructuredEvidence(payload, "path", "bytes", "title", "author"))
		}
	}
	return dedupeEvidence(out)
}
