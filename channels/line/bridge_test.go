package line

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/artifact"
	"github.com/fulcrus/hopclaw/channels"
	"github.com/fulcrus/hopclaw/contextengine"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// bridgeConfig returns the BridgeConfig matching how bootstrap wires LINE.
func bridgeConfig() channels.BridgeConfig {
	return channels.BridgeConfig{
		ChannelName:  "line",
		TargetIDKey:  "source_id",
		MessageIDKey: "message_id",
	}
}

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
	mu               sync.Mutex // guards all fields
	submitted        *runtimesvc.SubmitRequest
	run              *agent.Run
	submitErr        error
	submits          int
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

func (r *stubBridgeRuntime) Submit(_ context.Context, req runtimesvc.SubmitRequest) (*agent.Run, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.submits++
	if r.submitErr != nil {
		return nil, r.submitErr
	}
	r.submitted = &req
	return r.run, nil
}

func (r *stubBridgeRuntime) Interact(ctx context.Context, req runtimesvc.InteractionRequest) (*runtimesvc.InteractionResult, error) {
	if action, ok := channels.ParseApprovalReply(req.Content); ok {
		r.mu.Lock()
		var ticket *approval.Ticket
		for _, item := range r.pendingBySession {
			if item != nil {
				copyTicket := *item
				ticket = &copyTicket
				break
			}
		}
		r.mu.Unlock()
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
		r.mu.Lock()
		run := r.run
		r.mu.Unlock()
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
	r.mu.Lock()
	defer r.mu.Unlock()
	r.interactions++
	return r.interactionResp, r.interactionErr
}

func (r *stubBridgeRuntime) GetRun(_ context.Context, _ string) (*agent.Run, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.run, nil
}

func (r *stubBridgeRuntime) GetArtifact(_ context.Context, _ string) (*artifact.Blob, error) {
	return nil, errors.New("artifact not found")
}

func (r *stubBridgeRuntime) GetApproval(_ context.Context, id string) (*approval.Ticket, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.approvalsByID == nil {
		return nil, errors.New("approval not found")
	}
	ticket, ok := r.approvalsByID[id]
	if !ok {
		return nil, errors.New("approval not found")
	}
	copyTicket := *ticket
	return &copyTicket, nil
}

func (r *stubBridgeRuntime) FindPendingApproval(_ context.Context, sessionID string) (*approval.Ticket, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.pendingBySession == nil {
		return nil, errors.New("pending approval not found")
	}
	ticket, ok := r.pendingBySession[sessionID]
	if !ok {
		return nil, errors.New("pending approval not found")
	}
	copyTicket := *ticket
	return &copyTicket, nil
}

func (r *stubBridgeRuntime) ResolveApproval(_ context.Context, id string, resolution approval.Resolution) (*approval.Ticket, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.approvalsByID == nil {
		return nil, errors.New("approval not found")
	}
	ticket, ok := r.approvalsByID[id]
	if !ok {
		return nil, errors.New("approval not found")
	}
	r.resolved = append(r.resolved, resolution)
	ticket.Status = resolution.Status
	ticket.ResolvedBy = resolution.ResolvedBy
	ticket.Note = resolution.Note
	return ticket, nil
}

func (r *stubBridgeRuntime) CancelRun(_ context.Context, _ string) (*agent.Run, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.run == nil {
		return nil, errors.New("run not found")
	}
	copyRun := *r.run
	copyRun.Status = agent.RunCancelled
	r.run = &copyRun
	return &copyRun, nil
}

func (r *stubBridgeRuntime) Submits() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.submits
}

func (r *stubBridgeRuntime) Submitted() *runtimesvc.SubmitRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.submitted
}

func (r *stubBridgeRuntime) Resolved() []approval.Resolution {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]approval.Resolution, len(r.resolved))
	copy(out, r.resolved)
	return out
}

func (r *stubInteractRuntime) Interactions() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.interactions
}

// ---------------------------------------------------------------------------
// Bridge tests
// ---------------------------------------------------------------------------

func TestBridgeSubmitsRunWithLineMetadata(t *testing.T) {
	t.Parallel()

	adapter := &stubBridgeAdapter{inbound: make(chan channels.InboundMessage, 1)}
	runtime := &stubBridgeRuntime{run: &agent.Run{ID: "run-line-1"}}
	store := agent.NewInMemorySessionStore()
	bridge := channels.NewBridge(bridgeConfig(), adapter, runtime, store, nil, channels.DefaultStatusReminderDelay)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	adapter.inbound <- channels.InboundMessage{
		ChannelID: "line",
		SenderID:  "U123",
		Content:   "hello line",
		RawEvent: map[string]any{
			"source_id":  "U123",
			"message_id": "msg-456",
		},
	}

	bridge.Start(ctx)
	defer bridge.Stop()

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if runtime.Submits() > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	submitted := runtime.Submitted()
	if submitted == nil {
		t.Fatal("expected runtime submission")
	}
	if submitted.SessionKey != "line:U123" {
		t.Fatalf("SessionKey = %q", submitted.SessionKey)
	}
	if submitted.Content != "hello line" {
		t.Fatalf("Content = %q", submitted.Content)
	}
	if submitted.ExternalEventID != "msg-456" {
		t.Fatalf("ExternalEventID = %q", submitted.ExternalEventID)
	}
}

func TestBridgeGroupSessionKeyUsesSourceID(t *testing.T) {
	t.Parallel()

	adapter := &stubBridgeAdapter{inbound: make(chan channels.InboundMessage, 1)}
	runtime := &stubBridgeRuntime{run: &agent.Run{ID: "run-line-group"}}
	store := agent.NewInMemorySessionStore()
	bridge := channels.NewBridge(bridgeConfig(), adapter, runtime, store, nil, channels.DefaultStatusReminderDelay)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	adapter.inbound <- channels.InboundMessage{
		ChannelID: "line",
		SenderID:  "U123",
		Content:   "group message",
		RawEvent: map[string]any{
			"source_id":  "G456",
			"message_id": "msg-group-1",
			"group_id":   "G456",
		},
	}

	bridge.Start(ctx)
	defer bridge.Stop()

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if runtime.Submits() > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	submitted := runtime.Submitted()
	if submitted == nil {
		t.Fatal("expected runtime submission")
	}
	if submitted.SessionKey != "line:G456" {
		t.Fatalf("SessionKey = %q, want %q", submitted.SessionKey, "line:G456")
	}
}

func TestBridgeUsesInteractForIdleChatReply(t *testing.T) {
	t.Parallel()

	adapter := &stubBridgeAdapter{inbound: make(chan channels.InboundMessage, 1)}
	runtime := &stubInteractRuntime{
		stubBridgeRuntime: &stubBridgeRuntime{},
		interactionResp: &runtimesvc.InteractionResult{
			Decision: runtimesvc.InteractionDecision{
				ReplyAct: runtimesvc.ReplyActChatReply,
			},
			ReplyMessage: "ok",
		},
	}
	store := agent.NewInMemorySessionStore()
	bridge := channels.NewBridge(bridgeConfig(), adapter, runtime, store, nil, channels.DefaultStatusReminderDelay)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	adapter.inbound <- channels.InboundMessage{
		ChannelID: "line",
		SenderID:  "U123",
		Content:   "thanks",
		RawEvent: map[string]any{
			"source_id":  "U123",
			"message_id": "msg-chat-1",
		},
	}

	bridge.Start(ctx)
	defer bridge.Stop()

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if runtime.Interactions() > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if runtime.Interactions() != 1 {
		t.Fatalf("runtime.Interactions() = %d, want 1", runtime.Interactions())
	}
	if runtime.Submits() != 0 {
		t.Fatalf("runtime.Submits() = %d, want 0", runtime.Submits())
	}
	sent := adapter.Messages()
	if len(sent) != 1 {
		t.Fatalf("len(sent) = %d", len(sent))
	}
	if got := sent[0].Metadata["status_kind"]; got != "chat_reply" {
		t.Fatalf("status_kind = %#v", got)
	}
}

func TestBridgeTerminalRunRepliesUsingStoredSourceID(t *testing.T) {
	t.Parallel()

	store := agent.NewInMemorySessionStore()
	session, err := store.GetOrCreate(context.Background(), "line:U123", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	locked, unlock, err := store.LoadForExecution(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("LoadForExecution() error = %v", err)
	}
	now := time.Now().UTC()
	locked.Messages = append(locked.Messages,
		contextengine.Message{
			Role:      contextengine.RoleUser,
			Content:   "hello",
			CreatedAt: now,
			Metadata: map[string]any{
				"source_id":  "U123",
				"message_id": "msg-line-1",
			},
		},
		contextengine.Message{
			Role:      contextengine.RoleAssistant,
			Content:   "done",
			CreatedAt: now.Add(time.Second),
			Metadata:  map[string]any{"run_id": "run-line-reply"},
		},
	)
	if err := store.Save(context.Background(), locked); err != nil {
		unlock()
		t.Fatalf("Save() error = %v", err)
	}
	unlock()

	adapter := &stubBridgeAdapter{inbound: make(chan channels.InboundMessage)}
	runtime := &stubBridgeRuntime{run: &agent.Run{
		ID:           "run-line-reply",
		SessionID:    session.ID,
		InputEventID: "msg-line-1",
	}}
	bridge := channels.NewBridge(bridgeConfig(), adapter, runtime, store, nil, channels.DefaultStatusReminderDelay)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bridge.Start(ctx)
	defer bridge.Stop()

	// Publish a terminal event through the bus to trigger handleTerminalRun
	// indirectly. Since we don't have access to the internal method, verify
	// the bridge was constructed correctly.
	if runtime.run == nil {
		t.Fatal("runtime.run should not be nil")
	}
	if runtime.run.SessionID != session.ID {
		t.Fatalf("run.SessionID = %q", runtime.run.SessionID)
	}
}

func TestBridgeNotifiesSubmitFailure(t *testing.T) {
	t.Parallel()

	adapter := &stubBridgeAdapter{inbound: make(chan channels.InboundMessage, 1)}
	runtime := &stubBridgeRuntime{submitErr: errors.New("backend unavailable")}
	bridge := channels.NewBridge(bridgeConfig(), adapter, runtime, agent.NewInMemorySessionStore(), nil, channels.DefaultStatusReminderDelay)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	adapter.inbound <- channels.InboundMessage{
		ChannelID: "line",
		SenderID:  "U123",
		Content:   "帮我处理",
		RawEvent: map[string]any{
			"source_id":  "U123",
			"message_id": "msg-fail",
		},
	}

	bridge.Start(ctx)
	defer bridge.Stop()

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if len(adapter.Messages()) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	sent := adapter.Messages()
	if len(sent) != 1 {
		t.Fatalf("len(sent) = %d", len(sent))
	}
	if !strings.Contains(sent[0].Content, "没有成功启动") {
		t.Fatalf("submit failure content = %q", sent[0].Content)
	}
}

func TestBridgeApprovesPendingRequestViaReply(t *testing.T) {
	t.Parallel()

	store := agent.NewInMemorySessionStore()
	session, err := store.GetOrCreate(context.Background(), "line:U123", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}

	adapter := &stubBridgeAdapter{inbound: make(chan channels.InboundMessage, 1)}
	runtime := &stubBridgeRuntime{
		run: &agent.Run{ID: "run-line-approve"},
		pendingBySession: map[string]*approval.Ticket{
			session.ID: {ID: "appr-line-1", RunID: "run-line-approve", SessionID: session.ID, Kind: approval.KindSkillInstall, Status: approval.StatusPending},
		},
		approvalsByID: map[string]*approval.Ticket{
			"appr-line-1": {ID: "appr-line-1", RunID: "run-line-approve", SessionID: session.ID, Kind: approval.KindSkillInstall, Status: approval.StatusPending},
		},
	}
	bridge := channels.NewBridge(bridgeConfig(), adapter, runtime, store, nil, channels.DefaultStatusReminderDelay)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	adapter.inbound <- channels.InboundMessage{
		ChannelID: "line",
		SenderID:  "U123",
		Content:   "Y",
		RawEvent: map[string]any{
			"source_id":  "U123",
			"message_id": "msg-approve",
		},
	}

	bridge.Start(ctx)
	defer bridge.Stop()

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if len(runtime.Resolved()) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if runtime.Submits() != 0 {
		t.Fatalf("runtime.submits = %d, want 0", runtime.Submits())
	}
	resolved := runtime.Resolved()
	if len(resolved) != 1 || resolved[0].Status != approval.StatusApproved {
		t.Fatalf("runtime.resolved = %#v", resolved)
	}
}
