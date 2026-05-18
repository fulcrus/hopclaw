package runtime

import (
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	capprofile "github.com/fulcrus/hopclaw/capability/profile"
	verifyrt "github.com/fulcrus/hopclaw/runtime/verify"
)

func TestBuildQualitySummaryAggregatesMetrics(t *testing.T) {
	t.Parallel()

	startedAt := time.Unix(100, 0).UTC()
	summary := buildQualitySummary(QualitySummaryRequest{Limit: 4}, []qualityRunSnapshot{
		testQualitySnapshot(
			"run-browser",
			agent.RunCompleted,
			RunOutcomeCompleted,
			startedAt,
			startedAt.Add(2*time.Second),
			&verifyrt.RunVerification{
				Status: verifyrt.StatusPassed,
				Checks: []verifyrt.Check{{Name: "browser.result", Status: verifyrt.StatusPassed}},
			},
			[]capprofile.ExecutionTrace{{
				Surface:         "browser",
				Capability:      "browser",
				ChosenTransport: capprofile.TransportBrowserDOM,
				ProfileHit:      true,
			}},
		),
		testQualitySnapshot(
			"run-false-success",
			agent.RunCompleted,
			RunOutcomeCompleted,
			startedAt.Add(5*time.Second),
			startedAt.Add(9*time.Second),
			&verifyrt.RunVerification{
				Status:           verifyrt.StatusFailed,
				Failures:         1,
				RequiredFailures: 1,
				Checks: []verifyrt.Check{
					{Name: "desktop.result", Status: verifyrt.StatusPassed},
					{Name: "task.contract", Status: verifyrt.StatusFailed},
				},
			},
			[]capprofile.ExecutionTrace{{
				Surface:         "desktop",
				Capability:      "desktop",
				ChosenTransport: capprofile.TransportOCRAnchoredVisual,
				FallbackPath:    []string{capprofile.TransportSemanticUIAction},
				ExecutionMode:   capprofile.ModeVisualFallback,
			}},
		),
		testQualitySnapshot(
			"run-approval",
			agent.RunWaitingApproval,
			RunOutcomeNeedsConfirmation,
			time.Time{},
			time.Time{},
			&verifyrt.RunVerification{Status: verifyrt.StatusSkipped},
			nil,
		),
		testQualitySnapshot(
			"run-recovered",
			agent.RunCompleted,
			RunOutcomeCompleted,
			startedAt.Add(12*time.Second),
			startedAt.Add(15*time.Second),
			&verifyrt.RunVerification{
				Status: verifyrt.StatusPassed,
				Checks: []verifyrt.Check{{Name: "desktop.result", Status: verifyrt.StatusPassed}},
			},
			[]capprofile.ExecutionTrace{{
				Surface:         "desktop",
				Capability:      "desktop",
				ChosenTransport: capprofile.TransportVerifiedHotkey,
				FallbackPath:    []string{capprofile.TransportSemanticUIAction},
				ProfileHit:      true,
			}},
		),
	})

	if summary.RunCount != 4 {
		t.Fatalf("RunCount = %d, want 4", summary.RunCount)
	}
	if summary.TerminalRunCount != 4 {
		t.Fatalf("TerminalRunCount = %d, want 4", summary.TerminalRunCount)
	}
	if summary.TaskSuccess.Count != 3 || summary.TaskSuccess.Total != 4 {
		t.Fatalf("TaskSuccess = %#v", summary.TaskSuccess)
	}
	if summary.FalseSuccess.Count != 1 || summary.FalseSuccess.Total != 3 {
		t.Fatalf("FalseSuccess = %#v", summary.FalseSuccess)
	}
	if summary.TraceCount != 3 {
		t.Fatalf("TraceCount = %d, want 3", summary.TraceCount)
	}
	if summary.ProfileHit.Count != 2 || summary.ProfileHit.Total != 3 {
		t.Fatalf("ProfileHit = %#v", summary.ProfileHit)
	}
	if summary.Fallback.Count != 2 || summary.Fallback.Total != 3 {
		t.Fatalf("Fallback = %#v", summary.Fallback)
	}
	if summary.VisualFallback.Count != 1 || summary.VisualFallback.Total != 3 {
		t.Fatalf("VisualFallback = %#v", summary.VisualFallback)
	}
	if summary.ApprovalInterrupted.Count != 1 || summary.ApprovalInterrupted.Total != 4 {
		t.Fatalf("ApprovalInterrupted = %#v", summary.ApprovalInterrupted)
	}
	if summary.FallbackRecoverySuccess.Count != 1 || summary.FallbackRecoverySuccess.Total != 2 {
		t.Fatalf("FallbackRecoverySuccess = %#v", summary.FallbackRecoverySuccess)
	}
	if summary.Latency.Samples != 3 {
		t.Fatalf("Latency.Samples = %d, want 3", summary.Latency.Samples)
	}

	desktop := findQualitySurfaceSummary(summary.Surfaces, "desktop")
	if desktop == nil {
		t.Fatal("expected desktop surface summary")
	}
	if desktop.TraceCount != 2 {
		t.Fatalf("desktop.TraceCount = %d, want 2", desktop.TraceCount)
	}
	if desktop.FallbackRecoverySuccess.Count != 1 || desktop.FallbackRecoverySuccess.Total != 2 {
		t.Fatalf("desktop.FallbackRecoverySuccess = %#v", desktop.FallbackRecoverySuccess)
	}
	if desktop.EvidenceChecks.Passed != 2 {
		t.Fatalf("desktop.EvidenceChecks = %#v", desktop.EvidenceChecks)
	}
}

func TestBuildReleaseReadinessReportBlocksOnFalseSuccessAndFallback(t *testing.T) {
	t.Parallel()

	summary := &QualitySummary{
		TerminalRunCount:    10,
		TaskSuccess:         qualityRate(9, 10),
		FalseSuccess:        qualityRate(1, 9),
		VerificationFailure: qualityRate(1, 10),
		TraceCount:          6,
		Fallback:            qualityRate(4, 6),
		VisualFallback:      qualityRate(1, 6),
		ProfileHit:          qualityRate(4, 6),
	}

	report := buildReleaseReadinessReport(summary, DefaultReleaseReadinessThresholds())
	if report.Ready {
		t.Fatalf("Ready = true, blockers=%#v", report.Blockers)
	}
	if !hasReadinessBlocker(report.Blockers, "false_success_rate") {
		t.Fatalf("expected false_success_rate blocker, got %#v", report.Blockers)
	}
	if !hasReadinessBlocker(report.Blockers, "fallback_rate") {
		t.Fatalf("expected fallback_rate blocker, got %#v", report.Blockers)
	}
}

func TestBuildReleaseReadinessReportPassesWithoutTraceData(t *testing.T) {
	t.Parallel()

	summary := &QualitySummary{
		TerminalRunCount:    5,
		TaskSuccess:         qualityRate(5, 5),
		FalseSuccess:        qualityRate(0, 5),
		VerificationFailure: qualityRate(0, 5),
		TraceCount:          0,
	}

	report := buildReleaseReadinessReport(summary, DefaultReleaseReadinessThresholds())
	if !report.Ready {
		t.Fatalf("Ready = false, blockers=%#v", report.Blockers)
	}
	if !hasReadinessCheck(report.Checks, "fallback_rate", "skipped") {
		t.Fatalf("expected skipped fallback_rate check, got %#v", report.Checks)
	}
}

func testQualitySnapshot(runID string, status agent.RunStatus, outcome RunOutcome, startedAt, finishedAt time.Time, verification *verifyrt.RunVerification, traces []capprofile.ExecutionTrace) qualityRunSnapshot {
	return qualityRunSnapshot{
		run: &agent.Run{
			ID:         runID,
			SessionID:  "session-" + runID,
			Status:     status,
			StartedAt:  startedAt,
			FinishedAt: finishedAt,
			UpdatedAt:  maxQualityTime(startedAt, finishedAt),
		},
		completion: &RunCompletion{
			RunID:        runID,
			SessionID:    "session-" + runID,
			Status:       status,
			Outcome:      outcome,
			Result:       &RunResult{RunID: runID, Status: status, Outcome: outcome, ExecutionTraces: traces},
			Verification: verification,
		},
	}
}

func maxQualityTime(values ...time.Time) time.Time {
	var out time.Time
	for _, value := range values {
		if value.After(out) {
			out = value
		}
	}
	return out
}

func findQualitySurfaceSummary(items []QualitySurfaceSummary, surface string) *QualitySurfaceSummary {
	for i := range items {
		if items[i].Surface == surface {
			return &items[i]
		}
	}
	return nil
}

func hasReadinessBlocker(items []ReleaseReadinessCheck, id string) bool {
	return hasReadinessCheck(items, id, "blocked")
}

func hasReadinessCheck(items []ReleaseReadinessCheck, id string, status string) bool {
	for _, item := range items {
		if item.ID == id && item.Status == status {
			return true
		}
	}
	return false
}
