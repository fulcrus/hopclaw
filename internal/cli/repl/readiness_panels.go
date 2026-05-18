package repl

import (
	"context"
	"fmt"
	"strings"
)

func (r *REPL) renderDoctorChecks(ctx context.Context) error {
	r.refreshReadinessProjection(ctx, true)
	categories := []ReadinessCategory(nil)
	if snapshot := r.readinessSnapshot; snapshot != nil && len(snapshot.Categories) > 0 {
		categories = append(categories, snapshot.Categories...)
	} else {
		items, err := r.service.DoctorChecks(ctx)
		if err != nil {
			return err
		}
		if len(items) == 0 {
			r.renderer.SystemLine("Doctor checks are unavailable.")
			return nil
		}
		categories = deriveReadinessCategories(items, r.targetName, r.targetKind)
	}
	recoverable := r.recoveryCandidates(ctx)
	lines := readinessPanelLines(
		readinessOverallStatus(readinessOverallSnapshotStatus(r.readinessSnapshot), categories),
		categories,
		recoverable,
	)
	r.openInfoPanel("System Readiness", lines, doctorPanelActions(recoverable))
	return nil
}

func (r *REPL) startupRecoveryCardSpec(ctx context.Context) (CardSpec, bool) {
	if r == nil || r.service == nil {
		return CardSpec{}, false
	}
	r.refreshReadinessProjection(ctx, true)
	recoverable := r.recoveryCandidates(ctx)
	health := r.startupHealthSummary(ctx)
	failed, _ := r.lastFailedRun(ctx)
	if len(recoverable) == 0 && health == "" {
		return CardSpec{}, false
	}

	rows := []CardRow{{
		Label: "Conversation",
		Value: defaultString(strings.TrimSpace(r.sessionKey), "default"),
	}}
	if health != "" {
		rows = append(rows, CardRow{Label: "Health", Value: health})
	}
	if len(recoverable) > 0 {
		rows = append(rows, CardRow{
			Label: "Recoverable",
			Value: startupRecoverySummary(recoverable),
		})
	}

	footer := ""
	if failed != nil {
		label := defaultString(failed.Error, defaultString(failed.Status, "failed"))
		footer = fmt.Sprintf("Last failed: %s (%s)", failed.ID, label)
	}

	return CardSpec{
		Title:   "Welcome back",
		Rows:    rows,
		Actions: r.startupRecoveryActions(recoverable, health),
		Footer:  footer,
		Width:   84,
	}, true
}

func (r *REPL) startupRecoveryActions(recoverable []RecoveryCandidate, health string) string {
	actions := make([]string, 0, 4)
	if len(recoverable) > 0 {
		if ref := strings.TrimSpace(startupRecoveryResumeRef(recoverable[0])); ref != "" {
			actions = append(actions, "continue", "/continue "+ref)
		} else {
			actions = append(actions, "/continue")
		}
	}
	if normalizeTargetKind(r.targetKind, r.targetName) == "remote" {
		actions = append(actions, "/remote")
	}
	if strings.TrimSpace(health) != "" || len(recoverable) == 0 {
		actions = append(actions, "/doctor")
	}
	return strings.Join(actions, "  ")
}

func startupRecoverySummary(candidates []RecoveryCandidate) string {
	if len(candidates) == 0 {
		return ""
	}
	summary := startupRecoveryCandidateLabel(candidates[0])
	if len(candidates) == 1 {
		return summary
	}
	return fmt.Sprintf("%s (+%d more)", summary, len(candidates)-1)
}

func startupRecoveryCandidateLabel(candidate RecoveryCandidate) string {
	kind := strings.TrimSpace(strings.ReplaceAll(candidate.Type, "_", " "))
	if kind == "" {
		kind = "recoverable"
	}
	if id := strings.TrimSpace(candidate.ID); id != "" {
		if strings.HasSuffix(kind, " run") {
			return strings.TrimSpace(strings.TrimSuffix(kind, " run")) + " " + id
		}
		return kind + " " + id
	}
	if summary := strings.TrimSpace(candidate.Summary); summary != "" {
		return summary
	}
	return kind
}

func startupRecoveryResumeRef(candidate RecoveryCandidate) string {
	return strings.TrimSpace(candidate.ID)
}

func (r *REPL) startupHealthSummary(ctx context.Context) string {
	if r == nil || r.service == nil {
		return ""
	}
	policy := startupDiagnosticsPolicy(r.readinessSnapshot, r.targetName, r.targetKind)
	if snapshot := r.readinessSnapshot; snapshot != nil && len(snapshot.Categories) > 0 {
		best := startupActionableReadinessCategory(snapshot.Categories)
		if best != nil {
			if strings.EqualFold(strings.TrimSpace(best.Status), "unknown") {
				return ""
			}
			detail := strings.TrimSpace(best.Summary)
			label := strings.ToLower(strings.TrimSpace(best.Label))
			switch {
			case detail != "" && !strings.EqualFold(detail, best.Status):
				return fmt.Sprintf("%s (%s: %s)", best.Status, label, detail)
			case label != "":
				return fmt.Sprintf("%s (%s)", best.Status, label)
			default:
				return best.Status
			}
		}
	}
	if strings.EqualFold(policy, "quiet_when_healthy") {
		return ""
	}
	items, err := r.service.DoctorChecks(ctx)
	if err != nil || len(items) == 0 {
		return ""
	}
	best := startupActionableReadinessCategory(deriveReadinessCategories(items, r.targetName, r.targetKind))
	if best == nil {
		return ""
	}
	if strings.EqualFold(strings.TrimSpace(best.Status), "unknown") {
		return ""
	}
	detail := strings.TrimSpace(best.Summary)
	label := strings.ToLower(strings.TrimSpace(best.Label))
	switch {
	case detail != "" && !strings.EqualFold(detail, best.Status):
		return fmt.Sprintf("%s (%s: %s)", best.Status, label, detail)
	case label != "":
		return fmt.Sprintf("%s (%s)", best.Status, label)
	default:
		return best.Status
	}
}

func startupDiagnosticsPolicy(snapshot *ReadinessSnapshot, targetName, targetKind string) string {
	if snapshot != nil && strings.TrimSpace(snapshot.StartupDiagnostics) != "" {
		return strings.TrimSpace(snapshot.StartupDiagnostics)
	}
	if !strings.EqualFold(strings.TrimSpace(targetKind), "local") {
		return "actionable_only"
	}
	switch strings.ToLower(strings.TrimSpace(targetName)) {
	case "", "local", "standalone":
		return "quiet_when_healthy"
	default:
		return "actionable_only"
	}
}

func startupActionableReadinessCategory(items []ReadinessCategory) *ReadinessCategory {
	var best *ReadinessCategory
	for _, item := range items {
		if !startupReadinessActionable(item) {
			continue
		}
		copyItem := item
		if best == nil || startupReadinessRank(copyItem) > startupReadinessRank(*best) {
			best = &copyItem
		}
	}
	return best
}

func startupReadinessActionable(item ReadinessCategory) bool {
	status := strings.ToLower(strings.TrimSpace(item.Status))
	if status == "" || status == "ready" || status == "unknown" {
		return false
	}
	switch strings.TrimSpace(item.ID) {
	case "remote_target", "gateway", "channel_delivery":
		return true
	case "model_provider", "memory_index", "automation_runtime":
		return status == "blocked"
	default:
		return false
	}
}

func readinessPriority(status string) int {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "blocked":
		return 3
	case "degraded":
		return 2
	case "unknown":
		return 1
	default:
		return 0
	}
}

func startupReadinessRank(item ReadinessCategory) int {
	rank := readinessPriority(item.Status) * 10
	switch strings.TrimSpace(item.ID) {
	case "remote_target":
		rank += 3
	case "gateway":
		rank += 2
	case "channel_delivery":
		rank += 2
	case "model_provider":
		rank += 1
	case "quality_release":
		rank += 0
	}
	return rank
}

func (r *REPL) recoveryCandidates(ctx context.Context) []RecoveryCandidate {
	if r == nil || r.service == nil {
		return nil
	}
	if snapshot := r.readinessSnapshot; snapshot != nil && len(snapshot.RecoveryCandidates) > 0 {
		return append([]RecoveryCandidate(nil), snapshot.RecoveryCandidates...)
	}
	items, err := r.service.RecoveryCandidates(ctx)
	if err != nil {
		return nil
	}
	return append([]RecoveryCandidate(nil), items...)
}

func doctorPanelActions(recoverable []RecoveryCandidate) string {
	if len(recoverable) > 0 {
		return "/doctor  /runs  /continue  Esc back"
	}
	return "/doctor  /remote  Esc back"
}

func (r *REPL) startupRecoveryResumeReference(ctx context.Context) (string, bool) {
	if r == nil || r.service == nil {
		return "", false
	}
	r.refreshReadinessProjection(ctx, true)
	recoverable := r.recoveryCandidates(ctx)
	for _, candidate := range recoverable {
		ref := startupRecoveryResumeRef(candidate)
		if ref == "" {
			continue
		}
		candidateType := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(candidate.Type), "_", " "))
		switch {
		case strings.Contains(candidateType, "session"):
			return ref, true
		default:
			detail, err := r.service.GetRunDetail(ctx, ref)
			if err == nil && detail != nil && strings.TrimSpace(detail.Run.ID) != "" {
				return ref, true
			}
		}
	}
	return "", false
}
