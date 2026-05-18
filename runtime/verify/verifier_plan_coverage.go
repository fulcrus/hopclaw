package verify

import "strings"

type planCoverageVerifier struct{}

func (planCoverageVerifier) Name() string             { return "plan.coverage" }
func (planCoverageVerifier) Applies(input Input) bool { return len(input.PlanCoverageWarnings) > 0 }

func (planCoverageVerifier) Verify(input Input) Check {
	check := Check{Name: "plan.coverage", Domain: "planning", Requirement: RequirementAdvisory}
	if !runCompleted(input.Status) {
		check.Status = StatusSkipped
		check.Summary = "plan coverage verification skipped because the run did not complete"
		return check
	}
	check.Status = StatusWarning
	check.Summary = "plan may not cover all required contract deliverables"
	for _, warning := range input.PlanCoverageWarnings {
		warning = strings.TrimSpace(warning)
		if warning == "" {
			continue
		}
		check.Issues = append(check.Issues, Issue{
			Code:     IssueCodePlanCoverageGap,
			Severity: SeverityWarning,
			Message:  warning,
		})
	}
	if len(check.Issues) == 0 {
		check.Status = StatusSkipped
		check.Summary = "plan coverage verification skipped because no concrete warnings were recorded"
	}
	return check
}
