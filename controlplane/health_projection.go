package controlplane

import (
	"strconv"
	"strings"
)

const (
	HealthStateReady    = "ready"
	HealthStateDegraded = "degraded"
)

type HealthProjection struct {
	OK       bool
	State    string
	Summary  string
	Warnings []string
}

func ProjectOperationalHealth(source OperationalWarningSource) HealthProjection {
	if source == nil {
		return ReadyHealthProjection()
	}
	return ProjectOperationalHealthFromWarnings(source.OperationalWarnings())
}

func ProjectOperationalHealthFromWarnings(warnings []OperationalWarning) HealthProjection {
	summaries := OperationalWarningSummaries(warnings)
	if len(summaries) == 0 {
		return ReadyHealthProjection()
	}
	return HealthProjection{
		OK:       false,
		State:    HealthStateDegraded,
		Summary:  OperationalWarningHeadline(warnings),
		Warnings: summaries,
	}
}

func ReadyHealthProjection() HealthProjection {
	return HealthProjection{
		OK:      true,
		State:   HealthStateReady,
		Summary: HealthStateReady,
	}
}

func OperationalWarningHeadline(warnings []OperationalWarning) string {
	summaries := OperationalWarningSummaries(warnings)
	if len(summaries) == 0 {
		return ""
	}
	headline := strings.TrimSpace(summaries[0])
	if headline == "" {
		return ""
	}
	if len(summaries) == 1 {
		return headline
	}
	return headline + " (+" + strconv.Itoa(len(summaries)-1) + " more)"
}
