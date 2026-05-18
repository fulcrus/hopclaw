package slack

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

func (r *stubInteractRuntime) Interact(_ context.Context, _ runtimesvc.InteractionRequest) (*runtimesvc.InteractionResult, error) {
	r.interactions++
	return r.interactionResp, r.interactionErr
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestBridgeAppliesInboundScopeMetadata(t *testing.T) {
	t.Parallel()

	adapter := &stubBridgeAdapter{inbound: make(chan channels.InboundMessage)}
	runtime := &stubBridgeRuntime{
		submitRun: &agent.Run{ID: "run-scope-1"},
	}
	bridge := NewBridge(adapter, runtime, agent.NewInMemorySessionStore(), nil, channels.DefaultStatusReminderDelay)

	bridge.HandleInboundMessage(context.Background(), channels.InboundMessage{
		ChannelID:  "slack",
		SenderID:   "U123",
		SenderName: "Scope Tester",
		Content:    "scope please",
		RawEvent: map[string]any{
			"channel":       "C123",
			"ts":            "111.222",
			"user":          "U123",
			"automation_id": "auto-slack-1",
		},
	})
	if got := runtime.lastInteractReq.AutomationID; got != "auto-slack-1" {
		t.Fatalf("AutomationID = %q, want auto-slack-1", got)
	}
	if got, _ := runtime.lastInteractReq.Metadata["automation_id"].(string); got != "auto-slack-1" {
		t.Fatalf("metadata[automation_id] = %q, want auto-slack-1", got)
	}
}

func TestBridgePolicyBlocksDirectMessage(t *testing.T) {
	t.Parallel()

	adapter := &stubBridgeAdapter{inbound: make(chan channels.InboundMessage)}
	runtime := &stubBridgeRuntime{submitRun: &agent.Run{ID: "run-blocked"}}
	bridge := NewBridge(adapter, runtime, agent.NewInMemorySessionStore(), nil, channels.DefaultStatusReminderDelay).
		WithPolicy(channels.PolicyConfig{
			DMPolicy:  "allowlist",
			AllowFrom: []string{"user-allowed"},
		})

	bridge.HandleInboundMessage(context.Background(), channels.InboundMessage{
		ChannelID: "slack",
		SenderID:  "user-denied",
		Content:   "run task",
		RawEvent: map[string]any{
			"channel": "D123",
			"ts":      "111.222",
			"user":    "user-denied",
		},
	})

	if runtime.submits != 0 {
		t.Fatalf("runtime.submits = %d, want 0", runtime.submits)
	}
}

func TestBridgeBindCommandUsesSlackThreadBinding(t *testing.T) {
	t.Parallel()

	adapter := &stubBridgeAdapter{inbound: make(chan channels.InboundMessage)}
	bindings := channels.NewThreadBinding()
	bridge := NewBridge(adapter, &stubBridgeRuntime{}, agent.NewInMemorySessionStore(), nil, channels.DefaultStatusReminderDelay).
		WithThreadBindings(bindings).
		WithMessageDeduper(channels.NewMessageDeduper("", time.Hour))

	msg := channels.InboundMessage{
		ChannelID: "slack",
		SenderID:  "user-1",
		Content:   "/bind",
		RawEvent: map[string]any{
			"channel":   "C123",
			"ts":        "111.222",
			"user":      "user-1",
			"thread_ts": "thread-1",
		},
	}
	bridge.HandleInboundMessage(context.Background(), msg)
	bridge.HandleInboundMessage(context.Background(), msg)

	if _, ok := bindings.Resolve("slack", "thread-1"); !ok {
		t.Fatal("expected slack thread binding to be created")
	}
	if len(adapter.Messages()) != 1 {
		t.Fatalf("len(adapter.Messages()) = %d, want 1 after dedupe", len(adapter.Messages()))
	}
}

func TestExtractReplyTargetDoesNotFallBackToSessionKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sessionKey string
		wantTarget string
	}{
		{name: "channel session key", sessionKey: "slack:C123", wantTarget: "C123"},
		{name: "dm session key", sessionKey: "slack:dm:U456", wantTarget: "dm:U456"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			adapter := &stubBridgeAdapter{inbound: make(chan channels.InboundMessage)}
			bridge := NewBridge(adapter, nil, agent.NewInMemorySessionStore(), nil, channels.DefaultStatusReminderDelay)

			session := &agent.Session{Key: tt.sessionKey}
			targetID, _ := bridge.ExtractReplyTarget(session, "")
			if targetID != "" {
				t.Fatalf("targetID = %q, want empty result without stored metadata", targetID)
			}
		})
	}
}

func TestBridgeHandleTerminalRunRepliesWithCorrectTarget(t *testing.T) {
	t.Parallel()

	sessionStore := agent.NewInMemorySessionStore()
	session, err := sessionStore.GetOrCreate(context.Background(), "slack:C123", "test-model")
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
				meta.KeyChannelName: "slack",
				"channel":           "C123",
				"ts":                "1234567890.111111",
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
			"run-1": {
				ID:           "run-1",
				SessionID:    session.ID,
				InputEventID: "1234567890.111111",
			},
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
	if sent[0].TargetID != "C123" {
		t.Fatalf("TargetID = %q, want %q", sent[0].TargetID, "C123")
	}
	if sent[0].ReplyToID != "1234567890.111111" {
		t.Fatalf("ReplyToID = %q, want %q", sent[0].ReplyToID, "1234567890.111111")
	}
	if sent[0].ChannelID != "slack" {
		t.Fatalf("ChannelID = %q, want %q", sent[0].ChannelID, "slack")
	}
}

func TestBridgeDeduplicatesTerminalRun(t *testing.T) {
	t.Parallel()

	sessionStore := agent.NewInMemorySessionStore()
	session, err := sessionStore.GetOrCreate(context.Background(), "slack:C123", "test-model")
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
				meta.KeyChannelName: "slack",
				"channel":           "C123",
				"ts":                "ts-1",
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
			"run-dedup": {ID: "run-dedup", SessionID: session.ID, InputEventID: "ts-1"},
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

func TestBridgeApprovesPendingRequestViaReply(t *testing.T) {
	t.Parallel()

	sessionStore := agent.NewInMemorySessionStore()
	session, err := sessionStore.GetOrCreate(context.Background(), "slack:C123", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}

	adapter := &stubBridgeAdapter{inbound: make(chan channels.InboundMessage)}
	runtime := &stubBridgeRuntime{
		pendingBySession: map[string]*approval.Ticket{
			session.ID: {ID: "appr-1", RunID: "run-1", SessionID: session.ID, Kind: approval.KindSkillInstall, Status: approval.StatusPending},
		},
		approvalsByID: map[string]*approval.Ticket{
			"appr-1": {ID: "appr-1", RunID: "run-1", SessionID: session.ID, Kind: approval.KindSkillInstall, Status: approval.StatusPending},
		},
	}
	bridge := NewBridge(adapter, runtime, sessionStore, nil, channels.DefaultStatusReminderDelay)

	bridge.HandleInboundMessage(context.Background(), channels.InboundMessage{
		ChannelID: "slack",
		SenderID:  "U456",
		Content:   "Y",
		RawEvent: map[string]any{
			"channel": "C123",
			"ts":      "ts-approve",
			"user":    "U456",
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

func TestBridgeNotifiesSubmitFailure(t *testing.T) {
	t.Parallel()

	adapter := &stubBridgeAdapter{inbound: make(chan channels.InboundMessage)}
	bridge := NewBridge(adapter, &stubBridgeRuntime{
		submitErr: errors.New("backend down"),
	}, agent.NewInMemorySessionStore(), nil, channels.DefaultStatusReminderDelay)

	bridge.HandleInboundMessage(context.Background(), channels.InboundMessage{
		ChannelID: "slack",
		SenderID:  "U456",
		Content:   "帮我处理",
		RawEvent: map[string]any{
			"channel": "C123",
			"ts":      "ts-fail",
			"user":    "U456",
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
				ID:     "run-status",
				Status: agent.RunRunning,
				Phase:  "tools",
			},
		},
	}
	bridge := NewBridge(adapter, runtime, agent.NewInMemorySessionStore(), nil, channels.DefaultStatusReminderDelay)

	bridge.HandleInboundMessage(context.Background(), channels.InboundMessage{
		ChannelID: "slack",
		SenderID:  "U456",
		Content:   "/status",
		RawEvent: map[string]any{
			"channel":   "C123",
			"ts":        "ts-status",
			"user":      "U456",
			"thread_ts": "thread-1",
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
	// StandardBridge uses messageID (ts) as reply target unless ReplyInThread is set.
	if sent[0].ReplyToID != "ts-status" {
		t.Fatalf("ReplyToID = %q, want %q", sent[0].ReplyToID, "ts-status")
	}
}
