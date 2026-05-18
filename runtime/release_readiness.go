package runtime

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type ReleaseReadinessRequest struct {
	SummaryRequest QualitySummaryRequest      `json:"summary_request,omitempty"`
	Thresholds     ReleaseReadinessThresholds `json:"thresholds,omitempty"`
}

type ReleaseReadinessThresholds struct {
	MinTerminalRuns            int     `json:"min_terminal_runs,omitempty"`
	MinTaskSuccessRate         float64 `json:"min_task_success_rate,omitempty"`
	MaxFalseSuccessRate        float64 `json:"max_false_success_rate,omitempty"`
	MaxVerificationFailureRate float64 `json:"max_verification_failure_rate,omitempty"`
	MaxFallbackRate            float64 `json:"max_fallback_rate,omitempty"`
	MaxVisualFallbackRate      float64 `json:"max_visual_fallback_rate,omitempty"`
	MinProfileHitRate          float64 `json:"min_profile_hit_rate,omitempty"`
}

type ReleaseReadinessCheck struct {
	ID         string  `json:"id"`
	Status     string  `json:"status"`
	Summary    string  `json:"summary,omitempty"`
	Measured   float64 `json:"measured,omitempty"`
	Threshold  float64 `json:"threshold,omitempty"`
	Count      int     `json:"count,omitempty"`
	Total      int     `json:"total,omitempty"`
	Comparator string  `json:"comparator,omitempty"`
}

type ReleaseReadinessReport struct {
	Ready       bool                       `json:"ready"`
	GeneratedAt string                     `json:"generated_at,omitempty"`
	Thresholds  ReleaseReadinessThresholds `json:"thresholds"`
	Summary     *QualitySummary            `json:"summary,omitempty"`
	Checks      []ReleaseReadinessCheck    `json:"checks,omitempty"`
	Blockers    []ReleaseReadinessCheck    `json:"blockers,omitempty"`
}

func DefaultReleaseReadinessThresholds() ReleaseReadinessThresholds {
	return ReleaseReadinessThresholds{
		MinTerminalRuns:            5,
		MinTaskSuccessRate:         0.80,
		MaxFalseSuccessRate:        0.00,
		MaxVerificationFailureRate: 0.05,
		MaxFallbackRate:            0.35,
		MaxVisualFallbackRate:      0.20,
		MinProfileHitRate:          0.60,
	}
}

func (s *Service) GetReleaseReadiness(ctx context.Context, req ReleaseReadinessRequest) (*ReleaseReadinessReport, error) {
	summary, err := s.GetQualitySummary(ctx, req.SummaryRequest)
	if err != nil {
		return nil, err
	}
	return buildReleaseReadinessReport(summary, req.Thresholds), nil
}

func buildReleaseReadinessReport(summary *QualitySummary, thresholds ReleaseReadinessThresholds) *ReleaseReadinessReport {
	if summary == nil {
		summary = &QualitySummary{}
	}
	if thresholds == (ReleaseReadinessThresholds{}) {
		thresholds = DefaultReleaseReadinessThresholds()
	}

	checks := make([]ReleaseReadinessCheck, 0, 8)
	appendCheck := func(check ReleaseReadinessCheck) {
		check.ID = strings.TrimSpace(check.ID)
		check.Status = strings.TrimSpace(check.Status)
		if check.Status == "" {
			check.Status = "skipped"
		}
		checks = append(checks, check)
	}

	if summary.TerminalRunCount < thresholds.MinTerminalRuns {
		appendCheck(ReleaseReadinessCheck{
			ID:         "sample_size",
			Status:     "blocked",
			Summary:    fmt.Sprintf("terminal run evidence is too small: have %d, need at least %d", summary.TerminalRunCount, thresholds.MinTerminalRuns),
			Count:      summary.TerminalRunCount,
			Total:      thresholds.MinTerminalRuns,
			Comparator: ">=",
		})
	} else {
		appendCheck(ReleaseReadinessCheck{
			ID:         "sample_size",
			Status:     "passed",
			Summary:    fmt.Sprintf("terminal run evidence is sufficient: %d runs", summary.TerminalRunCount),
			Count:      summary.TerminalRunCount,
			Total:      thresholds.MinTerminalRuns,
			Comparator: ">=",
		})
	}

	appendRateCheck(&checks, "task_success_rate", summary.TaskSuccess, thresholds.MinTaskSuccessRate, ">=", "task success rate")
	appendRateCheck(&checks, "false_success_rate", summary.FalseSuccess, thresholds.MaxFalseSuccessRate, "<=", "false-success rate")
	appendRateCheck(&checks, "verification_failure_rate", summary.VerificationFailure, thresholds.MaxVerificationFailureRate, "<=", "verification failure rate")

	if summary.TraceCount == 0 {
		appendCheck(ReleaseReadinessCheck{
			ID:      "fallback_rate",
			Status:  "skipped",
			Summary: "fallback rate skipped because no execution traces were recorded",
		})
		appendCheck(ReleaseReadinessCheck{
			ID:      "visual_fallback_rate",
			Status:  "skipped",
			Summary: "visual fallback rate skipped because no execution traces were recorded",
		})
		appendCheck(ReleaseReadinessCheck{
			ID:      "profile_hit_rate",
			Status:  "skipped",
			Summary: "profile hit rate skipped because no execution traces were recorded",
		})
	} else {
		appendRateCheck(&checks, "fallback_rate", summary.Fallback, thresholds.MaxFallbackRate, "<=", "fallback reliance")
		appendRateCheck(&checks, "visual_fallback_rate", summary.VisualFallback, thresholds.MaxVisualFallbackRate, "<=", "visual fallback reliance")
		appendRateCheck(&checks, "profile_hit_rate", summary.ProfileHit, thresholds.MinProfileHitRate, ">=", "profile hit rate")
	}

	report := &ReleaseReadinessReport{
		Ready:       true,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Thresholds:  thresholds,
		Summary:     summary,
		Checks:      checks,
	}
	for _, check := range checks {
		if check.Status == "blocked" {
			report.Ready = false
			report.Blockers = append(report.Blockers, check)
		}
	}
	return report
}

func appendRateCheck(checks *[]ReleaseReadinessCheck, id string, measured QualityRate, threshold float64, comparator string, label string) {
	check := ReleaseReadinessCheck{
		ID:         id,
		Measured:   measured.Rate,
		Threshold:  threshold,
		Count:      measured.Count,
		Total:      measured.Total,
		Comparator: comparator,
	}
	if measured.Total == 0 {
		check.Status = "skipped"
		check.Summary = label + " skipped because there are no applicable samples"
		*checks = append(*checks, check)
		return
	}

	passed := false
	switch comparator {
	case "<=":
		passed = measured.Rate <= threshold
	case ">=":
		passed = measured.Rate >= threshold
	}
	if passed {
		check.Status = "passed"
		check.Summary = fmt.Sprintf("%s is within threshold (%.3f %s %.3f)", label, measured.Rate, comparator, threshold)
	} else {
		check.Status = "blocked"
		check.Summary = fmt.Sprintf("%s violates threshold (%.3f %s %.3f)", label, measured.Rate, comparator, threshold)
	}
	*checks = append(*checks, check)
}
