package webhook

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/artifact"
	"github.com/fulcrus/hopclaw/channels"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/internal/meta"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
)

func TestBridgeHandleInboundSubmitsRun(t *testing.T) {
	t.Parallel()

	adapter := &stubBridgeAdapter{inbound: make(chan channels.InboundMessage)}
	runtime := &stubBridgeRuntime{
		submitRun: &agent.Run{ID: "run-wh-1"},
	}
	sessionStore := agent.NewInMemorySessionStore()
	bridge := NewBridge("wh-test", adapter, runtime, sessionStore, nil, channels.DefaultStatusReminderDelay)

	bridge.HandleInboundMessage(context.Background(), channels.InboundMessage{
		ChannelID: "webhook:wh-test",
		SenderID:  "user-1",
		Content:   "hello from webhook",
	})

	if runtime.submits != 1 {
		t.Fatalf("runtime.submits = %d, want 1", runtime.submits)
	}
}

func TestBridgeHandleInboundIgnoresEmptyContent(t *testing.T) {
	t.Parallel()

	adapter := &stubBridgeAdapter{inbound: make(chan channels.InboundMessage)}
	runtime := &stubBridgeRuntime{
		submitRun: &agent.Run{ID: "run-wh-empty"},
	}
	sessionStore := agent.NewInMemorySessionStore()
	bridge := NewBridge("wh-empty", adapter, runtime, sessionStore, nil, channels.DefaultStatusReminderDelay)

	bridge.HandleInboundMessage(context.Background(), channels.InboundMessage{
		ChannelID: "webhook:wh-empty",
		SenderID:  "user-1",
		Content:   "   ",
	})

	if runtime.submits != 0 {
		t.Fatalf("runtime.submits = %d, want 0", runtime.submits)
	}
}

func TestBridgeHandleInboundNotifiesSubmitFailure(t *testing.T) {
	t.Parallel()

	adapter := &stubBridgeAdapter{inbound: make(chan channels.InboundMessage)}
	runtime := &stubBridgeRuntime{
		submitErr: errors.New("service unavailable"),
	}
	sessionStore := agent.NewInMemorySessionStore()
	bridge := NewBridge("wh-fail", adapter, runtime, sessionStore, nil, channels.DefaultStatusReminderDelay)

	bridge.HandleInboundMessage(context.Background(), channels.InboundMessage{
		ChannelID: "webhook:wh-fail",
		SenderID:  "user-1",
		Content:   "please process",
	})

	sent := adapter.Messages()
	if len(sent) != 1 {
		t.Fatalf("len(sent) = %d, want 1", len(sent))
	}
}

func TestBridgeUsesInteractForIdleChatReplies(t *testing.T) {
	t.Parallel()

	adapter := &stubBridgeAdapter{inbound: make(chan channels.InboundMessage)}
	runtime := &stubInteractRuntime{
		stubBridgeRuntime: &stubBridgeRuntime{},
		interactionResp: &runtimesvc.InteractionResult{
			Decision: runtimesvc.InteractionDecision{
				ReplyAct: runtimesvc.ReplyActChatReply,
			},
			ReplyMessage: "ok",
		},
	}
	sessionStore := agent.NewInMemorySessionStore()
	bridge := NewBridge("wh-chat", adapter, runtime, sessionStore, nil, channels.DefaultStatusReminderDelay)

	bridge.HandleInboundMessage(context.Background(), channels.InboundMessage{
		ChannelID: "webhook:wh-chat",
		SenderID:  "user-1",
		Content:   "thanks",
	})

	if runtime.interactions != 1 {
		t.Fatalf("runtime.interactions = %d, want 1", runtime.interactions)
	}
	if runtime.submits != 0 {
		t.Fatalf("runtime.submits = %d, want 0", runtime.submits)
	}
	sent := adapter.Messages()
	if len(sent) != 1 {
		t.Fatalf("len(sent) = %d", len(sent))
	}
	if got := sent[0].Metadata["status_kind"]; got != "chat_reply" {
		t.Fatalf("status_kind = %#v", got)
	}
}

func TestBridgeDeduplicatesTerminalRunDelivery(t *testing.T) {
	t.Parallel()

	sessionStore := agent.NewInMemorySessionStore()
	session, err := sessionStore.GetOrCreate(context.Background(), "webhook:wh-dedup:user-1", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	locked, unlock, err := sessionStore.LoadForExecution(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("LoadForExecution() error = %v", err)
	}
	now := time.Now().UTC()
	locked.Messages = append(locked.Messages,
		contextengine.Message{
			Role:      contextengine.RoleUser,
			Content:   "test task",
			CreatedAt: now,
			Metadata: map[string]any{
				meta.KeyChannelName: "webhook:wh-dedup",
				"sender_id":         "user-1",
				"message_id":        "msg-dedup",
			},
		},
		contextengine.Message{
			Role:      contextengine.RoleAssistant,
			Content:   "task result",
			CreatedAt: now.Add(time.Second),
			Metadata:  map[string]any{"run_id": "run-dedup"},
		},
	)
	locked.UpdatedAt = now.Add(time.Second)
	if err := sessionStore.Save(context.Background(), locked); err != nil {
		unlock()
		t.Fatalf("Save() error = %v", err)
	}
	unlock()

	adapter := &stubBridgeAdapter{inbound: make(chan channels.InboundMessage)}
	runtime := &stubBridgeRuntime{
		runs: map[string]*agent.Run{
			"run-dedup": {
				ID:           "run-dedup",
				SessionID:    session.ID,
				InputEventID: "msg-dedup",
			},
		},
	}
	bridge := NewBridge("wh-dedup", adapter, runtime, sessionStore, nil, channels.DefaultStatusReminderDelay)

	event := eventbus.Event{
		Type:      eventbus.EventRunCompleted,
		RunID:     "run-dedup",
		SessionID: session.ID,
	}

	bridge.HandleTerminalRun(context.Background(), event)
	bridge.HandleTerminalRun(context.Background(), event)

	sent := adapter.Messages()
	if len(sent) != 1 {
		t.Fatalf("len(sent) = %d, want 1 (dedup should prevent second delivery)", len(sent))
	}
}

func TestBridgeAppliesInboundScopeMetadata(t *testing.T) {
	t.Parallel()

	adapter := &stubBridgeAdapter{inbound: make(chan channels.InboundMessage)}
	runtime := &stubBridgeRuntime{
		submitRun: &agent.Run{ID: "run-scope-1"},
	}
	bridge := NewBridge("wh-scope", adapter, runtime, agent.NewInMemorySessionStore(), nil, channels.DefaultStatusReminderDelay)

	bridge.HandleInboundMessage(context.Background(), channels.InboundMessage{
		ChannelID: "webhook:wh-scope",
		SenderID:  "user-1",
		Content:   "scope please",
		RawEvent: map[string]any{
			"automation_id": "auto-webhook-1",
		},
	})
	if got := runtime.lastInteractReq.AutomationID; got != "auto-webhook-1" {
		t.Fatalf("AutomationID = %q, want auto-webhook-1", got)
	}
}

// ---------------------------------------------------------------------------
// Stubs
// ---------------------------------------------------------------------------

type stubBridgeAdapter struct {
	inbound chan channels.InboundMessage

	mu   sync.Mutex
	sent []channels.OutboundMessage
}

func (a *stubBridgeAdapter) Connect(context.Context) error    { return nil }
func (a *stubBridgeAdapter) Disconnect(context.Context) error { return nil }
func (a *stubBridgeAdapter) Send(_ context.Context, msg channels.OutboundMessage) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sent = append(a.sent, msg)
	return nil
}
func (a *stubBridgeAdapter) Capabilities() channels.ChannelCapabilityDescriptor {
	return channels.Capabilities{SendText: true, ReceiveMessage: true, ReceiveEvent: true}
}
func (a *stubBridgeAdapter) Status() channels.Status                         { return channels.StatusConnected }
func (a *stubBridgeAdapter) SubscribeEvents() <-chan channels.InboundMessage { return a.inbound }
func (a *stubBridgeAdapter) Messages() []channels.OutboundMessage {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]channels.OutboundMessage, len(a.sent))
	copy(out, a.sent)
	return out
}

type stubBridgeRuntime struct {
	runs             map[string]*agent.Run
	submitRun        *agent.Run
	submitErr        error
	submits          int
	cancelled        []string
	lastInteractReq  runtimesvc.InteractionRequest
	pendingBySession map[string]*approval.Ticket
	approvalsByID    map[string]*approval.Ticket
	resolved         []approval.Resolution
}

type stubInteractRuntime struct {
	*stubBridgeRuntime
	interactionResp *runtimesvc.InteractionResult
	interactionErr  error
	interactions    int
}

func (r *stubBridgeRuntime) Submit(context.Context, runtimesvc.SubmitRequest) (*agent.Run, error) {
	r.submits++
	if r.submitErr != nil {
		return nil, r.submitErr
	}
	if r.submitRun != nil {
		copyRun := *r.submitRun
		return &copyRun, nil
	}
	return nil, nil
}

func (r *stubBridgeRuntime) Interact(ctx context.Context, req runtimesvc.InteractionRequest) (*runtimesvc.InteractionResult, error) {
	r.lastInteractReq = req
	if action, ok := channels.ParseApprovalReply(req.Content); ok {
		var ticket *approval.Ticket
		for _, item := range r.pendingBySession {
			if item != nil {
				copyTicket := *item
				ticket = &copyTicket
				break
			}
		}
		if ticket == nil {
			return &runtimesvc.InteractionResult{
				Decision: runtimesvc.InteractionDecision{SpeechAct: runtimesvc.SpeechActApprovalReply, ReplyAct: runtimesvc.ReplyActChatReply, Reason: "approval_reply_no_pending"},
			}, nil
		}
		resolution := approval.Resolution{ResolvedBy: "interaction"}
		if action == channels.ApprovalReplyDeny {
			resolution.Status = approval.StatusDenied
		} else {
			resolution.Status = approval.StatusApproved
		}
		if _, err := r.ResolveApproval(ctx, ticket.ID, resolution); err != nil {
			return nil, err
		}
		return &runtimesvc.InteractionResult{
			Decision:         runtimesvc.InteractionDecision{SpeechAct: runtimesvc.SpeechActApprovalReply, ReplyAct: runtimesvc.ReplyActResumeAck, Reason: "text_approval_" + string(action)},
			Context:          runtimesvc.InteractionContextSnapshot{SessionID: ticket.SessionID, ActiveRunID: ticket.RunID, WaitingApproval: true, PendingTicketID: ticket.ID},
			ApprovalResolved: true,
			ApprovalStatus:   resolution.Status,
		}, nil
	}
	if cmd, ok := channels.ParseControlCommand(req.Content); ok && cmd == channels.ControlCommandStatus {
		var run *agent.Run
		for _, item := range r.runs {
			if item != nil {
				copyRun := *item
				run = &copyRun
				break
			}
		}
		return &runtimesvc.InteractionResult{
			Decision: runtimesvc.InteractionDecision{SpeechAct: runtimesvc.SpeechActStatusQuery, ReplyAct: runtimesvc.ReplyActStatusReply, Reason: "text_command_status"},
			Run:      run,
		}, nil
	}
	submitReq := runtimesvc.SubmitRequest{SessionKey: req.SessionKey, ExternalEventID: req.ExternalEventID, Content: req.Content, AutomationID: req.AutomationID, Metadata: req.Metadata}
	run, err := r.Submit(ctx, submitReq)
	if err != nil {
		return &runtimesvc.InteractionResult{
			Decision:      runtimesvc.InteractionDecision{SpeechAct: runtimesvc.SpeechActNewTask, ReplyAct: runtimesvc.ReplyActTaskFailure},
			SubmitRequest: &submitReq,
			Error:         err.Error(),
		}, nil
	}
	return &runtimesvc.InteractionResult{
		Decision:      runtimesvc.InteractionDecision{SpeechAct: runtimesvc.SpeechActNewTask, ReplyAct: runtimesvc.ReplyActTaskAccept},
		Run:           run,
		SubmitRequest: &submitReq,
	}, nil
}

func (r *stubInteractRuntime) Interact(_ context.Context, _ runtimesvc.InteractionRequest) (*runtimesvc.InteractionResult, error) {
	r.interactions++
	return r.interactionResp, r.interactionErr
}

func (r *stubBridgeRuntime) GetRun(_ context.Context, id string) (*agent.Run, error) {
	if r.runs != nil {
		run, ok := r.runs[id]
		if ok {
			copyRun := *run
			return &copyRun, nil
		}
	}
	return nil, context.Canceled
}

func (r *stubBridgeRuntime) GetArtifact(_ context.Context, _ string) (*artifact.Blob, error) {
	return nil, errors.New("artifact not found")
}

func (r *stubBridgeRuntime) GetApproval(_ context.Context, id string) (*approval.Ticket, error) {
	if r.approvalsByID != nil {
		ticket, ok := r.approvalsByID[id]
		if ok {
			copyTicket := *ticket
			return &copyTicket, nil
		}
	}
	return nil, context.Canceled
}

func (r *stubBridgeRuntime) FindPendingApproval(_ context.Context, sessionID string) (*approval.Ticket, error) {
	if r.pendingBySession != nil {
		ticket, ok := r.pendingBySession[sessionID]
		if ok {
			copyTicket := *ticket
			return &copyTicket, nil
		}
	}
	return nil, context.Canceled
}

func (r *stubBridgeRuntime) ResolveApproval(_ context.Context, id string, resolution approval.Resolution) (*approval.Ticket, error) {
	if r.approvalsByID != nil {
		ticket, ok := r.approvalsByID[id]
		if ok {
			r.resolved = append(r.resolved, resolution)
			ticket.Status = resolution.Status
			copyTicket := *ticket
			return &copyTicket, nil
		}
	}
	return nil, context.Canceled
}

func (r *stubBridgeRuntime) CancelRun(_ context.Context, runID string) (*agent.Run, error) {
	r.cancelled = append(r.cancelled, runID)
	if r.runs != nil {
		run, ok := r.runs[runID]
		if ok {
			copyRun := *run
			copyRun.Status = agent.RunCancelled
			return &copyRun, nil
		}
	}
	return nil, context.Canceled
}
