package channels

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/approval"
	"github.com/fulcrus/hopclaw/contextengine"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/internal/meta"
	runtimesvc "github.com/fulcrus/hopclaw/runtime"
)

func TestBridge_InboundMessageSubmitsRun(t *testing.T) {
	t.Parallel()

	adapter := &bridgeStubAdapter{inbound: make(chan InboundMessage, 1)}
	runtime := &bridgeStubRuntime{
		run: &agent.Run{ID: "run-inbound-submit"},
	}
	bridge := NewBridge(BridgeConfig{
		ChannelName:  "whatsapp",
		TargetIDKey:  "from",
		MessageIDKey: "message_id",
	}, adapter, runtime, agent.NewInMemorySessionStore(), nil, DefaultStatusReminderDelay)

	bridge.handleInbound(context.Background(), InboundMessage{
		ChannelID: "whatsapp",
		SenderID:  "8613800138000",
		Content:   "help me draft this",
		RawEvent: map[string]any{
			"from":       "8613800138000",
			"message_id": "wamid-scenario-submit-1",
		},
	})

	if runtime.Submits() != 1 {
		t.Fatalf("runtime.Submits() = %d, want 1", runtime.Submits())
	}
	submitted := runtime.Submitted()
	if submitted == nil {
		t.Fatal("expected submitted request")
	}
	if submitted.Content != "help me draft this" {
		t.Fatalf("submitted.Content = %q, want %q", submitted.Content, "help me draft this")
	}
}

func TestBridge_DuplicateMessageDropped(t *testing.T) {
	t.Parallel()

	adapter := &bridgeStubAdapter{inbound: make(chan InboundMessage, 1)}
	runtime := &bridgeStubRuntime{
		run: &agent.Run{ID: "run-duplicate"},
	}
	bridge := NewBridge(BridgeConfig{
		ChannelName:  "whatsapp",
		TargetIDKey:  "from",
		MessageIDKey: "message_id",
	}, adapter, runtime, agent.NewInMemorySessionStore(), nil, DefaultStatusReminderDelay).
		WithMessageDeduper(NewMessageDeduper("", time.Hour))

	msg := InboundMessage{
		ChannelID: "whatsapp",
		SenderID:  "8613800138000",
		Content:   "same message twice",
		RawEvent: map[string]any{
			"from":       "8613800138000",
			"message_id": "wamid-scenario-dup-1",
		},
	}

	bridge.handleInbound(context.Background(), msg)
	bridge.handleInbound(context.Background(), msg)

	if runtime.Submits() != 1 {
		t.Fatalf("runtime.Submits() = %d, want 1", runtime.Submits())
	}
}

func TestBridge_ApprovalReplyResolves(t *testing.T) {
	t.Parallel()

	store := agent.NewInMemorySessionStore()
	session, err := store.GetOrCreate(context.Background(), "whatsapp:8613800138000", "test-model")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}

	adapter := &bridgeStubAdapter{inbound: make(chan InboundMessage, 1)}
	runtime := &bridgeStubRuntime{
		run: &agent.Run{ID: "run-approval-scenario"},
		pendingBySession: map[string]*approval.Ticket{
			session.ID: {
				ID:        "appr-scenario-1",
				RunID:     "run-approval-scenario",
				SessionID: session.ID,
				Status:    approval.StatusPending,
			},
		},
		approvalsByID: map[string]*approval.Ticket{
			"appr-scenario-1": {
				ID:        "appr-scenario-1",
				RunID:     "run-approval-scenario",
				SessionID: session.ID,
				Status:    approval.StatusPending,
			},
		},
	}
	bridge := NewBridge(BridgeConfig{
		ChannelName:  "whatsapp",
		TargetIDKey:  "from",
		MessageIDKey: "message_id",
	}, adapter, runtime, store, nil, DefaultStatusReminderDelay)

	bridge.handleInbound(context.Background(), InboundMessage{
		ChannelID: "whatsapp",
		SenderID:  "8613800138000",
		Content:   "yes",
		RawEvent: map[string]any{
			"from":       "8613800138000",
			"message_id": "wamid-scenario-approval-1",
		},
	})

	if runtime.Submits() != 0 {
		t.Fatalf("runtime.Submits() = %d, want 0", runtime.Submits())
	}
	if len(runtime.Resolved()) != 1 {
		t.Fatalf("len(runtime.Resolved()) = %d, want 1", len(runtime.Resolved()))
	}
	if runtime.Resolved()[0].Status != approval.StatusApproved {
		t.Fatalf("runtime.Resolved()[0].Status = %q, want %q", runtime.Resolved()[0].Status, approval.StatusApproved)
	}
}

func TestBridge_StreamingDelivery(t *testing.T) {
	t.Parallel()

	adapter := newBridgeStreamingStubAdapter()
	runtime := &bridgeStubRuntime{
		run: &agent.Run{ID: "run-streaming-scenario", InputEventID: "msg-streaming-1"},
		runResult: &runtimesvc.RunResult{
			Output: "streamed final output",
		},
	}
	store := agent.NewInMemorySessionStore()
	session, err := store.GetOrCreate(context.Background(), "scenario:chat-1", "test-model", "sess-streaming-scenario")
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}
	runtime.run.SessionID = session.ID

	locked, unlock, err := store.LoadForExecution(context.Background(), session.ID)
	if err != nil {
		t.Fatalf("LoadForExecution() error = %v", err)
	}
	locked.Messages = append(locked.Messages, contextengine.Message{
		Role:    contextengine.RoleUser,
		Content: "please stream",
		Metadata: map[string]any{
			meta.KeyChannel: "scenario",
			"chat_id":       "chat-1",
			"message_id":    "msg-streaming-1",
		},
	})
	locked.MessageCount = len(locked.Messages)
	if err := store.Save(context.Background(), locked); err != nil {
		unlock()
		t.Fatalf("Save() error = %v", err)
	}
	unlock()

	bus := eventbus.NewInMemoryBus()
	bridge := NewBridge(BridgeConfig{
		ChannelName:  "scenario",
		TargetIDKey:  "chat_id",
		MessageIDKey: "message_id",
	}, adapter, runtime, store, bus, DefaultStatusReminderDelay).WithDirectSessionUsesChatID(true)
	bridge.status.Track(context.Background(), RunNotificationTarget{
		RunID:        "run-streaming-scenario",
		SessionKey:   "scenario:chat-1",
		ChannelID:    "scenario",
		TargetID:     "chat-1",
		ReplyToID:    "msg-streaming-1",
		InputContent: "please stream",
		Format:       "text",
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bridge.Start(ctx)
	defer bridge.Stop()

	for _, event := range []eventbus.Event{
		{Type: eventbus.EventModelTextDelta, RunID: "run-streaming-scenario", SessionID: session.ID, Attrs: map[string]any{"delta": "streamed"}},
		{Type: eventbus.EventModelTextDelta, RunID: "run-streaming-scenario", SessionID: session.ID, Attrs: map[string]any{"delta": " final output"}},
		{Type: eventbus.EventRunCompleted, RunID: "run-streaming-scenario", SessionID: session.ID},
	} {
		if err := bus.Publish(context.Background(), event); err != nil {
			t.Fatalf("Publish(%s) error = %v", event.Type, err)
		}
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		adapter.streamMu.Lock()
		beginCount := adapter.beginCount
		updateCount := adapter.updateCount
		endCount := adapter.endCount
		adapter.streamMu.Unlock()
		if beginCount == 1 && updateCount >= 1 && endCount == 1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	adapter.streamMu.Lock()
	defer adapter.streamMu.Unlock()
	t.Fatalf("stream counts = begin:%d update:%d end:%d contents:%#v", adapter.beginCount, adapter.updateCount, adapter.endCount, adapter.streamContent)
}

func TestBridge_SendFailureDoesNotPanic(t *testing.T) {
	t.Parallel()

	adapter := &bridgeStubAdapter{
		inbound:  make(chan InboundMessage, 1),
		sendErrs: []error{errors.New("send failed"), errors.New("send failed"), errors.New("send failed"), errors.New("send failed")},
	}
	runtime := &bridgeInteractStubRuntime{
		bridgeStubRuntime: &bridgeStubRuntime{},
		interactionResp: &runtimesvc.InteractionResult{
			Decision: runtimesvc.InteractionDecision{
				SpeechAct: runtimesvc.SpeechActCasualChat,
				ReplyAct:  runtimesvc.ReplyActChatReply,
			},
			ReplyMessage: "still handled",
		},
	}
	bridge := NewBridge(BridgeConfig{
		ChannelName:  "whatsapp",
		TargetIDKey:  "from",
		MessageIDKey: "message_id",
	}, adapter, runtime, agent.NewInMemorySessionStore(), eventbus.NewInMemoryBus(), DefaultStatusReminderDelay)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("handleInbound() panicked: %v", r)
		}
	}()

	ctx := withInteractionDeliveryRetryBackoffs(context.Background(), []time.Duration{0, 0, 0})
	bridge.handleInbound(ctx, InboundMessage{
		ChannelID: "whatsapp",
		SenderID:  "8613800138000",
		Content:   "hello despite send failure",
		RawEvent: map[string]any{
			"from":       "8613800138000",
			"message_id": "wamid-scenario-sendfail-1",
		},
	})

	if runtime.interactions != 1 {
		t.Fatalf("runtime.interactions = %d, want 1", runtime.interactions)
	}
	if adapter.SendCalls() != 5 {
		t.Fatalf("adapter.SendCalls() = %d, want 5", adapter.SendCalls())
	}
	if sent := adapter.Messages(); len(sent) != 1 || sent[0].Content != BridgeDeliveryFailureNoticeMessage("hello despite send failure") {
		t.Fatalf("adapter.Messages() = %#v, want one delivery-failure notice", sent)
	}
}
