package runtime

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/eventbus"
)

// ---------------------------------------------------------------------------
// WithMemoryStore
// ---------------------------------------------------------------------------

func TestWithMemoryStore(t *testing.T) {
	t.Parallel()
	svc := newFullService()
	result := svc.WithMemoryStore(nil)
	if result != svc {
		t.Fatal("WithMemoryStore should return same service")
	}
	if svc.memory != nil {
		t.Fatal("expected nil memory store")
	}
}

// ---------------------------------------------------------------------------
// AgentRouter
// ---------------------------------------------------------------------------

func TestSetAndGetAgentRouter(t *testing.T) {
	t.Parallel()
	svc := newFullService()
	if svc.AgentRouter() != nil {
		t.Fatal("expected nil agent router initially")
	}
	router := NewAgentRouter(nil)
	svc.SetAgentRouter(router)
	if svc.AgentRouter() != router {
		t.Fatal("AgentRouter() should return the set router")
	}
}

func TestSetAndGetAgentRouterConcurrent(t *testing.T) {
	t.Parallel()

	svc := newFullService()
	done := make(chan struct{})
	var writers sync.WaitGroup

	for i := 0; i < 4; i++ {
		writers.Add(1)
		go func() {
			defer writers.Done()
			for j := 0; j < 200; j++ {
				svc.SetAgentRouter(NewAgentRouter([]AgentProfile{{Name: "agent", Model: "model"}}))
				_ = svc.AgentRouter()
			}
		}()
	}

	for i := 0; i < 4; i++ {
		go func() {
			for {
				select {
				case <-done:
					return
				default:
					_ = svc.AgentRouter()
				}
			}
		}()
	}

	writers.Wait()
	close(done)
}

// ---------------------------------------------------------------------------
// EventsSince
// ---------------------------------------------------------------------------

func TestEventsSinceWithInMemoryBus(t *testing.T) {
	t.Parallel()
	bus := eventbus.NewInMemoryBus()
	_ = bus.Publish(context.Background(), eventbus.Event{Type: eventbus.EventRunCompleted})
	_ = bus.Publish(context.Background(), eventbus.Event{Type: eventbus.EventRunFailed})

	runs := agent.NewInMemoryRunStore()
	svc := NewService(nil, agent.NewInMemorySessionStore(), runs, nil, bus, nil)

	result := svc.EventsSince("", 0)
	if len(result.Events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(result.Events))
	}
}

func TestEventsSinceWithCursor(t *testing.T) {
	t.Parallel()
	bus := eventbus.NewInMemoryBus()
	_ = bus.Publish(context.Background(), eventbus.Event{Type: eventbus.EventRunCompleted})
	_ = bus.Publish(context.Background(), eventbus.Event{Type: eventbus.EventRunFailed})

	runs := agent.NewInMemoryRunStore()
	svc := NewService(nil, agent.NewInMemorySessionStore(), runs, nil, bus, nil)

	all := svc.EventsSince("", 0)
	if len(all.Events) < 2 {
		t.Fatalf("expected at least 2 events, got %d", len(all.Events))
	}

	// Get events since the first one.
	cursor := all.Events[0].ID
	result := svc.EventsSince(cursor, 0)
	if len(result.Events) != 1 {
		t.Fatalf("expected 1 event after cursor, got %d", len(result.Events))
	}
}

func TestEventsSinceWithLimit(t *testing.T) {
	t.Parallel()
	bus := eventbus.NewInMemoryBus()
	for i := 0; i < 5; i++ {
		_ = bus.Publish(context.Background(), eventbus.Event{Type: eventbus.EventRunCompleted})
	}

	runs := agent.NewInMemoryRunStore()
	svc := NewService(nil, agent.NewInMemorySessionStore(), runs, nil, bus, nil)

	result := svc.EventsSince("", 3)
	if len(result.Events) > 3 {
		t.Fatalf("expected at most 3 events, got %d", len(result.Events))
	}
}

func TestEventsSinceReadOnlySnapshotter(t *testing.T) {
	t.Parallel()
	runs := agent.NewInMemoryRunStore()
	svc := NewService(nil, agent.NewInMemorySessionStore(), runs, nil, readOnlySnapshotter{}, nil)

	result := svc.EventsSince("", 0)
	if len(result.Events) != 0 {
		t.Fatalf("expected 0 events, got %d", len(result.Events))
	}
}

func TestEventsSincePrefersReplayReader(t *testing.T) {
	t.Parallel()
	bus := eventbus.NewInMemoryBus()
	_ = bus.Publish(context.Background(), eventbus.Event{ID: "evt-live", Type: eventbus.EventRunCompleted})
	runs := agent.NewInMemoryRunStore()
	svc := NewService(nil, agent.NewInMemorySessionStore(), runs, nil, bus, nil).
		WithEventReader(replayOnlyEventReader{events: []eventbus.Event{
			{ID: "evt-001", Type: eventbus.EventRunCompleted},
			{ID: "evt-002", Type: eventbus.EventRunFailed},
		}})

	result := svc.EventsSince("evt-001", 0)
	if len(result.Events) != 1 {
		t.Fatalf("len(result.Events) = %d, want 1", len(result.Events))
	}
	if result.Events[0].ID != "evt-002" {
		t.Fatalf("result.Events[0].ID = %q, want evt-002", result.Events[0].ID)
	}
}

func TestEventsSinceNilSnapshotter(t *testing.T) {
	t.Parallel()
	runs := agent.NewInMemoryRunStore()
	svc := NewService(nil, agent.NewInMemorySessionStore(), runs, nil, nil, nil)

	result := svc.EventsSince("", 0)
	if len(result.Events) != 0 {
		t.Fatalf("expected 0 events, got %d", len(result.Events))
	}
}

// ---------------------------------------------------------------------------
// SubscribeEvents
// ---------------------------------------------------------------------------

func TestSubscribeEventsWithInMemoryBus(t *testing.T) {
	t.Parallel()
	bus := eventbus.NewInMemoryBus()
	runs := agent.NewInMemoryRunStore()
	svc := NewService(nil, agent.NewInMemorySessionStore(), runs, nil, bus, nil)

	sub := svc.SubscribeEvents(10)
	if sub == nil {
		t.Fatal("expected non-nil subscription with InMemoryBus")
	}
	sub.Close()
}

func TestSubscribeEventsNonBus(t *testing.T) {
	t.Parallel()
	runs := agent.NewInMemoryRunStore()
	svc := NewService(nil, agent.NewInMemorySessionStore(), runs, nil, readOnlySnapshotter{}, nil)

	sub := svc.SubscribeEvents(10)
	if sub != nil {
		t.Fatal("expected nil subscription for non-bus snapshotter")
	}
}

// ---------------------------------------------------------------------------
// CancelRun
// ---------------------------------------------------------------------------

func TestCancelRunNilAgent(t *testing.T) {
	t.Parallel()
	runs := agent.NewInMemoryRunStore()
	svc := NewService(nil, agent.NewInMemorySessionStore(), runs, nil, nil, nil)
	_, err := svc.CancelRun(context.Background(), "run-1")
	if err == nil {
		t.Fatal("expected error when agent is nil")
	}
}

func TestDispatchRunDedupesConcurrentResume(t *testing.T) {
	t.Parallel()

	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	approvals := approval.NewInMemoryStore()
	bus := eventbus.NewInMemoryBus()
	queue := &blockingCoordinator{
		started: make(chan struct{}, 2),
		release: make(chan struct{}),
	}
	component := agent.NewComponent(agent.AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    agent.QueueEnqueue,
	}, sessions, runs, queue, newContextEngine(), &resumeTestModelClient{
		response: &agent.ModelResponse{
			Message: contextengine.Message{
				Role:    contextengine.RoleAssistant,
				Content: "done",
			},
		},
	}, nil, nil).
		WithApprovals(approvals).
		WithEventBus(bus)

	svc := NewService(component, sessions, runs, approvals, bus, nil)

	session, err := sessions.GetOrCreate(context.Background(), "resume-dedupe", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	run, err := runs.Create(context.Background(), session.ID, agent.IncomingMessage{
		SessionKey: "resume-dedupe",
		Content:    "resume me",
	}, agent.AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    agent.QueueEnqueue,
	})
	if err != nil {
		t.Fatalf("runs.Create() error = %v", err)
	}
	ticket, err := approvals.Create(context.Background(), approval.Ticket{
		RunID:     run.ID,
		SessionID: session.ID,
	})
	if err != nil {
		t.Fatalf("approvals.Create() error = %v", err)
	}
	if _, err := approvals.Resolve(context.Background(), ticket.ID, approval.Resolution{
		Status:     approval.StatusApproved,
		ResolvedBy: "tester",
	}); err != nil {
		t.Fatalf("approvals.Resolve() error = %v", err)
	}

	if err := svc.dispatchRun(context.Background(), run.ID, true); err != nil {
		t.Fatalf("dispatchRun(first) error = %v", err)
	}
	select {
	case <-queue.started:
	case <-time.After(2 * time.Second):
		t.Fatal("first resume did not reach StartRun")
	}

	if err := svc.dispatchRun(context.Background(), run.ID, true); err != nil {
		t.Fatalf("dispatchRun(second) error = %v", err)
	}
	if got := queue.startCount.Load(); got != 1 {
		t.Fatalf("StartRun count = %d, want 1", got)
	}

	sub := newRunEventSubscription(t, svc)
	defer sub.Close()

	close(queue.release)
	waitForRunEventStatus(t, svc, sub, run.ID, agent.RunCompleted)
}

// ---------------------------------------------------------------------------
// Memory KV store
// ---------------------------------------------------------------------------

func TestMemoryStoreNilErrors(t *testing.T) {
	t.Parallel()
	runs := agent.NewInMemoryRunStore()
	svc := NewService(nil, agent.NewInMemorySessionStore(), runs, nil, nil, nil)

	_, err := svc.GetMemory(context.Background(), "key")
	if err == nil {
		t.Fatal("expected error for GetMemory with nil store")
	}

	err = svc.SetMemory(context.Background(), "key", "value")
	if err == nil {
		t.Fatal("expected error for SetMemory with nil store")
	}

	err = svc.DeleteMemory(context.Background(), "key")
	if err == nil {
		t.Fatal("expected error for DeleteMemory with nil store")
	}

	_, err = svc.SearchMemory(context.Background(), "query")
	if err == nil {
		t.Fatal("expected error for SearchMemory with nil store")
	}
}

// ---------------------------------------------------------------------------
// ListRuns
// ---------------------------------------------------------------------------

func TestListRunsNonLister(t *testing.T) {
	t.Parallel()
	runs := &nonListerRunStore{}
	svc := NewService(nil, agent.NewInMemorySessionStore(), runs, nil, nil, nil)
	_, err := svc.ListRuns(context.Background(), agent.RunListFilter{})
	if err == nil {
		t.Fatal("expected error for non-lister run store")
	}
}

// nonListerRunStore implements RunStore but not RunLister.
type nonListerRunStore struct{}

func (s *nonListerRunStore) Seen(_ context.Context, _ string, _ time.Duration) bool {
	return false
}
func (s *nonListerRunStore) FindByExternalEvent(_ context.Context, _ string) (*agent.Run, error) {
	return nil, errors.New("not implemented")
}

type blockingCoordinator struct {
	startCount atomic.Int32
	started    chan struct{}
	release    chan struct{}
	once       sync.Once
}

func (c *blockingCoordinator) EnqueueSessionRun(context.Context, string, string, agent.QueueMode) error {
	return nil
}

func (c *blockingCoordinator) NextQueuedRun(context.Context, string) (string, bool, error) {
	return "", false, nil
}

func (c *blockingCoordinator) StartRun(context.Context, string, string) error {
	c.startCount.Add(1)
	c.started <- struct{}{}
	c.once.Do(func() {
		<-c.release
	})
	return nil
}

func (c *blockingCoordinator) FinishRun(context.Context, string, string) error {
	return nil
}

type closeBlockingModelClient struct {
	started chan struct{}
	done    chan struct{}
	once    sync.Once
}

func (m *closeBlockingModelClient) Chat(ctx context.Context, _ agent.ChatRequest) (*agent.ModelResponse, error) {
	m.once.Do(func() {
		close(m.started)
	})
	<-ctx.Done()
	close(m.done)
	return nil, ctx.Err()
}

type resumeTestModelClient struct {
	response *agent.ModelResponse
}

func (m *resumeTestModelClient) Chat(context.Context, agent.ChatRequest) (*agent.ModelResponse, error) {
	return m.response, nil
}
func (s *nonListerRunStore) Create(_ context.Context, _ string, _ agent.IncomingMessage, _ agent.AgentConfig) (*agent.Run, error) {
	return nil, errors.New("not implemented")
}
func (s *nonListerRunStore) ClaimQueuedRun(_ context.Context, _ string) (*agent.Run, bool, error) {
	return nil, false, errors.New("not implemented")
}
func (s *nonListerRunStore) Get(_ context.Context, _ string) (*agent.Run, error) {
	return nil, errors.New("not implemented")
}
func (s *nonListerRunStore) Update(_ context.Context, _ *agent.Run) error {
	return errors.New("not implemented")
}

// ---------------------------------------------------------------------------
// ListSessions
// ---------------------------------------------------------------------------

func TestListSessionsNonLister(t *testing.T) {
	t.Parallel()
	sessions := &nonListerSessionStore{}
	runs := agent.NewInMemoryRunStore()
	svc := NewService(nil, sessions, runs, nil, nil, nil)
	_, err := svc.ListSessions(context.Background())
	if err == nil {
		t.Fatal("expected error for non-lister session store")
	}
}

// nonListerSessionStore implements SessionStore but not SessionLister.
type nonListerSessionStore struct{}

func (s *nonListerSessionStore) GetOrCreate(_ context.Context, _ string, _ string, _ ...string) (*agent.Session, error) {
	return nil, errors.New("not implemented")
}
func (s *nonListerSessionStore) AppendUserMessage(_ context.Context, _ string, _ agent.IncomingMessage) error {
	return errors.New("not implemented")
}
func (s *nonListerSessionStore) LoadForExecution(_ context.Context, _ string) (*agent.Session, func(), error) {
	return nil, func() {}, errors.New("not implemented")
}
func (s *nonListerSessionStore) Save(_ context.Context, _ *agent.Session) error {
	return errors.New("not implemented")
}

// ---------------------------------------------------------------------------
// GetSession
// ---------------------------------------------------------------------------

func TestGetSessionNonReader(t *testing.T) {
	t.Parallel()
	sessions := &nonListerSessionStore{}
	runs := agent.NewInMemoryRunStore()
	svc := NewService(nil, sessions, runs, nil, nil, nil)
	_, err := svc.GetSession(context.Background(), "sess-1")
	if err == nil {
		t.Fatal("expected error for non-reader session store")
	}
}

func TestScopedSessionAndRunAccess(t *testing.T) {
	t.Parallel()

	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	svc := NewService(nil, sessions, runs, nil, nil, nil)
	ctx := context.Background()

	session, err := sessions.GetOrCreate(ctx, "scope:test", "model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	if err := sessions.AppendUserMessage(ctx, session.ID, agent.IncomingMessage{
		Content: "hello",
	}); err != nil {
		t.Fatalf("AppendUserMessage() error = %v", err)
	}
	run, err := runs.Create(ctx, session.ID, agent.IncomingMessage{
		Content: "run",
	}, agent.AgentConfig{DefaultModel: "model", QueueMode: agent.QueueEnqueue})
	if err != nil {
		t.Fatalf("runs.Create() error = %v", err)
	}

	scope := agent.ScopeFilter{}
	if _, err := svc.GetSessionScoped(ctx, session.ID, scope); err != nil {
		t.Fatalf("GetSessionScoped() error = %v", err)
	}
	if _, err := svc.GetRunScoped(ctx, run.ID, scope); err != nil {
		t.Fatalf("GetRunScoped() error = %v", err)
	}

	items, err := svc.ListSessionsFiltered(ctx, agent.SessionListFilter{Scope: scope})
	if err != nil {
		t.Fatalf("ListSessionsFiltered() error = %v", err)
	}
	if len(items) != 1 || items[0].ID != session.ID {
		t.Fatalf("ListSessionsFiltered() = %#v", items)
	}
	if got, err := svc.getSessionByKeyScoped(ctx, "scope:test", scope); err != nil || got == nil || got.ID != session.ID {
		t.Fatalf("getSessionByKeyScoped() = (%#v, %v)", got, err)
	}
}

func TestRecoverOrphanedRunsMarksRunningAsFailed(t *testing.T) {
	t.Parallel()

	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	bus := eventbus.NewInMemoryBus()
	svc := NewService(nil, sessions, runs, nil, bus, nil)
	ctx := context.Background()

	session, err := sessions.GetOrCreate(ctx, "slack:ops", "model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	approvals := approval.NewInMemoryStore()
	svc.approvals = approvals
	statuses := []agent.RunStatus{
		agent.RunQueued,
		agent.RunRunning,
		agent.RunStreaming,
		agent.RunWaitingInput,
		agent.RunWaitingApproval,
	}
	var runIDs []string
	for _, status := range statuses {
		run, err := runs.Create(ctx, session.ID, agent.IncomingMessage{
			Content: "resume me later",
		}, agent.AgentConfig{DefaultModel: "model", QueueMode: agent.QueueEnqueue})
		if err != nil {
			t.Fatalf("runs.Create() error = %v", err)
		}
		run.Status = status
		run.StartedAt = time.Now().Add(-time.Minute).UTC()
		run.PendingTools = []agent.ToolCall{{ID: "call-1", Name: "fs.write"}}
		if status == agent.RunWaitingApproval {
			ticket, err := approvals.Create(ctx, approval.Ticket{
				RunID:     run.ID,
				SessionID: session.ID,
			})
			if err != nil {
				t.Fatalf("approvals.Create() error = %v", err)
			}
			run.ApprovalID = ticket.ID
		}
		if err := runs.Update(ctx, run); err != nil {
			t.Fatalf("runs.Update() error = %v", err)
		}
		runIDs = append(runIDs, run.ID)
	}

	recovered, err := svc.RecoverOrphanedRuns(ctx, "process_restart")
	if err != nil {
		t.Fatalf("RecoverOrphanedRuns() error = %v", err)
	}
	if recovered != len(statuses) {
		t.Fatalf("recovered = %d, want %d", recovered, len(statuses))
	}
	for _, runID := range runIDs {
		got, err := runs.Get(ctx, runID)
		if err != nil {
			t.Fatalf("runs.Get() error = %v", err)
		}
		if got.Status != agent.RunFailed {
			t.Fatalf("status = %q, want %q", got.Status, agent.RunFailed)
		}
		if got.Error != "process_restart" {
			t.Fatalf("error = %q, want process_restart", got.Error)
		}
		if got.ApprovalID != "" {
			t.Fatalf("ApprovalID = %q, want cleared", got.ApprovalID)
		}
		if len(got.PendingTools) != 0 {
			t.Fatalf("PendingTools = %#v, want cleared", got.PendingTools)
		}
	}
	events := bus.Snapshot()
	if len(events) < len(statuses) || events[len(events)-1].Type != eventbus.EventRunFailed {
		t.Fatalf("events = %#v", events)
	}
	if events[len(events)-1].Attrs["channel"] != "slack" {
		t.Fatalf("channel attr = %#v", events[len(events)-1].Attrs["channel"])
	}
	pendingApproval, err := approvals.List(ctx, approval.ListFilter{Status: approval.StatusPending})
	if err != nil {
		t.Fatalf("approvals.List() error = %v", err)
	}
	if len(pendingApproval) != 0 {
		t.Fatalf("pending approvals = %d, want 0", len(pendingApproval))
	}
}

func TestPruneStateUsesConfiguredRetention(t *testing.T) {
	t.Parallel()

	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	svc := NewService(nil, sessions, runs, nil, nil, nil).WithDataRetention(DataRetentionPolicy{
		Sessions: time.Hour,
		Runs:     time.Hour,
	})
	ctx := context.Background()

	session, err := sessions.GetOrCreate(ctx, "prune:test", "model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	session.UpdatedAt = time.Now().Add(-2 * time.Hour).UTC()
	if err := sessions.Save(ctx, session); err != nil {
		t.Fatalf("sessions.Save() error = %v", err)
	}
	run, err := runs.Create(ctx, session.ID, agent.IncomingMessage{Content: "done"}, agent.AgentConfig{
		DefaultModel: "model",
		QueueMode:    agent.QueueEnqueue,
	})
	if err != nil {
		t.Fatalf("runs.Create() error = %v", err)
	}
	run.Status = agent.RunCompleted
	run.FinishedAt = time.Now().Add(-2 * time.Hour).UTC()
	if err := runs.Update(ctx, run); err != nil {
		t.Fatalf("runs.Update() error = %v", err)
	}
	activeSession, err := sessions.GetOrCreate(ctx, "prune:active", "model")
	if err != nil {
		t.Fatalf("GetOrCreate(activeSession) error = %v", err)
	}
	activeSession.UpdatedAt = time.Now().Add(-2 * time.Hour).UTC()
	if err := sessions.Save(ctx, activeSession); err != nil {
		t.Fatalf("sessions.Save(activeSession) error = %v", err)
	}
	activeRun, err := runs.Create(ctx, activeSession.ID, agent.IncomingMessage{Content: "still running"}, agent.AgentConfig{
		DefaultModel: "model",
		QueueMode:    agent.QueueEnqueue,
	})
	if err != nil {
		t.Fatalf("runs.Create(activeRun) error = %v", err)
	}
	activeRun.Status = agent.RunRunning
	if err := runs.Update(ctx, activeRun); err != nil {
		t.Fatalf("runs.Update(activeRun) error = %v", err)
	}

	result, err := svc.PruneState(ctx)
	if err != nil {
		t.Fatalf("PruneState() error = %v", err)
	}
	if result.SessionsDeleted != 1 || result.RunsDeleted != 1 {
		t.Fatalf("result = %#v", result)
	}
	if _, err := sessions.Get(ctx, session.ID); err == nil {
		t.Fatal("session should have been pruned")
	}
	if _, err := runs.Get(ctx, run.ID); err == nil {
		t.Fatal("run should have been pruned")
	}
	if _, err := sessions.Get(ctx, activeSession.ID); err != nil {
		t.Fatalf("active session should have been preserved, got error %v", err)
	}
	if _, err := runs.Get(ctx, activeRun.ID); err != nil {
		t.Fatalf("active run should have been preserved, got error %v", err)
	}
}

// ---------------------------------------------------------------------------
// ResumeRun
// ---------------------------------------------------------------------------

func TestResumeRunCompletedRun(t *testing.T) {
	t.Parallel()
	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	comp := agent.NewComponent(agent.AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 3,
		QueueMode:     agent.QueueEnqueue,
	}, sessions, runs, agent.NewInMemoryCoordinator(), newContextEngine(), mockModelClient{}, nil, nil)

	svc := NewService(comp, sessions, runs, nil, nil, nil)

	run, err := svc.Submit(context.Background(), SubmitRequest{
		SessionKey: "resume-completed",
		Content:    "test",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	waitForRunStatus(t, svc, run.ID, agent.RunCompleted)

	// Try to resume a completed run — should fail.
	_, err = svc.ResumeRun(context.Background(), run.ID)
	if err == nil {
		t.Fatal("expected error for resuming completed run")
	}
}

func TestResumeRunNotFound(t *testing.T) {
	t.Parallel()
	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	comp := agent.NewComponent(agent.AgentConfig{
		DefaultModel:  "test-model",
		MaxToolRounds: 3,
		QueueMode:     agent.QueueEnqueue,
	}, sessions, runs, agent.NewInMemoryCoordinator(), newContextEngine(), mockModelClient{}, nil, nil)

	svc := NewService(comp, sessions, runs, nil, nil, nil)
	_, err := svc.ResumeRun(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent run")
	}
}

// ---------------------------------------------------------------------------
// Submit with agent routing
// ---------------------------------------------------------------------------

func TestSubmitWithAgentRouter(t *testing.T) {
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

	router := NewAgentRouter([]AgentProfile{
		{Name: "sales", Model: "gpt-4", SystemPrompt: "You are a sales bot."},
	})
	svc.SetAgentRouter(router)

	run, err := svc.Submit(context.Background(), SubmitRequest{
		SessionKey: "agent:sales:user123",
		Content:    "hello",
	})
	if err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	if run == nil {
		t.Fatal("expected non-nil run")
	}
	waitForRunStatus(t, svc, run.ID, agent.RunCompleted)
}

// ---------------------------------------------------------------------------
// Service retention settings
// ---------------------------------------------------------------------------

func TestWithArtifactRetentionChaining(t *testing.T) {
	t.Parallel()
	svc := newFullService()
	ret := 48 * time.Hour
	result := svc.WithArtifactRetention(ret)
	if result != svc {
		t.Fatal("WithArtifactRetention should return same service for chaining")
	}
	if svc.retention != ret {
		t.Fatalf("retention = %v, want %v", svc.retention, ret)
	}
}

// ---------------------------------------------------------------------------
// dispatchRun
// ---------------------------------------------------------------------------

func TestDispatchRunNilAgent(t *testing.T) {
	t.Parallel()
	runs := agent.NewInMemoryRunStore()
	svc := NewService(nil, agent.NewInMemorySessionStore(), runs, nil, nil, nil)
	err := svc.dispatchRun(context.Background(), "run-1", false)
	if err == nil {
		t.Fatal("expected error when agent is nil")
	}
}

func TestDispatchRunEmptyRunID(t *testing.T) {
	t.Parallel()
	svc := newFullService()
	err := svc.dispatchRun(context.Background(), "", false)
	if err == nil {
		t.Fatal("expected error for empty run ID")
	}
}

func TestServiceCloseCancelsBackgroundDispatch(t *testing.T) {
	t.Parallel()

	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	model := &closeBlockingModelClient{
		started: make(chan struct{}),
		done:    make(chan struct{}),
	}
	component := agent.NewComponent(agent.AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    agent.QueueEnqueue,
	}, sessions, runs, agent.NewInMemoryCoordinator(), newContextEngine(), model, noOpToolExecutor{}, nil)
	svc := NewService(component, sessions, runs, nil, nil, nil)

	session, err := sessions.GetOrCreate(context.Background(), "close-dispatch", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	run, err := runs.Create(context.Background(), session.ID, agent.IncomingMessage{
		SessionKey: "close-dispatch",
		Content:    "block until service close",
	}, agent.AgentConfig{
		DefaultModel: "test-model",
		QueueMode:    agent.QueueEnqueue,
	})
	if err != nil {
		t.Fatalf("runs.Create() error = %v", err)
	}

	if err := svc.dispatchRun(context.Background(), run.ID, false); err != nil {
		t.Fatalf("dispatchRun() error = %v", err)
	}
	select {
	case <-model.started:
	case <-time.After(2 * time.Second):
		t.Fatal("background dispatch did not reach model client")
	}

	if err := svc.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	select {
	case <-model.done:
	case <-time.After(2 * time.Second):
		t.Fatal("background dispatch did not stop after Close")
	}
	if _, ok := svc.dispatching.Load(run.ID); ok {
		t.Fatalf("dispatching still contains %q after Close", run.ID)
	}
}

func TestServiceCloseStopsRateLimiter(t *testing.T) {
	t.Parallel()

	svc := newFullService()
	rl := NewSessionRateLimiter(60, 2)
	if rl == nil {
		t.Fatal("expected non-nil rate limiter")
	}
	svc.WithRateLimiter(rl)

	if err := svc.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	select {
	case <-rl.stop:
	default:
		t.Fatal("expected Close to stop the rate limiter")
	}
}

// ---------------------------------------------------------------------------
// FindPendingApproval edge cases
// ---------------------------------------------------------------------------

func TestFindPendingApprovalSkipsResolvedTickets(t *testing.T) {
	t.Parallel()
	store := approval.NewInMemoryStore()
	ticket, _ := store.Create(context.Background(), approval.Ticket{
		RunID:     "run-resolved",
		SessionID: "sess-resolved",
	})
	_, _ = store.Resolve(context.Background(), ticket.ID, approval.Resolution{Status: approval.StatusApproved})

	runs := agent.NewInMemoryRunStore()
	svc := NewService(nil, agent.NewInMemorySessionStore(), runs, store, nil, nil)
	_, err := svc.FindPendingApproval(context.Background(), "sess-resolved")
	if err == nil {
		t.Fatal("expected error when only resolved tickets exist")
	}
}
