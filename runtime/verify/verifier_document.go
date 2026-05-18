package verify

import (
	"strings"

	"github.com/fulcrus/hopclaw/internal/support/normalize"
)

type documentVerifier struct{}

func (documentVerifier) Name() string { return "document.result" }
func (documentVerifier) Applies(input Input) bool {
	if input.HasToolPrefix("document.") {
		return true
	}
	for _, ref := range input.Deliverables {
		if deliverableMatches(ref, ".docx", ".doc", ".rtf", ".odt") {
			return true
		}
	}
	return false
}

func (documentVerifier) Verify(input Input) Check {
	check := Check{Name: "document.result", Domain: "document", Requirement: RequirementAdvisory, Tools: matchingTools(input.ToolNames, "document.")}
	if !runCompleted(input.Status) {
		check.Status = StatusSkipped
		check.Summary = "document verification skipped because the run did not complete"
		return check
	}
	if hasDocumentEvidence(input) {
		check.Status = StatusPassed
		check.Summary = "document run produced metadata or file evidence"
		check.Evidence = collectDocumentEvidence(input)
		return check
	}
	check.Status = StatusWarning
	check.Summary = "document run completed without clear file or metadata evidence"
	check.Issues = []Issue{{
		Code:     IssueCodeDocumentEvidenceMissing,
		Severity: SeverityWarning,
		Message:  "expected document path, paragraph metadata, or a document deliverable",
	}}
	return check
}

func hasDocumentEvidence(input Input) bool {
	return len(collectDocumentEvidence(input)) > 0
}

func collectDocumentEvidence(input Input) []string {
	out := make([]string, 0, 4)
	for _, ref := range input.Deliverables {
		if deliverableMatches(ref, ".docx", ".doc", ".odt", ".rtf", ".md", ".markdown", ".txt") {
			out = append(out, summarizeDeliverableEvidence(ref))
		}
	}
	for _, raw := range input.ToolOutputs {
		payload := parseJSONObject(raw)
		if len(payload) == 0 {
			continue
		}
		if hasKey(payload, "paragraph_count") || hasKey(payload, "word_count") || hasKey(payload, "matches") {
			out = append(out, summarizeStructuredEvidence(payload, "path", "paragraph_count", "word_count", "matches"))
		}
		path := strings.TrimSpace(normalize.String(payload["path"]))
		if path != "" && looksLikeDocumentPath(path) &&
			(hasKey(payload, "bytes") || hasKey(payload, "bytes_written") || hasKey(payload, "bytes_appended") || hasKey(payload, "title") || hasKey(payload, "author")) {
			out = append(out, summarizeStructuredEvidence(payload, "path", "bytes", "bytes_written", "bytes_appended", "title", "author"))
		}
	}
	return dedupeEvidence(out)
}

func looksLikeDocumentPath(path string) bool {
	lower := strings.ToLower(strings.TrimSpace(path))
	switch {
	case strings.HasSuffix(lower, ".docx"),
		strings.HasSuffix(lower, ".doc"),
		strings.HasSuffix(lower, ".odt"),
		strings.HasSuffix(lower, ".rtf"),
		strings.HasSuffix(lower, ".md"),
		strings.HasSuffix(lower, ".markdown"),
		strings.HasSuffix(lower, ".txt"):
		return true
	default:
		return false
	}
}
