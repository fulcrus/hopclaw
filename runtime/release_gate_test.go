package runtime

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/internal/meta"
)

func TestDispatchRunReleaseGateRequiresApprovalForHighRiskFirstRun(t *testing.T) {
	t.Parallel()

	svc, _, _ := newReleaseGateService()
	execute := false
	run, err := svc.Submit(context.Background(), SubmitRequest{
		SessionKey: "release-gate-high-risk",
		Content:    "Deploy the latest build to staging.",
		Execute:    &execute,
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.Status != agent.RunQueued {
		t.Fatalf("run.Status = %q, want queued", run.Status)
	}

	if err := svc.dispatchRun(context.Background(), run.ID, false); err != nil {
		t.Fatalf("dispatchRun() error = %v", err)
	}

	run = waitForRunStatus(t, svc, run.ID, agent.RunWaitingApproval)
	if run.ApprovalID == "" {
		t.Fatal("expected release gate approval id")
	}
	if !strings.Contains(run.Error, "release readiness blocked") {
		t.Fatalf("run.Error = %q", run.Error)
	}
	ticket, err := svc.GetApproval(context.Background(), run.ApprovalID)
	if err != nil {
		t.Fatalf("GetApproval() error = %v", err)
	}
	if !releaseGateApprovalTicket(ticket) {
		t.Fatalf("ticket.Metadata = %#v, want release gate marker", ticket.Metadata)
	}
	if got := strings.TrimSpace(ticket.Metadata["policy_source"].(string)); got != releaseGatePolicySource {
		t.Fatalf("policy_source = %q, want %q", got, releaseGatePolicySource)
	}
}

func TestResolveApprovalBypassesReleaseGateAndStartsRun(t *testing.T) {
	t.Parallel()

	svc, _, _ := newReleaseGateService()
	execute := false
	run, err := svc.Submit(context.Background(), SubmitRequest{
		SessionKey: "release-gate-resume",
		Content:    "Deploy the latest build to staging.",
		Execute:    &execute,
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if err := svc.dispatchRun(context.Background(), run.ID, false); err != nil {
		t.Fatalf("dispatchRun() error = %v", err)
	}
	run = waitForRunStatus(t, svc, run.ID, agent.RunWaitingApproval)

	if _, err := svc.ResolveApproval(context.Background(), run.ApprovalID, approval.Resolution{
		Status:     approval.StatusApproved,
		ResolvedBy: "test",
	}); err != nil {
		t.Fatalf("ResolveApproval() error = %v", err)
	}

	run = waitForRunStatus(t, svc, run.ID, agent.RunCompleted)
	if run.StartedAt.IsZero() {
		t.Fatal("expected run to start after release gate approval")
	}
}

func TestDispatchRunReleaseGateAllowsLowRiskRun(t *testing.T) {
	t.Parallel()

	svc, _, _ := newReleaseGateService()
	execute := false
	run, err := svc.Submit(context.Background(), SubmitRequest{
		SessionKey: "release-gate-low-risk",
		Content:    "Summarize it into a markdown report.",
		Execute:    &execute,
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	if err := svc.dispatchRun(context.Background(), run.ID, false); err != nil {
		t.Fatalf("dispatchRun() error = %v", err)
	}

	run = waitForRunStatus(t, svc, run.ID, agent.RunCompleted)
	if run.ApprovalID != "" {
		t.Fatalf("run.ApprovalID = %q, want empty", run.ApprovalID)
	}
}

func TestDispatchRunReleaseGateAllowsHighRiskRunWhenReadinessPasses(t *testing.T) {
	t.Parallel()

	svc, sessions, runs := newReleaseGateService()
	seedReleaseReadinessReadyRunsForSession(t, sessions, runs, "release-gate-ready")

	execute := false
	run, err := svc.Submit(context.Background(), SubmitRequest{
		SessionKey: "release-gate-ready",
		Content:    "Deploy the latest build to staging.",
		Execute:    &execute,
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if err := svc.dispatchRun(context.Background(), run.ID, false); err != nil {
		t.Fatalf("dispatchRun() error = %v", err)
	}

	run = waitForRunStatus(t, svc, run.ID, agent.RunCompleted)
	if run.ApprovalID != "" {
		t.Fatalf("run.ApprovalID = %q, want empty", run.ApprovalID)
	}
}

func TestDispatchRunReleaseGateDoesNotUseOtherSessionEvidence(t *testing.T) {
	t.Parallel()

	svc, sessions, runs := newReleaseGateService()
	seedReleaseReadinessReadyRunsForSession(t, sessions, runs, "release-gate-other-session")

	execute := false
	run, err := svc.Submit(context.Background(), SubmitRequest{
		SessionKey: "release-gate-isolated",
		Content:    "Deploy the latest build to staging.",
		Execute:    &execute,
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if err := svc.dispatchRun(context.Background(), run.ID, false); err != nil {
		t.Fatalf("dispatchRun() error = %v", err)
	}

	run = waitForRunStatus(t, svc, run.ID, agent.RunWaitingApproval)
	if run.ApprovalID == "" {
		t.Fatal("expected release gate approval id")
	}
}

func newReleaseGateService() (*Service, *agent.InMemorySessionStore, *agent.InMemoryRunStore) {
	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	queue := agent.NewInMemoryCoordinator()
	approvals := approval.NewInMemoryStore()
	component := agent.NewComponent(agent.AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 3,
		QueueMode:     agent.QueueEnqueue,
	}, sessions, runs, queue, newContextEngine(), mockModelClient{}, nil, nil).
		WithPreflightAnalyzer(testPreflightAnalyzer{}).
		WithApprovals(approvals)
	svc := NewService(component, sessions, runs, approvals, eventbus.NewInMemoryBus(), nil).
		WithReleaseExecutionGate(DefaultReleaseExecutionGatePolicy())
	return svc, sessions, runs
}

func seedReleaseReadinessReadyRuns(t *testing.T, sessions *agent.InMemorySessionStore, runs *agent.InMemoryRunStore, prefix string) {
	t.Helper()

	for i := 0; i < DefaultReleaseReadinessThresholds().MinTerminalRuns; i++ {
		seedCompletedRunForReleaseReadiness(t, sessions, runs, prefix+"-"+string(rune('a'+i)), "done")
	}
}

func seedReleaseReadinessReadyRunsForSession(t *testing.T, sessions *agent.InMemorySessionStore, runs *agent.InMemoryRunStore, sessionKey string) {
	t.Helper()

	for i := 0; i < DefaultReleaseReadinessThresholds().MinTerminalRuns; i++ {
		seedCompletedRunForReleaseReadiness(t, sessions, runs, sessionKey, "done")
	}
}

func seedCompletedRunForReleaseReadiness(t *testing.T, sessions *agent.InMemorySessionStore, runs *agent.InMemoryRunStore, sessionKey, output string) {
	t.Helper()

	ctx := context.Background()
	session, err := sessions.GetOrCreate(ctx, sessionKey, "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	run, err := runs.Create(ctx, session.ID, agent.IncomingMessage{
		SessionKey: sessionKey,
		Content:    "say done",
	}, agent.AgentConfig{DefaultModel: "test-model"})
	if err != nil {
		t.Fatalf("runs.Create() error = %v", err)
	}
	loaded, unlock, err := sessions.LoadForExecution(ctx, session.ID)
	if err != nil {
		t.Fatalf("LoadForExecution() error = %v", err)
	}
	loaded.Messages = append(loaded.Messages, contextengine.Message{
		Role:    contextengine.RoleAssistant,
		Content: output,
		Metadata: map[string]any{
			meta.KeyRunID: run.ID,
		},
	})
	loaded.MessageCount = len(loaded.Messages)
	if err := sessions.Save(ctx, loaded); err != nil {
		unlock()
		t.Fatalf("sessions.Save() error = %v", err)
	}
	unlock()

	run.Status = agent.RunCompleted
	run.StartedAt = time.Unix(100, 0).UTC()
	run.FinishedAt = run.StartedAt.Add(1500 * time.Millisecond)
	if err := runs.Update(ctx, run); err != nil {
		t.Fatalf("runs.Update() error = %v", err)
	}
}
