package eventbus

import (
	"fmt"
	"strings"
	"time"

	domainscope "github.com/fulcrus/hopclaw/internal/domain/scope"
	"github.com/fulcrus/hopclaw/internal/support/normalize"
)

func NewRunStartedEvent(runID, sessionID string, payload RunDispatchAttrs, extraAttrs map[string]any) Event {
	return NewRunDispatchEvent(EventRunStarted, runID, sessionID, payload, extraAttrs)
}

func NewRunResumedEvent(runID, sessionID string, payload RunDispatchAttrs, extraAttrs map[string]any) Event {
	return NewRunDispatchEvent(EventRunResumed, runID, sessionID, payload, extraAttrs)
}

func NewRunCompletedEvent(runID, sessionID string, payload RunStatusAttrs, extraAttrs map[string]any) Event {
	return NewRunStatusEvent(EventRunCompleted, runID, sessionID, payload, extraAttrs)
}

func NewRunFailedEvent(runID, sessionID string, payload RunStatusAttrs, extraAttrs map[string]any) Event {
	return NewRunStatusEvent(EventRunFailed, runID, sessionID, payload, extraAttrs)
}

func NewRunCancelledEvent(runID, sessionID string, payload RunControlAttrs, extraAttrs map[string]any) Event {
	return NewRunControlEvent(EventRunCancelled, runID, sessionID, payload, extraAttrs)
}

func NewRunPreflightUpdatedEvent(runID, sessionID string, payload RunPreflightAttrs, extraAttrs map[string]any) Event {
	return newEventWithAttrs(EventRunPreflightUpdated, runID, sessionID, mergePayloadAttrs(extraAttrs, payload.ToMap()))
}

func NewRunWaitingInputEvent(runID, sessionID string, payload RunPreflightAttrs, extraAttrs map[string]any) Event {
	return newEventWithAttrs(EventRunWaitingInput, runID, sessionID, mergePayloadAttrs(extraAttrs, payload.ToMap()))
}

func NewRunWaitingApprovalEvent(runID, sessionID string, payload ApprovalEventAttrs, extraAttrs map[string]any) Event {
	return newEventWithAttrs(EventRunWaitingApproval, runID, sessionID, mergePayloadAttrs(extraAttrs, payload.ToMap()))
}

func NewRunTimeoutEvent(runID, sessionID string, payload RunTimeoutAttrs, extraAttrs map[string]any) Event {
	return newEventWithAttrs(EventRunTimeout, runID, sessionID, mergePayloadAttrs(extraAttrs, payload.ToMap()))
}

func NewRunPlannedEvent(runID, sessionID string, payload RunPlannedAttrs, extraAttrs map[string]any) Event {
	return newEventWithAttrs(EventRunPlanned, runID, sessionID, mergePayloadAttrs(extraAttrs, payload.ToMap()))
}

func NewRunSteeredEvent(runID, sessionID string, payload RunSteeredAttrs, extraAttrs map[string]any) Event {
	return newEventWithAttrs(EventRunSteered, runID, sessionID, mergePayloadAttrs(extraAttrs, payload.ToMap()))
}

func NewPlanTaskStartedEvent(runID, sessionID string, payload PlanTaskAttrs, extraAttrs map[string]any) Event {
	return NewPlanTaskEvent(EventPlanTaskStarted, runID, sessionID, payload, extraAttrs)
}

func NewPlanTaskCompletedEvent(runID, sessionID string, payload PlanTaskAttrs, extraAttrs map[string]any) Event {
	return NewPlanTaskEvent(EventPlanTaskCompleted, runID, sessionID, payload, extraAttrs)
}

func NewPlanTaskFailedEvent(runID, sessionID string, payload PlanTaskAttrs, extraAttrs map[string]any) Event {
	return NewPlanTaskEvent(EventPlanTaskFailed, runID, sessionID, payload, extraAttrs)
}

func NewPlanTaskCancelledEvent(runID, sessionID string, payload PlanTaskAttrs, extraAttrs map[string]any) Event {
	return NewPlanTaskEvent(EventPlanTaskCancelled, runID, sessionID, payload, extraAttrs)
}

func NewPlanTaskSkippedEvent(runID, sessionID string, payload PlanTaskAttrs, extraAttrs map[string]any) Event {
	return NewPlanTaskEvent(EventPlanTaskSkipped, runID, sessionID, payload, extraAttrs)
}

func NewPlanSnapshotUpdatedEvent(runID, sessionID string, payload PlanSnapshotAttrs, extraAttrs map[string]any) Event {
	return newEventWithAttrs(EventPlanSnapshotUpdated, runID, sessionID, mergePayloadAttrs(extraAttrs, payload.ToMap()))
}

func NewModelRoutedEvent(runID, sessionID string, payload ModelRoutedAttrs, extraAttrs map[string]any) Event {
	return newEventWithAttrs(EventModelRouted, runID, sessionID, mergePayloadAttrs(extraAttrs, payload.ToMap()))
}

func NewModelTextDeltaEvent(runID, sessionID string, payload DeltaAttrs, extraAttrs map[string]any) Event {
	return newEventWithAttrs(EventModelTextDelta, runID, sessionID, mergePayloadAttrs(extraAttrs, payload.ToMap()))
}

func NewModelReasoningDeltaEvent(runID, sessionID string, payload DeltaAttrs, extraAttrs map[string]any) Event {
	return newEventWithAttrs(EventModelReasoningDelta, runID, sessionID, mergePayloadAttrs(extraAttrs, payload.ToMap()))
}

func NewModelStreamCompleteEvent(runID, sessionID string, payload ModelStreamCompleteAttrs, extraAttrs map[string]any) Event {
	return newEventWithAttrs(EventModelStreamComplete, runID, sessionID, mergePayloadAttrs(extraAttrs, payload.ToMap()))
}

func NewModelRetryEvent(runID, sessionID string, payload ModelRetryAttrs, extraAttrs map[string]any) Event {
	return newEventWithAttrs(EventModelRetry, runID, sessionID, mergePayloadAttrs(extraAttrs, payload.ToMap()))
}

func NewModelFailoverEvent(runID, sessionID string, payload ModelFailoverAttrs, extraAttrs map[string]any) Event {
	return newEventWithAttrs(EventModelFailover, runID, sessionID, mergePayloadAttrs(extraAttrs, payload.ToMap()))
}

func NewThinkingDegradedEvent(runID, sessionID string, payload ThinkingDegradedAttrs, extraAttrs map[string]any) Event {
	return newEventWithAttrs(EventThinkingDegraded, runID, sessionID, mergePayloadAttrs(extraAttrs, payload.ToMap()))
}

func NewApprovalRequestedEvent(runID, sessionID string, payload ApprovalEventAttrs, extraAttrs map[string]any) Event {
	return newEventWithAttrs(EventApprovalRequested, runID, sessionID, mergePayloadAttrs(extraAttrs, payload.ToMap()))
}

func NewApprovalResolvedEvent(runID, sessionID string, payload ApprovalEventAttrs, extraAttrs map[string]any) Event {
	return newEventWithAttrs(EventApprovalResolved, runID, sessionID, mergePayloadAttrs(extraAttrs, payload.ToMap()))
}

func NewApprovalTimedOutEvent(runID, sessionID string, payload ApprovalEventAttrs, extraAttrs map[string]any) Event {
	return newEventWithAttrs(EventApprovalTimedOut, runID, sessionID, mergePayloadAttrs(extraAttrs, payload.ToMap()))
}

func NewApprovalGraceWarningEvent(runID, sessionID string, payload ApprovalEventAttrs, extraAttrs map[string]any) Event {
	return newEventWithAttrs(EventApprovalGraceWarning, runID, sessionID, mergePayloadAttrs(extraAttrs, payload.ToMap()))
}

func NewSecurityRiskDetectedEvent(runID, sessionID string, payload SecurityRiskDetectedAttrs, extraAttrs map[string]any) Event {
	return newEventWithAttrs(EventSecurityRiskDetected, runID, sessionID, mergePayloadAttrs(extraAttrs, payload.ToMap()))
}

func NewSecurityPathViolationEvent(runID, sessionID string, payload SecurityFindingAttrs, extraAttrs map[string]any) Event {
	return newEventWithAttrs(EventSecurityPathViolation, runID, sessionID, mergePayloadAttrs(extraAttrs, payload.ToMap()))
}

func NewSecurityInjectionAttemptEvent(runID, sessionID string, payload SecurityFindingAttrs, extraAttrs map[string]any) Event {
	return newEventWithAttrs(EventSecurityInjectionAttempt, runID, sessionID, mergePayloadAttrs(extraAttrs, payload.ToMap()))
}

func NewGovernanceDeliveryQueuedEvent(runID, sessionID string, payload GovernanceDeliveryAttrs, extraAttrs map[string]any) Event {
	return newEventWithAttrs(EventGovernanceDeliveryQueued, runID, sessionID, mergePayloadAttrs(extraAttrs, payload.ToMap()))
}

func NewGovernanceDeliveryRedrivenEvent(runID, sessionID string, payload GovernanceDeliveryAttrs, extraAttrs map[string]any) Event {
	return newEventWithAttrs(EventGovernanceDeliveryRedriven, runID, sessionID, mergePayloadAttrs(extraAttrs, payload.ToMap()))
}

func NewGovernanceDeliveryRetryScheduledEvent(runID, sessionID string, payload GovernanceDeliveryAttrs, extraAttrs map[string]any) Event {
	return newEventWithAttrs(EventGovernanceDeliveryRetryScheduled, runID, sessionID, mergePayloadAttrs(extraAttrs, payload.ToMap()))
}

func NewGovernanceDeliveryDeliveredEvent(runID, sessionID string, payload GovernanceDeliveryAttrs, extraAttrs map[string]any) Event {
	return newEventWithAttrs(EventGovernanceDeliveryDelivered, runID, sessionID, mergePayloadAttrs(extraAttrs, payload.ToMap()))
}

func NewGovernanceDeliveryDeadLetteredEvent(runID, sessionID string, payload GovernanceDeliveryAttrs, extraAttrs map[string]any) Event {
	return newEventWithAttrs(EventGovernanceDeliveryDeadLettered, runID, sessionID, mergePayloadAttrs(extraAttrs, payload.ToMap()))
}

func NewArtifactPrunedEvent(runID, sessionID string, payload ArtifactPrunedAttrs, extraAttrs map[string]any) Event {
	return newEventWithAttrs(EventArtifactPruned, runID, sessionID, mergePayloadAttrs(extraAttrs, payload.ToMap()))
}

type WorkflowEventAttrs struct {
	OriginalRunID     string `json:"original_run_id,omitempty"`
	ContinuationIndex int    `json:"continuation_index"`
	TotalRoundsUsed   int    `json:"total_rounds_used"`
	CompletedTasks    int    `json:"completed_tasks"`
	TotalTasks        int    `json:"total_tasks"`
	YieldReason       string `json:"yield_reason,omitempty"`
	Summary           string `json:"summary,omitempty"`
}

func (a WorkflowEventAttrs) ToMap() map[string]any {
	out := map[string]any{
		"continuation_index": a.ContinuationIndex,
		"total_rounds_used":  a.TotalRoundsUsed,
		"completed_tasks":    a.CompletedTasks,
		"total_tasks":        a.TotalTasks,
	}
	if a.OriginalRunID != "" {
		out["original_run_id"] = a.OriginalRunID
	}
	if a.YieldReason != "" {
		out["yield_reason"] = a.YieldReason
	}
	if a.Summary != "" {
		out["summary"] = a.Summary
	}
	return out
}

func NewWorkflowYieldedEvent(runID, sessionID string, payload WorkflowEventAttrs, extraAttrs map[string]any) Event {
	return newEventWithAttrs(EventWorkflowYielded, runID, sessionID, mergePayloadAttrs(extraAttrs, payload.ToMap()))
}

func NewWorkflowContinuedEvent(runID, sessionID string, payload WorkflowEventAttrs, extraAttrs map[string]any) Event {
	return newEventWithAttrs(EventWorkflowContinued, runID, sessionID, mergePayloadAttrs(extraAttrs, payload.ToMap()))
}

func NewWorkflowCompletedEvent(runID, sessionID string, payload WorkflowEventAttrs, extraAttrs map[string]any) Event {
	return newEventWithAttrs(EventWorkflowCompleted, runID, sessionID, mergePayloadAttrs(extraAttrs, payload.ToMap()))
}

func NewWorkflowFailedEvent(runID, sessionID string, payload WorkflowEventAttrs, extraAttrs map[string]any) Event {
	return newEventWithAttrs(EventWorkflowFailed, runID, sessionID, mergePayloadAttrs(extraAttrs, payload.ToMap()))
}

func (e Event) RunStartedPayload() (RunDispatchAttrs, bool) {
	if e.Type != EventRunStarted {
		return RunDispatchAttrs{}, false
	}
	return e.RunDispatchPayload()
}

func (e Event) RunResumedPayload() (RunDispatchAttrs, bool) {
	if e.Type != EventRunResumed {
		return RunDispatchAttrs{}, false
	}
	return e.RunDispatchPayload()
}

func (e Event) RunCompletedPayload() (RunStatusAttrs, bool) {
	if e.Type != EventRunCompleted {
		return RunStatusAttrs{}, false
	}
	return e.RunStatusPayload()
}

func (e Event) RunFailedPayload() (RunStatusAttrs, bool) {
	if e.Type != EventRunFailed {
		return RunStatusAttrs{}, false
	}
	return e.RunStatusPayload()
}

func (e Event) RunCancelledPayload() (RunControlAttrs, bool) {
	if e.Type != EventRunCancelled {
		return RunControlAttrs{}, false
	}
	return e.RunControlPayload()
}

func (e Event) WorkflowPayload() (WorkflowEventAttrs, bool) {
	var payload WorkflowEventAttrs
	ok := false
	if originalRunID := strings.TrimSpace(normalize.String(e.Attrs["original_run_id"])); originalRunID != "" {
		payload.OriginalRunID = originalRunID
		ok = true
	}
	if continuationIndex, found := intValue(e.Attrs["continuation_index"]); found {
		payload.ContinuationIndex = continuationIndex
		ok = true
	}
	if totalRoundsUsed, found := intValue(e.Attrs["total_rounds_used"]); found {
		payload.TotalRoundsUsed = totalRoundsUsed
		ok = true
	}
	if completedTasks, found := intValue(e.Attrs["completed_tasks"]); found {
		payload.CompletedTasks = completedTasks
		ok = true
	}
	if totalTasks, found := intValue(e.Attrs["total_tasks"]); found {
		payload.TotalTasks = totalTasks
		ok = true
	}
	if yieldReason := strings.TrimSpace(normalize.String(e.Attrs["yield_reason"])); yieldReason != "" {
		payload.YieldReason = yieldReason
		ok = true
	}
	if summary := strings.TrimSpace(normalize.String(e.Attrs["summary"])); summary != "" {
		payload.Summary = summary
		ok = true
	}
	return payload, ok
}

func (e Event) WorkflowYieldedPayload() (WorkflowEventAttrs, bool) {
	if e.Type != EventWorkflowYielded {
		return WorkflowEventAttrs{}, false
	}
	return e.WorkflowPayload()
}

func (e Event) WorkflowContinuedPayload() (WorkflowEventAttrs, bool) {
	if e.Type != EventWorkflowContinued {
		return WorkflowEventAttrs{}, false
	}
	return e.WorkflowPayload()
}

func (e Event) WorkflowCompletedPayload() (WorkflowEventAttrs, bool) {
	if e.Type != EventWorkflowCompleted {
		return WorkflowEventAttrs{}, false
	}
	return e.WorkflowPayload()
}

func (e Event) WorkflowFailedPayload() (WorkflowEventAttrs, bool) {
	if e.Type != EventWorkflowFailed {
		return WorkflowEventAttrs{}, false
	}
	return e.WorkflowPayload()
}

func (e Event) RunPreflightPayload() (RunPreflightAttrs, bool) {
	var payload RunPreflightAttrs
	ok := false
	if state := strings.TrimSpace(normalize.String(e.Attrs["state"])); state != "" {
		payload.State = state
		ok = true
	}
	if summary := strings.TrimSpace(normalize.String(e.Attrs["summary"])); summary != "" {
		payload.Summary = summary
		ok = true
	}
	if prompt := strings.TrimSpace(normalize.String(e.Attrs["prompt"])); prompt != "" {
		payload.Prompt = prompt
		ok = true
	}
	if question := strings.TrimSpace(normalize.String(e.Attrs["question"])); question != "" {
		payload.Question = question
		ok = true
	}
	if replyTemplate := strings.TrimSpace(normalize.String(e.Attrs["reply_template"])); replyTemplate != "" {
		payload.ReplyTemplate = replyTemplate
		ok = true
	}
	if continueHint := strings.TrimSpace(normalize.String(e.Attrs["continue_hint"])); continueHint != "" {
		payload.ContinueHint = continueHint
		ok = true
	}
	if blocking, found := boolValue(e.Attrs["blocking"]); found {
		payload.Blocking = blocking
		ok = true
	}
	if generatedAt, found := timeValue(e.Attrs["generated"]); found {
		payload.GeneratedAt = generatedAt
		ok = true
	}
	if replyHints := stringSliceValue(e.Attrs["reply_hints"]); len(replyHints) > 0 {
		payload.ReplyHints = replyHints
		ok = true
	}
	if suggestedDomains := stringSliceValue(e.Attrs["suggested_domains"]); len(suggestedDomains) > 0 {
		payload.SuggestedDomains = suggestedDomains
		ok = true
	}
	if detectedDomains := stringSliceValue(e.Attrs["detected_domains"]); len(detectedDomains) > 0 {
		payload.DetectedDomains = detectedDomains
		ok = true
	}
	if rawSlots := mapSliceValue(e.Attrs["clarification_slots"]); len(rawSlots) > 0 {
		payload.ClarificationSlots = make([]RunPreflightClarificationSlotAttrs, 0, len(rawSlots))
		for _, item := range rawSlots {
			slot := RunPreflightClarificationSlotAttrs{
				ID:          strings.TrimSpace(normalize.String(item["id"])),
				Label:       strings.TrimSpace(normalize.String(item["label"])),
				Question:    strings.TrimSpace(normalize.String(item["question"])),
				InputMode:   strings.TrimSpace(normalize.String(item["input_mode"])),
				Placeholder: strings.TrimSpace(normalize.String(item["placeholder"])),
				Hints:       stringSliceValue(item["hints"]),
			}
			if required, found := boolValue(item["required"]); found {
				slot.Required = required
			}
			payload.ClarificationSlots = append(payload.ClarificationSlots, slot)
		}
		ok = true
	}
	if rawChecks := mapSliceValue(e.Attrs["checks"]); len(rawChecks) > 0 {
		payload.Checks = make([]RunPreflightCheckAttrs, 0, len(rawChecks))
		for _, item := range rawChecks {
			check := RunPreflightCheckAttrs{
				ID:     strings.TrimSpace(normalize.String(item["id"])),
				Title:  strings.TrimSpace(normalize.String(item["title"])),
				State:  strings.TrimSpace(normalize.String(item["state"])),
				Detail: strings.TrimSpace(normalize.String(item["detail"])),
			}
			if blocking, found := boolValue(item["blocking"]); found {
				check.Blocking = blocking
			}
			payload.Checks = append(payload.Checks, check)
		}
		ok = true
	}
	return payload, ok
}

func (e Event) RunPreflightUpdatedPayload() (RunPreflightAttrs, bool) {
	if e.Type != EventRunPreflightUpdated {
		return RunPreflightAttrs{}, false
	}
	return e.RunPreflightPayload()
}

func (e Event) RunWaitingInputPayload() (RunPreflightAttrs, bool) {
	if e.Type != EventRunWaitingInput {
		return RunPreflightAttrs{}, false
	}
	return e.RunPreflightPayload()
}

func (e Event) RunWaitingApprovalPayload() (ApprovalEventAttrs, bool) {
	if e.Type != EventRunWaitingApproval {
		return ApprovalEventAttrs{}, false
	}
	return e.ApprovalPayload()
}

func (e Event) RunTimeoutPayload() (RunTimeoutAttrs, bool) {
	if e.Type != EventRunTimeout {
		return RunTimeoutAttrs{}, false
	}
	var payload RunTimeoutAttrs
	ok := false
	if channel := strings.TrimSpace(normalize.String(e.Attrs["channel"])); channel != "" {
		payload.Channel = channel
		ok = true
	}
	if reason := strings.TrimSpace(normalize.String(e.Attrs["reason"])); reason != "" {
		payload.Reason = reason
		ok = true
	}
	if errText := strings.TrimSpace(normalize.String(e.Attrs["error"])); errText != "" {
		payload.Error = errText
		ok = true
	}
	if maxRunDuration := strings.TrimSpace(normalize.String(e.Attrs["max_run_duration"])); maxRunDuration != "" {
		payload.MaxRunDuration = maxRunDuration
		ok = true
	}
	return payload, ok
}

func (e Event) RunPlannedPayload() (RunPlannedAttrs, bool) {
	if e.Type != EventRunPlanned {
		return RunPlannedAttrs{}, false
	}
	var payload RunPlannedAttrs
	ok := false
	if taskCount, found := intValue(e.Attrs["task_count"]); found {
		payload.TaskCount = taskCount
		ok = true
	}
	if strategy := strings.TrimSpace(normalize.String(e.Attrs["strategy"])); strategy != "" {
		payload.Strategy = strategy
		ok = true
	}
	if replanned, found := boolValue(e.Attrs["replanned"]); found {
		payload.Replanned = replanned
		ok = true
	}
	if warnings := stringSliceValue(e.Attrs["plan_coverage_warnings"]); len(warnings) > 0 {
		payload.CoverageWarnings = warnings
		ok = true
	}
	return payload, ok
}

func (e Event) RunSteeredPayload() (RunSteeredAttrs, bool) {
	if e.Type != EventRunSteered {
		return RunSteeredAttrs{}, false
	}
	var payload RunSteeredAttrs
	ok := false
	if count, found := intValue(e.Attrs["count"]); found {
		payload.Count = count
		ok = true
	}
	if autoRecovery, found := boolValue(e.Attrs["auto_recovery"]); found {
		payload.AutoRecovery = autoRecovery
		ok = true
	}
	return payload, ok
}

func (e Event) PlanTaskStartedPayload() (PlanTaskAttrs, bool) {
	if e.Type != EventPlanTaskStarted {
		return PlanTaskAttrs{}, false
	}
	return e.PlanTaskPayload()
}

func (e Event) PlanTaskCompletedPayload() (PlanTaskAttrs, bool) {
	if e.Type != EventPlanTaskCompleted {
		return PlanTaskAttrs{}, false
	}
	return e.PlanTaskPayload()
}

func (e Event) PlanTaskFailedPayload() (PlanTaskAttrs, bool) {
	if e.Type != EventPlanTaskFailed {
		return PlanTaskAttrs{}, false
	}
	return e.PlanTaskPayload()
}

func (e Event) PlanTaskCancelledPayload() (PlanTaskAttrs, bool) {
	if e.Type != EventPlanTaskCancelled {
		return PlanTaskAttrs{}, false
	}
	return e.PlanTaskPayload()
}

func (e Event) PlanTaskSkippedPayload() (PlanTaskAttrs, bool) {
	if e.Type != EventPlanTaskSkipped {
		return PlanTaskAttrs{}, false
	}
	return e.PlanTaskPayload()
}

func (e Event) PlanSnapshotPayload() (PlanSnapshotAttrs, bool) {
	var payload PlanSnapshotAttrs
	ok := false
	if completed, found := intValue(e.Attrs["completed_count"]); found {
		payload.CompletedCount = completed
		ok = true
	}
	if failed, found := intValue(e.Attrs["failed_count"]); found {
		payload.FailedCount = failed
		ok = true
	}
	if skipped, found := intValue(e.Attrs["skipped_count"]); found {
		payload.SkippedCount = skipped
		ok = true
	}
	if total, found := intValue(e.Attrs["total_tasks"]); found {
		payload.TotalTasks = total
		ok = true
	}
	if finalTask := strings.TrimSpace(normalize.String(e.Attrs["final_task"])); finalTask != "" {
		payload.FinalTask = finalTask
		ok = true
	}
	if running := stringSliceValue(e.Attrs["running_task_ids"]); len(running) > 0 {
		payload.RunningTaskIDs = running
		ok = true
	}
	if warnings := stringSliceValue(e.Attrs["plan_coverage_warnings"]); len(warnings) > 0 {
		payload.CoverageWarnings = warnings
		ok = true
	}
	return payload, ok
}

func (e Event) PlanSnapshotUpdatedPayload() (PlanSnapshotAttrs, bool) {
	if e.Type != EventPlanSnapshotUpdated {
		return PlanSnapshotAttrs{}, false
	}
	return e.PlanSnapshotPayload()
}

func (e Event) DeltaPayload() (DeltaAttrs, bool) {
	var payload DeltaAttrs
	raw, found := e.Attrs["delta"]
	if !found || raw == nil {
		return payload, false
	}
	delta := ""
	switch typed := raw.(type) {
	case string:
		delta = typed
	default:
		delta = fmt.Sprint(typed)
	}
	if delta == "" {
		return payload, false
	}
	payload.Delta = delta
	return payload, true
}

func (e Event) ModelRoutedPayload() (ModelRoutedAttrs, bool) {
	if e.Type != EventModelRouted {
		return ModelRoutedAttrs{}, false
	}
	var payload ModelRoutedAttrs
	ok := false
	if requested := strings.TrimSpace(normalize.String(e.Attrs["requested_model"])); requested != "" {
		payload.RequestedModel = requested
		ok = true
	}
	if selected := strings.TrimSpace(normalize.String(e.Attrs["selected_model"])); selected != "" {
		payload.SelectedModel = selected
		ok = true
	}
	if failoverFrom := strings.TrimSpace(normalize.String(e.Attrs["failover_from"])); failoverFrom != "" {
		payload.FailoverFrom = failoverFrom
		ok = true
	}
	if reason := strings.TrimSpace(normalize.String(e.Attrs["reason"])); reason != "" {
		payload.Reason = reason
		ok = true
	}
	return payload, ok
}

func (e Event) ModelTextDeltaPayload() (DeltaAttrs, bool) {
	if e.Type != EventModelTextDelta {
		return DeltaAttrs{}, false
	}
	return e.DeltaPayload()
}

func (e Event) ModelReasoningDeltaPayload() (DeltaAttrs, bool) {
	if e.Type != EventModelReasoningDelta {
		return DeltaAttrs{}, false
	}
	return e.DeltaPayload()
}

func (e Event) ModelStreamCompletePayload() (ModelStreamCompleteAttrs, bool) {
	if e.Type != EventModelStreamComplete {
		return ModelStreamCompleteAttrs{}, false
	}
	var payload ModelStreamCompleteAttrs
	if prompt, found := intValue(e.Attrs["prompt_tokens"]); found {
		payload.PromptTokens = prompt
	}
	if completion, found := intValue(e.Attrs["completion_tokens"]); found {
		payload.CompletionTokens = completion
	}
	if total, found := intValue(e.Attrs["total_tokens"]); found {
		payload.TotalTokens = total
	}
	return payload, true
}

func (e Event) ModelRetryPayload() (ModelRetryAttrs, bool) {
	if e.Type != EventModelRetry {
		return ModelRetryAttrs{}, false
	}
	var payload ModelRetryAttrs
	ok := false
	if model := strings.TrimSpace(normalize.String(e.Attrs["model"])); model != "" {
		payload.Model = model
		ok = true
	}
	if attempt, found := intValue(e.Attrs["attempt"]); found {
		payload.Attempt = attempt
		ok = true
	}
	if maxAttempts, found := intValue(e.Attrs["max_attempts"]); found {
		payload.MaxAttempts = maxAttempts
		ok = true
	}
	if failureReason := strings.TrimSpace(normalize.String(e.Attrs["failure_reason"])); failureReason != "" {
		payload.FailureReason = failureReason
		ok = true
	}
	if delayMs, found := int64Value(e.Attrs["delay_ms"]); found {
		payload.DelayMs = delayMs
		ok = true
	}
	if errText := strings.TrimSpace(normalize.String(e.Attrs["error"])); errText != "" {
		payload.Error = errText
		ok = true
	}
	return payload, ok
}

func (e Event) ModelFailoverPayload() (ModelFailoverAttrs, bool) {
	if e.Type != EventModelFailover {
		return ModelFailoverAttrs{}, false
	}
	var payload ModelFailoverAttrs
	ok := false
	if fromModel := strings.TrimSpace(normalize.String(e.Attrs["from_model"])); fromModel != "" {
		payload.FromModel = fromModel
		ok = true
	}
	if toModel := strings.TrimSpace(normalize.String(e.Attrs["to_model"])); toModel != "" {
		payload.ToModel = toModel
		ok = true
	}
	if reason := strings.TrimSpace(normalize.String(e.Attrs["reason"])); reason != "" {
		payload.Reason = reason
		ok = true
	}
	return payload, ok
}

func (e Event) ThinkingDegradedPayload() (ThinkingDegradedAttrs, bool) {
	if e.Type != EventThinkingDegraded {
		return ThinkingDegradedAttrs{}, false
	}
	var payload ThinkingDegradedAttrs
	ok := false
	if model := strings.TrimSpace(normalize.String(e.Attrs["model"])); model != "" {
		payload.Model = model
		ok = true
	}
	if from := strings.TrimSpace(normalize.String(e.Attrs["from"])); from != "" {
		payload.From = from
		ok = true
	}
	if to := strings.TrimSpace(normalize.String(e.Attrs["to"])); to != "" {
		payload.To = to
		ok = true
	}
	if reason := strings.TrimSpace(normalize.String(e.Attrs["reason"])); reason != "" {
		payload.Reason = reason
		ok = true
	}
	if errText := strings.TrimSpace(normalize.String(e.Attrs["error"])); errText != "" {
		payload.Error = errText
		ok = true
	}
	if attempt, found := intValue(e.Attrs["attempt"]); found {
		payload.Attempt = attempt
		ok = true
	}
	return payload, ok
}

func (e Event) ApprovalPayload() (ApprovalEventAttrs, bool) {
	var payload ApprovalEventAttrs
	ok := false
	if approvalID := strings.TrimSpace(normalize.String(e.Attrs["approval_id"])); approvalID != "" {
		payload.ApprovalID = approvalID
		ok = true
	}
	if approvalKind := strings.TrimSpace(normalize.String(e.Attrs["approval_kind"])); approvalKind != "" {
		payload.ApprovalKind = approvalKind
		ok = true
	}
	if status := strings.TrimSpace(normalize.String(e.Attrs["status"])); status != "" {
		payload.Status = status
		ok = true
	}
	if resolvedBy := strings.TrimSpace(normalize.String(e.Attrs["resolved_by"])); resolvedBy != "" {
		payload.ResolvedBy = resolvedBy
		ok = true
	}
	if remainingMs, found := int64Value(e.Attrs["remaining_ms"]); found {
		payload.RemainingMs = remainingMs
		ok = true
	}
	if toolCount, found := intValue(e.Attrs["tool_count"]); found {
		payload.ToolCount = toolCount
		ok = true
	}
	if reasons := stringSliceValue(e.Attrs["reasons"]); len(reasons) > 0 {
		payload.Reasons = reasons
		ok = true
	}
	if policySource := strings.TrimSpace(normalize.String(e.Attrs["policy_source"])); policySource != "" {
		payload.PolicySource = policySource
		ok = true
	}
	if policySummary := strings.TrimSpace(normalize.String(e.Attrs["policy_summary"])); policySummary != "" {
		payload.PolicySummary = policySummary
		ok = true
	}
	if refs := ApprovalExternalRefAttrsFromTicketRefs(ApprovalExternalReferencesFromValue(e.Attrs["approval_external_refs"])); len(refs) > 0 {
		payload.ExternalRefs = refs
		ok = true
	}
	return payload, ok
}

func (e Event) ApprovalRequestedPayload() (ApprovalEventAttrs, bool) {
	if e.Type != EventApprovalRequested {
		return ApprovalEventAttrs{}, false
	}
	return e.ApprovalPayload()
}

func (e Event) ApprovalResolvedPayload() (ApprovalEventAttrs, bool) {
	if e.Type != EventApprovalResolved {
		return ApprovalEventAttrs{}, false
	}
	return e.ApprovalPayload()
}

func (e Event) ApprovalTimedOutPayload() (ApprovalEventAttrs, bool) {
	if e.Type != EventApprovalTimedOut {
		return ApprovalEventAttrs{}, false
	}
	return e.ApprovalPayload()
}

func (e Event) ApprovalGraceWarningPayload() (ApprovalEventAttrs, bool) {
	if e.Type != EventApprovalGraceWarning {
		return ApprovalEventAttrs{}, false
	}
	return e.ApprovalPayload()
}

func (e Event) SecurityRiskDetectedPayload() (SecurityRiskDetectedAttrs, bool) {
	if e.Type != EventSecurityRiskDetected {
		return SecurityRiskDetectedAttrs{}, false
	}
	var payload SecurityRiskDetectedAttrs
	ok := false
	if toolName := strings.TrimSpace(normalize.String(e.Attrs["tool_name"])); toolName != "" {
		payload.ToolName = toolName
		ok = true
	}
	if riskCount, found := intValue(e.Attrs["risk_count"]); found {
		payload.RiskCount = riskCount
		ok = true
	}
	if severity := strings.TrimSpace(normalize.String(e.Attrs["severity"])); severity != "" {
		payload.Severity = severity
		ok = true
	}
	if risks := securityRiskItemsFromValue(e.Attrs["risks"]); len(risks) > 0 {
		payload.Risks = risks
		ok = true
	}
	return payload, ok
}

func (e Event) SecurityPathViolationPayload() (SecurityFindingAttrs, bool) {
	if e.Type != EventSecurityPathViolation {
		return SecurityFindingAttrs{}, false
	}
	return e.SecurityFindingPayload()
}

func (e Event) SecurityInjectionAttemptPayload() (SecurityFindingAttrs, bool) {
	if e.Type != EventSecurityInjectionAttempt {
		return SecurityFindingAttrs{}, false
	}
	return e.SecurityFindingPayload()
}

func (e Event) SecurityFindingPayload() (SecurityFindingAttrs, bool) {
	var payload SecurityFindingAttrs
	ok := false
	if toolName := strings.TrimSpace(normalize.String(e.Attrs["tool_name"])); toolName != "" {
		payload.ToolName = toolName
		ok = true
	}
	if findingType := strings.TrimSpace(normalize.String(e.Attrs["type"])); findingType != "" {
		payload.Type = findingType
		ok = true
	}
	if detail := strings.TrimSpace(normalize.String(e.Attrs["detail"])); detail != "" {
		payload.Detail = detail
		ok = true
	}
	if severity := strings.TrimSpace(normalize.String(e.Attrs["severity"])); severity != "" {
		payload.Severity = severity
		ok = true
	}
	return payload, ok
}

func (e Event) GovernanceDeliveryPayload() (GovernanceDeliveryAttrs, bool) {
	var payload GovernanceDeliveryAttrs
	ok := false
	if deliveryID := strings.TrimSpace(normalize.String(e.Attrs["delivery_id"])); deliveryID != "" {
		payload.DeliveryID = deliveryID
		ok = true
	}
	if adapterName := strings.TrimSpace(normalize.String(e.Attrs["adapter_name"])); adapterName != "" {
		payload.AdapterName = adapterName
		ok = true
	}
	if idempotencyKey := strings.TrimSpace(normalize.String(e.Attrs["idempotency_key"])); idempotencyKey != "" {
		payload.IdempotencyKey = idempotencyKey
		ok = true
	}
	if deliveryStatus := strings.TrimSpace(normalize.String(e.Attrs["delivery_status"])); deliveryStatus != "" {
		payload.DeliveryStatus = deliveryStatus
		ok = true
	}
	if deliveryAttempts, found := intValue(e.Attrs["delivery_attempts"]); found {
		payload.DeliveryAttempts = deliveryAttempts
		ok = true
	}
	if deliveryMaxAttempts, found := intValue(e.Attrs["delivery_max_attempts"]); found {
		payload.DeliveryMaxAttempts = deliveryMaxAttempts
		ok = true
	}
	if governanceKind := strings.TrimSpace(normalize.String(e.Attrs["governance_kind"])); governanceKind != "" {
		payload.GovernanceKind = governanceKind
		ok = true
	}
	if sourceEventID := strings.TrimSpace(normalize.String(e.Attrs["source_event_id"])); sourceEventID != "" {
		payload.SourceEventID = sourceEventID
		ok = true
	}
	if sourceEventType := strings.TrimSpace(normalize.String(e.Attrs["source_event_type"])); sourceEventType != "" {
		payload.SourceEventType = sourceEventType
		ok = true
	}
	if nextAttemptAt, found := timeValue(e.Attrs["next_attempt_at"]); found {
		payload.NextAttemptAt = nextAttemptAt
		ok = true
	}
	if deliveredAt, found := timeValue(e.Attrs["delivered_at"]); found {
		payload.DeliveredAt = deliveredAt
		ok = true
	}
	if errText := strings.TrimSpace(normalize.String(e.Attrs["error"])); errText != "" {
		payload.Error = errText
		ok = true
	}
	return payload, ok
}

func (e Event) GovernanceDeliveryQueuedPayload() (GovernanceDeliveryAttrs, bool) {
	if e.Type != EventGovernanceDeliveryQueued {
		return GovernanceDeliveryAttrs{}, false
	}
	return e.GovernanceDeliveryPayload()
}

func (e Event) GovernanceDeliveryRedrivenPayload() (GovernanceDeliveryAttrs, bool) {
	if e.Type != EventGovernanceDeliveryRedriven {
		return GovernanceDeliveryAttrs{}, false
	}
	return e.GovernanceDeliveryPayload()
}

func (e Event) GovernanceDeliveryRetryScheduledPayload() (GovernanceDeliveryAttrs, bool) {
	if e.Type != EventGovernanceDeliveryRetryScheduled {
		return GovernanceDeliveryAttrs{}, false
	}
	return e.GovernanceDeliveryPayload()
}

func (e Event) GovernanceDeliveryDeliveredPayload() (GovernanceDeliveryAttrs, bool) {
	if e.Type != EventGovernanceDeliveryDelivered {
		return GovernanceDeliveryAttrs{}, false
	}
	return e.GovernanceDeliveryPayload()
}

func (e Event) GovernanceDeliveryDeadLetteredPayload() (GovernanceDeliveryAttrs, bool) {
	if e.Type != EventGovernanceDeliveryDeadLettered {
		return GovernanceDeliveryAttrs{}, false
	}
	return e.GovernanceDeliveryPayload()
}

func (e Event) ArtifactPrunedPayload() (ArtifactPrunedAttrs, bool) {
	if e.Type != EventArtifactPruned {
		return ArtifactPrunedAttrs{}, false
	}
	var payload ArtifactPrunedAttrs
	ok := false
	if deletedCount, found := intValue(e.Attrs["deleted_count"]); found {
		payload.DeletedCount = deletedCount
		ok = true
	}
	if deletedIDs := stringSliceValue(e.Attrs["deleted_ids"]); len(deletedIDs) > 0 {
		payload.DeletedIDs = deletedIDs
		ok = true
	}
	if cutoff, found := timeValue(e.Attrs["cutoff"]); found {
		payload.Cutoff = cutoff
		ok = true
	}
	if kind := strings.TrimSpace(normalize.String(e.Attrs["kind"])); kind != "" {
		payload.Kind = kind
		ok = true
	}
	if runID := strings.TrimSpace(normalize.String(e.Attrs["run_id"])); runID != "" {
		payload.RunID = runID
		ok = true
	}
	if sessionID := strings.TrimSpace(normalize.String(e.Attrs["session_id"])); sessionID != "" {
		payload.SessionID = sessionID
		ok = true
	}
	if toolName := strings.TrimSpace(normalize.String(e.Attrs["tool_name"])); toolName != "" {
		payload.ToolName = toolName
		ok = true
	}
	if toolCallID := strings.TrimSpace(normalize.String(e.Attrs["tool_call_id"])); toolCallID != "" {
		payload.ToolCallID = toolCallID
		ok = true
	}
	return payload, ok
}

func (e Event) GovernancePayload() (GovernanceAttrs, bool) {
	var payload GovernanceAttrs
	ok := false
	if scope := domainscope.FromValue(e.Attrs["scope"]); !scope.IsZero() {
		payload.Scope = scope
		ok = true
	}
	if id := strings.TrimSpace(normalize.String(e.Attrs["effective_config_snapshot_id"])); id != "" {
		payload.EffectiveConfigSnapshotID = id
		ok = true
	}
	if action := strings.TrimSpace(normalize.String(e.Attrs["policy_action"])); action != "" {
		payload.PolicyAction = action
		ok = true
	}
	if source := strings.TrimSpace(normalize.String(e.Attrs["policy_source"])); source != "" {
		payload.PolicySource = source
		ok = true
	}
	if summary := strings.TrimSpace(normalize.String(e.Attrs["policy_summary"])); summary != "" {
		payload.PolicySummary = summary
		ok = true
	}
	if reasons := stringSliceValue(e.Attrs["policy_reasons"]); len(reasons) > 0 {
		payload.PolicyReasons = reasons
		ok = true
	}
	if codes := stringSliceValue(e.Attrs["policy_reason_codes"]); len(codes) > 0 {
		payload.PolicyReasonCodes = codes
		ok = true
	}
	if layers := stringSliceValue(e.Attrs["policy_layers"]); len(layers) > 0 {
		payload.PolicyLayers = layers
		ok = true
	}
	if labels := stringSliceValue(e.Attrs["policy_audit_labels"]); len(labels) > 0 {
		payload.PolicyAuditLabels = labels
		ok = true
	}
	if approvalDefaultScope := strings.TrimSpace(normalize.String(e.Attrs["policy_approval_default_scope"])); approvalDefaultScope != "" {
		payload.PolicyApprovalDefaultScope = approvalDefaultScope
		ok = true
	}
	if approvalMaxScope := strings.TrimSpace(normalize.String(e.Attrs["policy_approval_max_scope"])); approvalMaxScope != "" {
		payload.PolicyApprovalMaxScope = approvalMaxScope
		ok = true
	}
	if toolNames := stringSliceValue(e.Attrs["policy_tool_names"]); len(toolNames) > 0 {
		payload.PolicyToolNames = toolNames
		ok = true
	}
	return payload, ok
}

func int64Value(value any) (int64, bool) {
	switch typed := value.(type) {
	case int:
		return int64(typed), true
	case int8:
		return int64(typed), true
	case int16:
		return int64(typed), true
	case int32:
		return int64(typed), true
	case int64:
		return typed, true
	case float32:
		return int64(typed), true
	case float64:
		return int64(typed), true
	case string:
		if parsed, err := time.ParseDuration(strings.TrimSpace(typed)); err == nil {
			return parsed.Milliseconds(), true
		}
		if parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(typed)); err == nil {
			return parsed.UnixMilli(), true
		}
		if parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(typed)); err == nil {
			return parsed.UnixMilli(), true
		}
		if parsed, err := time.ParseDuration(strings.TrimSpace(typed) + "ms"); err == nil {
			return parsed.Milliseconds(), true
		}
		return 0, false
	default:
		return 0, false
	}
}

func timeValue(value any) (time.Time, bool) {
	switch typed := value.(type) {
	case time.Time:
		if typed.IsZero() {
			return time.Time{}, false
		}
		return typed.UTC(), true
	case string:
		text := strings.TrimSpace(typed)
		if text == "" {
			return time.Time{}, false
		}
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
			if parsed, err := time.Parse(layout, text); err == nil {
				return parsed.UTC(), true
			}
		}
		return time.Time{}, false
	default:
		return time.Time{}, false
	}
}

func securityRiskItemsFromValue(value any) []SecurityRiskItemAttrs {
	items := mapSliceValue(value)
	if len(items) == 0 {
		return nil
	}
	out := make([]SecurityRiskItemAttrs, 0, len(items))
	for _, item := range items {
		risk := SecurityRiskItemAttrs{
			Category: strings.TrimSpace(normalize.String(item["category"])),
			Type:     strings.TrimSpace(normalize.String(item["type"])),
			Detail:   strings.TrimSpace(normalize.String(item["detail"])),
			Severity: strings.TrimSpace(normalize.String(item["severity"])),
		}
		if risk.Category == "" && risk.Type == "" && risk.Detail == "" && risk.Severity == "" {
			continue
		}
		out = append(out, risk)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
