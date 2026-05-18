package eventbus

import (
	"reflect"
	"testing"
	"time"

	domainscope "github.com/fulcrus/hopclaw/internal/domain/scope"
)

func TestExtendedPayloadRoundTrips(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 2, 10, 20, 30, 0, time.UTC)
	cutoff := now.Add(-2 * time.Hour)
	nextAttempt := now.Add(15 * time.Minute)
	deliveredAt := now.Add(20 * time.Minute)

	cases := []struct {
		name  string
		event Event
		check func(*testing.T, Event)
	}{
		{
			name: "run_started",
			event: NewRunStartedEvent("run-1", "sess-1", RunDispatchAttrs{
				Channel:       "cli",
				Model:         "gpt-5.4",
				ExecutionMode: "direct",
				AgentProfile:  map[string]any{"name": "support"},
			}, map[string]any{"trace_id": "tr-1"}),
			check: func(t *testing.T, event Event) {
				payload, ok := event.RunStartedPayload()
				if !ok {
					t.Fatal("RunStartedPayload() ok = false")
				}
				if payload.Channel != "cli" || payload.Model != "gpt-5.4" || payload.ExecutionMode != "direct" {
					t.Fatalf("payload = %#v", payload)
				}
				if event.Attrs["trace_id"] != "tr-1" {
					t.Fatalf("trace_id = %#v", event.Attrs["trace_id"])
				}
			},
		},
		{
			name: "run_resumed",
			event: NewRunResumedEvent("run-r", "sess-r", RunDispatchAttrs{
				Channel:    "cli",
				ApprovalID: "appr-r",
			}, nil),
			check: func(t *testing.T, event Event) {
				payload, ok := event.RunResumedPayload()
				if !ok || payload.ApprovalID != "appr-r" {
					t.Fatalf("payload = %#v, ok=%v", payload, ok)
				}
			},
		},
		{
			name: "run_completed",
			event: NewRunCompletedEvent("run-2", "sess-2", RunStatusAttrs{
				Channel: "slack",
				Summary: "done",
			}, map[string]any{"policy_source": "policy.test"}),
			check: func(t *testing.T, event Event) {
				payload, ok := event.RunCompletedPayload()
				if !ok {
					t.Fatal("RunCompletedPayload() ok = false")
				}
				if payload.Channel != "slack" || payload.Summary != "done" {
					t.Fatalf("payload = %#v", payload)
				}
				if event.Attrs["policy_source"] != "policy.test" {
					t.Fatalf("policy_source = %#v", event.Attrs["policy_source"])
				}
			},
		},
		{
			name: "run_cancelled",
			event: NewRunCancelledEvent("run-3", "sess-3", RunControlAttrs{
				Channel:    "discord",
				ApprovalID: "appr-1",
				Status:     "cancelled",
				Reason:     "operator_cancel",
			}, nil),
			check: func(t *testing.T, event Event) {
				payload, ok := event.RunCancelledPayload()
				if !ok {
					t.Fatal("RunCancelledPayload() ok = false")
				}
				if payload.Channel != "discord" || payload.ApprovalID != "appr-1" || payload.Reason != "operator_cancel" {
					t.Fatalf("payload = %#v", payload)
				}
			},
		},
		{
			name: "run_preflight",
			event: NewRunPreflightUpdatedEvent("run-4", "sess-4", RunPreflightAttrs{
				State:            "needs_confirmation",
				Summary:          "Need a target",
				Question:         "What should I watch?",
				Blocking:         true,
				GeneratedAt:      now,
				ReplyHints:       []string{"URL", "session key"},
				SuggestedDomains: []string{"watch"},
				DetectedDomains:  []string{"ops"},
				ClarificationSlots: []RunPreflightClarificationSlotAttrs{{
					ID:       "target",
					Label:    "Target",
					Question: "What should I monitor?",
					Required: true,
					Hints:    []string{"url", "session"},
				}},
				Checks: []RunPreflightCheckAttrs{{
					ID:       "watch_target_required",
					Title:    "Need target",
					State:    "needs_confirmation",
					Detail:   "target missing",
					Blocking: true,
				}},
			}, nil),
			check: func(t *testing.T, event Event) {
				payload, ok := event.RunPreflightUpdatedPayload()
				if !ok {
					t.Fatal("RunPreflightUpdatedPayload() ok = false")
				}
				if payload.State != "needs_confirmation" || payload.Question != "What should I watch?" || !payload.Blocking {
					t.Fatalf("payload = %#v", payload)
				}
				if len(payload.ClarificationSlots) != 1 || len(payload.Checks) != 1 {
					t.Fatalf("payload = %#v", payload)
				}
				if !payload.GeneratedAt.Equal(now) {
					t.Fatalf("GeneratedAt = %v, want %v", payload.GeneratedAt, now)
				}
			},
		},
		{
			name: "run_waiting_input",
			event: NewRunWaitingInputEvent("run-5", "sess-5", RunPreflightAttrs{
				State:   "needs_confirmation",
				Summary: "Reply needed",
			}, nil),
			check: func(t *testing.T, event Event) {
				payload, ok := event.RunWaitingInputPayload()
				if !ok {
					t.Fatal("RunWaitingInputPayload() ok = false")
				}
				if payload.Summary != "Reply needed" {
					t.Fatalf("payload = %#v", payload)
				}
			},
		},
		{
			name: "run_waiting_approval",
			event: NewRunWaitingApprovalEvent("run-6", "sess-6", ApprovalEventAttrs{
				ApprovalID:   "appr-2",
				ApprovalKind: "tool_calls",
				Status:       "pending",
				Reasons:      []string{"sensitive tool"},
			}, nil),
			check: func(t *testing.T, event Event) {
				payload, ok := event.RunWaitingApprovalPayload()
				if !ok {
					t.Fatal("RunWaitingApprovalPayload() ok = false")
				}
				if payload.ApprovalID != "appr-2" || payload.Status != "pending" {
					t.Fatalf("payload = %#v", payload)
				}
			},
		},
		{
			name: "run_timeout",
			event: NewRunTimeoutEvent("run-7", "sess-7", RunTimeoutAttrs{
				Channel:        "telegram",
				Reason:         "run_timeout",
				Error:          "deadline exceeded",
				MaxRunDuration: "30s",
			}, map[string]any{"policy_source": "policy.timeout"}),
			check: func(t *testing.T, event Event) {
				payload, ok := event.RunTimeoutPayload()
				if !ok {
					t.Fatal("RunTimeoutPayload() ok = false")
				}
				if payload.Channel != "telegram" || payload.MaxRunDuration != "30s" {
					t.Fatalf("payload = %#v", payload)
				}
				if event.Attrs["policy_source"] != "policy.timeout" {
					t.Fatalf("policy_source = %#v", event.Attrs["policy_source"])
				}
			},
		},
		{
			name: "run_planned",
			event: NewRunPlannedEvent("run-8", "sess-8", RunPlannedAttrs{
				TaskCount:        2,
				Strategy:         "serial",
				Replanned:        true,
				CoverageWarnings: []string{"missing source"},
			}, nil),
			check: func(t *testing.T, event Event) {
				payload, ok := event.RunPlannedPayload()
				if !ok {
					t.Fatal("RunPlannedPayload() ok = false")
				}
				if payload.TaskCount != 2 || payload.Strategy != "serial" || !payload.Replanned {
					t.Fatalf("payload = %#v", payload)
				}
			},
		},
		{
			name: "run_steered",
			event: NewRunSteeredEvent("run-9", "sess-9", RunSteeredAttrs{
				Count:        2,
				AutoRecovery: true,
			}, nil),
			check: func(t *testing.T, event Event) {
				payload, ok := event.RunSteeredPayload()
				if !ok {
					t.Fatal("RunSteeredPayload() ok = false")
				}
				if payload.Count != 2 || !payload.AutoRecovery {
					t.Fatalf("payload = %#v", payload)
				}
			},
		},
		{
			name: "plan_task_started",
			event: NewPlanTaskStartedEvent("run-10", "sess-10", PlanTaskAttrs{
				TaskID:         "task-1",
				Title:          "Research",
				Kind:           "research",
				Goal:           "Find sources",
				CompletedCount: 1,
				TotalTasks:     3,
			}, nil),
			check: func(t *testing.T, event Event) {
				payload, ok := event.PlanTaskStartedPayload()
				if !ok {
					t.Fatal("PlanTaskStartedPayload() ok = false")
				}
				if payload.TaskID != "task-1" || payload.Title != "Research" {
					t.Fatalf("payload = %#v", payload)
				}
			},
		},
		{
			name: "plan_snapshot",
			event: NewPlanSnapshotUpdatedEvent("run-11", "sess-11", PlanSnapshotAttrs{
				CompletedCount:   1,
				FailedCount:      1,
				SkippedCount:     0,
				TotalTasks:       3,
				FinalTask:        "deliver",
				RunningTaskIDs:   []string{"task-2"},
				CoverageWarnings: []string{"missing source"},
			}, nil),
			check: func(t *testing.T, event Event) {
				payload, ok := event.PlanSnapshotUpdatedPayload()
				if !ok {
					t.Fatal("PlanSnapshotUpdatedPayload() ok = false")
				}
				if payload.CompletedCount != 1 || payload.FailedCount != 1 || payload.TotalTasks != 3 {
					t.Fatalf("payload = %#v", payload)
				}
			},
		},
		{
			name: "plan_task_completed",
			event: NewPlanTaskCompletedEvent("run-pc", "sess-pc", PlanTaskAttrs{
				TaskID:        "task-done",
				ResultSummary: "done",
			}, nil),
			check: func(t *testing.T, event Event) {
				payload, ok := event.PlanTaskCompletedPayload()
				if !ok || payload.ResultSummary != "done" {
					t.Fatalf("payload = %#v, ok=%v", payload, ok)
				}
			},
		},
		{
			name: "plan_task_cancelled",
			event: NewPlanTaskCancelledEvent("run-px", "sess-px", PlanTaskAttrs{
				TaskID: "task-cancel",
				Error:  "cancelled",
			}, nil),
			check: func(t *testing.T, event Event) {
				payload, ok := event.PlanTaskCancelledPayload()
				if !ok || payload.TaskID != "task-cancel" {
					t.Fatalf("payload = %#v, ok=%v", payload, ok)
				}
			},
		},
		{
			name: "plan_task_skipped",
			event: NewPlanTaskSkippedEvent("run-ps", "sess-ps", PlanTaskAttrs{
				TaskID: "task-skip",
				Error:  "skipped",
			}, nil),
			check: func(t *testing.T, event Event) {
				payload, ok := event.PlanTaskSkippedPayload()
				if !ok || payload.TaskID != "task-skip" {
					t.Fatalf("payload = %#v, ok=%v", payload, ok)
				}
			},
		},
		{
			name: "plan_task_failed",
			event: NewPlanTaskFailedEvent("run-pf", "sess-pf", PlanTaskAttrs{
				TaskID: "task-fail",
				Error:  "boom",
			}, nil),
			check: func(t *testing.T, event Event) {
				payload, ok := event.PlanTaskFailedPayload()
				if !ok || payload.Error != "boom" {
					t.Fatalf("payload = %#v, ok=%v", payload, ok)
				}
			},
		},
		{
			name: "model_routed",
			event: NewModelRoutedEvent("run-12", "sess-12", ModelRoutedAttrs{
				RequestedModel: "auto",
				SelectedModel:  "gpt-5.4",
				FailoverFrom:   "gpt-4.1",
				Reason:         "router_choice",
			}, nil),
			check: func(t *testing.T, event Event) {
				payload, ok := event.ModelRoutedPayload()
				if !ok {
					t.Fatal("ModelRoutedPayload() ok = false")
				}
				if payload.SelectedModel != "gpt-5.4" || payload.FailoverFrom != "gpt-4.1" {
					t.Fatalf("payload = %#v", payload)
				}
			},
		},
		{
			name:  "model_text_delta",
			event: NewModelTextDeltaEvent("run-13", "sess-13", DeltaAttrs{Delta: "hello"}, map[string]any{"trace_id": "tr-13"}),
			check: func(t *testing.T, event Event) {
				payload, ok := event.ModelTextDeltaPayload()
				if !ok || payload.Delta != "hello" {
					t.Fatalf("payload = %#v, ok=%v", payload, ok)
				}
				if event.Attrs["trace_id"] != "tr-13" {
					t.Fatalf("trace_id = %#v", event.Attrs["trace_id"])
				}
			},
		},
		{
			name:  "model_reasoning_delta",
			event: NewModelReasoningDeltaEvent("run-14", "sess-14", DeltaAttrs{Delta: "thinking"}, nil),
			check: func(t *testing.T, event Event) {
				payload, ok := event.ModelReasoningDeltaPayload()
				if !ok || payload.Delta != "thinking" {
					t.Fatalf("payload = %#v, ok=%v", payload, ok)
				}
			},
		},
		{
			name: "model_stream_complete",
			event: NewModelStreamCompleteEvent("run-15", "sess-15", ModelStreamCompleteAttrs{
				PromptTokens:     11,
				CompletionTokens: 22,
				TotalTokens:      33,
			}, map[string]any{"trace_id": "tr-15"}),
			check: func(t *testing.T, event Event) {
				payload, ok := event.ModelStreamCompletePayload()
				if !ok {
					t.Fatal("ModelStreamCompletePayload() ok = false")
				}
				if payload.PromptTokens != 11 || payload.CompletionTokens != 22 || payload.TotalTokens != 33 {
					t.Fatalf("payload = %#v", payload)
				}
				if event.Attrs["trace_id"] != "tr-15" {
					t.Fatalf("trace_id = %#v", event.Attrs["trace_id"])
				}
			},
		},
		{
			name: "model_retry",
			event: NewModelRetryEvent("run-16", "sess-16", ModelRetryAttrs{
				Model:         "gpt-5.4",
				Attempt:       2,
				MaxAttempts:   4,
				FailureReason: "rate_limit",
				DelayMs:       250,
				Error:         "429",
			}, nil),
			check: func(t *testing.T, event Event) {
				payload, ok := event.ModelRetryPayload()
				if !ok || payload.DelayMs != 250 || payload.Attempt != 2 {
					t.Fatalf("payload = %#v, ok=%v", payload, ok)
				}
			},
		},
		{
			name: "model_failover",
			event: NewModelFailoverEvent("run-17", "sess-17", ModelFailoverAttrs{
				FromModel: "gpt-4.1",
				ToModel:   "gpt-5.4-mini",
				Reason:    "cooldown",
			}, nil),
			check: func(t *testing.T, event Event) {
				payload, ok := event.ModelFailoverPayload()
				if !ok || payload.ToModel != "gpt-5.4-mini" {
					t.Fatalf("payload = %#v, ok=%v", payload, ok)
				}
			},
		},
		{
			name: "thinking_degraded",
			event: NewThinkingDegradedEvent("run-18", "sess-18", ThinkingDegradedAttrs{
				Model:   "gpt-5.4",
				From:    "extended",
				To:      "regular",
				Reason:  "timeout",
				Error:   "upstream timeout",
				Attempt: 1,
			}, nil),
			check: func(t *testing.T, event Event) {
				payload, ok := event.ThinkingDegradedPayload()
				if !ok || payload.To != "regular" || payload.Attempt != 1 {
					t.Fatalf("payload = %#v, ok=%v", payload, ok)
				}
			},
		},
		{
			name: "approval_grace_warning",
			event: NewApprovalGraceWarningEvent("run-19", "sess-19", ApprovalEventAttrs{
				ApprovalID:   "appr-9",
				ApprovalKind: "tool_calls",
				Status:       "pending",
				RemainingMs:  9000,
				PolicySource: "policy.approval",
			}, nil),
			check: func(t *testing.T, event Event) {
				payload, ok := event.ApprovalGraceWarningPayload()
				if !ok || payload.RemainingMs != 9000 || payload.ApprovalID != "appr-9" {
					t.Fatalf("payload = %#v, ok=%v", payload, ok)
				}
			},
		},
		{
			name: "approval_resolved",
			event: NewApprovalResolvedEvent("run-ar", "sess-ar", ApprovalEventAttrs{
				ApprovalID: "appr-resolved",
				Status:     "approved",
				ResolvedBy: "user",
			}, nil),
			check: func(t *testing.T, event Event) {
				payload, ok := event.ApprovalResolvedPayload()
				if !ok || payload.ResolvedBy != "user" {
					t.Fatalf("payload = %#v, ok=%v", payload, ok)
				}
			},
		},
		{
			name: "approval_timed_out",
			event: NewApprovalTimedOutEvent("run-at", "sess-at", ApprovalEventAttrs{
				ApprovalID:  "appr-timeout",
				Status:      "cancelled",
				RemainingMs: 1,
			}, nil),
			check: func(t *testing.T, event Event) {
				payload, ok := event.ApprovalTimedOutPayload()
				if !ok || payload.ApprovalID != "appr-timeout" {
					t.Fatalf("payload = %#v, ok=%v", payload, ok)
				}
			},
		},
		{
			name: "security_risk_detected",
			event: NewSecurityRiskDetectedEvent("", "", SecurityRiskDetectedAttrs{
				ToolName:  "fs.read",
				RiskCount: 2,
				Severity:  "high",
				Risks: []SecurityRiskItemAttrs{{
					Category: "path_safety",
					Type:     "path_traversal",
					Detail:   "path escapes workspace",
					Severity: "high",
				}},
			}, nil),
			check: func(t *testing.T, event Event) {
				payload, ok := event.SecurityRiskDetectedPayload()
				if !ok || payload.ToolName != "fs.read" || payload.Severity != "high" {
					t.Fatalf("payload = %#v, ok=%v", payload, ok)
				}
				if len(payload.Risks) != 1 || payload.Risks[0].Category != "path_safety" {
					t.Fatalf("payload.Risks = %#v", payload.Risks)
				}
			},
		},
		{
			name: "security_path_violation",
			event: NewSecurityPathViolationEvent("", "", SecurityFindingAttrs{
				ToolName: "fs.read",
				Type:     "path_traversal",
				Detail:   "path escapes workspace",
				Severity: "high",
			}, nil),
			check: func(t *testing.T, event Event) {
				payload, ok := event.SecurityPathViolationPayload()
				if !ok || payload.Detail != "path escapes workspace" {
					t.Fatalf("payload = %#v, ok=%v", payload, ok)
				}
			},
		},
		{
			name: "security_injection_attempt",
			event: NewSecurityInjectionAttemptEvent("", "", SecurityFindingAttrs{
				ToolName: "shell.exec",
				Type:     "sql_comment",
				Detail:   "injection-like payload",
				Severity: "medium",
			}, nil),
			check: func(t *testing.T, event Event) {
				payload, ok := event.SecurityInjectionAttemptPayload()
				if !ok || payload.Type != "sql_comment" {
					t.Fatalf("payload = %#v, ok=%v", payload, ok)
				}
			},
		},
		{
			name: "governance_delivery_queued",
			event: NewGovernanceDeliveryQueuedEvent("run-gq", "sess-gq", GovernanceDeliveryAttrs{
				DeliveryID:     "gq-1",
				AdapterName:    "webhook",
				DeliveryStatus: "pending",
			}, nil),
			check: func(t *testing.T, event Event) {
				payload, ok := event.GovernanceDeliveryQueuedPayload()
				if !ok || payload.DeliveryID != "gq-1" {
					t.Fatalf("payload = %#v, ok=%v", payload, ok)
				}
			},
		},
		{
			name: "governance_delivery_redriven",
			event: NewGovernanceDeliveryRedrivenEvent("run-gr", "sess-gr", GovernanceDeliveryAttrs{
				DeliveryID:     "gr-1",
				AdapterName:    "webhook",
				DeliveryStatus: "pending",
			}, nil),
			check: func(t *testing.T, event Event) {
				payload, ok := event.GovernanceDeliveryRedrivenPayload()
				if !ok || payload.DeliveryID != "gr-1" {
					t.Fatalf("payload = %#v, ok=%v", payload, ok)
				}
			},
		},
		{
			name: "governance_delivery_retry_scheduled",
			event: NewGovernanceDeliveryRetryScheduledEvent("run-gs", "sess-gs", GovernanceDeliveryAttrs{
				DeliveryID:     "gs-1",
				AdapterName:    "webhook",
				DeliveryStatus: "pending",
			}, nil),
			check: func(t *testing.T, event Event) {
				payload, ok := event.GovernanceDeliveryRetryScheduledPayload()
				if !ok || payload.DeliveryID != "gs-1" {
					t.Fatalf("payload = %#v, ok=%v", payload, ok)
				}
			},
		},
		{
			name: "governance_delivery_dead_lettered",
			event: NewGovernanceDeliveryDeadLetteredEvent("run-gd", "sess-gd", GovernanceDeliveryAttrs{
				DeliveryID:     "gd-1",
				AdapterName:    "webhook",
				DeliveryStatus: "dead_letter",
				Error:          "final failure",
			}, nil),
			check: func(t *testing.T, event Event) {
				payload, ok := event.GovernanceDeliveryDeadLetteredPayload()
				if !ok || payload.Error != "final failure" {
					t.Fatalf("payload = %#v, ok=%v", payload, ok)
				}
			},
		},
		{
			name: "governance_delivery_delivered",
			event: NewGovernanceDeliveryDeliveredEvent("run-20", "sess-20", GovernanceDeliveryAttrs{
				DeliveryID:          "gdel-1",
				AdapterName:         "webhook",
				IdempotencyKey:      "idem-1",
				DeliveryStatus:      "delivered",
				DeliveryAttempts:    2,
				DeliveryMaxAttempts: 3,
				GovernanceKind:      "approval_resolved",
				SourceEventID:       "evt-1",
				SourceEventType:     "approval.resolved",
				NextAttemptAt:       nextAttempt,
				DeliveredAt:         deliveredAt,
			}, nil),
			check: func(t *testing.T, event Event) {
				payload, ok := event.GovernanceDeliveryDeliveredPayload()
				if !ok || payload.AdapterName != "webhook" || payload.DeliveryAttempts != 2 {
					t.Fatalf("payload = %#v, ok=%v", payload, ok)
				}
				if !payload.NextAttemptAt.Equal(nextAttempt) || !payload.DeliveredAt.Equal(deliveredAt) {
					t.Fatalf("payload = %#v", payload)
				}
			},
		},
		{
			name: "artifact_pruned",
			event: NewArtifactPrunedEvent("run-21", "sess-21", ArtifactPrunedAttrs{
				DeletedCount: 2,
				DeletedIDs:   []string{"art-1", "art-2"},
				Cutoff:       cutoff,
				Kind:         "file",
				RunID:        "run-21",
				SessionID:    "sess-21",
				ToolName:     "fs.write",
				ToolCallID:   "call-9",
			}, nil),
			check: func(t *testing.T, event Event) {
				payload, ok := event.ArtifactPrunedPayload()
				if !ok || payload.DeletedCount != 2 || payload.ToolCallID != "call-9" {
					t.Fatalf("payload = %#v, ok=%v", payload, ok)
				}
				if !payload.Cutoff.Equal(cutoff) {
					t.Fatalf("Cutoff = %v, want %v", payload.Cutoff, cutoff)
				}
			},
		},
		{
			name: "governance_payload",
			event: NewRunCompletedEvent("run-22", "sess-22", RunStatusAttrs{
				Summary: "done",
			}, GovernanceAttrs{
				Scope:                     domainscope.Ref{AutomationID: "auto-1"},
				EffectiveConfigSnapshotID: "ecs-1",
				PolicySource:              "policy.source",
				PolicySummary:             "allow",
				PolicyToolNames:           []string{"fs.read"},
			}.ToMap()),
			check: func(t *testing.T, event Event) {
				payload, ok := event.GovernancePayload()
				if !ok {
					t.Fatal("GovernancePayload() ok = false")
				}
				if payload.EffectiveConfigSnapshotID != "ecs-1" || payload.PolicySource != "policy.source" {
					t.Fatalf("payload = %#v", payload)
				}
				if payload.Scope.AutomationID != "auto-1" {
					t.Fatalf("scope = %#v", payload.Scope)
				}
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.check(t, tc.event)
		})
	}
}

func TestWorkflowEventAttrsToMap(t *testing.T) {
	t.Parallel()

	attrs := WorkflowEventAttrs{
		OriginalRunID:     "run-001",
		ContinuationIndex: 3,
		TotalRoundsUsed:   42,
		CompletedTasks:    5,
		TotalTasks:        8,
		YieldReason:       "round_budget",
		Summary:           "continued",
	}
	mapped := attrs.ToMap()
	if mapped["continuation_index"] != 3 {
		t.Fatalf("continuation_index = %#v", mapped["continuation_index"])
	}
	if mapped["original_run_id"] != "run-001" {
		t.Fatalf("original_run_id = %#v", mapped["original_run_id"])
	}
	event := NewWorkflowYieldedEvent("run-001", "sess-001", attrs, nil)
	payload, ok := event.WorkflowYieldedPayload()
	if !ok {
		t.Fatal("WorkflowYieldedPayload() ok = false")
	}
	if payload.TotalRoundsUsed != 42 || payload.YieldReason != "round_budget" {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestApprovalPayloadParsesExternalReferences(t *testing.T) {
	t.Parallel()

	event := NewApprovalRequestedEvent("run-ext", "sess-ext", ApprovalEventAttrs{
		ApprovalID: "appr-ext",
		ExternalRefs: []ApprovalExternalRefAttrs{{
			Provider:   "jira",
			ExternalID: "JIRA-1",
			URL:        "https://jira.example/JIRA-1",
			Status:     "open",
			SyncedAt:   "2026-04-02T10:20:30Z",
		}},
	}, nil)

	payload, ok := event.ApprovalRequestedPayload()
	if !ok {
		t.Fatal("ApprovalRequestedPayload() ok = false")
	}
	if len(payload.ExternalRefs) != 1 {
		t.Fatalf("ExternalRefs = %#v", payload.ExternalRefs)
	}
	if !reflect.DeepEqual(payload.ExternalRefs[0], ApprovalExternalRefAttrs{
		Provider:   "jira",
		ExternalID: "JIRA-1",
		URL:        "https://jira.example/JIRA-1",
		Status:     "open",
		SyncedAt:   "2026-04-02T10:20:30Z",
	}) {
		t.Fatalf("ExternalRefs[0] = %#v", payload.ExternalRefs[0])
	}
}
