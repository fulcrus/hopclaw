package runtime

import (
	"context"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
)

// ---------------------------------------------------------------------------
// ResolveApproval — nil agent
// ---------------------------------------------------------------------------

func TestResolveApprovalNilAgent(t *testing.T) {
	t.Parallel()
	runs := agent.NewInMemoryRunStore()
	svc := NewService(nil, agent.NewInMemorySessionStore(), runs, nil, nil, nil)
	_, err := svc.ResolveApproval(context.Background(), "ticket-1", approval.Resolution{
		Status: approval.StatusApproved,
	})
	if err == nil {
		t.Fatal("expected error when agent is nil")
	}
}

// ---------------------------------------------------------------------------
// ResolveApproval — denial flow
// ---------------------------------------------------------------------------

func TestResolveApprovalDenialCancelsRun(t *testing.T) {
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

	session, err := sessions.GetOrCreate(context.Background(), "deny-flow", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	run, err := runs.Create(context.Background(), session.ID, agent.IncomingMessage{
		SessionKey: "deny-flow",
		Content:    "test",
	}, agent.AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    agent.QueueEnqueue,
	})
	if err != nil {
		t.Fatalf("runs.Create() error = %v", err)
	}

	ticket, err := approvalStore.Create(context.Background(), approval.Ticket{
		RunID:     run.ID,
		SessionID: session.ID,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	resolved, err := svc.ResolveApproval(context.Background(), ticket.ID, approval.Resolution{
		Status:     approval.StatusDenied,
		ResolvedBy: "admin",
	})
	if err != nil {
		t.Fatalf("ResolveApproval() error = %v", err)
	}
	if resolved.Status != approval.StatusDenied {
		t.Fatalf("resolved.Status = %q, want denied", resolved.Status)
	}

	// After denial, the run should be cancelled.
	updatedRun, err := runs.Get(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("runs.Get() error = %v", err)
	}
	if updatedRun.Status != agent.RunCancelled {
		t.Fatalf("run.Status = %q, want cancelled", updatedRun.Status)
	}
}

func TestResolveApprovalApprovedAppliesGrantScope(t *testing.T) {
	t.Parallel()

	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	approvalStore := approval.NewInMemoryStore()
	grantStore := approval.NewGrantStore()
	comp := agent.NewComponent(agent.AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 3,
		QueueMode:     agent.QueueEnqueue,
	}, sessions, runs, agent.NewInMemoryCoordinator(), newContextEngine(), mockModelClient{}, nil, nil).WithApprovals(approvalStore)

	svc := NewService(comp, sessions, runs, approvalStore, nil, nil).WithGrantStore(grantStore)

	session, err := sessions.GetOrCreate(context.Background(), "grant-flow", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	run, err := runs.Create(context.Background(), session.ID, agent.IncomingMessage{
		SessionKey: "grant-flow",
		Content:    "test",
	}, agent.AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    agent.QueueEnqueue,
	})
	if err != nil {
		t.Fatalf("runs.Create() error = %v", err)
	}

	ticket, err := approvalStore.Create(context.Background(), approval.Ticket{
		RunID:     run.ID,
		SessionID: session.ID,
		ToolCalls: []approval.ToolCall{{
			ID:   "call-1",
			Name: "exec.run",
			Input: map[string]any{
				"command": "git",
				"args":    []any{"status"},
			},
			ResourceScope: approval.ResourceScope{
				CommandPrefixes: []string{"git status"},
			},
		}},
		Metadata: map[string]any{
			"policy_approval_max_scope": "session",
		},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	resolved, err := svc.ResolveApproval(context.Background(), ticket.ID, approval.Resolution{
		Status:     approval.StatusApproved,
		ResolvedBy: "admin",
		Scope:      approval.ScopeSession,
	})
	if err != nil {
		t.Fatalf("ResolveApproval() error = %v", err)
	}
	if resolved.Status != approval.StatusApproved {
		t.Fatalf("resolved.Status = %q, want approved", resolved.Status)
	}
	if !grantStore.IsGranted(session.ID, "exec.run") {
		t.Fatal("expected runtime resolve path to grant approved tool call")
	}
	if decision := grantStore.Evaluate(session.ID, "exec.run", map[string]any{
		"command": "git",
		"args":    []any{"status", "-sb"},
	}); !decision.Granted {
		t.Fatalf("Evaluate(git status) = %#v, want granted", decision)
	}
	if decision := grantStore.Evaluate(session.ID, "exec.run", map[string]any{
		"command": "git",
		"args":    []any{"checkout", "-b", "topic"},
	}); decision.Granted {
		t.Fatalf("Evaluate(git checkout) = %#v, want not granted", decision)
	}
}

func TestGetApprovalViewIncludesResourceScopeSummary(t *testing.T) {
	t.Parallel()

	store := approval.NewInMemoryStore()
	ticket, err := store.Create(context.Background(), approval.Ticket{
		RunID:     "run-scope",
		SessionID: "sess-scope",
		ToolCalls: []approval.ToolCall{{
			ID:   "call-1",
			Name: "fs.write",
			Input: map[string]any{
				"path": "reports/daily.txt",
			},
			ResourceScope: approval.ResourceScope{
				PathPrefixes: []string{"reports/daily.txt"},
			},
		}},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	svc := NewService(nil, agent.NewInMemorySessionStore(), agent.NewInMemoryRunStore(), store, nil, nil)
	view, err := svc.GetApprovalView(context.Background(), ticket.ID)
	if err != nil {
		t.Fatalf("GetApprovalView() error = %v", err)
	}
	if view.ResourceScopeSummary == "" {
		t.Fatalf("ResourceScopeSummary = %q, want non-empty", view.ResourceScopeSummary)
	}
	if got := view.ToolCalls[0].ResourceScope.Normalized().Summary; got != "paths=reports/daily.txt" {
		t.Fatalf("ToolCalls[0].ResourceScope.Summary = %q", got)
	}
}

// ---------------------------------------------------------------------------
// FindPendingApproval
// ---------------------------------------------------------------------------

func TestFindPendingApprovalNilStore(t *testing.T) {
	t.Parallel()
	runs := agent.NewInMemoryRunStore()
	svc := NewService(nil, agent.NewInMemorySessionStore(), runs, nil, nil, nil)
	_, err := svc.FindPendingApproval(context.Background(), "sess-1")
	if err == nil {
		t.Fatal("expected error when approval store is nil")
	}
}

func TestFindPendingApprovalEmptySessionID(t *testing.T) {
	t.Parallel()
	store := approval.NewInMemoryStore()
	runs := agent.NewInMemoryRunStore()
	svc := NewService(nil, agent.NewInMemorySessionStore(), runs, store, nil, nil)
	_, err := svc.FindPendingApproval(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty session ID")
	}
}

func TestFindPendingApprovalNotFound(t *testing.T) {
	t.Parallel()
	store := approval.NewInMemoryStore()
	runs := agent.NewInMemoryRunStore()
	svc := NewService(nil, agent.NewInMemorySessionStore(), runs, store, nil, nil)
	_, err := svc.FindPendingApproval(context.Background(), "sess-nonexistent")
	if err == nil {
		t.Fatal("expected error when no pending approval")
	}
}

func TestFindPendingApprovalReturnsMatchingSession(t *testing.T) {
	t.Parallel()
	store := approval.NewInMemoryStore()
	first, _ := store.Create(context.Background(), approval.Ticket{
		RunID:     "run-old",
		SessionID: "sess-find",
	})
	second, _ := store.Create(context.Background(), approval.Ticket{
		RunID:     "run-new",
		SessionID: "sess-find",
	})

	runs := agent.NewInMemoryRunStore()
	svc := NewService(nil, agent.NewInMemorySessionStore(), runs, store, nil, nil)
	found, err := svc.FindPendingApproval(context.Background(), "sess-find")
	if err != nil {
		t.Fatalf("FindPendingApproval() error = %v", err)
	}
	// Should return one of the pending tickets for this session.
	if found.ID != first.ID && found.ID != second.ID {
		t.Fatalf("found.ID = %q, want %q or %q", found.ID, first.ID, second.ID)
	}
}

func TestFindPendingApprovalSkipsOtherSessions(t *testing.T) {
	t.Parallel()
	store := approval.NewInMemoryStore()
	_, _ = store.Create(context.Background(), approval.Ticket{
		RunID:     "run-other",
		SessionID: "sess-other",
	})

	runs := agent.NewInMemoryRunStore()
	svc := NewService(nil, agent.NewInMemorySessionStore(), runs, store, nil, nil)
	_, err := svc.FindPendingApproval(context.Background(), "sess-mine")
	if err == nil {
		t.Fatal("expected error when only other sessions have pending approvals")
	}
}
