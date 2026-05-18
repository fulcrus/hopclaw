package verify

type spreadsheetVerifier struct{}

func (spreadsheetVerifier) Name() string { return "spreadsheet.result" }
func (spreadsheetVerifier) Applies(input Input) bool {
	if input.HasToolPrefix("spreadsheet.") {
		return true
	}
	for _, ref := range input.Deliverables {
		if deliverableMatches(ref, ".csv", ".tsv", ".xlsx", ".xls", ".ods") {
			return true
		}
	}
	return false
}

func (spreadsheetVerifier) Verify(input Input) Check {
	check := Check{Name: "spreadsheet.result", Domain: "spreadsheet", Requirement: RequirementAdvisory, Tools: matchingTools(input.ToolNames, "spreadsheet.")}
	if !runCompleted(input.Status) {
		check.Status = StatusSkipped
		check.Summary = "spreadsheet verification skipped because the run did not complete"
		return check
	}
	if hasSpreadsheetEvidence(input) {
		check.Status = StatusPassed
		check.Summary = "spreadsheet run produced writable or readable sheet evidence"
		check.Evidence = collectSpreadsheetEvidence(input)
		return check
	}
	check.Status = StatusWarning
	check.Summary = "spreadsheet run completed without clear sheet evidence"
	check.Issues = []Issue{{
		Code:     IssueCodeSpreadsheetEvidenceMissing,
		Severity: SeverityWarning,
		Message:  "expected range, sheet, export, or spreadsheet deliverable evidence",
	}}
	return check
}

func hasSpreadsheetEvidence(input Input) bool {
	return len(collectSpreadsheetEvidence(input)) > 0
}

func collectSpreadsheetEvidence(input Input) []string {
	out := make([]string, 0, 4)
	for _, ref := range input.Deliverables {
		if deliverableMatches(ref, ".csv", ".tsv", ".xlsx", ".xls", ".ods") {
			out = append(out, summarizeDeliverableEvidence(ref))
		}
	}
	for _, raw := range input.ToolOutputs {
		payload := parseJSONObject(raw)
		if len(payload) == 0 {
			continue
		}
		if (hasKey(payload, "range") && (hasKey(payload, "path") || hasKey(payload, "output") || hasKey(payload, "sheet"))) ||
			hasKey(payload, "row_count") ||
			hasKey(payload, "headers") ||
			hasKey(payload, "rows") ||
			hasKey(payload, "objects") ||
			hasKey(payload, "sheets") ||
			hasKey(payload, "format") {
			out = append(out, summarizeStructuredEvidence(payload, "path", "sheet", "range", "format", "row_count"))
		}
	}
	return dedupeEvidence(out)
}
