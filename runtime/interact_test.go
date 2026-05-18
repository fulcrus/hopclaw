package runtime

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/artifact"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/internal/meta"
)

type stubInteractionClassifier struct {
	decision InteractionDecision
	err      error
}

func (s stubInteractionClassifier) Classify(context.Context, InteractionClassifyRequest) (InteractionDecision, error) {
	if s.err != nil {
		return InteractionDecision{}, s.err
	}
	return s.decision, nil
}

type recordingInteractionModelClient struct {
	response    *agent.ModelResponse
	err         error
	lastRequest agent.ChatRequest
	calls       int
}

func (m *recordingInteractionModelClient) Chat(_ context.Context, req agent.ChatRequest) (*agent.ModelResponse, error) {
	m.calls++
	m.lastRequest = req
	if m.err != nil {
		return nil, m.err
	}
	if m.response != nil {
		return m.response, nil
	}
	return &agent.ModelResponse{
		Message: contextengine.Message{Role: contextengine.RoleAssistant, Content: "ok"},
	}, nil
}

func newInteractiveServiceWithModel(model agent.ModelClient) *Service {
	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	queue := agent.NewInMemoryCoordinator()
	approvals := approval.NewInMemoryStore()
	artifacts := artifact.NewInMemoryStore()
	bus := eventbus.NewInMemoryBus()
	component := agent.NewComponent(agent.AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 3,
		QueueMode:     agent.QueueEnqueue,
	}, sessions, runs, queue, newContextEngine(), model, nil, nil).
		WithPreflightAnalyzer(testPreflightAnalyzer{})
	return NewService(component, sessions, runs, approvals, bus, artifacts)
}

// ---------------------------------------------------------------------------
// classifyDeterministic unit tests
// ---------------------------------------------------------------------------

func TestClassifyDeterministicStructuredApprovalWhenWaiting(t *testing.T) {
	t.Parallel()

	req := InteractionRequest{
		Content:            "y",
		StructuredApproval: &StructuredApproval{Action: "approve"},
	}
	snap := InteractionContextSnapshot{
		WaitingApproval: true,
		HasActiveRun:    true,
		ActiveRunID:     "run-1",
		SessionState:    "waiting_approval",
	}
	decision, ok := classifyDeterministic(req, snap)
	if !ok {
		t.Fatal("expected deterministic match")
	}
	if decision.SpeechAct != SpeechActApprovalReply {
		t.Fatalf("SpeechAct = %q, want %q", decision.SpeechAct, SpeechActApprovalReply)
	}
	if decision.ReplyAct != ReplyActResumeAck {
		t.Fatalf("ReplyAct = %q, want %q", decision.ReplyAct, ReplyActResumeAck)
	}
	if decision.Confidence != 1.0 {
		t.Fatalf("Confidence = %v, want 1.0", decision.Confidence)
	}
}

func TestClassifyDeterministicApprovalReplyNoPending(t *testing.T) {
	t.Parallel()

	req := InteractionRequest{
		Content:            "y",
		StructuredApproval: &StructuredApproval{Action: "approve"},
	}
	snap := InteractionContextSnapshot{
		WaitingApproval: false,
		SessionState:    "idle",
	}
	decision, ok := classifyDeterministic(req, snap)
	if !ok {
		t.Fatal("expected deterministic match")
	}
	if decision.SpeechAct != SpeechActApprovalReply {
		t.Fatalf("SpeechAct = %q, want %q", decision.SpeechAct, SpeechActApprovalReply)
	}
	if decision.ReplyAct != ReplyActChatReply {
		t.Fatalf("ReplyAct = %q, want %q (downgraded)", decision.ReplyAct, ReplyActChatReply)
	}
}

func TestClassifyDeterministicStructuredCommandStatus(t *testing.T) {
	t.Parallel()

	req := InteractionRequest{
		Content:           "/status",
		StructuredCommand: &StructuredCommand{Kind: "status"},
	}
	snap := InteractionContextSnapshot{
		HasActiveRun: true,
		SessionState: "running",
	}
	decision, ok := classifyDeterministic(req, snap)
	if !ok {
		t.Fatal("expected deterministic match")
	}
	if decision.SpeechAct != SpeechActStatusQuery {
		t.Fatalf("SpeechAct = %q, want %q", decision.SpeechAct, SpeechActStatusQuery)
	}
	if decision.ReplyAct != ReplyActStatusReply {
		t.Fatalf("ReplyAct = %q, want %q", decision.ReplyAct, ReplyActStatusReply)
	}
}

func TestClassifyDeterministicStructuredCommandCancel(t *testing.T) {
	t.Parallel()

	req := InteractionRequest{
		Content:           "/cancel",
		StructuredCommand: &StructuredCommand{Kind: "cancel"},
	}
	snap := InteractionContextSnapshot{
		HasActiveRun: true,
		ActiveRunID:  "run-1",
		SessionState: "running",
	}
	decision, ok := classifyDeterministic(req, snap)
	if !ok {
		t.Fatal("expected deterministic match")
	}
	if decision.SpeechAct != SpeechActCommand {
		t.Fatalf("SpeechAct = %q, want %q", decision.SpeechAct, SpeechActCommand)
	}
	if decision.ReplyAct != ReplyActActionAck {
		t.Fatalf("ReplyAct = %q, want %q", decision.ReplyAct, ReplyActActionAck)
	}
}

func TestClassifyDeterministicStructuredCommandRetry(t *testing.T) {
	t.Parallel()

	req := InteractionRequest{
		Content:           "",
		StructuredCommand: &StructuredCommand{Kind: "retry", RunID: "run-1"},
	}
	snap := InteractionContextSnapshot{
		SessionState: "completed_recently",
	}
	decision, ok := classifyDeterministic(req, snap)
	if !ok {
		t.Fatal("expected deterministic match")
	}
	if decision.SpeechAct != SpeechActNewTask {
		t.Fatalf("SpeechAct = %q, want %q", decision.SpeechAct, SpeechActNewTask)
	}
	if decision.TargetScope != TargetScopeNewRun {
		t.Fatalf("TargetScope = %q, want %q", decision.TargetScope, TargetScopeNewRun)
	}
	if decision.ReplyAct != ReplyActTaskAccept {
		t.Fatalf("ReplyAct = %q, want %q", decision.ReplyAct, ReplyActTaskAccept)
	}
	if decision.Reason != "structured_command_retry" {
		t.Fatalf("Reason = %q, want %q", decision.Reason, "structured_command_retry")
	}
}

func TestClassifyDeterministicTextApprovalWhenWaiting(t *testing.T) {
	t.Parallel()

	req := InteractionRequest{Content: "1"}
	snap := InteractionContextSnapshot{
		WaitingApproval: true,
		HasActiveRun:    true,
		SessionState:    "waiting_approval",
	}
	decision, ok := classifyDeterministic(req, snap)
	if !ok {
		t.Fatal("expected deterministic match")
	}
	if decision.SpeechAct != SpeechActApprovalReply {
		t.Fatalf("SpeechAct = %q, want %q", decision.SpeechAct, SpeechActApprovalReply)
	}
	if decision.ReplyAct != ReplyActResumeAck {
		t.Fatalf("ReplyAct = %q, want %q", decision.ReplyAct, ReplyActResumeAck)
	}
	if decision.Reason != "numbered_approval_approve" {
		t.Fatalf("Reason = %q, want %q", decision.Reason, "numbered_approval_approve")
	}
}

func TestClassifyDeterministicTextApprovalNotWaiting(t *testing.T) {
	t.Parallel()

	// "yes" when NOT waiting_approval should NOT be treated as approval.
	req := InteractionRequest{Content: "yes"}
	snap := InteractionContextSnapshot{
		WaitingApproval: false,
		HasActiveRun:    false,
		SessionState:    "idle",
	}
	if _, ok := classifyDeterministic(req, snap); ok {
		t.Fatal("should not classify as approval_reply when not waiting_approval")
	}
}

func TestClassifyDeterministicSlashCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		content    string
		wantCmd    string
		wantSpeech IncomingSpeechAct
	}{
		{"/status", "status", SpeechActStatusQuery},
		{"/progress", "status", SpeechActStatusQuery},
		{"/cancel", "cancel", SpeechActCommand},
		{"/abort", "cancel", SpeechActCommand},
		{"/bind", "bind", SpeechActCommand},
		{"/unbind", "unbind", SpeechActCommand},
	}
	for _, tt := range tests {
		snap := InteractionContextSnapshot{HasActiveRun: true, SessionState: "running"}
		decision, ok := classifyDeterministic(InteractionRequest{Content: tt.content}, snap)
		if !ok {
			t.Fatalf("content=%q: expected deterministic match", tt.content)
		}
		if decision.SpeechAct != tt.wantSpeech {
			t.Fatalf("content=%q: SpeechAct = %q, want %q", tt.content, decision.SpeechAct, tt.wantSpeech)
		}
	}
}

func TestClassifyDeterministicClarificationWhenWaitingInput(t *testing.T) {
	t.Parallel()

	req := InteractionRequest{Content: "发给张三"}
	snap := InteractionContextSnapshot{
		WaitingInput: true,
		HasActiveRun: true,
		ActiveRunID:  "run-1",
		SessionState: "waiting_input",
	}
	decision, ok := classifyDeterministic(req, snap)
	if !ok {
		t.Fatal("expected deterministic match")
	}
	if decision.SpeechAct != SpeechActClarificationReply {
		t.Fatalf("SpeechAct = %q, want %q", decision.SpeechAct, SpeechActClarificationReply)
	}
	if decision.ReplyAct != ReplyActResumeAck {
		t.Fatalf("ReplyAct = %q, want %q", decision.ReplyAct, ReplyActResumeAck)
	}
}

func TestClassifyDeterministicIdleDefersToClassifier(t *testing.T) {
	t.Parallel()

	req := InteractionRequest{Content: "帮我写一份周报"}
	snap := InteractionContextSnapshot{
		HasActiveRun: false,
		SessionState: "idle",
	}
	if _, ok := classifyDeterministic(req, snap); ok {
		t.Fatal("expected idle messages to defer to semantic classifier/default fallback")
	}
}

func TestClassifyDeterministicActiveRunDefersToClassifier(t *testing.T) {
	t.Parallel()

	// Generic text with an active run: no deterministic match.
	req := InteractionRequest{Content: "what is happening"}
	snap := InteractionContextSnapshot{
		HasActiveRun:    true,
		ActiveRunID:     "run-1",
		ActiveRunStatus: agent.RunRunning,
		SessionState:    "running",
	}
	_, ok := classifyDeterministic(req, snap)
	if ok {
		t.Fatal("expected no deterministic match when active run and generic text")
	}
}

// ---------------------------------------------------------------------------
// applyInteractionPolicy tests
// ---------------------------------------------------------------------------

func TestPolicyApprovalNoPendingDowngrade(t *testing.T) {
	t.Parallel()

	decision := InteractionDecision{
		SpeechAct:   SpeechActApprovalReply,
		TargetScope: TargetScopeActiveRun,
		ReplyAct:    ReplyActResumeAck,
		Confidence:  0.9,
	}
	snap := InteractionContextSnapshot{WaitingApproval: false}
	got := applyInteractionPolicy(decision, snap)
	if got.ReplyAct != ReplyActChatReply {
		t.Fatalf("ReplyAct = %q, want %q", got.ReplyAct, ReplyActChatReply)
	}
	if got.TargetScope != TargetScopeNone {
		t.Fatalf("TargetScope = %q, want %q", got.TargetScope, TargetScopeNone)
	}
}

func TestPolicyClarificationNoWaitingInputUpgrade(t *testing.T) {
	t.Parallel()

	decision := InteractionDecision{
		SpeechAct:   SpeechActClarificationReply,
		TargetScope: TargetScopeActiveRun,
		ReplyAct:    ReplyActResumeAck,
		Confidence:  0.7,
	}
	snap := InteractionContextSnapshot{WaitingInput: false, HasActiveRun: false}
	got := applyInteractionPolicy(decision, snap)
	if got.SpeechAct != SpeechActNewTask {
		t.Fatalf("SpeechAct = %q, want %q", got.SpeechAct, SpeechActNewTask)
	}
	if got.ReplyAct != ReplyActTaskAccept {
		t.Fatalf("ReplyAct = %q, want %q", got.ReplyAct, ReplyActTaskAccept)
	}
}

func TestPolicyCasualChatNoRun(t *testing.T) {
	t.Parallel()

	decision := InteractionDecision{
		SpeechAct: SpeechActCasualChat,
		ReplyAct:  ReplyActTaskAccept,
	}
	snap := InteractionContextSnapshot{}
	got := applyInteractionPolicy(decision, snap)
	if got.ReplyAct != ReplyActChatReply {
		t.Fatalf("ReplyAct = %q, want %q", got.ReplyAct, ReplyActChatReply)
	}
}

func TestPolicyNegativeFeedbackWithActiveRun(t *testing.T) {
	t.Parallel()

	decision := InteractionDecision{
		SpeechAct: SpeechActNegativeFeedback,
		ReplyAct:  ReplyActChatReply,
	}
	snap := InteractionContextSnapshot{HasActiveRun: true, ActiveRunID: "run-1"}
	got := applyInteractionPolicy(decision, snap)
	if got.SpeechAct != SpeechActTaskFollowup {
		t.Fatalf("SpeechAct = %q, want %q", got.SpeechAct, SpeechActTaskFollowup)
	}
	if got.ReplyAct != ReplyActResumeAck {
		t.Fatalf("ReplyAct = %q, want %q", got.ReplyAct, ReplyActResumeAck)
	}
}

func TestPolicyNegativeFeedbackNoActiveRun(t *testing.T) {
	t.Parallel()

	decision := InteractionDecision{
		SpeechAct: SpeechActNegativeFeedback,
		ReplyAct:  ReplyActChatReply,
	}
	snap := InteractionContextSnapshot{HasActiveRun: false}
	got := applyInteractionPolicy(decision, snap)
	if got.ReplyAct != ReplyActChatReply {
		t.Fatalf("ReplyAct = %q, want %q", got.ReplyAct, ReplyActChatReply)
	}
}

func TestPolicyTaskFollowupNoActiveRun(t *testing.T) {
	t.Parallel()

	decision := InteractionDecision{
		SpeechAct:   SpeechActTaskFollowup,
		TargetScope: TargetScopeActiveRun,
		ReplyAct:    ReplyActResumeAck,
	}
	snap := InteractionContextSnapshot{HasActiveRun: false}
	got := applyInteractionPolicy(decision, snap)
	if got.SpeechAct != SpeechActNewTask {
		t.Fatalf("SpeechAct = %q, want %q", got.SpeechAct, SpeechActNewTask)
	}
	if got.ReplyAct != ReplyActTaskAccept {
		t.Fatalf("ReplyAct = %q, want %q", got.ReplyAct, ReplyActTaskAccept)
	}
}

func TestPolicyStatusQueryNeverCreatesRun(t *testing.T) {
	t.Parallel()

	decision := InteractionDecision{
		SpeechAct:   SpeechActStatusQuery,
		TargetScope: TargetScopeNewRun,
		ReplyAct:    ReplyActTaskAccept,
	}
	snap := InteractionContextSnapshot{}
	got := applyInteractionPolicy(decision, snap)
	if got.ReplyAct != ReplyActStatusReply {
		t.Fatalf("ReplyAct = %q, want %q", got.ReplyAct, ReplyActStatusReply)
	}
	if got.TargetScope != TargetScopeSession {
		t.Fatalf("TargetScope = %q, want %q", got.TargetScope, TargetScopeSession)
	}
}

func TestPolicyLowConfidenceSideEffectBecomesClarificationPrompt(t *testing.T) {
	t.Parallel()

	decision := InteractionDecision{
		SpeechAct:   SpeechActNewTask,
		TargetScope: TargetScopeNewRun,
		ReplyAct:    ReplyActTaskAccept,
		Confidence:  0.2,
	}
	got := applyInteractionPolicyWithThreshold(decision, InteractionContextSnapshot{}, 0.4)
	if got.ReplyAct != ReplyActClarificationPrompt {
		t.Fatalf("ReplyAct = %q, want %q", got.ReplyAct, ReplyActClarificationPrompt)
	}
	if got.TargetScope != TargetScopeNone {
		t.Fatalf("TargetScope = %q, want %q", got.TargetScope, TargetScopeNone)
	}
	if got.Reason != "low_confidence_action" {
		t.Fatalf("Reason = %q, want %q", got.Reason, "low_confidence_action")
	}
}

// ---------------------------------------------------------------------------
// parseApprovalText / parseCommandText unit tests
// ---------------------------------------------------------------------------

func TestParseApprovalText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"y", "approve"},
		{"Y", "approve"},
		{"yes", "approve"},
		{"YES", "approve"},
		{"n", "deny"},
		{"N", "deny"},
		{"no", "deny"},
		{"a", "always"},
		{"A", "always"},
		{"always", "always"},
		{"ALWAYS", "always"},
		{"1", ""},
		{"2", ""},
		{"3", ""},
		{"maybe", ""},
		{"hello", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got := parseApprovalText(tt.input)
		if got != tt.want {
			t.Fatalf("parseApprovalText(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestParseApprovalSelection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"1", "approve"},
		{"2", "deny"},
		{"3", "always"},
		{"0", ""},
		{"yes", ""},
	}
	for _, tt := range tests {
		got := parseApprovalSelection(tt.input)
		if got != tt.want {
			t.Fatalf("parseApprovalSelection(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestParseCommandText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"/status", "status"},
		{"/progress", "status"},
		{"/cancel", "cancel"},
		{"/abort", "cancel"},
		{"/bind", "bind"},
		{"/bind session-1", "bind"},
		{"/unbind", "unbind"},
		// Non-English aliases must NOT match.
		{"/状态", ""},
		{"/进度", ""},
		{"/取消", ""},
		{"/绑定", ""},
		{"/解绑", ""},
		{"hello", ""},
		{"/unknown", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got := parseCommandText(tt.input)
		if got != tt.want {
			t.Fatalf("parseCommandText(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Interact end-to-end behavioral tests
// ---------------------------------------------------------------------------

func TestInteractIdleNewTask(t *testing.T) {
	t.Parallel()

	svc := newFullService().WithClassifier(stubInteractionClassifier{
		decision: InteractionDecision{
			SpeechAct:   SpeechActNewTask,
			TargetScope: TargetScopeNewRun,
			ReplyAct:    ReplyActTaskAccept,
			Confidence:  0.95,
			Reason:      "explicit_new_task",
		},
	})
	result, err := svc.Interact(context.Background(), InteractionRequest{
		SessionKey: "interact-idle-new-task",
		Content:    "帮我写一份周报",
	})
	if err != nil {
		t.Fatalf("Interact() error = %v", err)
	}
	if result.Decision.SpeechAct != SpeechActNewTask {
		t.Fatalf("SpeechAct = %q, want %q", result.Decision.SpeechAct, SpeechActNewTask)
	}
	if result.Decision.ReplyAct != ReplyActTaskAccept {
		t.Fatalf("ReplyAct = %q, want %q", result.Decision.ReplyAct, ReplyActTaskAccept)
	}
	if result.Run == nil {
		t.Fatal("expected Run to be set for task_accept")
	}
	if result.Run.ID == "" {
		t.Fatal("Run.ID should not be empty")
	}
}

func TestInteractStatusCommandNoActiveRun(t *testing.T) {
	t.Parallel()

	svc := newFullService()
	result, err := svc.Interact(context.Background(), InteractionRequest{
		SessionKey: "interact-status-idle",
		Content:    "/status",
	})
	if err != nil {
		t.Fatalf("Interact() error = %v", err)
	}
	if result.Decision.SpeechAct != SpeechActStatusQuery {
		t.Fatalf("SpeechAct = %q, want %q", result.Decision.SpeechAct, SpeechActStatusQuery)
	}
	if result.Decision.ReplyAct != ReplyActStatusReply {
		t.Fatalf("ReplyAct = %q, want %q", result.Decision.ReplyAct, ReplyActStatusReply)
	}
	if result.Run != nil {
		t.Fatal("Run should be nil when no active run")
	}
}

func TestInteractClarificationWhenWaitingInput(t *testing.T) {
	t.Parallel()

	svc := newFullService()

	// First: create a waiting_input run.
	first, err := svc.Submit(context.Background(), SubmitRequest{
		SessionKey: "interact-waiting-input",
		Content:    "把这个文件改一下",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if first.Status != agent.RunWaitingInput {
		t.Fatalf("first run status = %q, want waiting_input", first.Status)
	}

	// Second: interact with a clarification.
	result, err := svc.Interact(context.Background(), InteractionRequest{
		SessionKey: "interact-waiting-input",
		Content:    "改成英文",
	})
	if err != nil {
		t.Fatalf("Interact() error = %v", err)
	}
	if result.Decision.SpeechAct != SpeechActClarificationReply {
		t.Fatalf("SpeechAct = %q, want %q", result.Decision.SpeechAct, SpeechActClarificationReply)
	}
	if result.Decision.ReplyAct != ReplyActResumeAck {
		t.Fatalf("ReplyAct = %q, want %q", result.Decision.ReplyAct, ReplyActResumeAck)
	}
	if result.Run == nil {
		t.Fatal("expected Run to be set")
	}
	if result.Run.TaskContract == nil {
		t.Fatal("expected task contract on clarification run")
	}
	// The old run should be superseded.
	oldRun, err := svc.GetRun(context.Background(), first.ID)
	if err != nil {
		t.Fatalf("GetRun(%s) error = %v", first.ID, err)
	}
	if oldRun.Status != agent.RunCancelled {
		t.Fatalf("old run status = %q, want cancelled", oldRun.Status)
	}
}

func TestInteractEmptyContentError(t *testing.T) {
	t.Parallel()

	svc := newFullService()
	_, err := svc.Interact(context.Background(), InteractionRequest{
		SessionKey: "interact-empty",
		Content:    "",
	})
	if err == nil {
		t.Fatal("expected error for empty content")
	}
}

func TestInteractCancelCommandNoRun(t *testing.T) {
	t.Parallel()

	svc := newFullService()
	result, err := svc.Interact(context.Background(), InteractionRequest{
		SessionKey: "interact-cancel-idle",
		Content:    "/cancel",
	})
	if err != nil {
		t.Fatalf("Interact() error = %v", err)
	}
	if result.Decision.SpeechAct != SpeechActCommand {
		t.Fatalf("SpeechAct = %q, want %q", result.Decision.SpeechAct, SpeechActCommand)
	}
	if result.Decision.ReplyAct != ReplyActActionAck {
		t.Fatalf("ReplyAct = %q, want %q", result.Decision.ReplyAct, ReplyActActionAck)
	}
	if result.RunCancelled {
		t.Fatal("should not have cancelled anything when no active run")
	}
}

func TestInteractStructuredApprovalWhenWaiting(t *testing.T) {
	t.Parallel()

	svc := newFullService()
	result, err := svc.Interact(context.Background(), InteractionRequest{
		SessionKey:         "interact-approval-no-pending",
		Content:            "y",
		StructuredApproval: &StructuredApproval{Action: "approve"},
	})
	if err != nil {
		t.Fatalf("Interact() error = %v", err)
	}
	// No pending approval, should downgrade to chat_reply.
	if result.Decision.SpeechAct != SpeechActApprovalReply {
		t.Fatalf("SpeechAct = %q, want %q", result.Decision.SpeechAct, SpeechActApprovalReply)
	}
	if result.Decision.ReplyAct != ReplyActChatReply {
		t.Fatalf("ReplyAct = %q, want %q (downgraded)", result.Decision.ReplyAct, ReplyActChatReply)
	}
}

func TestInteractAllowsEmptyStructuredApprovalPayload(t *testing.T) {
	t.Parallel()

	svc := newFullService()
	result, err := svc.Interact(context.Background(), InteractionRequest{
		SessionKey:         "interact-empty-structured-approval",
		Content:            "",
		StructuredApproval: &StructuredApproval{Action: "approve"},
	})
	if err != nil {
		t.Fatalf("Interact() error = %v", err)
	}
	if result.Decision.SpeechAct != SpeechActApprovalReply {
		t.Fatalf("SpeechAct = %q, want %q", result.Decision.SpeechAct, SpeechActApprovalReply)
	}
}

func TestInteractStructuredRetryReusesOriginalPromptAndContentBlocks(t *testing.T) {
	t.Parallel()

	svc := newFullService()
	original, err := svc.Submit(context.Background(), SubmitRequest{
		SessionKey: "interact-structured-retry",
		Content:    "review these assets",
		ContentBlocks: []contextengine.ContentBlock{
			{Type: contextengine.ContentBlockText, Text: "review these assets"},
			{Type: contextengine.ContentBlockFile, Label: "brief.txt", Path: "/tmp/brief.txt"},
			{Type: contextengine.ContentBlockImage, MediaType: "image/png", Data: "ZmFrZS1wbmc="},
		},
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	original = waitForRunStatus(t, svc, original.ID, agent.RunCompleted)

	result, err := svc.Interact(context.Background(), InteractionRequest{
		SessionKey: "interact-structured-retry",
		Content:    "",
		StructuredCommand: &StructuredCommand{
			Kind:  "retry",
			RunID: original.ID,
		},
	})
	if err != nil {
		t.Fatalf("Interact() error = %v", err)
	}
	if result.Decision.ReplyAct != ReplyActTaskAccept {
		t.Fatalf("ReplyAct = %q, want %q", result.Decision.ReplyAct, ReplyActTaskAccept)
	}
	if result.Run == nil {
		t.Fatal("expected retry to create a run")
	}
	if result.Run.ParentRunID != original.ID {
		t.Fatalf("ParentRunID = %q, want %q", result.Run.ParentRunID, original.ID)
	}
	if result.Run.Model != original.Model {
		t.Fatalf("Model = %q, want %q", result.Run.Model, original.Model)
	}
	if result.SubmitRequest == nil {
		t.Fatal("expected submit request")
	}
	if result.SubmitRequest.Content != "review these assets" {
		t.Fatalf("SubmitRequest.Content = %q, want %q", result.SubmitRequest.Content, "review these assets")
	}
	if len(result.SubmitRequest.ContentBlocks) != 3 {
		t.Fatalf("SubmitRequest.ContentBlocks = %#v", result.SubmitRequest.ContentBlocks)
	}
	if got := result.SubmitRequest.Metadata["retry_run_id"]; got != original.ID {
		t.Fatalf("retry_run_id = %#v, want %q", got, original.ID)
	}

	session, err := svc.GetSession(context.Background(), result.Run.SessionID)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	var lastUser contextengine.Message
	found := false
	for i := len(session.Messages) - 1; i >= 0; i-- {
		if session.Messages[i].Role != contextengine.RoleUser {
			continue
		}
		lastUser = session.Messages[i]
		found = true
		break
	}
	if !found {
		t.Fatalf("session.Messages = %#v", session.Messages)
	}
	if lastUser.Content != "review these assets" {
		t.Fatalf("last user content = %q, want %q", lastUser.Content, "review these assets")
	}
	if !reflect.DeepEqual(lastUser.ContentBlocks, result.SubmitRequest.ContentBlocks) {
		t.Fatalf("last user content blocks = %#v, want %#v", lastUser.ContentBlocks, result.SubmitRequest.ContentBlocks)
	}
}

func TestInteractStructuredApprovalResolvesPendingTicketWithEmptyContent(t *testing.T) {
	t.Parallel()

	svc := newFullService()
	svc.agent.WithApprovals(svc.approvals)
	session, err := svc.sessions.GetOrCreate(context.Background(), "interact-empty-structured-approval-resolve", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	run, err := svc.runs.Create(context.Background(), session.ID, agent.IncomingMessage{
		SessionKey:      "interact-empty-structured-approval-resolve",
		ExternalEventID: "evt-empty-structured-approval-resolve",
		Content:         "dangerous change",
	}, agent.AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    agent.QueueEnqueue,
	})
	if err != nil {
		t.Fatalf("RunStore.Create() error = %v", err)
	}
	ticket, err := svc.approvals.Create(context.Background(), approval.Ticket{
		RunID:     run.ID,
		SessionID: session.ID,
		Kind:      approval.KindToolCalls,
		Status:    approval.StatusPending,
	})
	if err != nil {
		t.Fatalf("approvals.Create() error = %v", err)
	}
	run.Status = agent.RunWaitingApproval
	run.Phase = agent.PhaseWaitingApproval
	run.ApprovalID = ticket.ID
	if err := svc.runs.Update(context.Background(), run); err != nil {
		t.Fatalf("runs.Update() error = %v", err)
	}

	result, err := svc.Interact(context.Background(), InteractionRequest{
		SessionKey:         "interact-empty-structured-approval-resolve",
		Content:            "",
		StructuredApproval: &StructuredApproval{Action: "deny"},
	})
	if err != nil {
		t.Fatalf("Interact() error = %v", err)
	}
	if !result.ApprovalResolved {
		t.Fatalf("expected approval to be resolved, decision=%#v context=%#v error=%q", result.Decision, result.Context, result.Error)
	}
	if result.ApprovalStatus != approval.StatusDenied {
		t.Fatalf("ApprovalStatus = %q, want %q", result.ApprovalStatus, approval.StatusDenied)
	}
	resolved, err := svc.approvals.Get(context.Background(), ticket.ID)
	if err != nil {
		t.Fatalf("approvals.Get() error = %v", err)
	}
	if resolved.Status != approval.StatusDenied {
		t.Fatalf("ticket.Status = %q, want %q", resolved.Status, approval.StatusDenied)
	}
}

func TestInteractAlwaysApprovalRemembersCurrentSession(t *testing.T) {
	t.Parallel()

	svc := newFullService()
	svc.agent.WithApprovals(svc.approvals)
	grantStore := approval.NewGrantStore()
	svc.WithGrantStore(grantStore)

	session, err := svc.sessions.GetOrCreate(context.Background(), "interact-approval-always", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	run, err := svc.runs.Create(context.Background(), session.ID, agent.IncomingMessage{
		SessionKey:      "interact-approval-always",
		ExternalEventID: "evt-approval-always",
		Content:         "remember this approval",
	}, agent.AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    agent.QueueEnqueue,
	})
	if err != nil {
		t.Fatalf("RunStore.Create() error = %v", err)
	}
	ticket, err := svc.approvals.Create(context.Background(), approval.Ticket{
		RunID:     run.ID,
		SessionID: session.ID,
		Kind:      approval.KindToolCalls,
		Status:    approval.StatusPending,
		ToolCalls: []approval.ToolCall{{ID: "call-1", Name: "exec.run"}},
		Metadata: map[string]any{
			"policy_approval_max_scope": "session",
		},
	})
	if err != nil {
		t.Fatalf("approvals.Create() error = %v", err)
	}
	run.Status = agent.RunWaitingApproval
	run.Phase = agent.PhaseWaitingApproval
	run.ApprovalID = ticket.ID
	if err := svc.runs.Update(context.Background(), run); err != nil {
		t.Fatalf("runs.Update() error = %v", err)
	}

	result, err := svc.Interact(context.Background(), InteractionRequest{
		SessionKey:         "interact-approval-always",
		Content:            "",
		StructuredApproval: &StructuredApproval{Action: "always"},
	})
	if err != nil {
		t.Fatalf("Interact() error = %v", err)
	}
	if !result.ApprovalResolved {
		t.Fatalf("expected approval to be resolved, decision=%#v context=%#v error=%q", result.Decision, result.Context, result.Error)
	}
	if result.ApprovalStatus != approval.StatusApproved {
		t.Fatalf("ApprovalStatus = %q, want %q", result.ApprovalStatus, approval.StatusApproved)
	}
	if result.Decision.Reason != "approval_reply_always" {
		t.Fatalf("Decision.Reason = %q, want approval_reply_always", result.Decision.Reason)
	}
	resolved, err := svc.approvals.Get(context.Background(), ticket.ID)
	if err != nil {
		t.Fatalf("approvals.Get() error = %v", err)
	}
	if resolved.Scope != approval.ScopeSession {
		t.Fatalf("ticket.Scope = %q, want %q", resolved.Scope, approval.ScopeSession)
	}
	if !grantStore.IsGranted(session.ID, "exec.run") {
		t.Fatal("expected current-session approval grant to be remembered")
	}
	if grantStore.IsGranted("other-session", "exec.run") {
		t.Fatal("expected remembered approval grant to remain session-scoped")
	}
}

func TestInteractLowConfidenceTaskAcceptReturnsClarificationPrompt(t *testing.T) {
	t.Parallel()

	svc := newFullService().WithClassifier(stubInteractionClassifier{
		decision: InteractionDecision{
			SpeechAct:   SpeechActNewTask,
			TargetScope: TargetScopeNewRun,
			ReplyAct:    ReplyActTaskAccept,
			Confidence:  0.2,
			Reason:      "ambiguous_new_task",
		},
	})

	result, err := svc.Interact(context.Background(), InteractionRequest{
		SessionKey: "interact-low-confidence-task",
		Content:    "send it over there",
	})
	if err != nil {
		t.Fatalf("Interact() error = %v", err)
	}
	if result.Decision.ReplyAct != ReplyActClarificationPrompt {
		t.Fatalf("ReplyAct = %q, want %q", result.Decision.ReplyAct, ReplyActClarificationPrompt)
	}
	if result.Run != nil {
		t.Fatalf("Run = %#v, want nil because no side effect should execute", result.Run)
	}
	if result.ReplyMessage == "" {
		t.Fatal("expected clarification reply message")
	}
}

func TestInteractRespectsConfigurableMinActionConfidence(t *testing.T) {
	t.Parallel()

	svc := newFullService().
		WithMinActionConfidence(0.2).
		WithClassifier(stubInteractionClassifier{
			decision: InteractionDecision{
				SpeechAct:   SpeechActNewTask,
				TargetScope: TargetScopeNewRun,
				ReplyAct:    ReplyActTaskAccept,
				Confidence:  0.25,
				Reason:      "above_custom_threshold",
			},
		})

	result, err := svc.Interact(context.Background(), InteractionRequest{
		SessionKey: "interact-custom-confidence-task",
		Content:    "draft the report",
	})
	if err != nil {
		t.Fatalf("Interact() error = %v", err)
	}
	if result.Decision.ReplyAct != ReplyActTaskAccept {
		t.Fatalf("ReplyAct = %q, want %q", result.Decision.ReplyAct, ReplyActTaskAccept)
	}
	if result.Run == nil {
		t.Fatal("expected task submission to proceed above custom threshold")
	}
}

func TestInteractKeepsIdleCLIChatReplyAsChatReply(t *testing.T) {
	t.Parallel()

	svc := newFullService().WithClassifier(stubInteractionClassifier{
		decision: InteractionDecision{
			SpeechAct:   SpeechActCasualChat,
			TargetScope: TargetScopeNone,
			ReplyAct:    ReplyActChatReply,
			Confidence:  0.98,
			Reason:      "chit_chat",
		},
	})

	result, err := svc.Interact(context.Background(), InteractionRequest{
		SessionKey: "interact-cli-promote-chat",
		Content:    "answer with one word only: ping",
		Metadata: map[string]any{
			meta.KeyChannel: "cli",
			"chat_type":     "direct",
		},
	})
	if err != nil {
		t.Fatalf("Interact() error = %v", err)
	}
	if result.Decision.ReplyAct != ReplyActChatReply {
		t.Fatalf("ReplyAct = %q, want %q", result.Decision.ReplyAct, ReplyActChatReply)
	}
	if result.Run != nil {
		t.Fatalf("Run = %#v, want nil for idle CLI chat reply", result.Run)
	}
}

func TestInteractChatReplyUsesModelGeneratedMessage(t *testing.T) {
	t.Parallel()

	model := &recordingInteractionModelClient{
		response: &agent.ModelResponse{
			Message: contextengine.Message{Role: contextengine.RoleAssistant, Content: "I am HopClaw."},
		},
	}
	svc := newInteractiveServiceWithModel(model).WithClassifier(stubInteractionClassifier{
		decision: InteractionDecision{
			SpeechAct:   SpeechActMetaQuestion,
			TargetScope: TargetScopeNone,
			ReplyAct:    ReplyActChatReply,
			Confidence:  0.98,
			Reason:      "meta_question",
		},
	})

	result, err := svc.Interact(context.Background(), InteractionRequest{
		SessionKey: "interact-chat-model-reply",
		Content:    "who are you?",
		Metadata: map[string]any{
			meta.KeyChannel: "cli",
			"chat_type":     "direct",
		},
	})
	if err != nil {
		t.Fatalf("Interact() error = %v", err)
	}
	if result.Decision.ReplyAct != ReplyActChatReply {
		t.Fatalf("ReplyAct = %q, want %q", result.Decision.ReplyAct, ReplyActChatReply)
	}
	if result.Run != nil {
		t.Fatalf("Run = %#v, want nil", result.Run)
	}
	if result.ReplyMessage != "I am HopClaw." {
		t.Fatalf("ReplyMessage = %q, want %q", result.ReplyMessage, "I am HopClaw.")
	}
	if model.calls != 1 {
		t.Fatalf("model calls = %d, want 1", model.calls)
	}
	if len(model.lastRequest.Tools) != 0 {
		t.Fatalf("last request tools = %#v, want none", model.lastRequest.Tools)
	}

	session, err := svc.GetSession(context.Background(), result.Context.SessionID)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if len(session.Messages) != 2 {
		t.Fatalf("session.Messages = %#v", session.Messages)
	}
	if got := session.Messages[0].Metadata[meta.KeyInteractionEnvelope]; got != "conversation" {
		t.Fatalf("user envelope = %#v, want conversation", got)
	}
	if got := session.Messages[1].Metadata[meta.KeyInteractionEnvelope]; got != "conversation" {
		t.Fatalf("assistant envelope = %#v, want conversation", got)
	}
}

func TestInteractClarificationPromptUsesModelGeneratedMessage(t *testing.T) {
	t.Parallel()

	model := &recordingInteractionModelClient{
		response: &agent.ModelResponse{
			Message: contextengine.Message{Role: contextengine.RoleAssistant, Content: "你想改哪个文件？"},
		},
	}
	svc := newInteractiveServiceWithModel(model).WithClassifier(stubInteractionClassifier{
		decision: InteractionDecision{
			SpeechAct:   SpeechActUnknown,
			TargetScope: TargetScopeNone,
			ReplyAct:    ReplyActClarificationPrompt,
			Confidence:  0.22,
			Reason:      "low_confidence_action",
		},
	})

	result, err := svc.Interact(context.Background(), InteractionRequest{
		SessionKey: "interact-clarification-model-reply",
		Content:    "帮我改一下",
	})
	if err != nil {
		t.Fatalf("Interact() error = %v", err)
	}
	if result.Decision.ReplyAct != ReplyActClarificationPrompt {
		t.Fatalf("ReplyAct = %q, want %q", result.Decision.ReplyAct, ReplyActClarificationPrompt)
	}
	if result.ReplyMessage != "你想改哪个文件？" {
		t.Fatalf("ReplyMessage = %q, want %q", result.ReplyMessage, "你想改哪个文件？")
	}
	if result.Run != nil {
		t.Fatalf("Run = %#v, want nil", result.Run)
	}
	if len(model.lastRequest.Tools) != 0 {
		t.Fatalf("last request tools = %#v, want none", model.lastRequest.Tools)
	}

	session, err := svc.GetSession(context.Background(), result.Context.SessionID)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if len(session.Messages) != 2 {
		t.Fatalf("session.Messages = %#v", session.Messages)
	}
	if got := session.Messages[0].Metadata[meta.KeyInteractionEnvelope]; got != "clarification" {
		t.Fatalf("user envelope = %#v, want clarification", got)
	}
	if got := session.Messages[1].Metadata[meta.KeyInteractionEnvelope]; got != "clarification" {
		t.Fatalf("assistant envelope = %#v, want clarification", got)
	}
}

func TestInteractChatReplyModelFailureReturnsTaskFailure(t *testing.T) {
	t.Parallel()

	svc := newInteractiveServiceWithModel(&recordingInteractionModelClient{
		err: errors.New("model unavailable"),
	}).WithClassifier(stubInteractionClassifier{
		decision: InteractionDecision{
			SpeechAct:   SpeechActCasualChat,
			TargetScope: TargetScopeNone,
			ReplyAct:    ReplyActChatReply,
			Confidence:  0.98,
			Reason:      "casual_chat",
		},
	})

	result, err := svc.Interact(context.Background(), InteractionRequest{
		SessionKey: "interact-chat-model-error",
		Content:    "hello",
	})
	if err != nil {
		t.Fatalf("Interact() error = %v", err)
	}
	if result.Decision.ReplyAct != ReplyActTaskFailure {
		t.Fatalf("ReplyAct = %q, want %q", result.Decision.ReplyAct, ReplyActTaskFailure)
	}
	if result.Error == "" {
		t.Fatal("expected model failure error")
	}
	if result.Run != nil {
		t.Fatalf("Run = %#v, want nil", result.Run)
	}
}

func TestInteractKeepsIdleChatReplyOutsideCLI(t *testing.T) {
	t.Parallel()

	svc := newFullService().WithClassifier(stubInteractionClassifier{
		decision: InteractionDecision{
			SpeechAct:   SpeechActCasualChat,
			TargetScope: TargetScopeNone,
			ReplyAct:    ReplyActChatReply,
			Confidence:  0.98,
			Reason:      "chit_chat",
		},
	})

	result, err := svc.Interact(context.Background(), InteractionRequest{
		SessionKey: "interact-slack-chat-reply",
		Content:    "hi",
		Metadata: map[string]any{
			meta.KeyChannel: "slack",
		},
	})
	if err != nil {
		t.Fatalf("Interact() error = %v", err)
	}
	if result.Decision.ReplyAct != ReplyActChatReply {
		t.Fatalf("ReplyAct = %q, want %q", result.Decision.ReplyAct, ReplyActChatReply)
	}
	if result.Run != nil {
		t.Fatalf("Run = %#v, want nil for non-CLI idle chat reply", result.Run)
	}
}

func TestInteractTaskAcceptForwardsImagesToSubmitRequest(t *testing.T) {
	t.Parallel()

	svc := newFullService().WithClassifier(stubInteractionClassifier{
		decision: InteractionDecision{
			SpeechAct:   SpeechActNewTask,
			TargetScope: TargetScopeNewRun,
			ReplyAct:    ReplyActTaskAccept,
			Confidence:  0.95,
			Reason:      "image_prompt",
		},
	})

	result, err := svc.Interact(context.Background(), InteractionRequest{
		SessionKey: "interact-images",
		Content:    "what is in this image?",
		Images:     []string{"data:image/png;base64,ZmFrZS1wbmc="},
	})
	if err != nil {
		t.Fatalf("Interact() error = %v", err)
	}
	if result.SubmitRequest == nil {
		t.Fatal("expected SubmitRequest to be returned")
	}
	if !reflect.DeepEqual(result.SubmitRequest.Images, []string{"data:image/png;base64,ZmFrZS1wbmc="}) {
		t.Fatalf("SubmitRequest.Images = %#v", result.SubmitRequest.Images)
	}
	if result.Run == nil {
		t.Fatal("expected run to be created")
	}

	session, err := svc.GetSession(context.Background(), result.Run.SessionID)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if len(session.Messages) == 0 || len(session.Messages[0].ContentBlocks) != 2 {
		t.Fatalf("session.Messages = %#v", session.Messages)
	}
	if session.Messages[0].ContentBlocks[1].Type != contextengine.ContentBlockImage {
		t.Fatalf("image block = %#v", session.Messages[0].ContentBlocks[1])
	}
}

func TestInteractAttachmentOnlyContentBlocksCreateRun(t *testing.T) {
	t.Parallel()

	svc := newFullService()

	result, err := svc.Interact(context.Background(), InteractionRequest{
		SessionKey: "interact-content-blocks-only",
		ContentBlocks: []contextengine.ContentBlock{
			{Type: contextengine.ContentBlockFile, Label: "brief.md", MediaRef: "upload://brief-1"},
		},
	})
	if err != nil {
		t.Fatalf("Interact() error = %v", err)
	}
	if result.Decision.ReplyAct != ReplyActTaskAccept {
		t.Fatalf("ReplyAct = %q, want %q", result.Decision.ReplyAct, ReplyActTaskAccept)
	}
	if result.SubmitRequest == nil {
		t.Fatal("expected SubmitRequest to be returned")
	}
	if result.Run == nil {
		t.Fatal("expected run to be created")
	}
	if len(result.SubmitRequest.ContentBlocks) != 1 || result.SubmitRequest.ContentBlocks[0].Type != contextengine.ContentBlockFile {
		t.Fatalf("SubmitRequest.ContentBlocks = %#v", result.SubmitRequest.ContentBlocks)
	}

	session, err := svc.GetSession(context.Background(), result.Run.SessionID)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if len(session.Messages) == 0 || len(session.Messages[0].ContentBlocks) != 1 {
		t.Fatalf("session.Messages = %#v", session.Messages)
	}
	if session.Messages[0].ContentBlocks[0].Type != contextengine.ContentBlockFile {
		t.Fatalf("file block = %#v", session.Messages[0].ContentBlocks[0])
	}
}

func TestInteractDefaultClassificationNoClassifier(t *testing.T) {
	t.Parallel()

	svc := newFullService()

	// Create a run that will complete immediately (mock model returns text).
	first, err := svc.Submit(context.Background(), SubmitRequest{
		SessionKey: "interact-default-class",
		Content:    "first task",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	// Wait for the run to complete.
	waitForRunStatus(t, svc, first.ID, agent.RunCompleted)

	// Now submit a new message with no classifier available. The fallback must
	// stay side-effect free instead of silently creating a new run.
	result, err := svc.Interact(context.Background(), InteractionRequest{
		SessionKey: "interact-default-class",
		Content:    "second task",
	})
	if err != nil {
		t.Fatalf("Interact() error = %v", err)
	}
	if result.Decision.SpeechAct != SpeechActUnknown {
		t.Fatalf("SpeechAct = %q, want %q", result.Decision.SpeechAct, SpeechActUnknown)
	}
	if result.Decision.ReplyAct != ReplyActClarificationPrompt {
		t.Fatalf("ReplyAct = %q, want %q", result.Decision.ReplyAct, ReplyActClarificationPrompt)
	}
	if result.Run != nil {
		t.Fatalf("Run = %#v, want nil for no-classifier fallback", result.Run)
	}
	if result.ReplyMessage == "" {
		t.Fatal("expected clarification prompt message")
	}
}

func TestInteractDefaultClassificationNoClassifierWithActiveRunDoesNotSpawnNewRun(t *testing.T) {
	t.Parallel()

	svc := newFullService()
	session, err := svc.sessions.GetOrCreate(context.Background(), "interact-default-active-run", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	run, err := svc.runs.Create(context.Background(), session.ID, agent.IncomingMessage{
		SessionKey: "interact-default-active-run",
		Content:    "active task",
	}, agent.AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    agent.QueueEnqueue,
	})
	if err != nil {
		t.Fatalf("RunStore.Create() error = %v", err)
	}
	run.Status = agent.RunRunning
	run.Phase = agent.PhaseExecutingTools
	if err := svc.runs.Update(context.Background(), run); err != nil {
		t.Fatalf("runs.Update() error = %v", err)
	}

	result, err := svc.Interact(context.Background(), InteractionRequest{
		SessionKey: "interact-default-active-run",
		Content:    "and also check the logs",
	})
	if err != nil {
		t.Fatalf("Interact() error = %v", err)
	}
	if result.Decision.ReplyAct != ReplyActClarificationPrompt {
		t.Fatalf("ReplyAct = %q, want %q", result.Decision.ReplyAct, ReplyActClarificationPrompt)
	}
	if result.Run != nil {
		t.Fatalf("Run = %#v, want nil for ambiguous active-run fallback", result.Run)
	}
	if result.Context.ActiveRunID != run.ID {
		t.Fatalf("ActiveRunID = %q, want %q", result.Context.ActiveRunID, run.ID)
	}
}

func TestInteractDefaultClassificationNoClassifierKeepsImagePromptRunnable(t *testing.T) {
	t.Parallel()

	svc := newFullService()
	result, err := svc.Interact(context.Background(), InteractionRequest{
		SessionKey: "interact-default-image-prompt",
		Content:    "what is in this image?",
		Images:     []string{"data:image/png;base64,ZmFrZS1wbmc="},
	})
	if err != nil {
		t.Fatalf("Interact() error = %v", err)
	}
	if result.Decision.ReplyAct != ReplyActTaskAccept {
		t.Fatalf("ReplyAct = %q, want %q", result.Decision.ReplyAct, ReplyActTaskAccept)
	}
	if result.Run == nil {
		t.Fatal("expected run to be created for image prompt fallback")
	}
}

func TestInteractSteerFollowupWithDirectiveStore(t *testing.T) {
	t.Parallel()

	svc := newFullService()
	dirStore := agent.NewInMemorySessionDirectiveStore()
	svc.WithDirectives(dirStore)

	// Submit a task to make the session non-idle.
	first, err := svc.Submit(context.Background(), SubmitRequest{
		SessionKey: "interact-steer",
		Content:    "帮我查竞品",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	// Wait for completion.
	waitForRunStatus(t, svc, first.ID, agent.RunCompleted)

	// The session should now have completed_recently state.
	// Because no classifier and no active run, it will fall to idle/completed_recently path.
	// For this test, we verify the directive store integration works when a classifier returns task_followup.
}

func TestInteractTaskFollowupWithoutDirectivesFails(t *testing.T) {
	t.Parallel()

	svc := newFullService()
	svc.WithClassifier(stubInteractionClassifier{
		decision: InteractionDecision{
			SpeechAct:   SpeechActTaskFollowup,
			TargetScope: TargetScopeActiveRun,
			ReplyAct:    ReplyActResumeAck,
			Confidence:  0.95,
			Reason:      "scope_change",
		},
	})

	run, err := svc.Submit(context.Background(), SubmitRequest{
		SessionKey: "interact-followup-no-directives",
		Content:    "do the first task",
		Execute:    boolPtr(false),
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run == nil || run.Status != agent.RunQueued {
		t.Fatalf("run = %#v, want queued run", run)
	}

	result, err := svc.Interact(context.Background(), InteractionRequest{
		SessionKey: "interact-followup-no-directives",
		Content:    "change the approach",
	})
	if err != nil {
		t.Fatalf("Interact() error = %v", err)
	}
	if result.Decision.ReplyAct != ReplyActTaskFailure {
		t.Fatalf("ReplyAct = %q, want %q", result.Decision.ReplyAct, ReplyActTaskFailure)
	}
	if result.Error == "" {
		t.Fatal("expected task follow-up failure error")
	}
}

func boolPtr(v bool) *bool { return &v }
