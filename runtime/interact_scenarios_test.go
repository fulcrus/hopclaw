package runtime

import "testing"

func TestClassifyDeterministic_EmptyContent(t *testing.T) {
	t.Parallel()

	decision, ok := classifyDeterministic(InteractionRequest{}, InteractionContextSnapshot{})
	if ok {
		t.Fatalf("classifyDeterministic() matched unexpectedly: %#v", decision)
	}
}

func TestClassifyDeterministic_SlashCancel(t *testing.T) {
	t.Parallel()

	decision, ok := classifyDeterministic(
		InteractionRequest{Content: "/cancel"},
		InteractionContextSnapshot{HasActiveRun: true, ActiveRunID: "run-1", SessionState: "running"},
	)
	if !ok {
		t.Fatal("expected deterministic match for /cancel")
	}
	if decision.SpeechAct != SpeechActCommand {
		t.Fatalf("SpeechAct = %q, want %q", decision.SpeechAct, SpeechActCommand)
	}
	if decision.ReplyAct != ReplyActActionAck {
		t.Fatalf("ReplyAct = %q, want %q", decision.ReplyAct, ReplyActActionAck)
	}
	if decision.Reason != "text_command_cancel" {
		t.Fatalf("Reason = %q, want %q", decision.Reason, "text_command_cancel")
	}
}

func TestClassifyDeterministic_NumberedApproval(t *testing.T) {
	t.Parallel()

	tests := []struct {
		content string
		reason  string
	}{
		{content: "1", reason: "numbered_approval_approve"},
		{content: "2", reason: "numbered_approval_deny"},
		{content: "3", reason: "numbered_approval_always"},
	}

	for _, tt := range tests {
		decision, ok := classifyDeterministic(
			InteractionRequest{Content: tt.content},
			InteractionContextSnapshot{
				WaitingApproval: true,
				HasActiveRun:    true,
				ActiveRunID:     "run-1",
				SessionState:    "waiting_approval",
			},
		)
		if !ok {
			t.Fatalf("content=%q: expected deterministic match", tt.content)
		}
		if decision.SpeechAct != SpeechActApprovalReply {
			t.Fatalf("content=%q: SpeechAct = %q, want %q", tt.content, decision.SpeechAct, SpeechActApprovalReply)
		}
		if decision.ReplyAct != ReplyActResumeAck {
			t.Fatalf("content=%q: ReplyAct = %q, want %q", tt.content, decision.ReplyAct, ReplyActResumeAck)
		}
		if decision.Reason != tt.reason {
			t.Fatalf("content=%q: Reason = %q, want %q", tt.content, decision.Reason, tt.reason)
		}
	}
}

func TestClassifyDeterministic_NumberedApproval_NotWaiting(t *testing.T) {
	t.Parallel()

	decision, ok := classifyDeterministic(
		InteractionRequest{Content: "1"},
		InteractionContextSnapshot{WaitingApproval: false, SessionState: "idle"},
	)
	if ok {
		t.Fatalf("classifyDeterministic() matched unexpectedly: %#v", decision)
	}
}

func TestClassifyDeterministic_ClarificationReply(t *testing.T) {
	t.Parallel()

	decision, ok := classifyDeterministic(
		InteractionRequest{Content: "需要发给法务"},
		InteractionContextSnapshot{
			WaitingInput: true,
			HasActiveRun: true,
			ActiveRunID:  "run-1",
			SessionState: "waiting_input",
		},
	)
	if !ok {
		t.Fatal("expected deterministic match while waiting for input")
	}
	if decision.SpeechAct != SpeechActClarificationReply {
		t.Fatalf("SpeechAct = %q, want %q", decision.SpeechAct, SpeechActClarificationReply)
	}
	if decision.ReplyAct != ReplyActResumeAck {
		t.Fatalf("ReplyAct = %q, want %q", decision.ReplyAct, ReplyActResumeAck)
	}
	if decision.Reason != "waiting_input_followup" {
		t.Fatalf("Reason = %q, want %q", decision.Reason, "waiting_input_followup")
	}
}

func TestInteractPolicyEnforcement_LowConfidence(t *testing.T) {
	t.Parallel()

	decision := applyInteractionPolicyWithThreshold(
		InteractionDecision{
			SpeechAct:   SpeechActNewTask,
			TargetScope: TargetScopeNewRun,
			ReplyAct:    ReplyActTaskAccept,
			Confidence:  0.39,
			Reason:      "semantic_new_task",
		},
		InteractionContextSnapshot{},
		0.4,
	)
	if decision.ReplyAct != ReplyActClarificationPrompt {
		t.Fatalf("ReplyAct = %q, want %q", decision.ReplyAct, ReplyActClarificationPrompt)
	}
	if decision.TargetScope != TargetScopeNone {
		t.Fatalf("TargetScope = %q, want %q", decision.TargetScope, TargetScopeNone)
	}
	if decision.Reason != "low_confidence_action" {
		t.Fatalf("Reason = %q, want %q", decision.Reason, "low_confidence_action")
	}
}
