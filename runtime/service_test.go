package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/artifact"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/eventbus"
	controlsnapshot "github.com/fulcrus/hopclaw/internal/controlplane/snapshot"
	"github.com/fulcrus/hopclaw/internal/meta"
	"github.com/fulcrus/hopclaw/policy"
)

// ---------------------------------------------------------------------------
// test helpers
// ---------------------------------------------------------------------------

type mockModelClient struct{}

func (mockModelClient) Chat(_ context.Context, _ agent.ChatRequest) (*agent.ModelResponse, error) {
	return &agent.ModelResponse{
		Message: contextengine.Message{Role: contextengine.RoleAssistant, Content: "ok"},
	}, nil
}

func preflightCheckExists(report *agent.RunPreflightReport, id string) bool {
	if report == nil {
		return false
	}
	for _, check := range report.Checks {
		if check.ID == id {
			return true
		}
	}
	return false
}

type toolCallModelClient struct {
	responses []*agent.ModelResponse
	index     int
}

func (m *toolCallModelClient) Chat(_ context.Context, _ agent.ChatRequest) (*agent.ModelResponse, error) {
	if len(m.responses) == 0 {
		return &agent.ModelResponse{}, nil
	}
	if m.index >= len(m.responses) {
		return m.responses[len(m.responses)-1], nil
	}
	response := m.responses[m.index]
	m.index++
	return response, nil
}

type requireApprovalPolicyEngine struct{}

func (requireApprovalPolicyEngine) EvaluateTool(context.Context, policy.ToolContext) (policy.Decision, error) {
	return policy.Decision{
		Action:       policy.ActionRequireApproval,
		Reasons:      []string{"tool requires approval by test policy"},
		PolicySource: "test.policy/runtime",
		Summary:      "approval required by runtime test policy",
	}, nil
}

type noOpToolExecutor struct{}

func (noOpToolExecutor) ExecuteBatch(context.Context, *agent.Run, *agent.Session, []agent.ToolCall) ([]contextengine.ToolResult, error) {
	return nil, nil
}

type testPreflightAnalyzer struct{}

func (testPreflightAnalyzer) Analyze(_ context.Context, req agent.PreflightAnalysisRequest) (agent.PreflightAnalysis, error) {
	if strings.Contains(req.Message, "这个文件") || strings.Contains(strings.ToLower(req.Message), "this file") {
		return agent.PreflightAnalysis{NeedsReference: true}, nil
	}
	// Simulate model detecting browser intent from natural language.
	// The real triage model would return browser domain for "打开页面" / "open a page"
	// even without a URL, then preflight detects the missing reference.
	lower := strings.ToLower(req.Message)
	if strings.Contains(lower, "打开页面") || strings.Contains(lower, "open page") || strings.Contains(lower, "open a page") {
		return agent.PreflightAnalysis{
			SuggestedDomains:  []string{"browser"},
			DomainsSpecified:  true,
			NeedsReference:    true,
			NeedsReferenceSet: true,
		}, nil
	}
	return agent.PreflightAnalysis{}, nil
}

func newContextEngine() contextengine.ContextEngine {
	return contextengine.NewSlidingWindowEngine(contextengine.Config{
		BaseSystemPrompt:     "test",
		DefaultContextWindow: 4000,
		DefaultOutputTokens:  1000,
	}, nil)
}

func newAgentComponent() *agent.AgentComponent {
	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	coordinator := agent.NewInMemoryCoordinator()
	ctxEngine := newContextEngine()
	model := mockModelClient{}
	return agent.NewComponent(agent.AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 3,
		QueueMode:     agent.QueueEnqueue,
	}, sessions, runs, coordinator, ctxEngine, model, nil, nil)
}

func newFullService() *Service {
	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	queue := agent.NewInMemoryCoordinator()
	approvals := approval.NewInMemoryStore()
	artifacts := artifact.NewInMemoryStore()
	bus := eventbus.NewInMemoryBus()
	comp := agent.NewComponent(agent.AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 3,
		QueueMode:     agent.QueueEnqueue,
	}, sessions, runs, queue, newContextEngine(), mockModelClient{}, nil, nil).
		WithPreflightAnalyzer(testPreflightAnalyzer{})
	return NewService(comp, sessions, runs, approvals, bus, artifacts)
}

func TestGetGovernanceSnapshotIncludesPolicyScopeAndConfig(t *testing.T) {
	t.Parallel()

	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	approvals := approval.NewInMemoryStore()
	model := &toolCallModelClient{
		responses: []*agent.ModelResponse{{
			ToolCalls: []agent.ToolCall{{
				ID:   "call-1",
				Name: "fs.write",
				Input: map[string]any{
					"path": "notes.txt",
				},
			}},
		}},
	}
	component := agent.NewComponent(agent.AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 2,
		QueueMode:     agent.QueueEnqueue,
	}, sessions, runs, agent.NewInMemoryCoordinator(), newContextEngine(), model, noOpToolExecutor{}, nil).
		WithPolicy(requireApprovalPolicyEngine{}).
		WithApprovals(approvals)
	svc := NewService(component, sessions, runs, approvals, eventbus.NewInMemoryBus(), nil).
		WithEffectiveConfigSnapshot(&controlsnapshot.EffectiveConfigSnapshot{
			ID: "ecs-governance-1",
		})

	execute := false
	run, err := svc.Submit(context.Background(), SubmitRequest{
		SessionKey:   "chat-governance-snapshot",
		Content:      "write file",
		AutomationID: "auto-42",
		Execute:      &execute,
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.Governance == nil || run.Governance.EffectiveConfigSnapshotID != "ecs-governance-1" {
		t.Fatalf("run.Governance = %#v", run.Governance)
	}

	if err := svc.dispatchRun(context.Background(), run.ID, false); err != nil {
		t.Fatalf("dispatchRun() error = %v", err)
	}
	run = waitForRunStatus(t, svc, run.ID, agent.RunWaitingApproval)

	snapshot, err := svc.GetGovernanceSnapshot(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("GetGovernanceSnapshot() error = %v", err)
	}
	if snapshot.EffectiveConfigSnapshotID != "ecs-governance-1" {
		t.Fatalf("EffectiveConfigSnapshotID = %q", snapshot.EffectiveConfigSnapshotID)
	}
	if snapshot.Scope.AutomationID != "auto-42" {
		t.Fatalf("Scope = %#v", snapshot.Scope)
	}
	if snapshot.Policy == nil || snapshot.Policy.PolicySource == "" || snapshot.Policy.Summary == "" {
		t.Fatalf("Policy = %#v", snapshot.Policy)
	}
	if snapshot.Approval == nil || snapshot.Approval.Status != approval.StatusPending {
		t.Fatalf("Approval = %#v", snapshot.Approval)
	}
	if _, err := approvals.UpsertExternalRef(context.Background(), run.ApprovalID, approval.ExternalReference{
		Provider:   "jira",
		ExternalID: "JIRA-900",
		URL:        "https://jira.example.com/browse/JIRA-900",
		Status:     "pending_remote",
		SyncedAt:   time.Date(2026, 3, 19, 10, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("UpsertExternalRef() error = %v", err)
	}
	snapshot, err = svc.GetGovernanceSnapshot(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("GetGovernanceSnapshot() error after external ref = %v", err)
	}
	if snapshot.Approval == nil || len(snapshot.Approval.External) != 1 {
		t.Fatalf("Approval.External = %#v", snapshot.Approval)
	}
	if snapshot.Approval.External[0].Provider != "jira" || snapshot.Approval.External[0].ExternalID != "JIRA-900" {
		t.Fatalf("Approval.External[0] = %#v", snapshot.Approval.External[0])
	}
	if len(snapshot.PolicyToolNames) != 1 || snapshot.PolicyToolNames[0] != "fs.write" {
		t.Fatalf("PolicyToolNames = %#v", snapshot.PolicyToolNames)
	}
}

func TestSubmitForwardsImagesToSessionMessages(t *testing.T) {
	t.Parallel()

	svc := newFullService()

	run, err := svc.Submit(context.Background(), SubmitRequest{
		SessionKey: "runtime-submit-images",
		Content:    "describe the screenshot",
		Images: []string{
			"data:image/png;base64,ZmFrZS1wbmc=",
			"cmF3LWpwZWc=",
		},
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	session, err := svc.GetSession(context.Background(), run.SessionID)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if len(session.Messages) == 0 {
		t.Fatal("expected user message to be recorded")
	}
	msg := session.Messages[0]
	if len(msg.ContentBlocks) != 3 {
		t.Fatalf("len(ContentBlocks) = %d, want 3", len(msg.ContentBlocks))
	}
	if msg.ContentBlocks[0].Type != contextengine.ContentBlockText || msg.ContentBlocks[0].Text != "describe the screenshot" {
		t.Fatalf("text block = %#v", msg.ContentBlocks[0])
	}
	if msg.ContentBlocks[1].Type != contextengine.ContentBlockImage || msg.ContentBlocks[1].MediaType != "image/png" {
		t.Fatalf("image block[1] = %#v", msg.ContentBlocks[1])
	}
	if msg.ContentBlocks[1].MediaRef == "" || msg.ContentBlocks[1].Data != "" {
		t.Fatalf("image block[1] should use media_ref only: %#v", msg.ContentBlocks[1])
	}
	if msg.ContentBlocks[2].Type != contextengine.ContentBlockImage || msg.ContentBlocks[2].MediaType != "image/jpeg" {
		t.Fatalf("image block[2] = %#v", msg.ContentBlocks[2])
	}
	if msg.ContentBlocks[2].MediaRef == "" || msg.ContentBlocks[2].Data != "" {
		t.Fatalf("image block[2] should use media_ref only: %#v", msg.ContentBlocks[2])
	}
	body, contentType, err := svc.artifacts.Read(context.Background(), msg.ContentBlocks[1].MediaRef)
	if err != nil {
		t.Fatalf("artifacts.Read(image1) error = %v", err)
	}
	if contentType != "image/png" || string(body) != "fake-png" {
		t.Fatalf("artifact image1 = %q/%q", contentType, string(body))
	}
	body, contentType, err = svc.artifacts.Read(context.Background(), msg.ContentBlocks[2].MediaRef)
	if err != nil {
		t.Fatalf("artifacts.Read(image2) error = %v", err)
	}
	if contentType != "image/jpeg" || string(body) != "raw-jpeg" {
		t.Fatalf("artifact image2 = %q/%q", contentType, string(body))
	}
}

func TestGetApprovalViewIncludesGovernance(t *testing.T) {
	t.Parallel()

	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	approvals := approval.NewInMemoryStore()
	model := &toolCallModelClient{
		responses: []*agent.ModelResponse{{
			ToolCalls: []agent.ToolCall{{
				ID:   "call-1",
				Name: "fs.write",
			}},
		}},
	}
	component := agent.NewComponent(agent.AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 2,
		QueueMode:     agent.QueueEnqueue,
	}, sessions, runs, agent.NewInMemoryCoordinator(), newContextEngine(), model, noOpToolExecutor{}, nil).
		WithPolicy(requireApprovalPolicyEngine{}).
		WithApprovals(approvals)
	svc := NewService(component, sessions, runs, approvals, eventbus.NewInMemoryBus(), nil).
		WithEffectiveConfigSnapshot(&controlsnapshot.EffectiveConfigSnapshot{
			ID: "ecs-approval-1",
		})

	execute := false
	run, err := svc.Submit(context.Background(), SubmitRequest{
		SessionKey:   "chat-approval-view",
		Content:      "write file",
		AutomationID: "auto-view",
		Execute:      &execute,
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if err := svc.dispatchRun(context.Background(), run.ID, false); err != nil {
		t.Fatalf("dispatchRun() error = %v", err)
	}
	run = waitForRunStatus(t, svc, run.ID, agent.RunWaitingApproval)

	view, err := svc.GetApprovalView(context.Background(), run.ApprovalID)
	if err != nil {
		t.Fatalf("GetApprovalView() error = %v", err)
	}
	if view.Governance == nil {
		t.Fatalf("view = %#v", view)
	}
	if view.Governance.EffectiveConfigSnapshotID != "ecs-approval-1" {
		t.Fatalf("EffectiveConfigSnapshotID = %q", view.Governance.EffectiveConfigSnapshotID)
	}
	if view.Governance.Scope.AutomationID != "auto-view" {
		t.Fatalf("Scope = %#v", view.Governance.Scope)
	}
	if view.Governance.Policy == nil || view.Governance.Policy.PolicySource != "test.policy/runtime" {
		t.Fatalf("Policy = %#v", view.Governance.Policy)
	}
	if view.Governance.Approval == nil || view.Governance.Approval.Status != approval.StatusPending {
		t.Fatalf("Approval = %#v", view.Governance.Approval)
	}
	if _, err := approvals.UpsertExternalRef(context.Background(), run.ApprovalID, approval.ExternalReference{
		Provider:   "jira",
		ExternalID: "JIRA-901",
		Status:     "pending_remote",
		SyncedAt:   time.Date(2026, 3, 19, 10, 5, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("UpsertExternalRef() error = %v", err)
	}
	view, err = svc.GetApprovalView(context.Background(), run.ApprovalID)
	if err != nil {
		t.Fatalf("GetApprovalView() error after external ref = %v", err)
	}
	if view.Governance.Approval == nil || len(view.Governance.Approval.External) != 1 {
		t.Fatalf("Governance.Approval.External = %#v", view.Governance.Approval)
	}
	if view.Governance.Approval.External[0].Provider != "jira" || view.Governance.Approval.External[0].ExternalID != "JIRA-901" {
		t.Fatalf("Governance.Approval.External[0] = %#v", view.Governance.Approval.External[0])
	}
	if !strings.Contains(view.Governance.Summary, "providers=jira") {
		t.Fatalf("Governance.Summary = %q", view.Governance.Summary)
	}
	if len(view.Governance.ToolNames) != 1 || view.Governance.ToolNames[0] != "fs.write" {
		t.Fatalf("ToolNames = %#v", view.Governance.ToolNames)
	}
}

func TestDispatchRunRecordsMemoryVerificationForRecalledMemories(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	queue := agent.NewInMemoryCoordinator()
	approvals := approval.NewInMemoryStore()
	artifacts := artifact.NewInMemoryStore()
	bus := eventbus.NewInMemoryBus()
	memory := agent.NewGovernedMemoryStore(agent.NewInMemoryKVStore())
	component := agent.NewComponent(agent.AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 3,
		QueueMode:     agent.QueueEnqueue,
	}, sessions, runs, queue, newContextEngine(), mockModelClient{}, nil, nil).
		WithMemoryStore(memory)
	svc := NewService(component, sessions, runs, approvals, bus, artifacts).WithMemoryStore(memory)

	if _, err := memory.UpsertRecord(ctx, agent.MemoryRecord{
		Namespace: "profile",
		ScopeKey:  "user",
		Field:     "reply_language",
		Value:     "zh-CN",
		Source:    agent.MemorySourceAgent,
	}); err != nil {
		t.Fatalf("UpsertRecord(global) error = %v", err)
	}
	sessionScoped, err := memory.UpsertRecord(ctx, agent.MemoryRecord{
		Namespace:  "workspace",
		ScopeKey:   "chat-memory-verification",
		Field:      "repo_name",
		Value:      "HopClaw",
		Source:     agent.MemorySourceAgent,
		SessionKey: "chat-memory-verification",
	})
	if err != nil {
		t.Fatalf("UpsertRecord(session) error = %v", err)
	}
	otherSession, err := memory.UpsertRecord(ctx, agent.MemoryRecord{
		Namespace:  "workspace",
		ScopeKey:   "chat-other",
		Field:      "repo_name",
		Value:      "OtherRepo",
		Source:     agent.MemorySourceAgent,
		SessionKey: "chat-other",
	})
	if err != nil {
		t.Fatalf("UpsertRecord(otherSession) error = %v", err)
	}

	run, err := svc.Submit(ctx, SubmitRequest{
		SessionKey: "chat-memory-verification",
		Content:    "Summarize the current task.",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	waitForRunStatus(t, svc, run.ID, agent.RunCompleted)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		globalEntry, err := memory.Get(ctx, "profile.user.reply_language")
		if err != nil {
			t.Fatalf("Get(global) error = %v", err)
		}
		sessionEntry, err := memory.Get(ctx, sessionScoped.Key)
		if err != nil {
			t.Fatalf("Get(session) error = %v", err)
		}
		otherEntry, err := memory.Get(ctx, otherSession.Key)
		if err != nil {
			t.Fatalf("Get(otherSession) error = %v", err)
		}
		if globalEntry != nil && sessionEntry != nil &&
			globalEntry.VerificationPassCount == 1 &&
			sessionEntry.VerificationPassCount == 1 &&
			otherEntry != nil && otherEntry.VerificationPassCount == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	globalEntry, _ := memory.Get(ctx, "profile.user.reply_language")
	sessionEntry, _ := memory.Get(ctx, sessionScoped.Key)
	otherEntry, _ := memory.Get(ctx, otherSession.Key)
	t.Fatalf("verification counters not recorded: global=%#v session=%#v other=%#v", globalEntry, sessionEntry, otherEntry)
}

func TestApprovalViewsCompatibilityScopeFilterIsNoop(t *testing.T) {
	t.Parallel()

	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	approvals := approval.NewInMemoryStore()
	svc := NewService(nil, sessions, runs, approvals, eventbus.NewInMemoryBus(), nil)
	ctx := context.Background()

	sessionA, err := sessions.GetOrCreate(ctx, "approval:tenant-a", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate(sessionA) error = %v", err)
	}
	if err := sessions.AppendUserMessage(ctx, sessionA.ID, agent.IncomingMessage{
		Content: "tenant a",
	}); err != nil {
		t.Fatalf("AppendUserMessage(sessionA) error = %v", err)
	}
	runA, err := runs.Create(ctx, sessionA.ID, agent.IncomingMessage{
		Content: "run a",
	}, agent.AgentConfig{DefaultModel: "test-model", QueueMode: agent.QueueEnqueue})
	if err != nil {
		t.Fatalf("runs.Create(runA) error = %v", err)
	}
	ticketA, err := approvals.Create(ctx, approval.Ticket{RunID: runA.ID, SessionID: sessionA.ID})
	if err != nil {
		t.Fatalf("approvals.Create(ticketA) error = %v", err)
	}

	sessionB, err := sessions.GetOrCreate(ctx, "approval:tenant-b", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate(sessionB) error = %v", err)
	}
	if err := sessions.AppendUserMessage(ctx, sessionB.ID, agent.IncomingMessage{
		Content: "tenant b",
	}); err != nil {
		t.Fatalf("AppendUserMessage(sessionB) error = %v", err)
	}
	runB, err := runs.Create(ctx, sessionB.ID, agent.IncomingMessage{
		Content: "run b",
	}, agent.AgentConfig{DefaultModel: "test-model", QueueMode: agent.QueueEnqueue})
	if err != nil {
		t.Fatalf("runs.Create(runB) error = %v", err)
	}
	if _, err := approvals.Create(ctx, approval.Ticket{RunID: runB.ID, SessionID: sessionB.ID}); err != nil {
		t.Fatalf("approvals.Create(ticketB) error = %v", err)
	}

	scope := agent.ScopeFilter{}
	items, err := svc.ListApprovalViewsFiltered(ctx, approval.ListFilter{Status: approval.StatusPending}, scope)
	if err != nil {
		t.Fatalf("ListApprovalViewsFiltered() error = %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("filtered approval views = %#v", items)
	}
	if _, err := svc.GetApprovalViewScoped(ctx, ticketA.ID, scope); err != nil {
		t.Fatalf("GetApprovalViewScoped(ticketA) error = %v", err)
	}
	if _, err := svc.GetApprovalViewScoped(ctx, ticketA.ID, agent.ScopeFilter{}); err != nil {
		t.Fatalf("GetApprovalViewScoped(no-op scope) error = %v", err)
	}
}

func waitForRunStatus(t *testing.T, svc *Service, runID string, want agent.RunStatus) *agent.Run {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		run, err := svc.GetRun(context.Background(), runID)
		if err != nil {
			t.Fatalf("GetRun(%s) error = %v", runID, err)
		}
		if run.Status == want {
			return run
		}
		time.Sleep(10 * time.Millisecond)
	}

	run, err := svc.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetRun(%s) error = %v", runID, err)
	}
	t.Fatalf("run %s status = %q, want %q, error=%q", runID, run.Status, want, run.Error)
	return nil
}

// ---------------------------------------------------------------------------
// NewService
// ---------------------------------------------------------------------------

func TestNewService(t *testing.T) {
	t.Parallel()
	svc := newFullService()
	if svc == nil {
		t.Fatal("NewService returned nil")
	}
	if svc.agent == nil {
		t.Fatal("agent component is nil")
	}
}

func TestNewServiceNilOptionalDeps(t *testing.T) {
	t.Parallel()
	runs := agent.NewInMemoryRunStore()
	svc := NewService(nil, agent.NewInMemorySessionStore(), runs, nil, nil, nil)
	if svc == nil {
		t.Fatal("NewService returned nil with nils")
	}
	if svc.agent != nil {
		t.Fatal("expected nil agent")
	}
	if svc.approvals != nil {
		t.Fatal("expected nil approvals")
	}
	if svc.artifacts != nil {
		t.Fatal("expected nil artifacts")
	}
	if svc.events != nil {
		t.Fatal("expected nil events")
	}
}

func TestWithEffectiveConfigSnapshotClones(t *testing.T) {
	t.Parallel()

	svc := newFullService()
	input := &controlsnapshot.EffectiveConfigSnapshot{
		ID: "ecs-test",
	}
	svc.WithEffectiveConfigSnapshot(input)
	got := svc.EffectiveConfigSnapshot()
	if got == nil {
		t.Fatal("EffectiveConfigSnapshot() returned nil")
	}
	if got.ID != "ecs-test" {
		t.Fatalf("snapshot.ID = %q", got.ID)
	}
	got.ID = "mutated"
	again := svc.EffectiveConfigSnapshot()
	if again.ID != "ecs-test" {
		t.Fatalf("snapshot clone leaked mutation: %#v", again)
	}
}

func TestSubmitBlockingPreflightWaitsForInput(t *testing.T) {
	t.Parallel()

	svc := newFullService()
	run, err := svc.Submit(context.Background(), SubmitRequest{
		SessionKey: "chat-waiting-input",
		Content:    "把这个文件改一下",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.Status != agent.RunWaitingInput {
		t.Fatalf("run.Status = %q, want waiting_input", run.Status)
	}
	if run.Preflight == nil || run.Preflight.Prompt == "" {
		t.Fatalf("run.Preflight = %#v", run.Preflight)
	}
}

func TestSubmitPersistsAutomationMetadataOnSession(t *testing.T) {
	t.Parallel()

	svc := newFullService()
	run, err := svc.Submit(context.Background(), SubmitRequest{
		SessionKey:   "chat-scope",
		Content:      "hello",
		AutomationID: "auto-nightly",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run.Scope.AutomationID != "auto-nightly" {
		t.Fatalf("run.Scope = %#v", run.Scope)
	}
	session, err := svc.GetSession(context.Background(), run.SessionID)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if session.Scope.AutomationID != "auto-nightly" {
		t.Fatalf("session.Scope = %#v", session.Scope)
	}
	if session.Metadata[agent.MetadataKeyAutomationID] != "auto-nightly" {
		t.Fatalf("automation metadata = %#v", session.Metadata)
	}
}

func TestSubmitPersistsChannelCapabilityMetadataOnSession(t *testing.T) {
	t.Parallel()

	svc := newFullService()
	run, err := svc.Submit(context.Background(), SubmitRequest{
		SessionKey: "chat-channel-capabilities",
		Content:    "hello",
		Metadata: map[string]any{
			meta.KeyChannelCapabilities: map[string]any{
				"interactive":     true,
				"threading":       true,
				"mobile":          true,
				"inline_delivery": true,
			},
			meta.KeyChannelInteractive:    true,
			meta.KeyChannelThreading:      true,
			meta.KeyChannelMobile:         true,
			meta.KeyChannelInlineDelivery: true,
		},
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	session, err := svc.GetSession(context.Background(), run.SessionID)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if got := session.Metadata[meta.KeyChannelInteractive]; got != true {
		t.Fatalf("channel_interactive = %#v, want true", got)
	}
	if got := session.Metadata[meta.KeyChannelThreading]; got != true {
		t.Fatalf("channel_threading = %#v, want true", got)
	}
	if got := session.Metadata[meta.KeyChannelMobile]; got != true {
		t.Fatalf("channel_mobile = %#v, want true", got)
	}
	if got := session.Metadata[meta.KeyChannelInlineDelivery]; got != true {
		t.Fatalf("channel_inline_delivery = %#v, want true", got)
	}
	rawCaps, ok := session.Metadata[meta.KeyChannelCapabilities].(map[string]any)
	if !ok {
		t.Fatalf("channel_capabilities = %#v, want map", session.Metadata[meta.KeyChannelCapabilities])
	}
	if got := rawCaps["interactive"]; got != true {
		t.Fatalf("channel_capabilities.interactive = %#v, want true", got)
	}
	if got := rawCaps["threading"]; got != true {
		t.Fatalf("channel_capabilities.threading = %#v, want true", got)
	}
	if got := rawCaps["mobile"]; got != true {
		t.Fatalf("channel_capabilities.mobile = %#v, want true", got)
	}
	if got := rawCaps["inline_delivery"]; got != true {
		t.Fatalf("channel_capabilities.inline_delivery = %#v, want true", got)
	}
}

func TestSubmitClarificationSupersedesWaitingInputRun(t *testing.T) {
	t.Parallel()

	svc := newFullService()
	first, err := svc.Submit(context.Background(), SubmitRequest{
		SessionKey: "chat-waiting-input-follow-up",
		Content:    "把这个文件改一下",
	})
	if err != nil {
		t.Fatalf("Submit(first) error = %v", err)
	}
	if first.Status != agent.RunWaitingInput {
		t.Fatalf("first.Status = %q", first.Status)
	}

	second, err := svc.Submit(context.Background(), SubmitRequest{
		SessionKey: "chat-waiting-input-follow-up",
		Content:    "/tmp/demo.txt，继续",
	})
	if err != nil {
		t.Fatalf("Submit(second) error = %v", err)
	}
	if second.ID == first.ID {
		t.Fatal("expected a new run after clarification")
	}
	if second.Status != agent.RunQueued {
		t.Fatalf("second.Status = %q, want queued", second.Status)
	}
	if second.Preflight == nil || second.Preflight.State != agent.RunPreflightReady {
		t.Fatalf("second.Preflight = %#v", second.Preflight)
	}
	if second.TaskContract == nil {
		t.Fatal("expected task contract on clarification run")
	}
	foundResolved := false
	for _, item := range second.TaskContract.ResolvedInfo {
		if item.ID == "source_target" && strings.Contains(item.Value, "/tmp/demo.txt") {
			foundResolved = true
			break
		}
	}
	if !foundResolved {
		t.Fatalf("resolved info = %#v", second.TaskContract.ResolvedInfo)
	}

	waitForRunStatus(t, svc, second.ID, agent.RunCompleted)
	firstAfter, err := svc.GetRun(context.Background(), first.ID)
	if err != nil {
		t.Fatalf("GetRun(first) error = %v", err)
	}
	if firstAfter.Status != agent.RunCancelled {
		t.Fatalf("firstAfter.Status = %q, want cancelled", firstAfter.Status)
	}
	if !strings.Contains(firstAfter.Error, "superseded") {
		t.Fatalf("firstAfter.Error = %q", firstAfter.Error)
	}
}

func TestSubmitStandaloneNewTaskDoesNotSupersedeWaitingInputRun(t *testing.T) {
	t.Parallel()

	svc := newFullService()
	first, err := svc.Submit(context.Background(), SubmitRequest{
		SessionKey: "chat-waiting-input-new-task",
		Content:    "打开页面，等搜索结果加载出来，再提取前 5 条",
	})
	if err != nil {
		t.Fatalf("Submit(first) error = %v", err)
	}
	// The preflight model (testPreflightAnalyzer) detects browser intent and missing
	// URL reference. The run should be waiting_input, not queued.
	if first.Status != agent.RunWaitingInput {
		t.Fatalf("first.Status = %q, want waiting_input", first.Status)
	}

	second, err := svc.Submit(context.Background(), SubmitRequest{
		SessionKey: "chat-waiting-input-new-task",
		Content:    "抓取页面信息，写到 docs/tmp/example-brief.md",
	})
	if err != nil {
		t.Fatalf("Submit(second) error = %v", err)
	}
	if second.ID == first.ID {
		t.Fatal("expected a new run for the standalone task")
	}
	if second.ParentRunID != "" {
		t.Fatalf("second.ParentRunID = %q, want empty", second.ParentRunID)
	}

	firstAfter, err := svc.GetRun(context.Background(), first.ID)
	if err != nil {
		t.Fatalf("GetRun(first) error = %v", err)
	}
	if firstAfter.Status != agent.RunWaitingInput {
		t.Fatalf("firstAfter.Status = %q, want waiting_input", firstAfter.Status)
	}
}

func TestSubmitClarificationKeepsRelativePathReference(t *testing.T) {
	t.Parallel()

	svc := newFullService()
	first, err := svc.Submit(context.Background(), SubmitRequest{
		SessionKey: "chat-waiting-input-relative-path",
		Content:    "把这个文件改一下",
	})
	if err != nil {
		t.Fatalf("Submit(first) error = %v", err)
	}
	if first.Status != agent.RunWaitingInput {
		t.Fatalf("first.Status = %q, want waiting_input", first.Status)
	}

	second, err := svc.Submit(context.Background(), SubmitRequest{
		SessionKey: "chat-waiting-input-relative-path",
		Content:    "docs/tmp/example-brief.md，继续",
	})
	if err != nil {
		t.Fatalf("Submit(second) error = %v", err)
	}
	if second.TaskContract == nil {
		t.Fatal("expected task contract on clarification run")
	}
	foundResolved := false
	for _, item := range second.TaskContract.ResolvedInfo {
		if item.ID == "source_target" && item.Value == "docs/tmp/example-brief.md" {
			foundResolved = true
			break
		}
	}
	if !foundResolved {
		t.Fatalf("resolved info = %#v", second.TaskContract.ResolvedInfo)
	}
}

func TestSubmitClarificationStopsAskingAfterMaxRounds(t *testing.T) {
	t.Parallel()

	svc := newFullService()
	execute := false

	first, err := svc.Submit(context.Background(), SubmitRequest{
		SessionKey: "chat-clarification-limit",
		Content:    "把这个文件改一下",
		Execute:    &execute,
	})
	if err != nil {
		t.Fatalf("Submit(first) error = %v", err)
	}
	if first.Status != agent.RunWaitingInput {
		t.Fatalf("first.Status = %q, want waiting_input", first.Status)
	}

	second, err := svc.Submit(context.Background(), SubmitRequest{
		SessionKey: "chat-clarification-limit",
		Content:    "目标对象：那个文件",
		Execute:    &execute,
	})
	if err != nil {
		t.Fatalf("Submit(second) error = %v", err)
	}
	if second.Status != agent.RunWaitingInput {
		t.Fatalf("second.Status = %q, want waiting_input", second.Status)
	}

	third, err := svc.Submit(context.Background(), SubmitRequest{
		SessionKey: "chat-clarification-limit",
		Content:    "目标对象：还是那个文件",
		Execute:    &execute,
	})
	if err != nil {
		t.Fatalf("Submit(third) error = %v", err)
	}
	if third.Status != agent.RunWaitingInput {
		t.Fatalf("third.Status = %q, want waiting_input", third.Status)
	}

	fourth, err := svc.Submit(context.Background(), SubmitRequest{
		SessionKey: "chat-clarification-limit",
		Content:    "目标对象：照上面的文件",
		Execute:    &execute,
	})
	if err != nil {
		t.Fatalf("Submit(fourth) error = %v", err)
	}
	if fourth.Status == agent.RunWaitingInput {
		t.Fatalf("fourth.Status = %q, expected best-effort continuation after clarification limit", fourth.Status)
	}
	if fourth.Status != agent.RunQueued {
		t.Fatalf("fourth.Status = %q, want queued", fourth.Status)
	}
	if fourth.Preflight == nil {
		t.Fatal("expected preflight summary on clarification-limit run")
	}
	if fourth.Preflight.Blocking {
		t.Fatalf("fourth.Preflight.Blocking = true, want false")
	}
	if !strings.Contains(fourth.Preflight.Summary, "proceed with what I have") {
		t.Fatalf("fourth.Preflight.Summary = %q", fourth.Preflight.Summary)
	}
	if !strings.Contains(fourth.Preflight.ContinueHint, "proceed with what I have") {
		t.Fatalf("fourth.Preflight.ContinueHint = %q", fourth.Preflight.ContinueHint)
	}
	if !preflightCheckExists(fourth.Preflight, "clarification_limit_reached") {
		t.Fatalf("fourth.Preflight.Checks = %#v, want clarification_limit_reached", fourth.Preflight.Checks)
	}
}

// ---------------------------------------------------------------------------
// Agent()
// ---------------------------------------------------------------------------

func TestAgent(t *testing.T) {
	t.Parallel()
	comp := newAgentComponent()
	runs := agent.NewInMemoryRunStore()
	svc := NewService(comp, agent.NewInMemorySessionStore(), runs, nil, nil, nil)
	if svc.Agent() != comp {
		t.Fatal("Agent() returned wrong component")
	}
}

func TestAgentNil(t *testing.T) {
	t.Parallel()
	runs := agent.NewInMemoryRunStore()
	svc := NewService(nil, agent.NewInMemorySessionStore(), runs, nil, nil, nil)
	if svc.Agent() != nil {
		t.Fatal("Agent() should return nil when component is nil")
	}
}

// ---------------------------------------------------------------------------
// WithArtifactRetention
// ---------------------------------------------------------------------------

func TestWithArtifactRetention(t *testing.T) {
	t.Parallel()
	svc := newFullService()
	ret := 24 * time.Hour
	result := svc.WithArtifactRetention(ret)
	if result != svc {
		t.Fatal("WithArtifactRetention should return same service")
	}
	if svc.retention != ret {
		t.Fatalf("retention = %v, want %v", svc.retention, ret)
	}
}

func TestWithArtifactRetentionZero(t *testing.T) {
	t.Parallel()
	svc := newFullService()
	svc.WithArtifactRetention(0)
	if svc.retention != 0 {
		t.Fatalf("retention = %v, want 0", svc.retention)
	}
}

// ---------------------------------------------------------------------------
// EventSnapshot
// ---------------------------------------------------------------------------

func TestEventSnapshotNilSnapshotter(t *testing.T) {
	t.Parallel()
	runs := agent.NewInMemoryRunStore()
	svc := NewService(nil, agent.NewInMemorySessionStore(), runs, nil, nil, nil)
	snap := svc.EventSnapshot()
	if snap != nil {
		t.Fatalf("expected nil snapshot, got %v", snap)
	}
}

func TestEventSnapshotEmpty(t *testing.T) {
	t.Parallel()
	bus := eventbus.NewInMemoryBus()
	runs := agent.NewInMemoryRunStore()
	svc := NewService(nil, agent.NewInMemorySessionStore(), runs, nil, bus, nil)
	snap := svc.EventSnapshot()
	if len(snap) != 0 {
		t.Fatalf("expected empty snapshot, got %d events", len(snap))
	}
}

func TestEventSnapshotWithEvents(t *testing.T) {
	t.Parallel()
	bus := eventbus.NewInMemoryBus()
	_ = bus.Publish(context.Background(), eventbus.Event{Type: eventbus.EventRunCompleted})
	_ = bus.Publish(context.Background(), eventbus.Event{Type: eventbus.EventRunFailed})

	runs := agent.NewInMemoryRunStore()
	svc := NewService(nil, agent.NewInMemorySessionStore(), runs, nil, bus, nil)
	snap := svc.EventSnapshot()
	if len(snap) != 2 {
		t.Fatalf("expected 2 events, got %d", len(snap))
	}
}

// ---------------------------------------------------------------------------
// isNilSnapshotter
// ---------------------------------------------------------------------------

func TestIsNilSnapshotterNilInterface(t *testing.T) {
	t.Parallel()
	if !isNilSnapshotter(nil) {
		t.Fatal("expected true for nil interface")
	}
}

func TestIsNilSnapshotterNilPointer(t *testing.T) {
	t.Parallel()
	var bus *eventbus.InMemoryBus
	if !isNilSnapshotter(bus) {
		t.Fatal("expected true for nil pointer in non-nil interface")
	}
}

func TestIsNilSnapshotterNonNil(t *testing.T) {
	t.Parallel()
	bus := eventbus.NewInMemoryBus()
	if isNilSnapshotter(bus) {
		t.Fatal("expected false for real bus")
	}
}

// ---------------------------------------------------------------------------
// ListApprovals
// ---------------------------------------------------------------------------

func TestListApprovalsNilStore(t *testing.T) {
	t.Parallel()
	runs := agent.NewInMemoryRunStore()
	svc := NewService(nil, agent.NewInMemorySessionStore(), runs, nil, nil, nil)
	_, err := svc.ListApprovals(context.Background(), approval.StatusPending)
	if !errors.Is(err, agent.ErrApprovalStoreNil) {
		t.Fatalf("expected ErrApprovalStoreNil, got %v", err)
	}
}

func TestListApprovalsEmpty(t *testing.T) {
	t.Parallel()
	svc := newFullService()
	list, err := svc.ListApprovals(context.Background(), "")
	if err != nil {
		t.Fatalf("ListApprovals() error = %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 approvals, got %d", len(list))
	}
}

func TestListApprovalsWithTickets(t *testing.T) {
	t.Parallel()
	store := approval.NewInMemoryStore()
	_, _ = store.Create(context.Background(), approval.Ticket{RunID: "run-a", SessionID: "sess-1"})
	_, _ = store.Create(context.Background(), approval.Ticket{RunID: "run-b", SessionID: "sess-2"})

	runs := agent.NewInMemoryRunStore()
	svc := NewService(nil, agent.NewInMemorySessionStore(), runs, store, nil, nil)
	list, err := svc.ListApprovals(context.Background(), approval.StatusPending)
	if err != nil {
		t.Fatalf("ListApprovals() error = %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 approvals, got %d", len(list))
	}
}

func TestListApprovalsFiltersByStatus(t *testing.T) {
	t.Parallel()
	store := approval.NewInMemoryStore()
	ticket, _ := store.Create(context.Background(), approval.Ticket{RunID: "run-filter", SessionID: "sess-1"})
	_, _ = store.Resolve(context.Background(), ticket.ID, approval.Resolution{Status: approval.StatusApproved})

	runs := agent.NewInMemoryRunStore()
	svc := NewService(nil, agent.NewInMemorySessionStore(), runs, store, nil, nil)
	pending, err := svc.ListApprovals(context.Background(), approval.StatusPending)
	if err != nil {
		t.Fatalf("ListApprovals(pending) error = %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("expected 0 pending, got %d", len(pending))
	}
	approved, err := svc.ListApprovals(context.Background(), approval.StatusApproved)
	if err != nil {
		t.Fatalf("ListApprovals(approved) error = %v", err)
	}
	if len(approved) != 1 {
		t.Fatalf("expected 1 approved, got %d", len(approved))
	}
}

// ---------------------------------------------------------------------------
// GetApproval
// ---------------------------------------------------------------------------

func TestGetApprovalNilStore(t *testing.T) {
	t.Parallel()
	runs := agent.NewInMemoryRunStore()
	svc := NewService(nil, agent.NewInMemorySessionStore(), runs, nil, nil, nil)
	_, err := svc.GetApproval(context.Background(), "ticket-1")
	if !errors.Is(err, agent.ErrApprovalStoreNil) {
		t.Fatalf("expected ErrApprovalStoreNil, got %v", err)
	}
}

func TestGetApprovalNotFound(t *testing.T) {
	t.Parallel()
	svc := newFullService()
	_, err := svc.GetApproval(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent ticket")
	}
}

func TestGetApprovalSuccess(t *testing.T) {
	t.Parallel()
	store := approval.NewInMemoryStore()
	ticket, _ := store.Create(context.Background(), approval.Ticket{RunID: "run-g", SessionID: "sess-g"})

	runs := agent.NewInMemoryRunStore()
	svc := NewService(nil, agent.NewInMemorySessionStore(), runs, store, nil, nil)
	got, err := svc.GetApproval(context.Background(), ticket.ID)
	if err != nil {
		t.Fatalf("GetApproval() error = %v", err)
	}
	if got.ID != ticket.ID {
		t.Fatalf("got ID = %q, want %q", got.ID, ticket.ID)
	}
	if got.RunID != "run-g" {
		t.Fatalf("got RunID = %q", got.RunID)
	}
}

// ---------------------------------------------------------------------------
// GetRun
// ---------------------------------------------------------------------------

func TestGetRunDelegatesToStore(t *testing.T) {
	t.Parallel()
	runs := agent.NewInMemoryRunStore()
	run, err := runs.Create(context.Background(), "sess-1", agent.IncomingMessage{
		SessionKey: "key-1",
		Content:    "hello",
	}, agent.AgentConfig{DefaultModel: "m", QueueMode: agent.QueueEnqueue})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	svc := NewService(nil, agent.NewInMemorySessionStore(), runs, nil, nil, nil)
	got, err := svc.GetRun(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if got.ID != run.ID {
		t.Fatalf("got ID = %q, want %q", got.ID, run.ID)
	}
	if got.SessionID != "sess-1" {
		t.Fatalf("got SessionID = %q", got.SessionID)
	}
}

func TestGetRunNotFound(t *testing.T) {
	t.Parallel()
	runs := agent.NewInMemoryRunStore()
	svc := NewService(nil, agent.NewInMemorySessionStore(), runs, nil, nil, nil)
	_, err := svc.GetRun(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent run")
	}
}

func TestGetRunResultIncludesOutputAndArtifacts(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	artifacts := artifact.NewInMemoryStore()
	bus := eventbus.NewInMemoryBus()

	session, err := sessions.GetOrCreate(ctx, "result-key", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	run, err := runs.Create(ctx, session.ID, agent.IncomingMessage{
		SessionKey: "result-key",
		Content:    "generate report",
	}, agent.AgentConfig{DefaultModel: "test-model", QueueMode: agent.QueueEnqueue})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	run.Status = agent.RunCompleted
	run.FinishedAt = time.Now().UTC()
	if err := runs.Update(ctx, run); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	session.Messages = append(session.Messages,
		contextengine.Message{
			Role:      contextengine.RoleAssistant,
			Content:   "stale output",
			CreatedAt: time.Now().UTC().Add(-2 * time.Second),
			Metadata:  map[string]any{meta.KeyRunID: "run-old"},
		},
		contextengine.Message{
			Role:      contextengine.RoleAssistant,
			Content:   "final report is ready",
			CreatedAt: time.Now().UTC(),
			Metadata:  map[string]any{meta.KeyRunID: run.ID},
		},
	)
	if err := sessions.Save(ctx, session); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	blob, err := artifacts.Put(ctx, artifact.PutRequest{
		Kind:        "report",
		ContentType: "text/plain",
		Body:        []byte("report body"),
		Metadata: map[string]any{
			meta.KeyRunID: run.ID,
		},
	})
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if err := bus.Publish(ctx, eventbus.Event{
		Type:      eventbus.EventToolExecuted,
		RunID:     run.ID,
		SessionID: session.ID,
		Attrs: map[string]any{
			"tool_name":     "text.write",
			"artifact_uris": []string{blob.URI},
		},
	}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	svc := NewService(nil, sessions, runs, nil, bus, artifacts)
	result, err := svc.GetRunResult(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRunResult() error = %v", err)
	}
	if result.RunID != run.ID {
		t.Fatalf("RunID = %q, want %q", result.RunID, run.ID)
	}
	if result.Status != agent.RunCompleted {
		t.Fatalf("Status = %q, want %q", result.Status, agent.RunCompleted)
	}
	if result.Output != "final report is ready" {
		t.Fatalf("Output = %q", result.Output)
	}
	if len(result.Deliverables) != 1 {
		t.Fatalf("Deliverables len = %d, want 1", len(result.Deliverables))
	}
	if result.Deliverables[0].URI != blob.URI {
		t.Fatalf("Deliverables[0].URI = %q, want %q", result.Deliverables[0].URI, blob.URI)
	}
	if result.Summary == "" {
		t.Fatal("expected non-empty summary")
	}
}

// ---------------------------------------------------------------------------
// GetArtifact
// ---------------------------------------------------------------------------

func TestGetArtifactNilStore(t *testing.T) {
	t.Parallel()
	runs := agent.NewInMemoryRunStore()
	svc := NewService(nil, agent.NewInMemorySessionStore(), runs, nil, nil, nil)
	_, err := svc.GetArtifact(context.Background(), "art-1")
	if !errors.Is(err, agent.ErrArtifactStoreNil) {
		t.Fatalf("expected ErrArtifactStoreNil, got %v", err)
	}
}

func TestGetArtifactNotFound(t *testing.T) {
	t.Parallel()
	svc := newFullService()
	_, err := svc.GetArtifact(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent artifact")
	}
}

func TestGetArtifactSuccess(t *testing.T) {
	t.Parallel()
	store := artifact.NewInMemoryStore()
	blob, _ := store.Put(context.Background(), artifact.PutRequest{
		Kind:        "test",
		ContentType: "text/plain",
		Body:        []byte("hello world"),
	})

	runs := agent.NewInMemoryRunStore()
	svc := NewService(nil, agent.NewInMemorySessionStore(), runs, nil, nil, store)
	got, err := svc.GetArtifact(context.Background(), blob.ID)
	if err != nil {
		t.Fatalf("GetArtifact() error = %v", err)
	}
	if got.ID != blob.ID {
		t.Fatalf("got ID = %q, want %q", got.ID, blob.ID)
	}
	if got.Kind != "test" {
		t.Fatalf("got Kind = %q", got.Kind)
	}
	if got.Size != int64(len("hello world")) {
		t.Fatalf("got Size = %d", got.Size)
	}
}

// ---------------------------------------------------------------------------
// ReadArtifact
// ---------------------------------------------------------------------------

func TestReadArtifactNilStore(t *testing.T) {
	t.Parallel()
	runs := agent.NewInMemoryRunStore()
	svc := NewService(nil, agent.NewInMemorySessionStore(), runs, nil, nil, nil)
	_, _, err := svc.ReadArtifact(context.Background(), "art-1")
	if !errors.Is(err, agent.ErrArtifactStoreNil) {
		t.Fatalf("expected ErrArtifactStoreNil, got %v", err)
	}
}

func TestReadArtifactNotFound(t *testing.T) {
	t.Parallel()
	svc := newFullService()
	_, _, err := svc.ReadArtifact(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent artifact")
	}
}

func TestReadArtifactSuccess(t *testing.T) {
	t.Parallel()
	store := artifact.NewInMemoryStore()
	blob, _ := store.Put(context.Background(), artifact.PutRequest{
		Kind:        "test",
		ContentType: "application/json",
		Body:        []byte(`{"key":"value"}`),
	})

	runs := agent.NewInMemoryRunStore()
	svc := NewService(nil, agent.NewInMemorySessionStore(), runs, nil, nil, store)
	body, contentType, err := svc.ReadArtifact(context.Background(), blob.ID)
	if err != nil {
		t.Fatalf("ReadArtifact() error = %v", err)
	}
	if string(body) != `{"key":"value"}` {
		t.Fatalf("body = %q", string(body))
	}
	if contentType != "application/json" {
		t.Fatalf("contentType = %q", contentType)
	}
}

// ---------------------------------------------------------------------------
// ListArtifacts
// ---------------------------------------------------------------------------

func TestListArtifactsNilStore(t *testing.T) {
	t.Parallel()
	runs := agent.NewInMemoryRunStore()
	svc := NewService(nil, agent.NewInMemorySessionStore(), runs, nil, nil, nil)
	_, err := svc.ListArtifacts(context.Background(), artifact.ListFilter{})
	if !errors.Is(err, agent.ErrArtifactStoreNil) {
		t.Fatalf("expected ErrArtifactStoreNil, got %v", err)
	}
}

func TestListArtifactsEmpty(t *testing.T) {
	t.Parallel()
	svc := newFullService()
	list, err := svc.ListArtifacts(context.Background(), artifact.ListFilter{})
	if err != nil {
		t.Fatalf("ListArtifacts() error = %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 artifacts, got %d", len(list))
	}
}

func TestListArtifactsReturnsAll(t *testing.T) {
	t.Parallel()
	store := artifact.NewInMemoryStore()
	_, _ = store.Put(context.Background(), artifact.PutRequest{Kind: "a", Body: []byte("1")})
	_, _ = store.Put(context.Background(), artifact.PutRequest{Kind: "b", Body: []byte("2")})

	runs := agent.NewInMemoryRunStore()
	svc := NewService(nil, agent.NewInMemorySessionStore(), runs, nil, nil, store)
	list, err := svc.ListArtifacts(context.Background(), artifact.ListFilter{})
	if err != nil {
		t.Fatalf("ListArtifacts() error = %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 artifacts, got %d", len(list))
	}
}

func TestListArtifactsFiltersByKind(t *testing.T) {
	t.Parallel()
	store := artifact.NewInMemoryStore()
	_, _ = store.Put(context.Background(), artifact.PutRequest{Kind: "log", Body: []byte("x")})
	_, _ = store.Put(context.Background(), artifact.PutRequest{Kind: "image", Body: []byte("y")})

	runs := agent.NewInMemoryRunStore()
	svc := NewService(nil, agent.NewInMemorySessionStore(), runs, nil, nil, store)
	list, err := svc.ListArtifacts(context.Background(), artifact.ListFilter{Kind: "log"})
	if err != nil {
		t.Fatalf("ListArtifacts() error = %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(list))
	}
	if list[0].Kind != "log" {
		t.Fatalf("got Kind = %q", list[0].Kind)
	}
}

// ---------------------------------------------------------------------------
// PruneArtifacts
// ---------------------------------------------------------------------------

func TestPruneArtifactsNilStore(t *testing.T) {
	t.Parallel()
	runs := agent.NewInMemoryRunStore()
	svc := NewService(nil, agent.NewInMemorySessionStore(), runs, nil, nil, nil)
	_, err := svc.PruneArtifacts(context.Background(), ArtifactPruneRequest{})
	if !errors.Is(err, agent.ErrArtifactStoreNil) {
		t.Fatalf("expected ErrArtifactStoreNil, got %v", err)
	}
}

func TestPruneArtifactsRequiresRetentionOrFilter(t *testing.T) {
	t.Parallel()
	svc := newFullService()
	_, err := svc.PruneArtifacts(context.Background(), ArtifactPruneRequest{})
	if err == nil {
		t.Fatal("expected error when no retention, before, or selector")
	}
}

func TestPruneArtifactsUsesServiceRetention(t *testing.T) {
	t.Parallel()
	store := artifact.NewInMemoryStore()
	// Create an artifact with a very old timestamp by storing then waiting.
	// InMemoryStore uses time.Now(), so we need a Before that is in the future.
	_, _ = store.Put(context.Background(), artifact.PutRequest{Kind: "old", Body: []byte("data")})

	runs := agent.NewInMemoryRunStore()
	bus := eventbus.NewInMemoryBus()
	svc := NewService(nil, agent.NewInMemorySessionStore(), runs, nil, bus, store)
	svc.WithArtifactRetention(0) // no retention -> needs selector or Before

	// With a filter selector, no retention needed.
	result, err := svc.PruneArtifacts(context.Background(), ArtifactPruneRequest{
		Filter: artifact.ListFilter{Kind: "old"},
	})
	if err != nil {
		t.Fatalf("PruneArtifacts() error = %v", err)
	}
	if result.DeletedCount != 1 {
		t.Fatalf("DeletedCount = %d, want 1", result.DeletedCount)
	}
}

func TestPruneArtifactsWithRetentionFromRequest(t *testing.T) {
	t.Parallel()
	clock := newManualClock(time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC))
	store := artifact.NewInMemoryStore()
	_, _ = store.Put(context.Background(), artifact.PutRequest{Kind: "old", Body: []byte("data")})

	runs := agent.NewInMemoryRunStore()
	bus := eventbus.NewInMemoryBus()
	svc := NewService(nil, agent.NewInMemorySessionStore(), runs, nil, bus, store).WithClock(clock)

	svc.WithArtifactRetention(time.Nanosecond)

	result, err := svc.PruneArtifacts(context.Background(), ArtifactPruneRequest{})
	if err != nil {
		t.Fatalf("PruneArtifacts() error = %v", err)
	}
	if result.DeletedCount != 1 {
		t.Fatalf("DeletedCount = %d, want 1", result.DeletedCount)
	}
}

func TestPruneArtifactsRequestRetentionOverridesService(t *testing.T) {
	t.Parallel()
	clock := newManualClock(time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC))
	store := artifact.NewInMemoryStore()
	_, _ = store.Put(context.Background(), artifact.PutRequest{Kind: "recent", Body: []byte("x")})

	runs := agent.NewInMemoryRunStore()
	bus := eventbus.NewInMemoryBus()
	svc := NewService(nil, agent.NewInMemorySessionStore(), runs, nil, bus, store).WithClock(clock)
	svc.WithArtifactRetention(24 * time.Hour) // very long retention

	result, err := svc.PruneArtifacts(context.Background(), ArtifactPruneRequest{
		Retention: time.Nanosecond,
	})
	if err != nil {
		t.Fatalf("PruneArtifacts() error = %v", err)
	}
	if result.DeletedCount != 1 {
		t.Fatalf("DeletedCount = %d, want 1", result.DeletedCount)
	}
}

func TestPruneArtifactsEmitsEvent(t *testing.T) {
	t.Parallel()
	store := artifact.NewInMemoryStore()
	_, _ = store.Put(context.Background(), artifact.PutRequest{Kind: "log", Body: []byte("x")})

	runs := agent.NewInMemoryRunStore()
	bus := eventbus.NewInMemoryBus()
	svc := NewService(nil, agent.NewInMemorySessionStore(), runs, nil, bus, store)

	_, err := svc.PruneArtifacts(context.Background(), ArtifactPruneRequest{
		Filter: artifact.ListFilter{Kind: "log"},
	})
	if err != nil {
		t.Fatalf("PruneArtifacts() error = %v", err)
	}

	events := bus.Snapshot()
	found := false
	for _, e := range events {
		if e.Type == eventbus.EventArtifactPruned {
			found = true
			if e.Attrs["deleted_count"] != 1 {
				t.Fatalf("deleted_count = %v", e.Attrs["deleted_count"])
			}
		}
	}
	if !found {
		t.Fatal("expected artifact.pruned event")
	}
}

func TestPruneArtifactsNothingToDelete(t *testing.T) {
	t.Parallel()
	store := artifact.NewInMemoryStore()
	runs := agent.NewInMemoryRunStore()
	bus := eventbus.NewInMemoryBus()
	svc := NewService(nil, agent.NewInMemorySessionStore(), runs, nil, bus, store)

	result, err := svc.PruneArtifacts(context.Background(), ArtifactPruneRequest{
		Filter: artifact.ListFilter{Kind: "nonexistent-kind"},
	})
	if err != nil {
		t.Fatalf("PruneArtifacts() error = %v", err)
	}
	if result.DeletedCount != 0 {
		t.Fatalf("DeletedCount = %d, want 0", result.DeletedCount)
	}
}

// ---------------------------------------------------------------------------
// ListTools
// ---------------------------------------------------------------------------

func TestListToolsNilAgent(t *testing.T) {
	t.Parallel()
	runs := agent.NewInMemoryRunStore()
	svc := NewService(nil, agent.NewInMemorySessionStore(), runs, nil, nil, nil)
	_, err := svc.ListTools(context.Background(), "")
	if err == nil {
		t.Fatal("expected error when agent is nil")
	}
}

func TestListToolsWithAgent(t *testing.T) {
	t.Parallel()
	svc := newFullService()
	tools, err := svc.ListTools(context.Background(), "")
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	// No tool executor provided, so should be empty.
	if len(tools) != 0 {
		t.Fatalf("expected 0 tools, got %d", len(tools))
	}
}

// ---------------------------------------------------------------------------
// ResolveApproval (delegates to agent component)
// ---------------------------------------------------------------------------

func TestResolveApprovalDelegatesToAgent(t *testing.T) {
	t.Parallel()
	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	approvalStore := approval.NewInMemoryStore()
	comp := agent.NewComponent(agent.AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 3,
		QueueMode:     agent.QueueEnqueue,
	}, sessions, runs, agent.NewInMemoryCoordinator(), newContextEngine(), mockModelClient{}, nil, nil).WithApprovals(approvalStore)

	svc := NewService(comp, sessions, runs, approvalStore, nil, nil)

	session, err := sessions.GetOrCreate(context.Background(), "resolve-approval", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	run, err := runs.Create(context.Background(), session.ID, agent.IncomingMessage{
		SessionKey: "resolve-approval",
		Content:    "hello",
	}, agent.AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    agent.QueueEnqueue,
	})
	if err != nil {
		t.Fatalf("runs.Create() error = %v", err)
	}

	ticket, err := approvalStore.Create(context.Background(), approval.Ticket{RunID: run.ID, SessionID: session.ID})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	resolved, err := svc.ResolveApproval(context.Background(), ticket.ID, approval.Resolution{
		Status:     approval.StatusDenied,
		ResolvedBy: "tester",
		Note:       "not allowed",
	})
	if err != nil {
		t.Fatalf("ResolveApproval() error = %v", err)
	}
	if resolved == nil {
		t.Fatal("expected resolved ticket")
	}
	if resolved.Status != approval.StatusDenied {
		t.Fatalf("ticket.Status = %q", resolved.Status)
	}
	if resolved.ResolvedBy != "tester" {
		t.Fatalf("ticket.ResolvedBy = %q", resolved.ResolvedBy)
	}
	run, err = runs.Get(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("runs.Get() error = %v", err)
	}
	if run.Status != agent.RunCancelled {
		t.Fatalf("run.Status = %q", run.Status)
	}
}

// ---------------------------------------------------------------------------
// publish (internal)
// ---------------------------------------------------------------------------

func TestPublishWithBus(t *testing.T) {
	t.Parallel()
	bus := eventbus.NewInMemoryBus()
	runs := agent.NewInMemoryRunStore()
	svc := NewService(nil, agent.NewInMemorySessionStore(), runs, nil, bus, nil)
	err := svc.publish(context.Background(), eventbus.Event{
		Type: eventbus.EventRunCompleted,
		Attrs: map[string]any{
			"key": "value",
		},
	})
	if err != nil {
		t.Fatalf("publish() error = %v", err)
	}
	events := bus.Snapshot()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != eventbus.EventRunCompleted {
		t.Fatalf("event.Type = %q", events[0].Type)
	}
}

func TestPublishWithoutBus(t *testing.T) {
	t.Parallel()
	runs := agent.NewInMemoryRunStore()
	svc := NewService(nil, agent.NewInMemorySessionStore(), runs, nil, nil, nil)
	err := svc.publish(context.Background(), eventbus.Event{Type: eventbus.EventRunCompleted})
	if err != nil {
		t.Fatalf("publish() error = %v, want nil", err)
	}
}

func TestPublishSnapshotterNotBus(t *testing.T) {
	t.Parallel()
	// A Snapshotter that is not a Bus. publish should return nil.
	runs := agent.NewInMemoryRunStore()
	svc := NewService(nil, agent.NewInMemorySessionStore(), runs, nil, readOnlySnapshotter{}, nil)
	err := svc.publish(context.Background(), eventbus.Event{Type: eventbus.EventRunCompleted})
	if err != nil {
		t.Fatalf("publish() error = %v, want nil", err)
	}
}

// readOnlySnapshotter satisfies eventbus.Snapshotter but not eventbus.Bus.
type readOnlySnapshotter struct{}

func (readOnlySnapshotter) Snapshot() []eventbus.Event { return nil }

type replayOnlyEventReader struct {
	events []eventbus.Event
	err    error
}

func (r replayOnlyEventReader) ReplayContext(_ context.Context) ([]eventbus.Event, error) {
	if r.err != nil {
		return nil, r.err
	}
	out := make([]eventbus.Event, len(r.events))
	copy(out, r.events)
	return out, nil
}

// ---------------------------------------------------------------------------
// EventSnapshot with typed-nil snapshotter
// ---------------------------------------------------------------------------

func TestEventSnapshotTypedNilSnapshotter(t *testing.T) {
	t.Parallel()
	var bus *eventbus.InMemoryBus // typed nil
	runs := agent.NewInMemoryRunStore()
	svc := NewService(nil, agent.NewInMemorySessionStore(), runs, nil, bus, nil)
	snap := svc.EventSnapshot()
	if snap != nil {
		t.Fatalf("expected nil snapshot for typed nil, got %v", snap)
	}
}

func TestEventSnapshotPrefersReplayReader(t *testing.T) {
	t.Parallel()
	bus := eventbus.NewInMemoryBus()
	_ = bus.Publish(context.Background(), eventbus.Event{ID: "evt-live", Type: eventbus.EventRunCompleted})
	runs := agent.NewInMemoryRunStore()
	svc := NewService(nil, agent.NewInMemorySessionStore(), runs, nil, bus, nil).
		WithEventReader(replayOnlyEventReader{events: []eventbus.Event{
			{ID: "evt-persisted", Type: eventbus.EventRunFailed},
		}})

	snap := svc.EventSnapshot()
	if len(snap) != 1 {
		t.Fatalf("len(snapshot) = %d, want 1", len(snap))
	}
	if snap[0].ID != "evt-persisted" {
		t.Fatalf("snapshot[0].ID = %q, want persisted event", snap[0].ID)
	}
}

// ---------------------------------------------------------------------------
// Submit (ingress)
// ---------------------------------------------------------------------------

func TestSubmitCreatesAndExecutesRun(t *testing.T) {
	t.Parallel()
	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	comp := agent.NewComponent(agent.AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 3,
		QueueMode:     agent.QueueEnqueue,
	}, sessions, runs, agent.NewInMemoryCoordinator(), newContextEngine(), mockModelClient{}, nil, nil)

	bus := eventbus.NewInMemoryBus()
	svc := NewService(comp, agent.NewInMemorySessionStore(), runs, nil, bus, nil)

	run, err := svc.Submit(context.Background(), SubmitRequest{
		SessionKey: "test-session",
		Content:    "hello there",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run == nil {
		t.Fatal("expected a run")
	}
	if run.Status != agent.RunQueued && run.Status != agent.RunRunning {
		t.Fatalf("run.Status = %q, want queued or running", run.Status)
	}
	waitForRunStatus(t, svc, run.ID, agent.RunCompleted)
}

func TestSubmitWithExecuteFalse(t *testing.T) {
	t.Parallel()
	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	comp := agent.NewComponent(agent.AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 3,
		QueueMode:     agent.QueueEnqueue,
	}, sessions, runs, agent.NewInMemoryCoordinator(), newContextEngine(), mockModelClient{}, nil, nil)

	svc := NewService(comp, agent.NewInMemorySessionStore(), runs, nil, nil, nil)
	executeFalse := false
	run, err := svc.Submit(context.Background(), SubmitRequest{
		SessionKey: "no-exec-session",
		Content:    "just submit",
		Execute:    &executeFalse,
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run == nil {
		t.Fatal("expected a run")
	}
	if run.Status != agent.RunQueued {
		t.Fatalf("run.Status = %q, want queued", run.Status)
	}
}

// ---------------------------------------------------------------------------
// GetRun after Submit
// ---------------------------------------------------------------------------

func TestGetRunAfterSubmit(t *testing.T) {
	t.Parallel()
	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	comp := agent.NewComponent(agent.AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 3,
		QueueMode:     agent.QueueEnqueue,
	}, sessions, runs, agent.NewInMemoryCoordinator(), newContextEngine(), mockModelClient{}, nil, nil)

	svc := NewService(comp, agent.NewInMemorySessionStore(), runs, nil, nil, nil)
	submitted, err := svc.Submit(context.Background(), SubmitRequest{
		SessionKey: "getrun-session",
		Content:    "test content",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	fetched, err := svc.GetRun(context.Background(), submitted.ID)
	if err != nil {
		t.Fatalf("GetRun() error = %v", err)
	}
	if fetched.ID != submitted.ID {
		t.Fatalf("fetched.ID = %q, want %q", fetched.ID, submitted.ID)
	}
}

// ---------------------------------------------------------------------------
// PruneArtifacts with explicit Before filter
// ---------------------------------------------------------------------------

func TestPruneArtifactsWithBeforeFilter(t *testing.T) {
	t.Parallel()
	store := artifact.NewInMemoryStore()
	_, _ = store.Put(context.Background(), artifact.PutRequest{Kind: "a", Body: []byte("1")})
	time.Sleep(2 * time.Millisecond)
	cutoff := time.Now().UTC()
	_, _ = store.Put(context.Background(), artifact.PutRequest{Kind: "a", Body: []byte("2")})

	runs := agent.NewInMemoryRunStore()
	bus := eventbus.NewInMemoryBus()
	svc := NewService(nil, agent.NewInMemorySessionStore(), runs, nil, bus, store)

	result, err := svc.PruneArtifacts(context.Background(), ArtifactPruneRequest{
		Filter: artifact.ListFilter{Before: cutoff},
	})
	if err != nil {
		t.Fatalf("PruneArtifacts() error = %v", err)
	}
	if result.DeletedCount != 1 {
		t.Fatalf("DeletedCount = %d, want 1", result.DeletedCount)
	}

	remaining, _ := store.List(context.Background(), artifact.ListFilter{})
	if len(remaining) != 1 {
		t.Fatalf("remaining = %d, want 1", len(remaining))
	}
}

// ---------------------------------------------------------------------------
// Concurrent access safety smoke test
// ---------------------------------------------------------------------------

func TestServiceConcurrentAccess(t *testing.T) {
	t.Parallel()
	svc := newFullService()

	ctx := context.Background()
	store := artifact.NewInMemoryStore()
	blob, _ := store.Put(ctx, artifact.PutRequest{Kind: "t", Body: []byte("hello")})
	svc.artifacts = store

	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			_, _ = svc.ListArtifacts(ctx, artifact.ListFilter{})
			_, _ = svc.GetArtifact(ctx, blob.ID)
			_, _, _ = svc.ReadArtifact(ctx, blob.ID)
			_ = svc.EventSnapshot()
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}
}
