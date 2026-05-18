package controlplane

import (
	"fmt"
	"strings"
)

// OperationalWarning is a user-actionable degraded condition that should be
// projected beyond subsystem-local logs.
type OperationalWarning struct {
	Component string `json:"component"`
	Summary   string `json:"summary"`
	Detail    string `json:"detail,omitempty"`
	Fix       string `json:"fix,omitempty"`
}

// OperationalWarningSource exposes the current set of actionable degraded
// conditions tracked by the runtime.
type OperationalWarningSource interface {
	OperationalWarnings() []OperationalWarning
}

func OperationalWarningSummaries(warnings []OperationalWarning) []string {
	if len(warnings) == 0 {
		return nil
	}
	out := make([]string, 0, len(warnings))
	for _, warning := range warnings {
		summary := strings.TrimSpace(warning.Summary)
		if summary == "" {
			summary = strings.TrimSpace(warning.Component)
		}
		if summary == "" {
			continue
		}
		out = append(out, summary)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func ProbeOperationalWarnings(warnings []OperationalWarning) ProbeResult {
	if len(warnings) == 0 {
		return ProbeResult{
			Status: ProbeStatusOK,
			Detail: "no operational warnings recorded",
		}
	}

	summaries := OperationalWarningSummaries(warnings)
	if len(summaries) == 0 {
		summaries = []string{"operational warnings recorded"}
	}
	detail := summaries[0]
	if len(summaries) > 1 {
		detail = fmt.Sprintf("%s (+%d more)", detail, len(summaries)-1)
	}

	result := ProbeResult{
		Status: ProbeStatusWarn,
		Detail: fmt.Sprintf("%d operational warning(s): %s", len(summaries), detail),
	}
	for _, warning := range warnings {
		if fix := strings.TrimSpace(warning.Fix); fix != "" {
			result.Fix = fix
			break
		}
	}
	return result
}
