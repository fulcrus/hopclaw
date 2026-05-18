package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/eventbus"
	verifyrt "github.com/fulcrus/hopclaw/runtime/verify"
)

func TestGetRunResultMarksNeedsConfirmationForWaitingInput(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()

	session, err := sessions.GetOrCreate(ctx, "chat-needs-confirmation", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	run, err := runs.Create(ctx, session.ID, agent.IncomingMessage{
		SessionKey: "chat-needs-confirmation",
		Content:    "send this to the client",
	}, agent.AgentConfig{DefaultModel: "test-model", QueueMode: agent.QueueEnqueue})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	run.Status = agent.RunWaitingInput
	run.Preflight = &agent.RunPreflightReport{
		State:    agent.RunPreflightNeedsConfirmation,
		Blocking: true,
		Question: "Which client should receive it?",
	}
	if err := runs.Update(ctx, run); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	svc := NewService(nil, sessions, runs, nil, nil, nil)
	result, err := svc.GetRunResult(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRunResult() error = %v", err)
	}
	if result.Outcome != RunOutcomeNeedsConfirmation {
		t.Fatalf("Outcome = %q, want %q", result.Outcome, RunOutcomeNeedsConfirmation)
	}
}

func TestGetRunResultKeepsCompletedForAdvisoryWarningWithUsableOutput(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	bus := eventbus.NewInMemoryBus()

	session, err := sessions.GetOrCreate(ctx, "doc-partial", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	run, err := runs.Create(ctx, session.ID, agent.IncomingMessage{
		SessionKey: "doc-partial",
		Content:    "summarize the document",
	}, agent.AgentConfig{DefaultModel: "test-model", QueueMode: agent.QueueEnqueue})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	now := time.Now().UTC()
	run.Status = agent.RunCompleted
	run.StartedAt = now.Add(-2 * time.Second)
	run.FinishedAt = now
	if err := runs.Update(ctx, run); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	session.Messages = append(session.Messages, contextengine.Message{
		Role:      contextengine.RoleAssistant,
		Content:   "I prepared the summary for the document.",
		CreatedAt: now,
		Metadata:  map[string]any{"run_id": run.ID},
	})
	if err := sessions.Save(ctx, session); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if err := bus.Publish(ctx, eventbus.Event{
		Type:      eventbus.EventToolExecuted,
		RunID:     run.ID,
		SessionID: session.ID,
		Attrs: map[string]any{
			"tool_names": []string{"document.read"},
		},
	}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	svc := NewService(nil, sessions, runs, nil, bus, nil)
	result, err := svc.GetRunResult(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRunResult() error = %v", err)
	}
	if result.Outcome != RunOutcomeCompleted {
		t.Fatalf("Outcome = %q, want %q", result.Outcome, RunOutcomeCompleted)
	}
	if result.VerificationStatus != string(verifyrt.StatusWarning) {
		t.Fatalf("VerificationStatus = %q, want %q", result.VerificationStatus, verifyrt.StatusWarning)
	}
	if result.VerificationSummary != "verification finished with 1 advisory warning" {
		t.Fatalf("VerificationSummary = %q", result.VerificationSummary)
	}
}

func TestDeriveRunOutcomeMarksPartialForRequiredWarningWithUsableOutput(t *testing.T) {
	t.Parallel()

	run := &agent.Run{Status: agent.RunCompleted}
	result := &RunResult{
		Status: agent.RunCompleted,
		Output: "usable output",
	}
	verification := &verifyrt.RunVerification{
		Status:           verifyrt.StatusWarning,
		RequiredWarnings: 1,
		Warnings:         1,
	}

	if got := DeriveRunOutcome(run, result, verification); got != RunOutcomePartial {
		t.Fatalf("Outcome = %q, want %q", got, RunOutcomePartial)
	}
}

func TestDeriveRunOutcomeMarksFailedForBlockingVerification(t *testing.T) {
	t.Parallel()

	run := &agent.Run{Status: agent.RunCompleted}
	result := &RunResult{
		Status: agent.RunCompleted,
		Output: "usable output",
	}
	verification := &verifyrt.RunVerification{
		Status:           verifyrt.StatusFailed,
		RequiredFailures: 1,
		BlockingFailures: 1,
	}

	if got := DeriveRunOutcome(run, result, verification); got != RunOutcomeFailed {
		t.Fatalf("Outcome = %q, want %q", got, RunOutcomeFailed)
	}
}

func TestDeriveRunOutcomeMarksFailedForTerminalWorkflowFailure(t *testing.T) {
	t.Parallel()

	run := &agent.Run{
		Status: agent.RunCompleted,
		WorkflowState: &agent.WorkflowState{
			TerminalOutcome: agent.WorkflowTerminalOutcomeFailed,
			TerminalReason:  "workflow auto-continuation stopped: continuation admission denied",
		},
	}

	if got := DeriveRunOutcome(run, nil, nil); got != RunOutcomeFailed {
		t.Fatalf("Outcome = %q, want %q", got, RunOutcomeFailed)
	}
}

func TestGetRunResultMarksPartialAfterRecoveredToolExecutionFailure(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()

	session, err := sessions.GetOrCreate(ctx, "tool-recovery-partial", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	run, err := runs.Create(ctx, session.ID, agent.IncomingMessage{
		SessionKey: "tool-recovery-partial",
		Content:    "inspect the file",
	}, agent.AgentConfig{DefaultModel: "test-model", QueueMode: agent.QueueEnqueue})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	now := time.Now().UTC()
	run.Status = agent.RunCompleted
	run.StartedAt = now.Add(-2 * time.Second)
	run.FinishedAt = now
	if err := runs.Update(ctx, run); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	session.Messages = append(session.Messages,
		contextengine.Message{
			Role:       contextengine.RoleTool,
			Name:       "fs.read",
			ToolCallID: "call-1",
			Content:    `{"status":"error","tool_execution_error":true,"tool_name":"fs.read","tool_call_id":"call-1","error":"permission denied"}`,
			CreatedAt:  now.Add(-time.Second),
		},
		contextengine.Message{
			Role:      contextengine.RoleAssistant,
			Content:   "I could not access the file, so this answer is partial.",
			CreatedAt: now,
			Metadata:  map[string]any{"run_id": run.ID},
		},
	)
	if err := sessions.Save(ctx, session); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	svc := NewService(nil, sessions, runs, nil, nil, nil)
	result, err := svc.GetRunResult(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRunResult() error = %v", err)
	}
	if result.Outcome != RunOutcomePartial {
		t.Fatalf("Outcome = %q, want %q", result.Outcome, RunOutcomePartial)
	}
	if result.VerificationStatus != string(verifyrt.StatusWarning) {
		t.Fatalf("VerificationStatus = %q, want %q", result.VerificationStatus, verifyrt.StatusWarning)
	}
	if result.VerificationSummary == "" {
		t.Fatal("expected verification summary")
	}
}

func TestGetRunResultIncludesTaskContract(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()

	session, err := sessions.GetOrCreate(ctx, "contract-result", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	run, err := runs.Create(ctx, session.ID, agent.IncomingMessage{
		SessionKey: "contract-result",
		Content:    "整理周报",
	}, agent.AgentConfig{DefaultModel: "test-model", QueueMode: agent.QueueEnqueue})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	now := time.Now().UTC()
	run.Status = agent.RunCompleted
	run.StartedAt = now.Add(-2 * time.Second)
	run.FinishedAt = now
	run.TaskContract = &agent.TaskContract{
		Goal:    "整理周报",
		JobType: "report",
		ExpectedDeliverables: []agent.TaskContractDeliverable{{
			Kind:     "document",
			Required: true,
		}},
	}
	if err := runs.Update(ctx, run); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	session.Messages = append(session.Messages, contextengine.Message{
		Role:      contextengine.RoleAssistant,
		Content:   "周报已整理。",
		CreatedAt: now,
		Metadata:  map[string]any{"run_id": run.ID},
	})
	if err := sessions.Save(ctx, session); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	svc := NewService(nil, sessions, runs, nil, nil, nil)
	result, err := svc.GetRunResult(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRunResult() error = %v", err)
	}
	if result.TaskContract == nil {
		t.Fatal("expected task contract in run result")
	}
	if result.TaskContract.JobType != "report" {
		t.Fatalf("result.TaskContract.JobType = %q", result.TaskContract.JobType)
	}
}
