package runtime

import (
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	capprofile "github.com/fulcrus/hopclaw/capability/profile"
	"github.com/fulcrus/hopclaw/resultmodel"
	verifyrt "github.com/fulcrus/hopclaw/runtime/verify"
)

type ResultBundle struct {
	RunID            string                      `json:"run_id"`
	SessionID        string                      `json:"session_id,omitempty"`
	Status           agent.RunStatus             `json:"status"`
	Outcome          RunOutcome                  `json:"outcome,omitempty"`
	Governance       *GovernanceReceipt          `json:"governance,omitempty"`
	Summary          string                      `json:"summary,omitempty"`
	FinalText        string                      `json:"final_text,omitempty"`
	Deliverables     []DeliverableRef            `json:"deliverables,omitempty"`
	Verification     *BundleVerification         `json:"verification,omitempty"`
	Delivery         *DeliveryPlan               `json:"delivery,omitempty"`
	Receipts         []DeliveryReceipt           `json:"receipts,omitempty"`
	SuggestedActions []BundleSuggestedAction     `json:"suggested_actions,omitempty"`
	ExecutionTraces  []capprofile.ExecutionTrace `json:"execution_traces,omitempty"`
	StructuredData   map[string]any              `json:"structured_data,omitempty"`
	UpdatedAt        string                      `json:"updated_at,omitempty"`
}

type BundleVerification struct {
	Status         string `json:"status,omitempty"`
	Summary        string `json:"summary,omitempty"`
	RequiredIssues int    `json:"required_issues,omitempty"`
	AdvisoryIssues int    `json:"advisory_issues,omitempty"`
}

type BundleSuggestedAction struct {
	Kind   string `json:"kind"`
	Label  string `json:"label"`
	Reason string `json:"reason,omitempty"`
}

func buildResultBundle(run *agent.Run, result *RunResult, verification *verifyrt.RunVerification) *ResultBundle {
	if result == nil {
		return nil
	}
	bundle := &ResultBundle{
		RunID:            result.RunID,
		SessionID:        result.SessionID,
		Status:           result.Status,
		Outcome:          result.Outcome,
		Governance:       cloneGovernanceReceipt(result.Governance),
		Summary:          strings.TrimSpace(result.Summary),
		FinalText:        strings.TrimSpace(result.Output),
		Deliverables:     cloneDeliverables(result.Deliverables),
		Delivery:         cloneDeliveryPlan(result.Delivery),
		Receipts:         cloneDeliveryReceipts(result.Receipts),
		SuggestedActions: buildSuggestedActions(run, result, verification),
		ExecutionTraces:  cloneExecutionTraces(result.ExecutionTraces),
		StructuredData:   buildBundleStructuredData(result),
		UpdatedAt:        bundleUpdatedAt(run),
	}
	if verification != nil {
		bundle.Verification = &BundleVerification{
			Status:         string(verification.Status),
			Summary:        strings.TrimSpace(verification.Summary),
			RequiredIssues: verification.RequiredWarnings + verification.RequiredFailures,
			AdvisoryIssues: verification.AdvisoryWarnings + verification.AdvisoryFailures,
		}
	}
	if len(bundle.StructuredData) == 0 {
		bundle.StructuredData = nil
	}
	return bundle
}

func cloneDeliverables(items []DeliverableRef) []DeliverableRef {
	if len(items) == 0 {
		return nil
	}
	out := make([]DeliverableRef, 0, len(items))
	for _, item := range items {
		cloned := item
		cloned.Metadata = cloneMetadata(item.Metadata)
		out = append(out, cloned)
	}
	return out
}

func cloneDeliveryPlan(in *DeliveryPlan) *DeliveryPlan {
	if in == nil {
		return nil
	}
	out := &DeliveryPlan{
		Summary: strings.TrimSpace(in.Summary),
	}
	if len(in.Blocks) > 0 {
		out.Blocks = append([]DeliveryBlock(nil), in.Blocks...)
	}
	if len(in.Attachments) > 0 {
		out.Attachments = append([]DeliveryAttachment(nil), in.Attachments...)
	}
	if len(in.NextActions) > 0 {
		out.NextActions = append([]resultmodel.ResultAction(nil), in.NextActions...)
	}
	if in.Verification != nil {
		clone := *in.Verification
		out.Verification = &clone
	}
	if in.Conversation != nil {
		clone := *in.Conversation
		out.Conversation = &clone
	}
	if len(in.Receipts) > 0 {
		out.Receipts = cloneDeliveryReceipts(in.Receipts)
	}
	out.Governance = cloneGovernanceReceipt(in.Governance)
	return out
}

func cloneExecutionTraces(items []capprofile.ExecutionTrace) []capprofile.ExecutionTrace {
	if len(items) == 0 {
		return nil
	}
	out := make([]capprofile.ExecutionTrace, 0, len(items))
	for _, item := range items {
		out = append(out, item.Normalized())
	}
	return out
}

func buildSuggestedActions(run *agent.Run, result *RunResult, verification *verifyrt.RunVerification) []BundleSuggestedAction {
	if result == nil {
		return nil
	}
	actions := make([]BundleSuggestedAction, 0, 2)
	appendAction := func(kind, label, reason string) {
		actions = append(actions, BundleSuggestedAction{
			Kind:   strings.TrimSpace(kind),
			Label:  strings.TrimSpace(label),
			Reason: strings.TrimSpace(reason),
		})
	}

	switch result.Outcome {
	case RunOutcomeNeedsConfirmation:
		if run != nil && strings.TrimSpace(run.ApprovalID) != "" {
			appendAction("review_approval", "review approval", "run is waiting for approval before continuing")
		} else {
			appendAction("provide_input", "provide input", "run is waiting for more information")
		}
	case RunOutcomePartial:
		appendAction("review_result", "review result", "run completed with warnings or partial output")
	case RunOutcomeFailed, RunOutcomeCancelled:
		appendAction("retry_run", "retry run", "run did not finish successfully")
	case RunOutcomeCompleted:
		if len(result.Deliverables) > 0 {
			appendAction("open_deliverables", "open deliverables", "artifacts are ready to inspect or share")
		}
	}
	if verification != nil && verification.RequiredFailures > 0 {
		appendAction("inspect_verification", "inspect verification", "required verification checks failed")
	}
	return actions
}

func buildBundleStructuredData(result *RunResult) map[string]any {
	if result == nil {
		return nil
	}
	data := map[string]any{
		"has_output":        strings.TrimSpace(result.Output) != "",
		"deliverable_count": len(result.Deliverables),
	}
	if result.Delivery != nil {
		data["has_delivery"] = true
		data["delivery_attachment_count"] = len(result.Delivery.Attachments)
		data["has_thread_context"] = result.Delivery.Conversation != nil
		data["has_governance"] = result.Delivery.Governance != nil
	}
	if len(result.TaskOutcomes) > 0 {
		data["task_outcome_count"] = len(result.TaskOutcomes)
		completed, failed := 0, 0
		for _, item := range result.TaskOutcomes {
			switch strings.TrimSpace(item.Status) {
			case "completed":
				completed++
			case "failed":
				failed++
			}
		}
		if completed > 0 {
			data["task_completed_count"] = completed
		}
		if failed > 0 {
			data["task_failed_count"] = failed
		}
	}
	if result.EventLedger != nil && len(result.EventLedger.Events) > 0 {
		data["event_ledger_count"] = len(result.EventLedger.Events)
		evidence, audit, delivery := 0, 0, 0
		for _, item := range result.EventLedger.Events {
			switch item.EventClass {
			case EventClassEvidence:
				evidence++
			case EventClassAudit:
				audit++
			case EventClassDelivery:
				delivery++
			}
		}
		if evidence > 0 {
			data["event_ledger_evidence_count"] = evidence
		}
		if audit > 0 {
			data["event_ledger_audit_count"] = audit
		}
		if delivery > 0 {
			data["event_ledger_delivery_count"] = delivery
		}
	}
	if len(result.ExecutionTraces) > 0 {
		data["execution_trace_count"] = len(result.ExecutionTraces)
		profileHits := 0
		fallbacks := 0
		visualFallbacks := 0
		for _, trace := range result.ExecutionTraces {
			normalizedTrace := trace.Normalized()
			if normalizedTrace.ProfileHit {
				profileHits++
			}
			if len(normalizedTrace.FallbackPath) > 0 {
				fallbacks++
			}
			if normalizedTrace.ExecutionMode == capprofile.ModeVisualFallback {
				visualFallbacks++
			}
		}
		if profileHits > 0 {
			data["execution_profile_hit_count"] = profileHits
		}
		if fallbacks > 0 {
			data["execution_fallback_count"] = fallbacks
		}
		if visualFallbacks > 0 {
			data["execution_visual_fallback_count"] = visualFallbacks
		}
	}
	if len(result.Receipts) > 0 {
		data["delivery_receipt_count"] = len(result.Receipts)
		pending, delivered, deadLetter := 0, 0, 0
		for _, item := range result.Receipts {
			switch strings.TrimSpace(item.Status) {
			case "pending":
				pending++
			case "delivered":
				delivered++
			case "dead_letter":
				deadLetter++
			}
		}
		if pending > 0 {
			data["delivery_pending_count"] = pending
		}
		if delivered > 0 {
			data["delivery_delivered_count"] = delivered
		}
		if deadLetter > 0 {
			data["delivery_dead_letter_count"] = deadLetter
		}
	}
	if strings.TrimSpace(result.VerificationStatus) != "" {
		data["verification_status"] = strings.TrimSpace(result.VerificationStatus)
	}
	if result.Governance != nil {
		if result.Governance.Policy != nil {
			data["policy_action"] = strings.TrimSpace(string(result.Governance.Policy.Action))
			data["policy_source"] = strings.TrimSpace(result.Governance.Policy.PolicySource)
		}
		if result.Governance.Approval != nil {
			data["approval_status"] = strings.TrimSpace(string(result.Governance.Approval.Status))
		}
		if strings.TrimSpace(result.Governance.EffectiveConfigSnapshotID) != "" {
			data["effective_config_snapshot_id"] = strings.TrimSpace(result.Governance.EffectiveConfigSnapshotID)
		}
	}
	return data
}

func bundleUpdatedAt(run *agent.Run) string {
	if run == nil {
		return ""
	}
	switch {
	case !run.FinishedAt.IsZero():
		return run.FinishedAt.UTC().Format(time.RFC3339)
	case !run.UpdatedAt.IsZero():
		return run.UpdatedAt.UTC().Format(time.RFC3339)
	case !run.StartedAt.IsZero():
		return run.StartedAt.UTC().Format(time.RFC3339)
	default:
		return ""
	}
}
