package repl

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func runPanelRow(item RunSummary, snapshot *SupervisorSnapshot, currentSessionID, currentSessionKey, currentTarget, currentTargetKind string) string {
	sessionLabel := strings.TrimSpace(item.SessionKey)
	switch {
	case sessionLabel != "":
	case item.SessionID == currentSessionID && strings.TrimSpace(currentSessionKey) != "":
		sessionLabel = currentSessionKey
	default:
		sessionLabel = compact(defaultString(item.SessionID, "-"), 12)
	}
	mode := "--"
	switch {
	case snapshot != nil && strings.TrimSpace(snapshot.ForegroundRunID) != "" && item.ID == strings.TrimSpace(snapshot.ForegroundRunID):
		mode = "FG"
	case supervisorRunActive(item):
		mode = "BG"
	case item.SessionID == currentSessionID:
		mode = "FG"
	}
	status := defaultString(strings.TrimSpace(item.Status), defaultString(strings.TrimSpace(item.Phase), "unknown"))
	tool := defaultString(strings.TrimSpace(item.ToolName), "-")
	attention := strings.TrimSpace(item.Attention)
	if attention == "" && item.Resumable {
		attention = "resumable"
	}
	elapsed := runElapsedLabel(item.CreatedAt)
	target := strings.TrimSpace(item.Target)
	targetKind := ""
	if target == "" {
		target = currentTarget
		targetKind = currentTargetKind
	}
	row := fmt.Sprintf("%-10s %-2s %-14s %-14s %-18s %s",
		compact(item.ID, 10),
		mode,
		compact(status, 16),
		compact(targetConnectionLabel(target, targetKind), 14),
		compact(tool, 18),
		elapsed,
	)
	if attention != "" {
		row += "  " + compact(attention, 12)
	}
	if sessionLabel != "" {
		row += "  " + compact("conversation "+sessionLabel, 24)
	}
	return row
}

func supervisorRunActive(item RunSummary) bool {
	switch normalizedExecutionState(firstNonEmpty(item.Status, item.Phase)) {
	case "running", "waiting approval":
		return true
	default:
		return false
	}
}

func supervisorRunPaused(item RunSummary) bool {
	state := normalizedExecutionState(firstNonEmpty(item.Status, item.Phase))
	return state == "paused" || item.Resumable
}

func runElapsedLabel(createdAt string) string {
	if strings.TrimSpace(createdAt) == "" {
		return "00:00"
	}
	started, err := time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return "00:00"
	}
	return formatClockDuration(time.Since(started))
}

func deliverySummaryLine(item *RunDelivery) string {
	if item == nil {
		return "-"
	}
	parts := []string{defaultString(strings.TrimSpace(item.Status), "unknown")}
	if strings.TrimSpace(item.Attempt) != "" {
		parts = append(parts, item.Attempt)
	}
	if strings.TrimSpace(item.NextAttempt) != "" {
		parts = append(parts, "next "+item.NextAttempt)
	}
	if strings.TrimSpace(item.Summary) != "" {
		parts = append(parts, compact(item.Summary, 48))
	}
	return strings.Join(parts, " · ")
}

func automationSummaryLine(item *AutomationProjection) string {
	if item == nil {
		return "-"
	}
	parts := []string{defaultString(item.Name, item.ID)}
	if strings.TrimSpace(item.Kind) != "" {
		parts = append(parts, item.Kind)
	}
	if strings.TrimSpace(item.NextRun) != "" {
		parts = append(parts, "next "+item.NextRun)
	}
	if strings.TrimSpace(item.Health) != "" {
		parts = append(parts, compact(item.Health, 48))
	}
	return strings.Join(parts, " · ")
}

func automationPanelRow(item AutomationItem) string {
	return joinNonEmpty("  ",
		compact(defaultString(item.Name, item.ID), 20),
		compact(defaultString(item.Kind, "-"), 8),
		compact(defaultString(item.Status, "-"), 12),
		compact(defaultString(item.Schedule, "-"), 18),
		compact(defaultString(item.Delivery, "-"), 18),
		compact(defaultString(item.NextRun, "-"), 16),
		compact(defaultString(item.Health, "-"), 18),
	)
}

func deriveReadinessCategories(checks []DoctorCheck, target, kind string) []ReadinessCategory {
	type bucket struct {
		id      string
		label   string
		kind    string
		status  string
		summary string
	}
	buckets := []bucket{
		{id: "model_provider", label: "AI Setup", kind: "setup", status: "ready"},
		{id: "gateway", label: "System", kind: "runtime", status: "ready"},
		{id: "remote_target", label: readinessTargetLabel(target, kind), kind: "runtime", status: "ready"},
		{id: "memory_index", label: "Memory", kind: "runtime", status: "ready"},
		{id: "quality_release", label: "Release Checks", kind: "quality", status: "ready"},
		{id: "channel_delivery", label: "Replies", kind: "runtime", status: "ready"},
		{id: "devices_helpers", label: "Local Helpers", kind: "runtime", status: "ready"},
		{id: "skills_plugins", label: "Extensions", kind: "runtime", status: "ready"},
		{id: "automation_runtime", label: "Automations", kind: "runtime", status: "ready"},
	}
	update := func(id, status, summary string) {
		for i := range buckets {
			if buckets[i].id != id {
				continue
			}
			previous := buckets[i].status
			buckets[i].status = mergeReadinessStatus(buckets[i].status, status)
			if strings.TrimSpace(summary) != "" && (buckets[i].summary == "" || readinessPriority(status) >= readinessPriority(previous)) {
				buckets[i].summary = strings.TrimSpace(summary)
			}
			return
		}
	}
	for _, item := range checks {
		haystack := strings.ToLower(strings.Join([]string{item.Category, item.Name}, " "))
		status := doctorStatus(item.Status)
		summary := strings.TrimSpace(item.Detail)
		switch {
		case strings.Contains(haystack, "model"), strings.Contains(haystack, "provider"):
			update("model_provider", status, summary)
		case strings.Contains(haystack, "gateway"), strings.Contains(haystack, "remote"):
			update("remote_target", status, summary)
			update("gateway", status, summary)
		case strings.Contains(haystack, "memory"), strings.Contains(haystack, "index"):
			update("memory_index", status, summary)
		case strings.Contains(haystack, "quality"), strings.Contains(haystack, "release"), strings.Contains(haystack, "verify"):
			update("quality_release", status, summary)
		case strings.Contains(haystack, "channel"), strings.Contains(haystack, "webhook"), strings.Contains(haystack, "delivery"), strings.Contains(haystack, "adapter"):
			update("channel_delivery", status, summary)
		case strings.Contains(haystack, "device"), strings.Contains(haystack, "helper"), strings.Contains(haystack, "browser"), strings.Contains(haystack, "desktop"), strings.Contains(haystack, "daemon"), strings.Contains(haystack, "sandbox"):
			update("devices_helpers", status, summary)
		case strings.Contains(haystack, "skill"), strings.Contains(haystack, "plugin"), strings.Contains(haystack, "mcp"), strings.Contains(haystack, "extension"):
			update("skills_plugins", status, summary)
		case strings.Contains(haystack, "automation"), strings.Contains(haystack, "scheduling"), strings.Contains(haystack, "cron"), strings.Contains(haystack, "watch"), strings.Contains(haystack, "wakeup"):
			update("automation_runtime", status, summary)
		default:
			update("gateway", status, summary)
		}
	}
	out := make([]ReadinessCategory, 0, len(buckets))
	for _, item := range buckets {
		out = append(out, ReadinessCategory{
			ID:      item.id,
			Label:   item.label,
			Status:  item.status,
			Summary: defaultString(item.summary, item.status),
			Kind:    item.kind,
		})
	}
	return out
}

func readinessTargetLabel(target, kind string) string {
	target = displayTargetName(target)
	switch normalizeTargetKind(kind, target) {
	case "local":
		if strings.EqualFold(target, "local") {
			return "Local runtime"
		}
		return "Local runtime " + target
	default:
		return "Remote " + defaultString(strings.TrimSpace(target), "local")
	}
}

func readinessLine(item ReadinessCategory) string {
	if strings.TrimSpace(item.Summary) == "" || strings.EqualFold(strings.TrimSpace(item.Summary), strings.TrimSpace(item.Status)) {
		return item.Status
	}
	return item.Status + " · " + item.Summary
}

func readinessOverallSnapshotStatus(snapshot *ReadinessSnapshot) string {
	if snapshot == nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(snapshot.OverallStatus))
}

func readinessOverallStatus(explicit string, categories []ReadinessCategory) string {
	status := strings.ToLower(strings.TrimSpace(explicit))
	switch status {
	case "ready", "degraded", "blocked", "unknown":
		return status
	}
	best := "ready"
	for _, item := range categories {
		next := strings.ToLower(strings.TrimSpace(item.Status))
		if readinessPriority(next) > readinessPriority(best) {
			best = next
		}
	}
	return best
}

func readinessPanelLines(overall string, categories []ReadinessCategory, recoverable []RecoveryCandidate) []string {
	lines := []string{
		fmt.Sprintf("Summary        [%s] %s", strings.ToUpper(defaultString(overall, "ready")), readinessPanelSummary(categories, len(recoverable))),
		"",
		"Categories",
	}
	for _, item := range categories {
		lines = append(lines, fmt.Sprintf("%-15s %s", compact(item.Label, 15), readinessLine(item)))
	}
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("Recovery Center  %d recoverable item(s)", len(recoverable)))
	if len(recoverable) == 0 {
		lines = append(lines, "none")
		return lines
	}
	for index, item := range recoverable[:min(len(recoverable), 3)] {
		lines = append(lines, fmt.Sprintf("%d. %s", index+1, readinessRecoveryLine(item)))
	}
	return lines
}

func readinessPanelSummary(categories []ReadinessCategory, recoverable int) string {
	blocked := 0
	warnings := 0
	for _, item := range categories {
		switch strings.ToLower(strings.TrimSpace(item.Status)) {
		case "blocked":
			blocked++
		case "degraded":
			warnings++
		}
	}
	parts := make([]string, 0, 3)
	if blocked > 0 {
		parts = append(parts, fmt.Sprintf("%d blocker%s", blocked, pluralSuffix(blocked)))
	}
	if warnings > 0 {
		parts = append(parts, fmt.Sprintf("%d warning%s", warnings, pluralSuffix(warnings)))
	}
	if recoverable > 0 {
		parts = append(parts, fmt.Sprintf("%d recoverable", recoverable))
	}
	if len(parts) == 0 {
		return "all required categories healthy"
	}
	return strings.Join(parts, " · ")
}

func readinessRecoveryLine(item RecoveryCandidate) string {
	label := startupRecoveryCandidateLabel(item)
	action := strings.TrimSpace(item.Action)
	if action == "" {
		return label
	}
	return label + " · " + action
}

func pluralSuffix(value int) string {
	if value == 1 {
		return ""
	}
	return "s"
}

func mergeReadinessStatus(current, incoming string) string {
	current = defaultString(strings.TrimSpace(current), "ready")
	incoming = defaultString(strings.TrimSpace(incoming), "ready")
	switch {
	case strings.EqualFold(current, "unknown") && !strings.EqualFold(incoming, "unknown"):
		return incoming
	case readinessPriority(incoming) > readinessPriority(current):
		return incoming
	default:
		return current
	}
}

func doctorStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "fail", "failed", "error":
		return "blocked"
	case "warn", "warning", "degraded":
		return "degraded"
	case "", "ok", "ready":
		return "ready"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func normalizeContextRecommendation(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "compact now":
		return "compact recommended"
	case "consider /compact soon":
		return "compact recommended"
	default:
		return "no action needed"
	}
}

func (r *REPL) refreshDeliveryState(runID string) {
	if r == nil || r.service == nil {
		return
	}
	detail, err := r.service.GetRunDetail(context.Background(), strings.TrimSpace(runID))
	if err != nil || detail == nil || detail.Delivery == nil {
		return
	}
	status := normalizeDeliveryStatus(detail.Delivery, r.service, runID)
	r.deliveryState = deliveryDockState{
		State:   status,
		Summary: deliverySummaryLine(detail.Delivery),
		Next:    strings.TrimSpace(detail.Delivery.NextAttempt),
	}
}

// normalizeDeliveryStatus maps raw delivery data to the design vocabulary:
// inline_only, delivering, delivered, partial, retrying, blocked, dead_letter.
func normalizeDeliveryStatus(delivery *RunDelivery, svc Service, runID string) string {
	raw := strings.ToLower(strings.TrimSpace(delivery.Status))

	// Try to get detailed target-level info for richer status detection.
	deliveryDetail, _ := svc.GetRunDelivery(context.Background(), strings.TrimSpace(runID))
	if deliveryDetail != nil && len(deliveryDetail.Targets) > 0 {
		return deriveDeliveryStatusFromTargets(deliveryDetail)
	}

	// Fallback: map from the summary-level status field.
	switch raw {
	case "delivered", "ok", "done":
		return "delivered"
	case "partial":
		return "partial"
	case "blocked", "verification_blocked":
		return "blocked"
	case "dead_letter", "exhausted":
		return "dead_letter"
	case "retrying", "retry":
		return "retrying"
	case "delivering", "in_progress", "pending":
		return "delivering"
	case "inline_only", "inline":
		return "inline_only"
	default:
		// Legacy fallback: detect retrying from NextAttempt presence.
		if delivery.NextAttempt != "" && raw != "delivered" && raw != "ok" {
			return "retrying"
		}
		return raw
	}
}

func deriveDeliveryStatusFromTargets(detail *RunDeliveryDetail) string {
	total := len(detail.Targets)
	if total == 0 {
		return strings.ToLower(strings.TrimSpace(detail.Status))
	}
	delivered := 0
	blocked := 0
	deadLetter := 0
	delivering := 0
	retrying := 0
	for _, t := range detail.Targets {
		switch strings.ToLower(strings.TrimSpace(t.Status)) {
		case "delivered", "ok", "done":
			delivered++
		case "blocked", "verification_blocked":
			blocked++
		case "dead_letter", "exhausted":
			deadLetter++
		case "pending", "in_progress", "delivering":
			delivering++
		case "retrying", "retry":
			retrying++
		}
	}
	switch {
	case deadLetter > 0 && delivered == 0:
		return "dead_letter"
	case deadLetter > 0 && delivered > 0:
		return "partial"
	case blocked > 0:
		return "blocked"
	case delivered == total:
		return "delivered"
	case delivered > 0 && delivered < total:
		return "partial"
	case delivering > 0:
		return "delivering"
	case retrying > 0:
		return "retrying"
	default:
		return "delivering"
	}
}

// normalizeDeliveryDisplayStatus derives the overall display status for a delivery detail
// using the design vocabulary: inline_only, delivering, delivered, partial, retrying, blocked, dead_letter.
func normalizeDeliveryDisplayStatus(detail *RunDeliveryDetail) string {
	if detail == nil {
		return "unknown"
	}
	if len(detail.Targets) > 0 {
		return deriveDeliveryStatusFromTargets(detail)
	}
	return normalizeTargetDeliveryStatus(detail.Status)
}

// normalizeTargetDeliveryStatus maps a raw target/receipt status to the design vocabulary.
func normalizeTargetDeliveryStatus(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "delivered", "ok", "done":
		return "delivered"
	case "partial":
		return "partial"
	case "blocked", "verification_blocked":
		return "blocked"
	case "dead_letter", "exhausted":
		return "dead_letter"
	case "retrying", "retry":
		return "retrying"
	case "delivering", "in_progress", "pending":
		return "delivering"
	case "inline_only", "inline":
		return "inline_only"
	case "":
		return "unknown"
	default:
		return strings.ToLower(strings.TrimSpace(raw))
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
