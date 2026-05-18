package bootstrap

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/automation"
	"github.com/fulcrus/hopclaw/channels"
	channelmgr "github.com/fulcrus/hopclaw/channels/manager"
	"github.com/fulcrus/hopclaw/contextengine"
	controlgov "github.com/fulcrus/hopclaw/internal/controlplane/governanceadapter"
	"github.com/fulcrus/hopclaw/internal/meta"
)

type runtimeResilienceAdapter struct {
	mu   sync.Mutex
	sent []channels.OutboundMessage
}

func (*runtimeResilienceAdapter) Connect(context.Context) error { return nil }

func (*runtimeResilienceAdapter) Disconnect(context.Context) error { return nil }

func (a *runtimeResilienceAdapter) Send(_ context.Context, msg channels.OutboundMessage) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sent = append(a.sent, msg)
	return nil
}

func (*runtimeResilienceAdapter) Capabilities() channels.ChannelCapabilityDescriptor {
	return channels.ChannelCapabilityDescriptor{}
}

func (*runtimeResilienceAdapter) Status() channels.Status { return channels.StatusConnected }

func (*runtimeResilienceAdapter) SubscribeEvents() <-chan channels.InboundMessage { return nil }

func (a *runtimeResilienceAdapter) Messages() []channels.OutboundMessage {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]channels.OutboundMessage, len(a.sent))
	copy(out, a.sent)
	return out
}

type runtimeResilienceRestoreCall struct {
	target channels.RunNotificationTarget
	runID  string
	status agent.RunStatus
}

type runtimeResilienceBridge struct {
	mu    sync.Mutex
	calls []runtimeResilienceRestoreCall
}

func (*runtimeResilienceBridge) Start(context.Context) {}

func (*runtimeResilienceBridge) Stop() {}

func (b *runtimeResilienceBridge) RestoreRun(_ context.Context, target channels.RunNotificationTarget, run *agent.Run) bool {
	if run == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls = append(b.calls, runtimeResilienceRestoreCall{
		target: target,
		runID:  run.ID,
		status: run.Status,
	})
	return true
}

func (b *runtimeResilienceBridge) Calls() []runtimeResilienceRestoreCall {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]runtimeResilienceRestoreCall, len(b.calls))
	copy(out, b.calls)
	return out
}

func TestChannelCronDelivererWaitsForReadyAndSwapsManagers(t *testing.T) {
	t.Parallel()

	firstManager := channelmgr.New()
	firstAdapter := &runtimeResilienceAdapter{}
	if err := firstManager.Register("slack", firstAdapter); err != nil {
		t.Fatalf("Register(first) error = %v", err)
	}
	secondManager := channelmgr.New()
	secondAdapter := &runtimeResilienceAdapter{}
	if err := secondManager.Register("slack", secondAdapter); err != nil {
		t.Fatalf("Register(second) error = %v", err)
	}

	deliverer := newChannelCronDeliverer(firstManager)
	target := automation.DeliveryTarget{Channel: "slack", Target: "ops-room"}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- deliverer.DeliverMessage(ctx, target, "first message")
	}()

	select {
	case err := <-done:
		t.Fatalf("DeliverMessage() returned before ready: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	deliverer.MarkReady()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("DeliverMessage() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("DeliverMessage() did not resume after MarkReady")
	}

	if got := firstAdapter.Messages(); len(got) != 1 || got[0].Content != "first message" {
		t.Fatalf("first adapter messages = %#v", got)
	}

	deliverer.MarkNotReady()
	deliverer.SetChannels(secondManager)
	done = make(chan error, 1)
	go func() {
		done <- deliverer.DeliverMessage(ctx, target, "second message")
	}()

	select {
	case err := <-done:
		t.Fatalf("DeliverMessage() returned while not ready after swap: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	deliverer.MarkReady()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("DeliverMessage() after swap error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("DeliverMessage() did not resume after swapped manager became ready")
	}

	if got := secondAdapter.Messages(); len(got) != 1 || got[0].Content != "second message" {
		t.Fatalf("second adapter messages = %#v", got)
	}
	if got := firstAdapter.Messages(); len(got) != 1 {
		t.Fatalf("first adapter should not receive swapped delivery, got %#v", got)
	}
}

func TestSyncActiveChannelSessionsForReloadRestoresOnlyActiveRunsWithRunScopedTarget(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sessions := agent.NewInMemorySessionStore()
	runs := agent.NewInMemoryRunStore()
	session, err := sessions.GetOrCreate(ctx, "slack:C-reload", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	locked, unlock, err := sessions.LoadForExecution(ctx, session.ID)
	if err != nil {
		t.Fatalf("LoadForExecution() error = %v", err)
	}
	now := time.Now().UTC()
	locked.Messages = append(locked.Messages,
		contextengine.Message{
			Role:      contextengine.RoleUser,
			Content:   "first question",
			CreatedAt: now,
			Metadata: map[string]any{
				meta.KeyChannel:   "slack",
				meta.KeyChannelID: "C-reload",
				meta.KeyMessageID: "msg-1",
			},
		},
		contextengine.Message{
			Role:      contextengine.RoleUser,
			Content:   "second question",
			CreatedAt: now.Add(time.Second),
			Metadata: map[string]any{
				meta.KeyChannel:   "slack",
				meta.KeyChannelID: "C-reload",
				meta.KeyMessageID: "msg-2",
			},
		},
	)
	locked.UpdatedAt = now.Add(time.Second)
	if err := sessions.Save(ctx, locked); err != nil {
		unlock()
		t.Fatalf("Save() error = %v", err)
	}
	unlock()

	activeRun, err := runs.Create(ctx, session.ID, agent.IncomingMessage{ExternalEventID: "msg-1"}, agent.AgentConfig{DefaultModel: "test-model"})
	if err != nil {
		t.Fatalf("Create(active) error = %v", err)
	}
	activeRun.Status = agent.RunWaitingApproval
	if err := runs.Update(ctx, activeRun); err != nil {
		t.Fatalf("Update(active) error = %v", err)
	}

	completedRun, err := runs.Create(ctx, session.ID, agent.IncomingMessage{ExternalEventID: "msg-2"}, agent.AgentConfig{DefaultModel: "test-model"})
	if err != nil {
		t.Fatalf("Create(completed) error = %v", err)
	}
	completedRun.Status = agent.RunCompleted
	if err := runs.Update(ctx, completedRun); err != nil {
		t.Fatalf("Update(completed) error = %v", err)
	}

	bridge := &runtimeResilienceBridge{}
	app := &App{
		AppStoreState: AppStoreState{
			Sessions: sessions,
			Runs:     runs,
		},
	}

	app.syncActiveChannelSessionsForReload(ctx, []string{"slack"}, []namedChannelBridge{{
		name:   "slack",
		bridge: bridge,
	}})

	calls := bridge.Calls()
	if len(calls) != 1 {
		t.Fatalf("RestoreRun() calls = %#v, want exactly one active run", calls)
	}
	if calls[0].runID != activeRun.ID {
		t.Fatalf("restored run ID = %q, want %q", calls[0].runID, activeRun.ID)
	}
	if calls[0].target.RunID != activeRun.ID {
		t.Fatalf("target.RunID = %q, want %q", calls[0].target.RunID, activeRun.ID)
	}
	if calls[0].target.TargetID != "C-reload" {
		t.Fatalf("target.TargetID = %q, want %q", calls[0].target.TargetID, "C-reload")
	}
	if calls[0].target.ReplyToID != "msg-1" {
		t.Fatalf("target.ReplyToID = %q, want %q", calls[0].target.ReplyToID, "msg-1")
	}
	if calls[0].target.InputContent != "first question" {
		t.Fatalf("target.InputContent = %q, want %q", calls[0].target.InputContent, "first question")
	}
}

func TestStopGovernanceInfrastructureStopsDistinctDispatcherPaths(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	primary := controlgov.NewReliableDispatcher(controlgov.DeliveryConfig{}, nil)
	secondary := controlgov.NewReliableDispatcher(controlgov.DeliveryConfig{}, nil)
	t.Cleanup(primary.Stop)
	t.Cleanup(secondary.Stop)

	control := &dynamicGovernanceDispatcher{}
	control.Swap(ctx, primary)
	secondary.Start(ctx)

	app := &App{
		appInternalState: appInternalState{
			governanceControl:    control,
			governanceDispatcher: secondary,
		},
	}

	app.stopGovernanceInfrastructure()

	if current := control.current(); current != nil {
		t.Fatalf("governanceControl current = %#v, want nil", current)
	}
	if app.governanceDispatcher != nil {
		t.Fatalf("governanceDispatcher = %#v, want nil", app.governanceDispatcher)
	}
	if governanceDispatcherStarted(primary) {
		t.Fatal("primary dispatcher should be stopped")
	}
	if governanceDispatcherStarted(secondary) {
		t.Fatal("secondary dispatcher should be stopped")
	}
}

func governanceDispatcherStarted(dispatcher *controlgov.ReliableDispatcher) bool {
	if dispatcher == nil {
		return false
	}
	value := reflect.ValueOf(dispatcher)
	if value.IsNil() {
		return false
	}
	field := value.Elem().FieldByName("cancel")
	return field.IsValid() && !field.IsNil()
}
