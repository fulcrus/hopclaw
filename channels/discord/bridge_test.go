package discord

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
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/internal/meta"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
)

// ---------------------------------------------------------------------------
// Test helpers
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

func TestBridgeAppliesInboundScopeMetadata(t *testing.T) {
	t.Parallel()

	adapter := &stubBridgeAdapter{inbound: make(chan channels.InboundMessage)}
	runtime := &stubBridgeRuntime{
		submitRun: &agent.Run{ID: "run-scope-1"},
	}
	bridge := NewBridge(adapter, runtime, agent.NewInMemorySessionStore(), nil, channels.DefaultStatusReminderDelay)

	bridge.HandleInboundMessage(context.Background(), channels.InboundMessage{
		ChannelID: "discord",
		SenderID:  "user-1",
		Content:   "scope please",
		RawEvent: map[string]any{
			"channel_id":    "chan-1",
			"message_id":    "msg-1",
			"automation_id": "auto-discord-1",
		},
	})
	if got := runtime.lastInteractReq.AutomationID; got != "auto-discord-1" {
		t.Fatalf("AutomationID = %q, want auto-discord-1", got)
	}
}

func (r *stubInteractRuntime) Interact(_ context.Context, _ runtimesvc.InteractionRequest) (*runtimesvc.InteractionResult, error) {
	r.interactions++
	return r.interactionResp, r.interactionErr
}

func (r *stubBridgeRuntime) GetRun(_ context.Context, id string) (*agent.Run, error) {
	run, ok := r.runs[id]
	if !ok {
		return nil, context.Canceled
	}
	copyRun := *run
	return &copyRun, nil
}

func (r *stubBridgeRuntime) GetArtifact(_ context.Context, _ string) (*artifact.Blob, error) {
	return nil, errors.New("artifact not found")
}

func (r *stubBridgeRuntime) GetApproval(_ context.Context, id string) (*approval.Ticket, error) {
	ticket, ok := r.approvalsByID[id]
	if !ok {
		return nil, context.Canceled
	}
	copyTicket := *ticket
	return &copyTicket, nil
}

func (r *stubBridgeRuntime) FindPendingApproval(_ context.Context, sessionID string) (*approval.Ticket, error) {
	ticket, ok := r.pendingBySession[sessionID]
	if !ok {
		return nil, context.Canceled
	}
	copyTicket := *ticket
	return &copyTicket, nil
}

func (r *stubBridgeRuntime) ResolveApproval(_ context.Context, id string, resolution approval.Resolution) (*approval.Ticket, error) {
	ticket, ok := r.approvalsByID[id]
	if !ok {
		return nil, context.Canceled
	}
	r.resolved = append(r.resolved, resolution)
	ticket.Status = resolution.Status
	ticket.ResolvedBy = resolution.ResolvedBy
	ticket.Note = resolution.Note
	copyTicket := *ticket
	return &copyTicket, nil
}

func (r *stubBridgeRuntime) CancelRun(_ context.Context, runID string) (*agent.Run, error) {
	run, ok := r.runs[runID]
	if !ok {
		return nil, context.Canceled
	}
	copyRun := *run
	copyRun.Status = agent.RunCancelled
	r.runs[runID] = &copyRun
	return &copyRun, nil
}

// ---------------------------------------------------------------------------
// Pure function tests
// ---------------------------------------------------------------------------

func TestLooksLikeAuthFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{name: "authentication_error", raw: "authentication_error: token invalid", want: true},
		{name: "token has expired", raw: "OAuth token has expired", want: true},
		{name: "unauthorized", raw: "Unauthorized", want: true},
		{name: "status 401", raw: "HTTP status 401", want: true},
		{name: "normal error", raw: "rate limit exceeded", want: false},
		{name: "empty string", raw: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := channels.IsAuthFailure(tt.raw); got != tt.want {
				t.Fatalf("IsAuthFailure(%q) = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
}

func TestExtractReplyTargetDoesNotFallBackToSessionKey(t *testing.T) {
	t.Parallel()

	adapter := &stubBridgeAdapter{inbound: make(chan channels.InboundMessage)}
	bridge := NewBridge(adapter, nil, agent.NewInMemorySessionStore(), nil, channels.DefaultStatusReminderDelay)

	session := &agent.Session{Key: "discord:ch-fallback"}
	targetID, _ := bridge.ExtractReplyTarget(session, "")
	if targetID != "" {
		t.Fatalf("targetID = %q, want empty result without stored metadata", targetID)
	}
}

func TestBridgeHandleTerminalRunRepliesWithCorrectTarget(t *testing.T) {
	t.Parallel()

	sessionStore := agent.NewInMemorySessionStore()
	session, err := sessionStore.GetOrCreate(context.Background(), "discord:ch-100", "test-model")
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
			Content:   "hello",
			CreatedAt: now,
			Metadata: map[string]any{
				meta.KeyChannel: "discord",
				"channel_id":    "ch-100",
				"message_id":    "msg-1",
			},
		},
		contextengine.Message{
			Role:      contextengine.RoleAssistant,
			Content:   "response",
			CreatedAt: now.Add(time.Second),
			Metadata:  map[string]any{"run_id": "run-1"},
		},
	)
	if err := sessionStore.Save(context.Background(), locked); err != nil {
		unlock()
		t.Fatalf("Save() error = %v", err)
	}
	unlock()

	adapter := &stubBridgeAdapter{inbound: make(chan channels.InboundMessage)}
	bridge := NewBridge(adapter, &stubBridgeRuntime{
		runs: map[string]*agent.Run{
			"run-1": {ID: "run-1", SessionID: session.ID, InputEventID: "msg-1"},
		},
	}, sessionStore, nil, channels.DefaultStatusReminderDelay)

	bridge.HandleTerminalRun(context.Background(), eventbus.Event{
		Type:      eventbus.EventRunCompleted,
		RunID:     "run-1",
		SessionID: session.ID,
	})

	sent := adapter.Messages()
	if len(sent) != 1 {
		t.Fatalf("len(sent) = %d", len(sent))
	}
	if sent[0].TargetID != "ch-100" {
		t.Fatalf("TargetID = %q, want %q", sent[0].TargetID, "ch-100")
	}
	if sent[0].ReplyToID != "msg-1" {
		t.Fatalf("ReplyToID = %q, want %q", sent[0].ReplyToID, "msg-1")
	}
	if sent[0].ChannelID != "discord" {
		t.Fatalf("ChannelID = %q, want %q", sent[0].ChannelID, "discord")
	}
}

func TestBridgeDeduplicatesTerminalRun(t *testing.T) {
	t.Parallel()

	sessionStore := agent.NewInMemorySessionStore()
	session, err := sessionStore.GetOrCreate(context.Background(), "discord:ch-200", "test-model")
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
			Content:   "hello",
			CreatedAt: now,
			Metadata: map[string]any{
				meta.KeyChannel: "discord",
				"channel_id":    "ch-200",
				"message_id":    "msg-dedup",
			},
		},
		contextengine.Message{
			Role:      contextengine.RoleAssistant,
			Content:   "done",
			CreatedAt: now.Add(time.Second),
			Metadata:  map[string]any{"run_id": "run-dedup"},
		},
	)
	if err := sessionStore.Save(context.Background(), locked); err != nil {
		unlock()
		t.Fatalf("Save() error = %v", err)
	}
	unlock()

	adapter := &stubBridgeAdapter{inbound: make(chan channels.InboundMessage)}
	bridge := NewBridge(adapter, &stubBridgeRuntime{
		runs: map[string]*agent.Run{
			"run-dedup": {ID: "run-dedup", SessionID: session.ID, InputEventID: "msg-dedup"},
		},
	}, sessionStore, nil, channels.DefaultStatusReminderDelay)

	event := eventbus.Event{
		Type:      eventbus.EventRunCompleted,
		RunID:     "run-dedup",
		SessionID: session.ID,
	}
	bridge.HandleTerminalRun(context.Background(), event)
	bridge.HandleTerminalRun(context.Background(), event)

	sent := adapter.Messages()
	if len(sent) != 1 {
		t.Fatalf("len(sent) = %d, want 1 (deduplication)", len(sent))
	}
}

func TestBridgeNotifiesSubmitFailure(t *testing.T) {
	t.Parallel()

	adapter := &stubBridgeAdapter{inbound: make(chan channels.InboundMessage)}
	bridge := NewBridge(adapter, &stubBridgeRuntime{
		submitErr: errors.New("backend unavailable"),
	}, agent.NewInMemorySessionStore(), nil, channels.DefaultStatusReminderDelay)

	bridge.HandleInboundMessage(context.Background(), channels.InboundMessage{
		ChannelID: "discord",
		SenderID:  "user-1",
		Content:   "帮我搜一下",
		RawEvent: map[string]any{
			"channel_id": "ch-300",
			"message_id": "msg-fail",
			"guild_id":   "guild-1",
		},
	})

	sent := adapter.Messages()
	if len(sent) != 1 {
		t.Fatalf("len(sent) = %d", len(sent))
	}
	if !strings.Contains(sent[0].Content, "没有成功启动") {
		t.Fatalf("submit failure content = %q", sent[0].Content)
	}
}

func TestBridgeUsesInteractForStatusReplies(t *testing.T) {
	t.Parallel()

	adapter := &stubBridgeAdapter{inbound: make(chan channels.InboundMessage)}
	runtime := &stubInteractRuntime{
		stubBridgeRuntime: &stubBridgeRuntime{},
		interactionResp: &runtimesvc.InteractionResult{
			Decision: runtimesvc.InteractionDecision{
				ReplyAct: runtimesvc.ReplyActStatusReply,
			},
			Run: &agent.Run{
				ID:     "run-discord-status",
				Status: agent.RunRunning,
				Phase:  "tools",
			},
		},
	}
	bridge := NewBridge(adapter, runtime, agent.NewInMemorySessionStore(), nil, channels.DefaultStatusReminderDelay)

	bridge.HandleInboundMessage(context.Background(), channels.InboundMessage{
		ChannelID: "discord",
		SenderID:  "user-1",
		Content:   "/status",
		RawEvent: map[string]any{
			"channel_id": "ch-300",
			"message_id": "msg-status",
			"guild_id":   "guild-1",
			"thread_id":  "thread-1",
		},
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
	if got := sent[0].Metadata["status_kind"]; got != "control_status" {
		t.Fatalf("status_kind = %#v", got)
	}
}

func TestBridgePolicyBlocksGroupMessageWithoutMention(t *testing.T) {
	t.Parallel()

	adapter := &stubBridgeAdapter{inbound: make(chan channels.InboundMessage)}
	runtime := &stubBridgeRuntime{
		submitRun: &agent.Run{ID: "run-should-not-submit"},
	}
	bridge := NewBridge(adapter, runtime, agent.NewInMemorySessionStore(), nil, channels.DefaultStatusReminderDelay).
		WithPolicy(channels.PolicyConfig{
			GroupPolicy:    "open",
			RequireMention: true,
		})

	bridge.HandleInboundMessage(context.Background(), channels.InboundMessage{
		ChannelID: "discord",
		SenderID:  "user-1",
		Content:   "run task",
		RawEvent: map[string]any{
			"channel_id": "ch-1",
			"message_id": "msg-1",
			"guild_id":   "guild-1",
		},
	})

	if runtime.submits != 0 {
		t.Fatalf("runtime.submits = %d, want 0", runtime.submits)
	}
}
