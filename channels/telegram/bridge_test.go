package telegram

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
		ChannelID: "telegram",
		SenderID:  "user-1",
		Content:   "scope please",
		RawEvent: map[string]any{
			"chat_id":       "67890",
			"message_id":    "42",
			"chat_type":     "private",
			"automation_id": "auto-telegram-1",
		},
	})
	if got := runtime.lastInteractReq.AutomationID; got != "auto-telegram-1" {
		t.Fatalf("AutomationID = %q, want auto-telegram-1", got)
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

func TestExtractReplyTargetMatchesByInputEventID(t *testing.T) {
	t.Parallel()

	bridge := NewBridge(nil, nil, agent.NewInMemorySessionStore(), nil, 0)

	session := &agent.Session{
		Key: "telegram:67890",
		Session: contextengine.Session{
			Messages: []contextengine.Message{
				{
					Role:    contextengine.RoleUser,
					Content: "hello",
					Metadata: map[string]any{
						"chat_id":    "67890",
						"message_id": "42",
					},
				},
				{
					Role:    contextengine.RoleUser,
					Content: "second",
					Metadata: map[string]any{
						"chat_id":    "67890",
						"message_id": "43",
					},
				},
			},
		},
	}

	chatID, messageID := bridge.ExtractReplyTarget(session, "42")
	if chatID != "67890" {
		t.Fatalf("chatID = %q, want %q", chatID, "67890")
	}
	if messageID != "42" {
		t.Fatalf("messageID = %q, want %q", messageID, "42")
	}
}

func TestExtractReplyTargetDoesNotFallBackToSessionKey(t *testing.T) {
	t.Parallel()

	bridge := NewBridge(nil, nil, agent.NewInMemorySessionStore(), nil, 0)
	session := &agent.Session{Key: "telegram:99999"}
	chatID, messageID := bridge.ExtractReplyTarget(session, "")
	if chatID != "" {
		t.Fatalf("chatID = %q, want empty result without stored metadata", chatID)
	}
	if messageID != "" {
		t.Fatalf("messageID = %q, want empty", messageID)
	}
}

func TestExtractReplyTargetUsesFallbackChatID(t *testing.T) {
	t.Parallel()

	bridge := NewBridge(nil, nil, agent.NewInMemorySessionStore(), nil, 0)
	session := &agent.Session{
		Key: "telegram:11111",
		Session: contextengine.Session{
			Messages: []contextengine.Message{
				{
					Role:    contextengine.RoleUser,
					Content: "older message",
					Metadata: map[string]any{
						"chat_id":    "11111",
						"message_id": "100",
					},
				},
				{
					Role:     contextengine.RoleAssistant,
					Content:  "response",
					Metadata: map[string]any{"run_id": "run-x"},
				},
			},
		},
	}

	chatID, messageID := bridge.ExtractReplyTarget(session, "nonexistent")
	if chatID != "11111" {
		t.Fatalf("chatID = %q, want %q", chatID, "11111")
	}
	if messageID != "100" {
		t.Fatalf("messageID = %q, want %q", messageID, "100")
	}
}

func TestLooksLikeAuthFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{name: "authentication_error", raw: "authentication_error: expired", want: true},
		{name: "invalid api key", raw: "Invalid API Key", want: true},
		{name: "status 401", raw: "HTTP status 401", want: true},
		{name: "normal error", raw: "server error 500", want: false},
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

func TestUserVisibleFailureMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		inputContent string
		raw          string
		wantContains string
	}{
		{name: "auth english", inputContent: "run it", raw: "unauthorized", wantContains: "Backend model authentication failed"},
		{name: "auth chinese", inputContent: "帮我做", raw: "unauthorized", wantContains: "后端模型鉴权失败"},
		{name: "timeout chinese", inputContent: "帮我做", raw: "LLM request timed out", wantContains: "操作超时"},
		{name: "unavailable english", inputContent: "help", raw: "service unavailable", wantContains: "temporarily unavailable"},
		{name: "general english", inputContent: "help", raw: "crash", wantContains: "The task failed"},
		{name: "general chinese", inputContent: "你好", raw: "crash", wantContains: "任务执行失败"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := channels.BridgeFailureMessage(tt.inputContent, tt.raw)
			if !strings.Contains(got, tt.wantContains) {
				t.Fatalf("BridgeFailureMessage() = %q, want to contain %q", got, tt.wantContains)
			}
		})
	}
}

func TestBridgeHandleTerminalRunRepliesWithCorrectTarget(t *testing.T) {
	t.Parallel()

	sessionStore := agent.NewInMemorySessionStore()
	session, err := sessionStore.GetOrCreate(context.Background(), "telegram:67890", "test-model")
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
				meta.KeyChannel: "telegram",
				"chat_id":       "67890",
				"message_id":    "42",
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
			"run-1": {ID: "run-1", SessionID: session.ID, InputEventID: "42"},
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
	if sent[0].TargetID != "67890" {
		t.Fatalf("TargetID = %q, want %q", sent[0].TargetID, "67890")
	}
	if sent[0].ReplyToID != "42" {
		t.Fatalf("ReplyToID = %q, want %q", sent[0].ReplyToID, "42")
	}
	if sent[0].ChannelID != "telegram" {
		t.Fatalf("ChannelID = %q, want %q", sent[0].ChannelID, "telegram")
	}
}

func TestBridgeNotifiesSubmitFailure(t *testing.T) {
	t.Parallel()

	adapter := &stubBridgeAdapter{inbound: make(chan channels.InboundMessage)}
	bridge := NewBridge(adapter, &stubBridgeRuntime{
		submitErr: errors.New("backend down"),
	}, agent.NewInMemorySessionStore(), nil, channels.DefaultStatusReminderDelay)

	bridge.HandleInboundMessage(context.Background(), channels.InboundMessage{
		ChannelID: "telegram",
		SenderID:  "12345",
		Content:   "帮我处理",
		RawEvent: map[string]any{
			"chat_id":    "67890",
			"message_id": "50",
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

func TestBridgeApprovesPendingRequestViaReply(t *testing.T) {
	t.Parallel()

	sessionStore := agent.NewInMemorySessionStore()
	session, err := sessionStore.GetOrCreate(context.Background(), "telegram:67890", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}

	adapter := &stubBridgeAdapter{inbound: make(chan channels.InboundMessage)}
	runtime := &stubBridgeRuntime{
		pendingBySession: map[string]*approval.Ticket{
			session.ID: {ID: "appr-tg-1", RunID: "run-tg-1", SessionID: session.ID, Kind: approval.KindSkillInstall, Status: approval.StatusPending},
		},
		approvalsByID: map[string]*approval.Ticket{
			"appr-tg-1": {ID: "appr-tg-1", RunID: "run-tg-1", SessionID: session.ID, Kind: approval.KindSkillInstall, Status: approval.StatusPending},
		},
	}
	bridge := NewBridge(adapter, runtime, sessionStore, nil, channels.DefaultStatusReminderDelay)

	bridge.HandleInboundMessage(context.Background(), channels.InboundMessage{
		ChannelID: "telegram",
		SenderID:  "12345",
		Content:   "Y",
		RawEvent: map[string]any{
			"chat_id":    "67890",
			"message_id": "51",
		},
	})

	if runtime.submits != 0 {
		t.Fatalf("runtime.submits = %d, want 0", runtime.submits)
	}
	if len(runtime.resolved) != 1 || runtime.resolved[0].Status != approval.StatusApproved {
		t.Fatalf("runtime.resolved = %#v", runtime.resolved)
	}
	sent := adapter.Messages()
	if len(sent) != 1 {
		t.Fatalf("len(sent) = %d", len(sent))
	}
	if got := sent[0].Metadata["status_kind"]; got != "approval_reply_approved" {
		t.Fatalf("status_kind = %#v", got)
	}
}

func TestBridgeUsesInteractForDeniedApprovalReply(t *testing.T) {
	t.Parallel()

	adapter := &stubBridgeAdapter{inbound: make(chan channels.InboundMessage)}
	runtime := &stubInteractRuntime{
		stubBridgeRuntime: &stubBridgeRuntime{},
		interactionResp: &runtimesvc.InteractionResult{
			Decision: runtimesvc.InteractionDecision{
				ReplyAct: runtimesvc.ReplyActResumeAck,
			},
			ApprovalResolved: true,
			ApprovalStatus:   approval.StatusDenied,
		},
	}
	bridge := NewBridge(adapter, runtime, agent.NewInMemorySessionStore(), nil, channels.DefaultStatusReminderDelay)

	bridge.HandleInboundMessage(context.Background(), channels.InboundMessage{
		ChannelID: "telegram",
		SenderID:  "12345",
		Content:   "n",
		RawEvent: map[string]any{
			"chat_id":    "67890",
			"message_id": "52",
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
	if got := sent[0].Metadata["status_kind"]; got != "approval_reply_denied" {
		t.Fatalf("status_kind = %#v", got)
	}
}

func TestBridgeBindCommandStillShortCircuitsBeforeInteract(t *testing.T) {
	t.Parallel()

	adapter := &stubBridgeAdapter{inbound: make(chan channels.InboundMessage)}
	runtime := &stubInteractRuntime{
		stubBridgeRuntime: &stubBridgeRuntime{},
	}
	bindings := channels.NewThreadBinding()
	bridge := NewBridge(adapter, runtime, agent.NewInMemorySessionStore(), nil, channels.DefaultStatusReminderDelay).
		WithThreadBindings(bindings)

	bridge.HandleInboundMessage(context.Background(), channels.InboundMessage{
		ChannelID: "telegram",
		SenderID:  "12345",
		Content:   "/bind",
		RawEvent: map[string]any{
			"chat_id":    "67890",
			"message_id": "53",
			"topic_id":   "topic-1",
		},
	})

	if runtime.interactions != 0 {
		t.Fatalf("runtime.interactions = %d, want 0", runtime.interactions)
	}
	if _, ok := bindings.Resolve("telegram", "topic-1"); !ok {
		t.Fatal("expected topic binding to be created")
	}
	sent := adapter.Messages()
	if len(sent) != 1 {
		t.Fatalf("len(sent) = %d", len(sent))
	}
	if got := sent[0].Metadata["status_kind"]; got != "control_bind" {
		t.Fatalf("status_kind = %#v", got)
	}
}

func TestBridgePolicyBlocksDirectMessageByAllowlist(t *testing.T) {
	t.Parallel()

	adapter := &stubBridgeAdapter{inbound: make(chan channels.InboundMessage)}
	runtime := &stubBridgeRuntime{
		submitRun: &agent.Run{ID: "run-should-not-submit"},
	}
	bridge := NewBridge(adapter, runtime, agent.NewInMemorySessionStore(), nil, channels.DefaultStatusReminderDelay).
		WithPolicy(channels.PolicyConfig{
			DMPolicy:  "allowlist",
			AllowFrom: []string{"999"},
		})

	bridge.HandleInboundMessage(context.Background(), channels.InboundMessage{
		ChannelID: "telegram",
		SenderID:  "12345",
		Content:   "run task",
		RawEvent: map[string]any{
			"chat_id":    "12345",
			"message_id": "99",
			"chat_type":  "private",
		},
	})

	if runtime.submits != 0 {
		t.Fatalf("runtime.submits = %d, want 0", runtime.submits)
	}
}
