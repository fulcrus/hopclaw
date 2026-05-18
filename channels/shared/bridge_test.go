package shared

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/artifact"
	"github.com/fulcrus/hopclaw/channels"
	"github.com/fulcrus/hopclaw/eventbus"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
)

type stubAdapter struct {
	inbound chan channels.InboundMessage
}

func (a *stubAdapter) Connect(context.Context) error    { return nil }
func (a *stubAdapter) Disconnect(context.Context) error { return nil }
func (a *stubAdapter) Send(context.Context, channels.OutboundMessage) error {
	return nil
}
func (a *stubAdapter) Capabilities() channels.ChannelCapabilityDescriptor {
	return channels.Capabilities{}
}
func (a *stubAdapter) Status() channels.Status                         { return channels.StatusConnected }
func (a *stubAdapter) SubscribeEvents() <-chan channels.InboundMessage { return a.inbound }

type stubRuntime struct {
	run        *agent.Run
	approval   *approval.Ticket
	resolveErr error
	resolved   []approval.Resolution
}

func (r *stubRuntime) Submit(context.Context, runtimesvc.SubmitRequest) (*agent.Run, error) {
	return nil, nil
}
func (r *stubRuntime) GetRun(context.Context, string) (*agent.Run, error) {
	copyRun := *r.run
	return &copyRun, nil
}
func (r *stubRuntime) GetApproval(context.Context, string) (*approval.Ticket, error) {
	copyTicket := *r.approval
	return &copyTicket, nil
}
func (r *stubRuntime) FindPendingApproval(context.Context, string) (*approval.Ticket, error) {
	return nil, nil
}
func (r *stubRuntime) ResolveApproval(_ context.Context, _ string, resolution approval.Resolution) (*approval.Ticket, error) {
	if r.resolveErr != nil {
		return nil, r.resolveErr
	}
	r.resolved = append(r.resolved, resolution)
	copyTicket := *r.approval
	copyTicket.Status = resolution.Status
	return &copyTicket, nil
}
func (r *stubRuntime) CancelRun(context.Context, string) (*agent.Run, error) {
	return nil, nil
}
func (r *stubRuntime) GetArtifact(context.Context, string) (*artifact.Blob, error) {
	return nil, nil
}
func (r *stubRuntime) Interact(context.Context, runtimesvc.InteractionRequest) (*runtimesvc.InteractionResult, error) {
	return &runtimesvc.InteractionResult{
		Decision:     runtimesvc.InteractionDecision{ReplyAct: runtimesvc.ReplyActChatReply},
		ReplyMessage: "ok",
	}, nil
}

type stubNotifier struct {
	mu     sync.Mutex
	runIDs []string
}

func (n *stubNotifier) NotifyAutoApproved(_ context.Context, runID string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.runIDs = append(n.runIDs, runID)
	return true
}

func TestStartBridgeLoopsHandlesInboundAndOutbound(t *testing.T) {
	t.Parallel()

	adapter := &stubAdapter{inbound: make(chan channels.InboundMessage, 1)}
	bus := eventbus.NewInMemoryBus()
	inboundSeen := make(chan string, 1)
	outboundSeen := make(chan eventbus.EventType, 1)

	cancel := StartBridgeLoops(
		context.Background(),
		adapter,
		bus,
		func(_ context.Context, msg channels.InboundMessage) {
			inboundSeen <- msg.Content
		},
		func(ctx context.Context, sub *eventbus.Subscription) {
			defer sub.Close()
			select {
			case <-ctx.Done():
			case event := <-sub.Events():
				outboundSeen <- event.Type
			}
		},
	)
	defer cancel()

	adapter.inbound <- channels.InboundMessage{Content: "hello"}
	if err := bus.Publish(context.Background(), eventbus.Event{Type: eventbus.EventRunCompleted}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	select {
	case got := <-inboundSeen:
		if got != "hello" {
			t.Fatalf("inbound content = %q, want hello", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for inbound loop")
	}

	select {
	case got := <-outboundSeen:
		if got != eventbus.EventRunCompleted {
			t.Fatalf("outbound event = %q, want %q", got, eventbus.EventRunCompleted)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for outbound loop")
	}
}

func TestApprovalForEventFallsBackToRunApprovalID(t *testing.T) {
	t.Parallel()

	runtime := &stubRuntime{
		run:      &agent.Run{ID: "run-1", ApprovalID: "approval-1"},
		approval: &approval.Ticket{ID: "approval-1", Status: approval.StatusPending},
	}

	ticket, err := ApprovalForEvent(context.Background(), runtime, eventbus.Event{RunID: "run-1"})
	if err != nil {
		t.Fatalf("ApprovalForEvent() error = %v", err)
	}
	if ticket == nil || ticket.ID != "approval-1" {
		t.Fatalf("ApprovalForEvent() = %#v, want approval-1", ticket)
	}
}

func TestTryAutoApproveSkillInstallResolvesPendingApproval(t *testing.T) {
	t.Parallel()

	sessions := agent.NewInMemorySessionStore()
	session, err := sessions.GetOrCreate(context.Background(), "shared:auto-approve", "test-model", "sess-1")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	if err := channels.EnableSessionAutoApproveSession(context.Background(), sessions, session.ID); err != nil {
		t.Fatalf("EnableSessionAutoApproveSession() error = %v", err)
	}

	runtime := &stubRuntime{
		run: &agent.Run{ID: "run-1", ApprovalID: "approval-1"},
		approval: &approval.Ticket{
			ID:        "approval-1",
			RunID:     "run-1",
			SessionID: session.ID,
			Status:    approval.StatusPending,
		},
	}
	notifier := &stubNotifier{}

	approved, err := TryAutoApproveSkillInstall(context.Background(), runtime, sessions, notifier, eventbus.Event{
		RunID:     "run-1",
		SessionID: session.ID,
	})
	if err != nil {
		t.Fatalf("TryAutoApproveSkillInstall() error = %v", err)
	}
	if !approved {
		t.Fatal("TryAutoApproveSkillInstall() = false, want true")
	}
	if len(runtime.resolved) != 1 || runtime.resolved[0].Status != approval.StatusApproved {
		t.Fatalf("resolved = %#v, want approved resolution", runtime.resolved)
	}
	if len(notifier.runIDs) != 1 || notifier.runIDs[0] != "run-1" {
		t.Fatalf("notifier runIDs = %#v", notifier.runIDs)
	}
}
