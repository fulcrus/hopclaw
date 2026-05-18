package feishu

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/artifact"
	"github.com/fulcrus/hopclaw/channels"
	channelpairing "github.com/fulcrus/hopclaw/channels/pairing"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/internal/meta"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
)

func TestBridgeRepliesWithRunScopedAssistantMessage(t *testing.T) {
	t.Parallel()

	sessionStore := agent.NewInMemorySessionStore()
	session, err := sessionStore.GetOrCreate(context.Background(), "feishu:chat-1", "test-model")
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
			Content:   "first question",
			CreatedAt: now,
			Metadata: map[string]any{
				meta.KeyChannel: "feishu",
				"chat_id":       "chat-1",
				"message_id":    "msg-1",
			},
		},
		contextengine.Message{
			Role:      contextengine.RoleAssistant,
			Content:   "first answer",
			CreatedAt: now.Add(time.Second),
			Metadata:  map[string]any{"run_id": "run-1"},
		},
		contextengine.Message{
			Role:      contextengine.RoleUser,
			Content:   "second question",
			CreatedAt: now.Add(2 * time.Second),
			Metadata: map[string]any{
				meta.KeyChannel: "feishu",
				"chat_id":       "chat-1",
				"message_id":    "msg-2",
			},
		},
		contextengine.Message{
			Role:      contextengine.RoleAssistant,
			Content:   "second answer",
			CreatedAt: now.Add(3 * time.Second),
			Metadata:  map[string]any{"run_id": "run-2"},
		},
	)
	locked.UpdatedAt = now.Add(3 * time.Second)
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
				InputEventID: "msg-1",
			},
		},
	}, sessionStore, nil, channels.DefaultStatusReminderDelay)

	bridge.handleTerminalRun(context.Background(), eventbus.Event{
		Type:      eventbus.EventRunCompleted,
		RunID:     "run-1",
		SessionID: session.ID,
	})

	sent := adapter.Messages()
	if len(sent) != 1 {
		t.Fatalf("len(sent) = %d", len(sent))
	}
	if sent[0].Content != "first answer" {
		t.Fatalf("sent[0].Content = %q", sent[0].Content)
	}
	if sent[0].TargetID != "chat-1" {
		t.Fatalf("sent[0].TargetID = %q", sent[0].TargetID)
	}
	if sent[0].ReplyToID != "msg-1" {
		t.Fatalf("sent[0].ReplyToID = %q", sent[0].ReplyToID)
	}
	if got, _ := sent[0].Metadata["receive_id_type"].(string); got != "chat_id" {
		t.Fatalf("receive_id_type = %q", got)
	}
}

func TestBridgeRepliesFromStoredChannelMetadataWithoutSessionKeyPrefix(t *testing.T) {
	t.Parallel()

	sessionStore := agent.NewInMemorySessionStore()
	session, err := sessionStore.GetOrCreate(context.Background(), "opaque-session", "test-model")
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
			Content:   "question",
			CreatedAt: now,
			Metadata: map[string]any{
				meta.KeyChannel:   "feishu",
				meta.KeyChatID:    "chat-opaque-1",
				meta.KeyMessageID: "msg-opaque-1",
			},
		},
		contextengine.Message{
			Role:      contextengine.RoleAssistant,
			Content:   "answer",
			CreatedAt: now.Add(time.Second),
			Metadata: map[string]any{
				meta.KeyRunID: "run-opaque-1",
			},
		},
	)
	locked.UpdatedAt = now.Add(time.Second)
	if err := sessionStore.Save(context.Background(), locked); err != nil {
		unlock()
		t.Fatalf("Save() error = %v", err)
	}
	unlock()

	adapter := &stubBridgeAdapter{inbound: make(chan channels.InboundMessage)}
	bridge := NewBridge(adapter, &stubBridgeRuntime{
		runs: map[string]*agent.Run{
			"run-opaque-1": {
				ID:           "run-opaque-1",
				SessionID:    session.ID,
				InputEventID: "msg-opaque-1",
			},
		},
	}, sessionStore, nil, channels.DefaultStatusReminderDelay)

	bridge.handleTerminalRun(context.Background(), eventbus.Event{
		Type:      eventbus.EventRunCompleted,
		RunID:     "run-opaque-1",
		SessionID: session.ID,
	})

	waitForMessages(t, adapter, 1)
	msgs := adapter.Messages()
	if msgs[0].TargetID != "chat-opaque-1" {
		t.Fatalf("TargetID = %q, want %q", msgs[0].TargetID, "chat-opaque-1")
	}
}

func TestBridgeSendsFriendlyFailureMessageAndDedupes(t *testing.T) {
	t.Parallel()

	sessionStore := agent.NewInMemorySessionStore()
	session, err := sessionStore.GetOrCreate(context.Background(), "feishu:user:ou_user_1", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	locked, unlock, err := sessionStore.LoadForExecution(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("LoadForExecution() error = %v", err)
	}
	now := time.Now().UTC()
	locked.Messages = append(locked.Messages, contextengine.Message{
		Role:      contextengine.RoleUser,
		Content:   "你能操作电脑关机？",
		CreatedAt: now,
		Metadata: map[string]any{
			meta.KeyChannel: "feishu",
			"sender_id":     "ou_user_1",
			"message_id":    "msg-zh-1",
		},
	})
	locked.UpdatedAt = now
	if err := sessionStore.Save(context.Background(), locked); err != nil {
		unlock()
		t.Fatalf("Save() error = %v", err)
	}
	unlock()

	adapter := &stubBridgeAdapter{inbound: make(chan channels.InboundMessage)}
	bridge := NewBridge(adapter, &stubBridgeRuntime{
		runs: map[string]*agent.Run{
			"run-auth": {
				ID:           "run-auth",
				SessionID:    session.ID,
				InputEventID: "msg-zh-1",
			},
		},
	}, sessionStore, nil, channels.DefaultStatusReminderDelay)

	event := eventbus.Event{
		Type:      eventbus.EventRunFailed,
		RunID:     "run-auth",
		SessionID: session.ID,
		Attrs: map[string]any{
			"error": "openai-compatible API error (401): authentication_error: OAuth token has expired",
		},
	}
	bridge.handleTerminalRun(context.Background(), event)
	bridge.handleTerminalRun(context.Background(), event)

	sent := adapter.Messages()
	if len(sent) != 1 {
		t.Fatalf("len(sent) = %d", len(sent))
	}
	if strings.Contains(sent[0].Content, "authentication_error") || strings.Contains(sent[0].Content, "401") {
		t.Fatalf("unexpected raw provider error in content: %q", sent[0].Content)
	}
	if !strings.Contains(sent[0].Content, "鉴权失败") {
		t.Fatalf("friendly auth message missing: %q", sent[0].Content)
	}
	if sent[0].TargetID != "ou_user_1" {
		t.Fatalf("sent[0].TargetID = %q", sent[0].TargetID)
	}
	if sent[0].ReplyToID != "msg-zh-1" {
		t.Fatalf("sent[0].ReplyToID = %q", sent[0].ReplyToID)
	}
	if got, _ := sent[0].Metadata["receive_id_type"].(string); got != "open_id" {
		t.Fatalf("receive_id_type = %q", got)
	}
}

// TestBridgeApprovalDelivered verifies that approval waits are sent to the
// user instead of staying invisible inside the runtime.
func TestBridgeApprovalDelivered(t *testing.T) {
	t.Parallel()

	sessionStore := agent.NewInMemorySessionStore()
	adapter := &stubBridgeAdapter{inbound: make(chan channels.InboundMessage)}
	bus := eventbus.NewInMemoryBus()
	bridge := NewBridge(adapter, &stubBridgeRuntime{
		submitRun: &agent.Run{ID: "run-pending"},
	}, sessionStore, bus, channels.DefaultStatusReminderDelay)
	bridge.status = channels.NewRunStatusNotifier(time.Hour, adapter.Send)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sub := bus.SubscribeChannel(128)
	go bridge.outboundLoop(ctx, sub)

	bridge.handleInbound(ctx, channels.InboundMessage{
		ChannelID: "feishu",
		SenderID:  "ou_user_1",
		Content:   "请继续执行",
		RawEvent: map[string]any{
			"chat_id":    "chat-1",
			"message_id": "msg-approve-1",
		},
	})
	if err := bus.Publish(ctx, eventbus.Event{Type: eventbus.EventRunWaitingApproval, RunID: "run-pending"}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	// Approval waits should be sent to the user.
	sent := adapter.Messages()
	foundApproval := false
	for _, msg := range sent {
		if kind, _ := msg.Metadata["status_kind"].(string); kind == "approval_waiting" {
			foundApproval = true
			if !strings.Contains(msg.Content, "等待审批确认") {
				t.Fatalf("unexpected approval_waiting message content: %q", msg.Content)
			}
		}
	}
	if !foundApproval {
		t.Fatalf("expected approval_waiting message, got %#v", sent)
	}

	// Internal state should still be tracked.
	snap, ok := bridge.status.SnapshotRun("run-pending")
	if !ok {
		t.Fatal("expected run to be tracked")
	}
	if snap.Target.RunID != "run-pending" {
		t.Fatalf("snapshot RunID = %q", snap.Target.RunID)
	}
}

func TestBridgeNotifiesSubmitFailure(t *testing.T) {
	t.Parallel()

	sessionStore := agent.NewInMemorySessionStore()
	adapter := &stubBridgeAdapter{inbound: make(chan channels.InboundMessage)}
	bridge := NewBridge(adapter, &stubBridgeRuntime{
		submitErr: errors.New("service unavailable"),
	}, sessionStore, nil, channels.DefaultStatusReminderDelay)

	bridge.handleInbound(context.Background(), channels.InboundMessage{
		ChannelID: "feishu",
		SenderID:  "ou_user_1",
		Content:   "帮我处理一下",
		RawEvent: map[string]any{
			"chat_id":    "chat-1",
			"message_id": "msg-submit-fail-1",
		},
	})

	sent := adapter.Messages()
	if len(sent) != 1 {
		t.Fatalf("len(sent) = %d", len(sent))
	}
	if got, _ := sent[0].Metadata["receive_id_type"].(string); got != "chat_id" {
		t.Fatalf("receive_id_type = %q", got)
	}
	if !strings.Contains(sent[0].Content, "没有成功启动") {
		t.Fatalf("submit failure content = %q", sent[0].Content)
	}
}

func TestBridgeStatusCommandReturnsActiveRunStatusWithoutSubmittingNewRun(t *testing.T) {
	t.Parallel()

	sessionStore := agent.NewInMemorySessionStore()
	adapter := &stubBridgeAdapter{inbound: make(chan channels.InboundMessage)}
	runtime := &stubBridgeRuntime{
		submitRun: &agent.Run{
			ID:     "run-progress",
			Status: agent.RunRunning,
			Phase:  "tools",
		},
		runs: map[string]*agent.Run{
			"run-progress": {
				ID:     "run-progress",
				Status: agent.RunRunning,
				Phase:  "tools",
			},
		},
	}
	bridge := NewBridge(adapter, runtime, sessionStore, nil, time.Hour)

	bridge.handleInbound(context.Background(), channels.InboundMessage{
		ChannelID: "feishu",
		SenderID:  "ou_user_1",
		Content:   "帮我搜今天的新闻",
		RawEvent: map[string]any{
			"chat_id":    "chat-1",
			"message_id": "msg-task-1",
		},
	})
	bridge.status.NotifyToolProgress("run-progress", 4, []string{"news.digest", "web.fetch"})
	bridge.handleInbound(context.Background(), channels.InboundMessage{
		ChannelID: "feishu",
		SenderID:  "ou_user_1",
		Content:   "/status",
		RawEvent: map[string]any{
			"chat_id":    "chat-1",
			"message_id": "msg-progress-1",
		},
	})

	if got := runtime.Submits(); got != 1 {
		t.Fatalf("runtime.Submits() = %d, want 1", got)
	}
	sent := adapter.Messages()
	if len(sent) != 1 {
		t.Fatalf("len(sent) = %d", len(sent))
	}
	if got, _ := sent[0].Metadata["status_kind"].(string); got != "control_status" {
		t.Fatalf("status_kind = %q", got)
	}
	if got, _ := sent[0].Metadata["control_command"].(string); got != "status" {
		t.Fatalf("control_command = %q", got)
	}
	if got, _ := sent[0].Metadata["receive_id_type"].(string); got != "chat_id" {
		t.Fatalf("receive_id_type = %q", got)
	}
	if !strings.Contains(sent[0].Content, "running") {
		t.Fatalf("progress content = %q", sent[0].Content)
	}
	if !strings.Contains(sent[0].Content, "already done some checks") {
		t.Fatalf("friendly progress summary missing from content = %q", sent[0].Content)
	}
}

func TestBridgeShortCircuitsRepeatedMessagesDuringAuthOutage(t *testing.T) {
	t.Parallel()

	sessionStore := agent.NewInMemorySessionStore()
	adapter := &stubBridgeAdapter{inbound: make(chan channels.InboundMessage)}
	runtime := &stubBridgeRuntime{
		submitRun: &agent.Run{ID: "run-auth-outage"},
	}
	bridge := NewBridge(adapter, runtime, sessionStore, nil, channels.DefaultStatusReminderDelay)
	bridge.authGate = channels.NewAuthFailureGate(time.Hour, time.Hour)
	bridge.authGate.Arm("feishu:chat-1")

	bridge.handleInbound(context.Background(), channels.InboundMessage{
		ChannelID: "feishu",
		SenderID:  "ou_user_1",
		Content:   "继续处理",
		RawEvent: map[string]any{
			"chat_id":    "chat-1",
			"message_id": "msg-auth-1",
		},
	})
	bridge.handleInbound(context.Background(), channels.InboundMessage{
		ChannelID: "feishu",
		SenderID:  "ou_user_1",
		Content:   "继续处理",
		RawEvent: map[string]any{
			"chat_id":    "chat-1",
			"message_id": "msg-auth-2",
		},
	})

	if got := runtime.Submits(); got != 0 {
		t.Fatalf("runtime.Submits() = %d, want 0", got)
	}
	sent := adapter.Messages()
	// With reminder-interval gating, only the first blocked message sends a
	// notification; the second is silently blocked (within the 1-hour reminder window).
	if len(sent) != 1 {
		t.Fatalf("len(sent) = %d, want 1 (second message suppressed by reminder interval)", len(sent))
	}
	if got, _ := sent[0].Metadata["status_kind"].(string); got != "backend_auth_unavailable" {
		t.Fatalf("status_kind = %q", got)
	}
	if !strings.Contains(sent[0].Content, "鉴权") {
		t.Fatalf("content = %q", sent[0].Content)
	}
}

func TestBridgeApprovesPendingRequestViaReply(t *testing.T) {
	t.Parallel()

	sessionStore := agent.NewInMemorySessionStore()
	session, err := sessionStore.GetOrCreate(context.Background(), "feishu:chat-1", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	adapter := &stubBridgeAdapter{inbound: make(chan channels.InboundMessage)}
	runtime := &stubBridgeRuntime{
		pendingBySession: map[string]*approval.Ticket{
			session.ID: {ID: "appr-feishu-1", RunID: "run-feishu-1", SessionID: session.ID, Kind: approval.KindSkillInstall, Status: approval.StatusPending},
		},
		approvalsByID: map[string]*approval.Ticket{
			"appr-feishu-1": {ID: "appr-feishu-1", RunID: "run-feishu-1", SessionID: session.ID, Kind: approval.KindSkillInstall, Status: approval.StatusPending},
		},
	}
	bridge := NewBridge(adapter, runtime, sessionStore, nil, channels.DefaultStatusReminderDelay)

	bridge.handleInbound(context.Background(), channels.InboundMessage{
		ChannelID: "feishu",
		SenderID:  "ou_user_1",
		Content:   "y",
		RawEvent: map[string]any{
			"chat_id":    "chat-1",
			"message_id": "msg-reply-1",
		},
	})

	if got := runtime.Submits(); got != 0 {
		t.Fatalf("runtime.Submits() = %d, want 0", got)
	}
	resolved := runtime.Resolved()
	if len(resolved) != 1 || resolved[0].Status != approval.StatusApproved {
		t.Fatalf("runtime.Resolved() = %#v", resolved)
	}
	sent := adapter.Messages()
	if len(sent) != 1 {
		t.Fatalf("len(sent) = %d", len(sent))
	}
	if got := sent[0].Metadata["status_kind"]; got != "approval_reply_approved" {
		t.Fatalf("status_kind = %q", got)
	}
	if got, _ := sent[0].Metadata["receive_id_type"].(string); got != "chat_id" {
		t.Fatalf("receive_id_type = %q", got)
	}
}

func TestBridgeUsesInteractAndAutoApprovesSessionOnTaskAccept(t *testing.T) {
	t.Parallel()

	sessionStore := agent.NewInMemorySessionStore()
	session, err := sessionStore.GetOrCreate(context.Background(), "feishu:chat-1", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	adapter := &stubBridgeAdapter{inbound: make(chan channels.InboundMessage)}
	runtime := &stubInteractRuntime{
		stubBridgeRuntime: &stubBridgeRuntime{},
		interactionResp: &runtimesvc.InteractionResult{
			Decision: runtimesvc.InteractionDecision{
				ReplyAct: runtimesvc.ReplyActTaskAccept,
			},
			Run: &agent.Run{
				ID:        "run-feishu-interact",
				SessionID: session.ID,
			},
		},
	}
	bridge := NewBridge(adapter, runtime, sessionStore, nil, channels.DefaultStatusReminderDelay)

	bridge.handleInbound(context.Background(), channels.InboundMessage{
		ChannelID: "feishu",
		SenderID:  "ou_user_1",
		Content:   "帮我查一下",
		RawEvent: map[string]any{
			"chat_id":    "chat-1",
			"message_id": "msg-interact-1",
		},
	})

	if runtime.interactions != 1 {
		t.Fatalf("runtime.interactions = %d, want 1", runtime.interactions)
	}
	if got := runtime.Submits(); got != 0 {
		t.Fatalf("runtime.Submits() = %d, want 0", got)
	}
	saved, err := sessionStore.Get(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !channels.SessionAutoApproveSession(saved) {
		t.Fatal("expected session auto-approve to be enabled")
	}
	snap, ok := bridge.status.SnapshotRun("run-feishu-interact")
	if !ok {
		t.Fatal("expected run to be tracked")
	}
	if snap.Target.RunID != "run-feishu-interact" {
		t.Fatalf("snapshot RunID = %q", snap.Target.RunID)
	}
}

func TestBridgeStreamingCardLifecycle(t *testing.T) {
	t.Parallel()

	sessionStore := agent.NewInMemorySessionStore()
	session, err := sessionStore.GetOrCreate(context.Background(), "feishu:chat-1", "test-model")
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
			Content:   "stream question",
			CreatedAt: now,
			Metadata: map[string]any{
				meta.KeyChannel:   "feishu",
				meta.KeyChatID:    "chat-1",
				meta.KeyMessageID: "msg-stream-1",
			},
		},
		contextengine.Message{
			Role:      contextengine.RoleAssistant,
			Content:   "hello world",
			CreatedAt: now.Add(time.Second),
			Metadata: map[string]any{
				meta.KeyRunID: "run-stream",
			},
		},
	)
	locked.UpdatedAt = now.Add(time.Second)
	if err := sessionStore.Save(context.Background(), locked); err != nil {
		unlock()
		t.Fatalf("Save() error = %v", err)
	}
	unlock()

	bus := eventbus.NewInMemoryBus()
	adapter := &stubBridgeAdapter{inbound: make(chan channels.InboundMessage)}
	bridge := NewBridge(adapter, &stubBridgeRuntime{
		runs: map[string]*agent.Run{
			"run-stream": {
				ID:           "run-stream",
				SessionID:    session.ID,
				InputEventID: "msg-stream-1",
			},
		},
	}, sessionStore, bus, channels.DefaultStatusReminderDelay)
	bridge.status.Track(context.Background(), channels.RunNotificationTarget{
		RunID:        "run-stream",
		SessionKey:   session.Key,
		ChannelID:    "feishu",
		TargetID:     "chat-1",
		ReplyToID:    "msg-stream-1",
		InputContent: "stream question",
		Format:       "text",
		Metadata: map[string]any{
			meta.KeyReceiveIDType: "chat_id",
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sub := bus.SubscribeChannel(128)
	go bridge.outboundLoop(ctx, sub)

	if err := bus.Publish(ctx, eventbus.Event{Type: eventbus.EventModelTextDelta, RunID: "run-stream", SessionID: session.ID, Attrs: map[string]any{"delta": "hello"}}); err != nil {
		t.Fatalf("Publish(delta) error = %v", err)
	}
	if err := bus.Publish(ctx, eventbus.Event{Type: eventbus.EventModelTextDelta, RunID: "run-stream", SessionID: session.ID, Attrs: map[string]any{"delta": " world"}}); err != nil {
		t.Fatalf("Publish(delta2) error = %v", err)
	}
	waitForCondition(t, func() bool {
		return adapter.StreamingStartCount() == 1 && adapter.StreamingUpdateCount() >= 1
	})
	if got := len(adapter.Messages()); got != 0 {
		t.Fatalf("len(adapter.Messages()) = %d, want 0 before terminal delivery", got)
	}

	if err := bus.Publish(ctx, eventbus.Event{Type: eventbus.EventRunCompleted, RunID: "run-stream", SessionID: session.ID}); err != nil {
		t.Fatalf("Publish(completed) error = %v", err)
	}
	waitForCondition(t, func() bool {
		update, ok := adapter.LastStreamingUpdate()
		return ok && update.Final
	})
	update, _ := adapter.LastStreamingUpdate()
	if update.Content != "hello world" {
		t.Fatalf("final streaming content = %q", update.Content)
	}
	if got := len(adapter.Messages()); got != 0 {
		t.Fatalf("len(adapter.Messages()) = %d, want 0 after final card update", got)
	}
}

func TestBridgeTypingReactionKeepaliveAndStop(t *testing.T) {
	t.Parallel()

	oldInterval := typingKeepaliveInterval
	oldCooldown := typingCircuitBreakerCooldown
	typingKeepaliveInterval = 20 * time.Millisecond
	typingCircuitBreakerCooldown = 40 * time.Millisecond
	t.Cleanup(func() {
		typingKeepaliveInterval = oldInterval
		typingCircuitBreakerCooldown = oldCooldown
	})

	sessionStore := agent.NewInMemorySessionStore()
	session, err := sessionStore.GetOrCreate(context.Background(), "feishu:chat-1", "test-model")
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
			Content:   "typing question",
			CreatedAt: now,
			Metadata: map[string]any{
				meta.KeyChannel:   "feishu",
				meta.KeyChatID:    "chat-1",
				meta.KeyMessageID: "msg-typing-1",
			},
		},
		contextengine.Message{
			Role:      contextengine.RoleAssistant,
			Content:   "typing done",
			CreatedAt: now.Add(time.Second),
			Metadata: map[string]any{
				meta.KeyRunID: "run-typing",
			},
		},
	)
	locked.UpdatedAt = now.Add(time.Second)
	if err := sessionStore.Save(context.Background(), locked); err != nil {
		unlock()
		t.Fatalf("Save() error = %v", err)
	}
	unlock()

	bus := eventbus.NewInMemoryBus()
	adapter := &stubBridgeAdapter{inbound: make(chan channels.InboundMessage)}
	bridge := NewBridge(adapter, &stubBridgeRuntime{
		runs: map[string]*agent.Run{
			"run-typing": {
				ID:           "run-typing",
				SessionID:    session.ID,
				InputEventID: "msg-typing-1",
			},
		},
	}, sessionStore, bus, channels.DefaultStatusReminderDelay)
	bridge.status.Track(context.Background(), channels.RunNotificationTarget{
		RunID:        "run-typing",
		SessionKey:   session.Key,
		ChannelID:    "feishu",
		TargetID:     "chat-1",
		ReplyToID:    "msg-typing-1",
		InputContent: "typing question",
		Format:       "text",
		Metadata: map[string]any{
			meta.KeyReceiveIDType: "chat_id",
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sub := bus.SubscribeChannel(128)
	go bridge.outboundLoop(ctx, sub)

	if err := bus.Publish(ctx, eventbus.Event{Type: eventbus.EventRunStarted, RunID: "run-typing", SessionID: session.ID}); err != nil {
		t.Fatalf("Publish(started) error = %v", err)
	}
	waitForCondition(t, func() bool {
		return adapter.TypingAddCount() >= 2
	})

	if err := bus.Publish(ctx, eventbus.Event{Type: eventbus.EventRunCompleted, RunID: "run-typing", SessionID: session.ID}); err != nil {
		t.Fatalf("Publish(completed) error = %v", err)
	}
	waitForCondition(t, func() bool {
		return adapter.TypingRemoveCount() >= 1
	})
}

func TestBridgePrunesDeliveredAndStreamingStateByTTL(t *testing.T) {
	t.Parallel()

	bridge := NewBridge(&stubBridgeAdapter{inbound: make(chan channels.InboundMessage)}, nil, nil, nil, channels.DefaultStatusReminderDelay)
	now := time.Now().UTC()
	bridge.delivered["expired"] = now.Add(-channels.BridgeDeliveredStateTTL - time.Second)
	bridge.delivered["fresh"] = now
	bridge.streams["expired"] = &streamingState{
		messageID: "stream-expired",
		updatedAt: now.Add(-channels.BridgeStreamingStateTTL - time.Second),
	}
	bridge.streams["fresh"] = &streamingState{
		messageID: "stream-fresh",
		updatedAt: now,
	}

	if bridge.isDelivered("expired") {
		t.Fatal("expired delivered state should have been pruned")
	}
	if !bridge.isDelivered("fresh") {
		t.Fatal("fresh delivered state should remain present")
	}

	bridge.mu.Lock()
	bridge.pruneStreamingStatesLocked(now)
	_, expiredOK := bridge.streams["expired"]
	_, freshOK := bridge.streams["fresh"]
	bridge.mu.Unlock()
	if expiredOK {
		t.Fatal("expired streaming state should have been pruned")
	}
	if !freshOK {
		t.Fatal("fresh streaming state should remain present")
	}
}

func TestBridgeStopClearsTransientStateAndCancelsTyping(t *testing.T) {
	t.Parallel()

	bridge := NewBridge(&stubBridgeAdapter{inbound: make(chan channels.InboundMessage)}, nil, nil, nil, channels.DefaultStatusReminderDelay)
	cancelled := false
	bridge.delivered["run-1"] = time.Now().UTC()
	bridge.streams["run-1"] = &streamingState{messageID: "stream-1", updatedAt: time.Now().UTC()}
	bridge.typing["run-1"] = &typingState{
		messageID:  "msg-1",
		reactionID: "reaction-1",
		cancel: func() {
			cancelled = true
		},
	}

	bridge.Stop()

	bridge.mu.Lock()
	deliveredCount := len(bridge.delivered)
	streamCount := len(bridge.streams)
	typingCount := len(bridge.typing)
	bridge.mu.Unlock()
	if !cancelled {
		t.Fatal("typing cancel func was not invoked")
	}
	if deliveredCount != 0 || streamCount != 0 || typingCount != 0 {
		t.Fatalf("transient state not cleared: delivered=%d streams=%d typing=%d", deliveredCount, streamCount, typingCount)
	}
}

func TestEnsureStreamingStateSerializesConcurrentCardCreation(t *testing.T) {
	t.Parallel()

	adapter := &blockingStreamingAdapter{
		stubBridgeAdapter: &stubBridgeAdapter{inbound: make(chan channels.InboundMessage)},
		entered:           make(chan struct{}),
		release:           make(chan struct{}),
	}
	bridge := NewBridge(adapter, nil, nil, nil, channels.DefaultStatusReminderDelay)
	target := channels.RunNotificationTarget{
		TargetID:     "chat-1",
		ReplyToID:    "msg-1",
		InputContent: "stream please",
		Format:       "text",
	}

	results := make(chan *streamingState, 2)
	go func() {
		results <- bridge.ensureStreamingState(context.Background(), adapter, "run-stream", target)
	}()

	select {
	case <-adapter.entered:
	case <-time.After(time.Second):
		t.Fatal("StartStreamingCard() was not invoked")
	}

	go func() {
		results <- bridge.ensureStreamingState(context.Background(), adapter, "run-stream", target)
	}()

	close(adapter.release)

	first := <-results
	second := <-results
	if first == nil || second == nil {
		t.Fatalf("ensureStreamingState() returned nil states: first=%#v second=%#v", first, second)
	}
	if first != second {
		t.Fatalf("ensureStreamingState() returned different state pointers: first=%p second=%p", first, second)
	}

	adapter.mu.Lock()
	starts := len(adapter.streamingStarts)
	adapter.mu.Unlock()
	if starts != 1 {
		t.Fatalf("StartStreamingCard() calls = %d, want 1", starts)
	}
}

func TestBridgePairingFlowForDirectMessages(t *testing.T) {
	t.Parallel()

	sessionStore := agent.NewInMemorySessionStore()
	store := channelpairing.NewInMemoryStore()
	manager := channelpairing.NewManager(store)
	adapter := &stubBridgeAdapter{
		inbound:          make(chan channels.InboundMessage),
		defaultAccountID: "default",
		accounts: map[string]ResolvedAccount{
			"default": {
				ID:       "default",
				DMPolicy: "pairing",
			},
		},
	}
	runtime := &stubBridgeRuntime{
		submitRun: &agent.Run{ID: "run-paired", SessionID: "session-paired"},
	}
	bridge := NewBridge(adapter, runtime, sessionStore, nil, channels.DefaultStatusReminderDelay).WithPairing(manager)

	bridge.handleInbound(context.Background(), channels.InboundMessage{
		ChannelID: "feishu",
		SenderID:  "ou_user_pair",
		Content:   "run a task",
		RawEvent: map[string]any{
			"account_id": "default",
			"chat_id":    "chat-dm-1",
			"message_id": "msg-pair-1",
			"chat_type":  "p2p",
		},
	})
	waitForMessages(t, adapter, 1)
	first := adapter.Messages()[0]
	if !strings.Contains(first.Content, "Pairing required") {
		t.Fatalf("pairing required reply = %q", first.Content)
	}
	if runtime.Submits() != 0 {
		t.Fatalf("runtime.Submits() = %d, want 0 before pairing", runtime.Submits())
	}
	record, err := store.Get("feishu", "ou_user_pair")
	if err != nil {
		t.Fatalf("store.Get() error = %v", err)
	}

	bridge.handleInbound(context.Background(), channels.InboundMessage{
		ChannelID: "feishu",
		SenderID:  "ou_user_pair",
		Content:   "/pair status",
		RawEvent: map[string]any{
			"account_id": "default",
			"chat_id":    "chat-dm-1",
			"message_id": "msg-pair-2",
			"chat_type":  "p2p",
		},
	})
	waitForMessages(t, adapter, 2)
	if !strings.Contains(adapter.Messages()[1].Content, "pending") {
		t.Fatalf("pairing status reply = %q", adapter.Messages()[1].Content)
	}

	bridge.handleInbound(context.Background(), channels.InboundMessage{
		ChannelID: "feishu",
		SenderID:  "ou_user_pair",
		Content:   "/pair verify " + record.Code,
		RawEvent: map[string]any{
			"account_id": "default",
			"chat_id":    "chat-dm-1",
			"message_id": "msg-pair-3",
			"chat_type":  "p2p",
		},
	})
	waitForMessages(t, adapter, 3)
	if !manager.IsVerified("feishu", "ou_user_pair") {
		t.Fatal("expected pairing to be verified")
	}
	if !strings.Contains(adapter.Messages()[2].Content, "Pairing verified") {
		t.Fatalf("pairing verify reply = %q", adapter.Messages()[2].Content)
	}

	bridge.handleInbound(context.Background(), channels.InboundMessage{
		ChannelID: "feishu",
		SenderID:  "ou_user_pair",
		Content:   "run a task again",
		RawEvent: map[string]any{
			"account_id": "default",
			"chat_id":    "chat-dm-1",
			"message_id": "msg-pair-4",
			"chat_type":  "p2p",
		},
	})
	if runtime.Submits() != 1 {
		t.Fatalf("runtime.Submits() = %d, want 1 after verify", runtime.Submits())
	}
}

type stubBridgeAdapter struct {
	inbound chan channels.InboundMessage

	mu               sync.Mutex
	sent             []channels.OutboundMessage
	defaultAccountID string
	accounts         map[string]ResolvedAccount
	streamingStarts  []channels.OutboundMessage
	streamingUpdates []stubStreamingUpdate
	typingAdds       []stubTypingEvent
	typingRemoves    []stubTypingEvent
}

type blockingStreamingAdapter struct {
	*stubBridgeAdapter
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (a *blockingStreamingAdapter) StartStreamingCard(ctx context.Context, msg channels.OutboundMessage) (string, error) {
	a.once.Do(func() {
		close(a.entered)
		<-a.release
	})
	return a.stubBridgeAdapter.StartStreamingCard(ctx, msg)
}

func (a *stubBridgeAdapter) Connect(context.Context) error { return nil }

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

func (a *stubBridgeAdapter) Status() channels.Status { return channels.StatusConnected }

func (a *stubBridgeAdapter) SubscribeEvents() <-chan channels.InboundMessage { return a.inbound }

func (a *stubBridgeAdapter) DefaultAccountID() string {
	if strings.TrimSpace(a.defaultAccountID) == "" {
		return "default"
	}
	return a.defaultAccountID
}

func (a *stubBridgeAdapter) Account(accountID string) (ResolvedAccount, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	account, ok := a.accounts[accountID]
	return account, ok
}

func (a *stubBridgeAdapter) StartStreamingCard(_ context.Context, msg channels.OutboundMessage) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.streamingStarts = append(a.streamingStarts, msg)
	return fmt.Sprintf("stream-%d", len(a.streamingStarts)), nil
}

func (a *stubBridgeAdapter) UpdateStreamingCard(_ context.Context, messageID string, content string, final bool, metadata map[string]any) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.streamingUpdates = append(a.streamingUpdates, stubStreamingUpdate{
		MessageID: messageID,
		Content:   content,
		Final:     final,
		Metadata:  cloneMetadata(metadata),
	})
	return nil
}

func (a *stubBridgeAdapter) AddTypingIndicator(_ context.Context, messageID string, metadata map[string]any) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	reactionID := fmt.Sprintf("reaction-%d", len(a.typingAdds)+1)
	a.typingAdds = append(a.typingAdds, stubTypingEvent{
		MessageID:  messageID,
		ReactionID: reactionID,
		Metadata:   cloneMetadata(metadata),
	})
	return reactionID, nil
}

func (a *stubBridgeAdapter) RemoveTypingIndicator(_ context.Context, messageID, reactionID string, metadata map[string]any) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.typingRemoves = append(a.typingRemoves, stubTypingEvent{
		MessageID:  messageID,
		ReactionID: reactionID,
		Metadata:   cloneMetadata(metadata),
	})
	return nil
}

func (a *stubBridgeAdapter) Messages() []channels.OutboundMessage {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]channels.OutboundMessage, len(a.sent))
	copy(out, a.sent)
	return out
}

func (a *stubBridgeAdapter) StreamingStartCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.streamingStarts)
}

func (a *stubBridgeAdapter) StreamingUpdateCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.streamingUpdates)
}

func (a *stubBridgeAdapter) LastStreamingUpdate() (stubStreamingUpdate, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.streamingUpdates) == 0 {
		return stubStreamingUpdate{}, false
	}
	return a.streamingUpdates[len(a.streamingUpdates)-1], true
}

func (a *stubBridgeAdapter) TypingAddCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.typingAdds)
}

func (a *stubBridgeAdapter) TypingRemoveCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.typingRemoves)
}

type stubStreamingUpdate struct {
	MessageID string
	Content   string
	Final     bool
	Metadata  map[string]any
}

type stubTypingEvent struct {
	MessageID  string
	ReactionID string
	Metadata   map[string]any
}

type stubBridgeRuntime struct {
	mu               sync.Mutex // guards all fields
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

func (r *stubBridgeRuntime) Submit(_ context.Context, _ runtimesvc.SubmitRequest) (*agent.Run, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
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
	r.mu.Lock()
	r.lastInteractReq = req
	r.mu.Unlock()
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
		var run *agent.Run
		for _, item := range r.runs {
			if item != nil {
				copyRun := *item
				run = &copyRun
				break
			}
		}
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

func TestBridgeAppliesInboundScopeMetadata(t *testing.T) {
	t.Parallel()

	adapter := &stubBridgeAdapter{inbound: make(chan channels.InboundMessage)}
	runtime := &stubBridgeRuntime{
		submitRun: &agent.Run{ID: "run-scope-1"},
	}
	bridge := NewBridge(adapter, runtime, agent.NewInMemorySessionStore(), nil, channels.DefaultStatusReminderDelay)

	bridge.handleInbound(context.Background(), channels.InboundMessage{
		ChannelID: "feishu",
		SenderID:  "ou_user_1",
		Content:   "scope please",
		RawEvent: map[string]any{
			"chat_id":       "chat-1",
			"message_id":    "msg-1",
			"automation_id": "auto-feishu-1",
		},
	})

	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if got := runtime.lastInteractReq.AutomationID; got != "auto-feishu-1" {
		t.Fatalf("AutomationID = %q, want auto-feishu-1", got)
	}
}

func (r *stubInteractRuntime) Interact(_ context.Context, _ runtimesvc.InteractionRequest) (*runtimesvc.InteractionResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.interactions++
	return r.interactionResp, r.interactionErr
}

func (r *stubBridgeRuntime) GetRun(_ context.Context, id string) (*agent.Run, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
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
	r.mu.Lock()
	defer r.mu.Unlock()
	ticket, ok := r.approvalsByID[id]
	if !ok {
		return nil, context.Canceled
	}
	copyTicket := *ticket
	return &copyTicket, nil
}

func (r *stubBridgeRuntime) FindPendingApproval(_ context.Context, sessionID string) (*approval.Ticket, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ticket, ok := r.pendingBySession[sessionID]
	if !ok {
		return nil, context.Canceled
	}
	copyTicket := *ticket
	return &copyTicket, nil
}

func (r *stubBridgeRuntime) ResolveApproval(_ context.Context, id string, resolution approval.Resolution) (*approval.Ticket, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
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
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cancelled = append(r.cancelled, runID)
	run, ok := r.runs[runID]
	if !ok {
		return nil, context.Canceled
	}
	copyRun := *run
	copyRun.Status = agent.RunCancelled
	r.runs[runID] = &copyRun
	return &copyRun, nil
}

// Thread-safe accessors for test assertions.

func (r *stubBridgeRuntime) Submits() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.submits
}

func (r *stubBridgeRuntime) Resolved() []approval.Resolution {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]approval.Resolution, len(r.resolved))
	copy(out, r.resolved)
	return out
}

func waitForMessages(t *testing.T, adapter *stubBridgeAdapter, want int) {
	t.Helper()
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if len(adapter.Messages()) >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d messages; got %d", want, len(adapter.Messages()))
}

func waitForCondition(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(750 * time.Millisecond)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for condition")
}
