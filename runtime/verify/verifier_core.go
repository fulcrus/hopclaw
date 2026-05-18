package verify

import (
	"strings"

	domainrun "github.com/fulcrus/hopclaw/internal/domain/runstate"
	"github.com/fulcrus/hopclaw/internal/support/normalize"
)

type coreVerifier struct{}

func (coreVerifier) Name() string       { return "run.outcome" }
func (coreVerifier) Applies(Input) bool { return true }

func (coreVerifier) Verify(input Input) Check {
	check := Check{Name: "run.outcome", Domain: "core", Requirement: RequirementRequired}
	switch domainrun.Status(strings.TrimSpace(input.Status)) {
	case domainrun.RunFailed:
		check.Status = StatusFailed
		check.Summary = "run failed before a valid result was produced"
		check.Issues = []Issue{{
			Code:     IssueCodeRunFailed,
			Severity: SeverityError,
			Message:  normalize.FirstNonEmpty(strings.TrimSpace(input.Error), "run status is failed"),
		}}
		return check
	case domainrun.RunCancelled:
		check.Status = StatusSkipped
		check.Summary = "run was cancelled before completion"
		return check
	case domainrun.RunCompleted:
		if hasUserVisibleResult(input) {
			check.Status = StatusPassed
			check.Summary = "run produced output, tool evidence, or deliverables"
			check.Evidence = collectCoreEvidence(input)
			return check
		}
		check.Status = StatusWarning
		check.Summary = "run completed without clear output or deliverables"
		check.Issues = []Issue{{
			Code:     IssueCodeMissingResult,
			Severity: SeverityWarning,
			Message:  "completed run has no final output, tool evidence, or deliverables",
		}}
		return check
	default:
		check.Status = StatusSkipped
		check.Summary = "run is not terminal yet"
		return check
	}
}

func hasUserVisibleResult(input Input) bool {
	if strings.TrimSpace(input.Output) != "" {
		return true
	}
	if len(input.Deliverables) > 0 {
		return true
	}
	for _, raw := range input.ToolOutputs {
		if strings.TrimSpace(raw) != "" {
			return true
		}
	}
	return false
}

func collectCoreEvidence(input Input) []string {
	out := make([]string, 0, 4)
	if summary := strings.TrimSpace(input.Output); summary != "" {
		out = append(out, compactEvidence(summary))
	}
	for _, ref := range input.Deliverables {
		out = append(out, summarizeDeliverableEvidence(ref))
	}
	for _, raw := range input.ToolOutputs {
		payload := parseJSONObject(raw)
		if len(payload) == 0 {
			continue
		}
		out = append(out, summarizeStructuredEvidence(payload, "path", "url", "title", "sheet", "slide_count"))
	}
	return dedupeEvidence(out)
}
