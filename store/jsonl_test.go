package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
)

func TestJSONLSessionStoreReloadsSessions(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "state")
	store, err := NewJSONLSessionStore(root)
	if err != nil {
		t.Fatalf("NewJSONLSessionStore() error = %v", err)
	}

	session, err := store.GetOrCreate(context.Background(), "chat-1", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	if session.Revision != 1 {
		t.Fatalf("session.Revision = %d, want 1", session.Revision)
	}
	if err := store.AppendUserMessage(context.Background(), session.ID, agent.IncomingMessage{
		Content: "hello",
		Model:   "test-model",
	}); err != nil {
		t.Fatalf("AppendUserMessage() error = %v", err)
	}
	locked, unlock, err := store.LoadForExecution(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("LoadForExecution() error = %v", err)
	}
	if locked.Revision != 2 {
		t.Fatalf("locked.Revision = %d, want 2", locked.Revision)
	}
	locked.Summary = "summary"
	if err := store.Save(context.Background(), locked); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if locked.Revision != 3 {
		t.Fatalf("locked.Revision = %d, want 3", locked.Revision)
	}
	unlock()

	reloaded, err := NewJSONLSessionStore(root)
	if err != nil {
		t.Fatalf("NewJSONLSessionStore(reload) error = %v", err)
	}
	session, err = reloaded.GetOrCreate(context.Background(), "chat-1", "ignored")
	if err != nil {
		t.Fatalf("GetOrCreate(reload) error = %v", err)
	}
	if session.Model != "test-model" {
		t.Fatalf("session.Model = %q", session.Model)
	}
	if session.Revision != 3 {
		t.Fatalf("session.Revision = %d, want 3", session.Revision)
	}
	if len(session.Messages) != 1 {
		t.Fatalf("len(session.Messages) = %d", len(session.Messages))
	}
	if session.Summary != "summary" {
		t.Fatalf("session.Summary = %q", session.Summary)
	}
}

func TestJSONLRunStoreReloadsRuns(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "state")
	store, err := NewJSONLRunStore(root)
	if err != nil {
		t.Fatalf("NewJSONLRunStore() error = %v", err)
	}

	run, err := store.Create(context.Background(), "sess-1", agent.IncomingMessage{
		ExternalEventID: "evt-1",
		Model:           "test-model",
	}, agent.AgentConfig{
		DefaultModel: "fallback-model",
		QueueMode:    agent.QueueEnqueue,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	run.Status = agent.RunWaitingApproval
	run.ApprovalID = "appr-1"
	run.LastSessionRevision = 7
	run.SemanticSignal = &agent.SemanticSignal{
		Language: agent.LanguageProfile{
			Family:           "es",
			Script:           "Latn",
			MainSemanticPath: true,
		},
		RequiresCurrentInfo: true,
		SuggestedDomains:    []string{"browser", "fs"},
		JobType:             "delivery",
		TargetSummary:       "docs/tmp/report.md",
		TriageReady:         true,
		TaskContractReady:   true,
	}
	run.TaskContract = &agent.TaskContract{
		Goal:    "send report",
		JobType: "delivery",
		ExpectedDeliverables: []agent.TaskContractDeliverable{{
			Kind:     "message_delivery",
			Required: true,
		}},
	}
	run.PendingTools = []agent.ToolCall{{
		ID:   "call-1",
		Name: "fs.write",
	}}
	if err := store.Update(context.Background(), run); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	reloaded, err := NewJSONLRunStore(root)
	if err != nil {
		t.Fatalf("NewJSONLRunStore(reload) error = %v", err)
	}
	byEvent, err := reloaded.FindByExternalEvent(context.Background(), "evt-1")
	if err != nil {
		t.Fatalf("FindByExternalEvent() error = %v", err)
	}
	if byEvent.Status != agent.RunWaitingApproval {
		t.Fatalf("byEvent.Status = %q", byEvent.Status)
	}
	if byEvent.ApprovalID != "appr-1" {
		t.Fatalf("byEvent.ApprovalID = %q", byEvent.ApprovalID)
	}
	if byEvent.LastSessionRevision != 7 {
		t.Fatalf("byEvent.LastSessionRevision = %d, want 7", byEvent.LastSessionRevision)
	}
	if byEvent.SemanticSignal == nil || byEvent.SemanticSignal.TargetSummary != "docs/tmp/report.md" {
		t.Fatalf("byEvent.SemanticSignal = %#v", byEvent.SemanticSignal)
	}
	if byEvent.TaskContract == nil || byEvent.TaskContract.JobType != "delivery" {
		t.Fatalf("byEvent.TaskContract = %#v", byEvent.TaskContract)
	}
	if len(byEvent.PendingTools) != 1 {
		t.Fatalf("len(byEvent.PendingTools) = %d", len(byEvent.PendingTools))
	}
}

func TestJSONLRunStoreClaimQueuedRun(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "state")
	store, err := NewJSONLRunStore(root)
	if err != nil {
		t.Fatalf("NewJSONLRunStore() error = %v", err)
	}

	run, err := store.Create(context.Background(), "sess-1", agent.IncomingMessage{
		ExternalEventID: "evt-claim",
		Content:         "start me",
	}, agent.AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    agent.QueueEnqueue,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	claimed, ok, err := store.ClaimQueuedRun(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("ClaimQueuedRun(first) error = %v", err)
	}
	if !ok {
		t.Fatal("ClaimQueuedRun(first) = false, want true")
	}
	if claimed.Status != agent.RunRunning {
		t.Fatalf("claimed.Status = %q, want %q", claimed.Status, agent.RunRunning)
	}
	if claimed.StartedAt.IsZero() {
		t.Fatal("claimed.StartedAt should be set")
	}

	second, ok, err := store.ClaimQueuedRun(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("ClaimQueuedRun(second) error = %v", err)
	}
	if ok {
		t.Fatal("ClaimQueuedRun(second) = true, want false")
	}
	if second.Status != agent.RunRunning {
		t.Fatalf("second.Status = %q, want %q", second.Status, agent.RunRunning)
	}

	reloaded, err := NewJSONLRunStore(root)
	if err != nil {
		t.Fatalf("NewJSONLRunStore(reload) error = %v", err)
	}
	got, err := reloaded.Get(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("Get(reload) error = %v", err)
	}
	if got.Status != agent.RunRunning {
		t.Fatalf("got.Status = %q, want %q", got.Status, agent.RunRunning)
	}
}

func TestJSONLRunStoreCreateDoesNotRetainRunWhenAppendFails(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "state")
	store, err := NewJSONLRunStore(root)
	if err != nil {
		t.Fatalf("NewJSONLRunStore() error = %v", err)
	}

	runsDir := store.layout.RunsDir()
	if err := os.RemoveAll(runsDir); err != nil {
		t.Fatalf("RemoveAll(runs dir) error = %v", err)
	}
	if err := os.WriteFile(runsDir, []byte("not-a-directory"), 0o644); err != nil {
		t.Fatalf("WriteFile(runs dir sentinel) error = %v", err)
	}

	if _, err := store.Create(context.Background(), "sess-1", agent.IncomingMessage{
		ExternalEventID: "evt-create-fail",
		Content:         "hello",
	}, agent.AgentConfig{DefaultModel: "test-model"}); err == nil {
		t.Fatal("expected Create() to fail when runs directory cannot be created")
	}

	runs, err := store.List(context.Background(), agent.RunListFilter{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(runs) != 0 {
		t.Fatalf("len(runs) = %d, want 0", len(runs))
	}

	if _, err := store.FindByExternalEvent(context.Background(), "evt-create-fail"); err == nil {
		t.Fatal("FindByExternalEvent() succeeded for failed create")
	}
}

func TestJSONLStoreStartupLimitAndScopeFilters(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "state")
	sessionStore, err := NewJSONLSessionStore(root)
	if err != nil {
		t.Fatalf("NewJSONLSessionStore() error = %v", err)
	}
	runStore, err := NewJSONLRunStore(root)
	if err != nil {
		t.Fatalf("NewJSONLRunStore() error = %v", err)
	}
	ctx := context.Background()

	sessionA, err := sessionStore.GetOrCreate(ctx, "scope:a", "model-a")
	if err != nil {
		t.Fatalf("GetOrCreate(sessionA) error = %v", err)
	}
	if err := sessionStore.AppendUserMessage(ctx, sessionA.ID, agent.IncomingMessage{
		Content: "tenant a",
	}); err != nil {
		t.Fatalf("AppendUserMessage(sessionA) error = %v", err)
	}
	sessionB, err := sessionStore.GetOrCreate(ctx, "scope:b", "model-b")
	if err != nil {
		t.Fatalf("GetOrCreate(sessionB) error = %v", err)
	}
	if err := sessionStore.AppendUserMessage(ctx, sessionB.ID, agent.IncomingMessage{
		Content: "tenant b",
	}); err != nil {
		t.Fatalf("AppendUserMessage(sessionB) error = %v", err)
	}

	_, err = runStore.Create(ctx, sessionA.ID, agent.IncomingMessage{
		Content: "run a",
	}, agent.AgentConfig{DefaultModel: "model-a", QueueMode: agent.QueueEnqueue})
	if err != nil {
		t.Fatalf("Create(runA) error = %v", err)
	}
	runB, err := runStore.Create(ctx, sessionB.ID, agent.IncomingMessage{
		Content: "run b",
	}, agent.AgentConfig{DefaultModel: "model-b", QueueMode: agent.QueueEnqueue})
	if err != nil {
		t.Fatalf("Create(runB) error = %v", err)
	}

	filter := agent.ScopeFilter{}
	sessions, err := sessionStore.ListScoped(ctx, agent.SessionListFilter{Scope: filter})
	if err != nil {
		t.Fatalf("ListScoped() error = %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("filtered sessions = %#v", sessions)
	}
	if _, err := sessionStore.GetByKeyScoped(ctx, "scope:b", filter); err != nil {
		t.Fatalf("GetByKeyScoped(sessionB) error = %v", err)
	}
	runs, err := runStore.List(ctx, agent.RunListFilter{Scope: filter})
	if err != nil {
		t.Fatalf("runs.List() error = %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("filtered runs = %#v", runs)
	}
	if _, err := runStore.GetScoped(ctx, runB.ID, filter); err != nil {
		t.Fatalf("GetScoped(runB) error = %v", err)
	}

	limitedSessions, err := NewJSONLSessionStoreWithOptions(root, JSONLStoreOptions{StartupLimit: 1})
	if err != nil {
		t.Fatalf("NewJSONLSessionStoreWithOptions(limit) error = %v", err)
	}
	if len(limitedSessions.byID) != 1 {
		t.Fatalf("len(limitedSessions.byID) = %d, want 1", len(limitedSessions.byID))
	}
	if _, err := limitedSessions.Get(ctx, sessionB.ID); err != nil {
		t.Fatalf("expected newest session to remain, got error %v", err)
	}

	limitedRuns, err := NewJSONLRunStoreWithOptions(root, JSONLStoreOptions{StartupLimit: 1})
	if err != nil {
		t.Fatalf("NewJSONLRunStoreWithOptions(limit) error = %v", err)
	}
	if len(limitedRuns.byID) != 1 {
		t.Fatalf("len(limitedRuns.byID) = %d, want 1", len(limitedRuns.byID))
	}
	if _, err := limitedRuns.Get(ctx, runB.ID); err != nil {
		t.Fatalf("expected newest run to remain, got error %v", err)
	}
}

func TestJSONLApprovalStoreReloadsTickets(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "state")
	store, err := NewJSONLApprovalStore(root)
	if err != nil {
		t.Fatalf("NewJSONLApprovalStore() error = %v", err)
	}

	ticket, err := store.Create(context.Background(), approval.Ticket{
		RunID:     "run-1",
		SessionID: "sess-1",
		Kind:      approval.KindSkillInstall,
		ToolCalls: []approval.ToolCall{{ID: "call-1", Name: "fs.write"}},
		Reasons:   []string{"needs approval"},
		Metadata: map[string]any{
			"requested_skills": []string{"news-research"},
		},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	if _, err := store.Resolve(context.Background(), ticket.ID, approval.Resolution{
		Status:     approval.StatusApproved,
		ResolvedBy: "tester",
		Note:       "ok",
		Scope:      approval.ScopeSession,
	}); err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if _, err := store.UpsertExternalRef(context.Background(), ticket.ID, approval.ExternalReference{
		Provider:   "jira",
		ExternalID: "jira-42",
		URL:        "https://jira.example/approvals/42",
		Status:     "approved_remote",
		SyncedAt:   time.Now().UTC(),
	}); err != nil {
		t.Fatalf("UpsertExternalRef() error = %v", err)
	}

	reloaded, err := NewJSONLApprovalStore(root)
	if err != nil {
		t.Fatalf("NewJSONLApprovalStore(reload) error = %v", err)
	}
	got, err := reloaded.GetByRun(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("GetByRun() error = %v", err)
	}
	if got.Status != approval.StatusApproved {
		t.Fatalf("got.Status = %q", got.Status)
	}
	if got.Scope != approval.ScopeSession {
		t.Fatalf("got.Scope = %q, want %q", got.Scope, approval.ScopeSession)
	}
	if got.Kind != approval.KindSkillInstall {
		t.Fatalf("got.Kind = %q", got.Kind)
	}
	if len(got.External) != 1 || got.External[0].Provider != "jira" || got.External[0].ExternalID != "jira-42" {
		t.Fatalf("got.External = %#v", got.External)
	}
	if len(got.Metadata) == 0 {
		t.Fatal("got.Metadata should not be empty")
	}
	items, err := reloaded.List(context.Background(), approval.ListFilter{Status: approval.StatusApproved})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d", len(items))
	}

	next, err := reloaded.Create(context.Background(), approval.Ticket{
		RunID:     "run-1",
		SessionID: "sess-1",
		Kind:      approval.KindToolCalls,
		ToolCalls: []approval.ToolCall{{ID: "call-2", Name: "net.http"}},
		Reasons:   []string{"second gate"},
	})
	if err != nil {
		t.Fatalf("Create(second) error = %v", err)
	}
	if next.Status != approval.StatusPending {
		t.Fatalf("next.Status = %q", next.Status)
	}
	got, err = reloaded.GetByRun(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("GetByRun(second) error = %v", err)
	}
	if got.ID != next.ID {
		t.Fatalf("GetByRun(second).ID = %q, want %q", got.ID, next.ID)
	}
}
