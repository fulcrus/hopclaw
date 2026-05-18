package runtime

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	capprofile "github.com/fulcrus/hopclaw/capability/profile"
	verifyrt "github.com/fulcrus/hopclaw/runtime/verify"
)

type QualitySummaryRequest struct {
	Scope agent.ScopeFilter `json:"scope,omitempty"`
	SessionID string        `json:"session_id,omitempty"`
	Since time.Time         `json:"since,omitempty"`
	Limit int               `json:"limit,omitempty"`
}

type QualitySummaryFilter struct {
	Scope agent.ScopeFilter `json:"scope,omitempty"`
	SessionID string        `json:"session_id,omitempty"`
	Since string            `json:"since,omitempty"`
	Limit int               `json:"limit,omitempty"`
}

type QualityRate struct {
	Count int     `json:"count"`
	Total int     `json:"total"`
	Rate  float64 `json:"rate"`
}

type QualityVerificationSummary struct {
	Total   int `json:"total"`
	Passed  int `json:"passed"`
	Warning int `json:"warning"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
}

type QualityLatencySummary struct {
	Samples                        int   `json:"samples"`
	MeanRunDurationMS              int64 `json:"mean_run_duration_ms,omitempty"`
	P50RunDurationMS               int64 `json:"p50_run_duration_ms,omitempty"`
	P95RunDurationMS               int64 `json:"p95_run_duration_ms,omitempty"`
	MeanTimeToVerifiedCompletionMS int64 `json:"mean_time_to_verified_completion_ms,omitempty"`
}

type QualityNamedCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type QualitySurfaceSummary struct {
	Surface                 string                     `json:"surface"`
	RunCount                int                        `json:"run_count"`
	TraceCount              int                        `json:"trace_count"`
	Verification            QualityVerificationSummary `json:"verification"`
	EvidenceChecks          QualityVerificationSummary `json:"evidence_checks"`
	ProfileHit              QualityRate                `json:"profile_hit"`
	Fallback                QualityRate                `json:"fallback"`
	VisualFallback          QualityRate                `json:"visual_fallback"`
	FallbackRecoverySuccess QualityRate                `json:"fallback_recovery_success"`
	Transports              []QualityNamedCount        `json:"transports,omitempty"`
	Profiles                []QualityNamedCount        `json:"profiles,omitempty"`
}

type QualitySummary struct {
	Filter                  QualitySummaryFilter       `json:"filter,omitempty"`
	GeneratedAt             string                     `json:"generated_at,omitempty"`
	RunCount                int                        `json:"run_count"`
	TerminalRunCount        int                        `json:"terminal_run_count"`
	StatusCounts            map[string]int             `json:"status_counts,omitempty"`
	OutcomeCounts           map[string]int             `json:"outcome_counts,omitempty"`
	Verification            QualityVerificationSummary `json:"verification"`
	TaskSuccess             QualityRate                `json:"task_success"`
	FalseSuccess            QualityRate                `json:"false_success"`
	VerificationFailure     QualityRate                `json:"verification_failure"`
	ApprovalInterrupted     QualityRate                `json:"approval_interrupted"`
	FallbackRecoverySuccess QualityRate                `json:"fallback_recovery_success"`
	Latency                 QualityLatencySummary      `json:"latency"`
	TraceCount              int                        `json:"trace_count"`
	ProfileHit              QualityRate                `json:"profile_hit"`
	Fallback                QualityRate                `json:"fallback"`
	VisualFallback          QualityRate                `json:"visual_fallback"`
	TransportDowngrade      QualityRate                `json:"transport_downgrade"`
	Transports              []QualityNamedCount        `json:"transports,omitempty"`
	Profiles                []QualityNamedCount        `json:"profiles,omitempty"`
	Surfaces                []QualitySurfaceSummary    `json:"surfaces,omitempty"`
}

type qualityRunSnapshot struct {
	run        *agent.Run
	completion *RunCompletion
}

type qualitySurfaceAccumulator struct {
	runCount                int
	traceCount              int
	verification            QualityVerificationSummary
	evidenceChecks          QualityVerificationSummary
	profileHits             int
	fallbacks               int
	visualFallbacks         int
	fallbackRecoveryRuns    int
	fallbackRecoverySamples int
	transports              map[string]int
	profiles                map[string]int
}

func (s *Service) GetQualitySummary(ctx context.Context, req QualitySummaryRequest) (*QualitySummary, error) {
	snapshots, err := s.collectQualityRunSnapshots(ctx, req)
	if err != nil {
		return nil, err
	}
	return buildQualitySummary(req, snapshots), nil
}

func (s *Service) collectQualityRunSnapshots(ctx context.Context, req QualitySummaryRequest) ([]qualityRunSnapshot, error) {
	req.Scope = req.Scope.Normalize()
	req.SessionID = strings.TrimSpace(req.SessionID)
	if req.Limit < 0 {
		req.Limit = 0
	}

	runs, err := s.ListRuns(ctx, agent.RunListFilter{Scope: req.Scope})
	if err != nil {
		return nil, err
	}
	filtered := make([]*agent.Run, 0, len(runs))
	for _, run := range runs {
		if run == nil {
			continue
		}
		if req.SessionID != "" && strings.TrimSpace(run.SessionID) != req.SessionID {
			continue
		}
		if !req.Since.IsZero() {
			ref := qualityReferenceTime(run)
			if ref.IsZero() || ref.Before(req.Since) {
				continue
			}
		}
		filtered = append(filtered, run)
	}
	sort.Slice(filtered, func(i, j int) bool {
		return qualityReferenceTime(filtered[i]).After(qualityReferenceTime(filtered[j]))
	})
	if req.Limit > 0 && len(filtered) > req.Limit {
		filtered = filtered[:req.Limit]
	}

	snapshots := make([]qualityRunSnapshot, 0, len(filtered))
	for _, run := range filtered {
		completion, err := s.GetRunCompletion(ctx, run.ID)
		if err != nil {
			return nil, err
		}
		snapshots = append(snapshots, qualityRunSnapshot{
			run:        run,
			completion: completion,
		})
	}
	return snapshots, nil
}

func buildQualitySummary(req QualitySummaryRequest, snapshots []qualityRunSnapshot) *QualitySummary {
	summary := &QualitySummary{
		Filter: QualitySummaryFilter{
			Scope:     req.Scope.Normalize(),
			SessionID: strings.TrimSpace(req.SessionID),
			Limit:     req.Limit,
		},
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		RunCount:    len(snapshots),
	}
	if !req.Since.IsZero() {
		summary.Filter.Since = req.Since.UTC().Format(time.RFC3339)
	}

	statusCounts := make(map[string]int, 8)
	outcomeCounts := make(map[string]int, 8)
	transportCounts := make(map[string]int, 12)
	profileCounts := make(map[string]int, 12)
	surfaceAccumulators := make(map[string]*qualitySurfaceAccumulator, 4)
	latencySamples := make([]int64, 0, len(snapshots))
	verifiedLatencySamples := make([]int64, 0, len(snapshots))

	completedRuns := 0
	taskSuccessRuns := 0
	falseSuccessRuns := 0
	verificationFailureRuns := 0
	approvalInterruptedRuns := 0
	traceCount := 0
	profileHits := 0
	fallbacks := 0
	visualFallbacks := 0
	fallbackRecoveryRuns := 0
	fallbackRecoverySamples := 0

	for _, snapshot := range snapshots {
		run := snapshot.run
		completion := snapshot.completion
		if run == nil || completion == nil {
			continue
		}

		statusKey := strings.TrimSpace(string(completion.Status))
		if statusKey == "" {
			statusKey = strings.TrimSpace(string(run.Status))
		}
		if statusKey != "" {
			statusCounts[statusKey]++
		}
		if isQualityTerminalStatus(completion.Status) {
			summary.TerminalRunCount++
		}

		outcomeKey := strings.TrimSpace(string(completion.Outcome))
		if outcomeKey != "" {
			outcomeCounts[outcomeKey]++
		}

		if completion.Outcome == RunOutcomeCompleted {
			taskSuccessRuns++
		}
		if completion.Status == agent.RunCompleted {
			completedRuns++
		}
		if completion.Outcome == RunOutcomeNeedsConfirmation || completion.Status == agent.RunWaitingApproval {
			approvalInterruptedRuns++
		}

		verification := completion.Verification
		if verification != nil {
			summary.Verification.observe(verification.Status)
			if verification.Status == verifyrt.StatusFailed {
				verificationFailureRuns++
			}
		} else {
			summary.Verification.observe("")
		}

		falseSuccess := completion.Status == agent.RunCompleted &&
			verification != nil &&
			(verification.Status == verifyrt.StatusFailed || verification.RequiredFailures > 0)
		if falseSuccess {
			falseSuccessRuns++
		}

		if durationMS, ok := qualityRunDurationMS(run); ok {
			latencySamples = append(latencySamples, durationMS)
			if completion.Status == agent.RunCompleted && verification != nil && verification.Status != verifyrt.StatusSkipped {
				verifiedLatencySamples = append(verifiedLatencySamples, durationMS)
			}
		}

		surfaceRuns := make(map[string]struct{}, 2)
		surfaceFallbackRuns := make(map[string]bool, 2)
		traces := qualityExecutionTraces(completion)
		for _, trace := range traces {
			normalizedTrace := trace.Normalized()
			surface := qualityTraceSurface(normalizedTrace)
			if surface != "" {
				surfaceRuns[surface] = struct{}{}
			}
			traceCount++
			if normalizedTrace.ProfileHit {
				profileHits++
			}
			if len(normalizedTrace.FallbackPath) > 0 {
				fallbacks++
				if surface != "" {
					surfaceFallbackRuns[surface] = true
				}
			}
			if normalizedTrace.ExecutionMode == capprofile.ModeVisualFallback {
				visualFallbacks++
			}
			if name := strings.TrimSpace(normalizedTrace.ChosenTransport); name != "" {
				transportCounts[name]++
			}
			if name := strings.TrimSpace(normalizedTrace.ProfileID); name != "" {
				profileCounts[name]++
			}

			if surface == "" {
				continue
			}
			acc := ensureQualitySurfaceAccumulator(surfaceAccumulators, surface)
			acc.traceCount++
			if normalizedTrace.ProfileHit {
				acc.profileHits++
			}
			if len(normalizedTrace.FallbackPath) > 0 {
				acc.fallbacks++
			}
			if normalizedTrace.ExecutionMode == capprofile.ModeVisualFallback {
				acc.visualFallbacks++
			}
			if name := strings.TrimSpace(normalizedTrace.ChosenTransport); name != "" {
				if acc.transports == nil {
					acc.transports = make(map[string]int, 8)
				}
				acc.transports[name]++
			}
			if name := strings.TrimSpace(normalizedTrace.ProfileID); name != "" {
				if acc.profiles == nil {
					acc.profiles = make(map[string]int, 8)
				}
				acc.profiles[name]++
			}
		}

		if verification != nil {
			for _, check := range verification.Checks {
				surface := qualitySurfaceForVerificationCheck(check.Name)
				if surface == "" {
					continue
				}
				surfaceRuns[surface] = struct{}{}
				acc := ensureQualitySurfaceAccumulator(surfaceAccumulators, surface)
				acc.evidenceChecks.observe(check.Status)
			}
		}

		for surface := range surfaceRuns {
			acc := ensureQualitySurfaceAccumulator(surfaceAccumulators, surface)
			acc.runCount++
			if verification != nil {
				acc.verification.observe(verification.Status)
			} else {
				acc.verification.observe("")
			}
		}

		runRecovered := completion.Status == agent.RunCompleted &&
			verification != nil &&
			verification.Status != verifyrt.StatusFailed
		if len(surfaceFallbackRuns) > 0 {
			fallbackRecoverySamples++
			if runRecovered {
				fallbackRecoveryRuns++
			}
		}
		for surface := range surfaceFallbackRuns {
			acc := ensureQualitySurfaceAccumulator(surfaceAccumulators, surface)
			acc.fallbackRecoverySamples++
			if runRecovered {
				acc.fallbackRecoveryRuns++
			}
		}
	}

	summary.StatusCounts = normalizeCountMap(statusCounts)
	summary.OutcomeCounts = normalizeCountMap(outcomeCounts)
	summary.TaskSuccess = qualityRate(taskSuccessRuns, summary.TerminalRunCount)
	summary.FalseSuccess = qualityRate(falseSuccessRuns, completedRuns)
	summary.VerificationFailure = qualityRate(verificationFailureRuns, summary.Verification.Total)
	summary.ApprovalInterrupted = qualityRate(approvalInterruptedRuns, summary.RunCount)
	summary.FallbackRecoverySuccess = qualityRate(fallbackRecoveryRuns, fallbackRecoverySamples)
	summary.TraceCount = traceCount
	summary.ProfileHit = qualityRate(profileHits, traceCount)
	summary.Fallback = qualityRate(fallbacks, traceCount)
	summary.VisualFallback = qualityRate(visualFallbacks, traceCount)
	summary.TransportDowngrade = qualityRate(fallbacks, traceCount)
	summary.Transports = qualityNamedCounts(transportCounts)
	summary.Profiles = qualityNamedCounts(profileCounts)
	summary.Surfaces = qualitySurfaceSummaries(surfaceAccumulators)
	summary.Latency = qualityLatencySummary(latencySamples, verifiedLatencySamples)
	return summary
}

func qualityExecutionTraces(completion *RunCompletion) []capprofile.ExecutionTrace {
	if completion == nil || completion.Result == nil {
		return nil
	}
	return completion.Result.ExecutionTraces
}

func ensureQualitySurfaceAccumulator(items map[string]*qualitySurfaceAccumulator, surface string) *qualitySurfaceAccumulator {
	surface = strings.TrimSpace(strings.ToLower(surface))
	if surface == "" {
		return &qualitySurfaceAccumulator{}
	}
	if items[surface] == nil {
		items[surface] = &qualitySurfaceAccumulator{}
	}
	return items[surface]
}

func qualitySurfaceSummaries(items map[string]*qualitySurfaceAccumulator) []QualitySurfaceSummary {
	if len(items) == 0 {
		return nil
	}
	out := make([]QualitySurfaceSummary, 0, len(items))
	for surface, acc := range items {
		if acc == nil {
			continue
		}
		out = append(out, QualitySurfaceSummary{
			Surface:                 surface,
			RunCount:                acc.runCount,
			TraceCount:              acc.traceCount,
			Verification:            acc.verification,
			EvidenceChecks:          acc.evidenceChecks,
			ProfileHit:              qualityRate(acc.profileHits, acc.traceCount),
			Fallback:                qualityRate(acc.fallbacks, acc.traceCount),
			VisualFallback:          qualityRate(acc.visualFallbacks, acc.traceCount),
			FallbackRecoverySuccess: qualityRate(acc.fallbackRecoveryRuns, acc.fallbackRecoverySamples),
			Transports:              qualityNamedCounts(acc.transports),
			Profiles:                qualityNamedCounts(acc.profiles),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Surface < out[j].Surface
	})
	return out
}

func qualityNamedCounts(counts map[string]int) []QualityNamedCount {
	if len(counts) == 0 {
		return nil
	}
	out := make([]QualityNamedCount, 0, len(counts))
	for name, count := range counts {
		name = strings.TrimSpace(name)
		if name == "" || count <= 0 {
			continue
		}
		out = append(out, QualityNamedCount{Name: name, Count: count})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count == out[j].Count {
			return out[i].Name < out[j].Name
		}
		return out[i].Count > out[j].Count
	})
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeCountMap(counts map[string]int) map[string]int {
	if len(counts) == 0 {
		return nil
	}
	out := make(map[string]int, len(counts))
	for key, count := range counts {
		key = strings.TrimSpace(key)
		if key == "" || count <= 0 {
			continue
		}
		out[key] = count
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func qualityRate(count, total int) QualityRate {
	rate := 0.0
	if total > 0 {
		rate = float64(count) / float64(total)
	}
	return QualityRate{
		Count: count,
		Total: total,
		Rate:  rate,
	}
}

func qualityLatencySummary(samples []int64, verifiedSamples []int64) QualityLatencySummary {
	summary := QualityLatencySummary{Samples: len(samples)}
	if len(samples) > 0 {
		sorted := append([]int64(nil), samples...)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
		var total int64
		for _, sample := range sorted {
			total += sample
		}
		summary.MeanRunDurationMS = total / int64(len(sorted))
		summary.P50RunDurationMS = percentileMillis(sorted, 0.50)
		summary.P95RunDurationMS = percentileMillis(sorted, 0.95)
	}
	if len(verifiedSamples) > 0 {
		var total int64
		for _, sample := range verifiedSamples {
			total += sample
		}
		summary.MeanTimeToVerifiedCompletionMS = total / int64(len(verifiedSamples))
	}
	return summary
}

func percentileMillis(sorted []int64, p float64) int64 {
	if len(sorted) == 0 {
		return 0
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 1 {
		return sorted[len(sorted)-1]
	}
	index := int(float64(len(sorted)-1) * p)
	if index < 0 {
		index = 0
	}
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return sorted[index]
}

func qualitySurfaceForVerificationCheck(name string) string {
	switch strings.TrimSpace(name) {
	case "browser.result":
		return string(capprofile.SurfaceBrowser)
	case "desktop.result":
		return string(capprofile.SurfaceDesktop)
	default:
		return ""
	}
}

func qualityTraceSurface(trace capprofile.ExecutionTrace) string {
	surface := strings.TrimSpace(trace.Surface)
	if surface == "" {
		surface = strings.TrimSpace(trace.Capability)
	}
	return strings.ToLower(surface)
}

func qualityReferenceTime(run *agent.Run) time.Time {
	if run == nil {
		return time.Time{}
	}
	switch {
	case !run.FinishedAt.IsZero():
		return run.FinishedAt.UTC()
	case !run.UpdatedAt.IsZero():
		return run.UpdatedAt.UTC()
	case !run.StartedAt.IsZero():
		return run.StartedAt.UTC()
	default:
		return time.Time{}
	}
}

func qualityRunDurationMS(run *agent.Run) (int64, bool) {
	if run == nil || run.StartedAt.IsZero() || run.FinishedAt.IsZero() {
		return 0, false
	}
	duration := run.FinishedAt.Sub(run.StartedAt)
	if duration < 0 {
		return 0, false
	}
	return duration.Milliseconds(), true
}

func isQualityTerminalStatus(status agent.RunStatus) bool {
	switch status {
	case agent.RunCompleted, agent.RunFailed, agent.RunCancelled, agent.RunWaitingInput, agent.RunWaitingApproval:
		return true
	default:
		return false
	}
}

func (s *QualityVerificationSummary) observe(status verifyrt.Status) {
	s.Total++
	switch status {
	case verifyrt.StatusPassed:
		s.Passed++
	case verifyrt.StatusWarning:
		s.Warning++
	case verifyrt.StatusFailed:
		s.Failed++
	default:
		s.Skipped++
	}
}
