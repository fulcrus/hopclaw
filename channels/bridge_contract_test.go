package channels

import (
	"context"
	"testing"

	"github.com/fulcrus/hopclaw/agent"
	"github.com/fulcrus/hopclaw/eventbus"
	"github.com/fulcrus/hopclaw/internal/meta"
)

func TestBridgeInboundInteractDeliveryContract(t *testing.T) {
	t.Parallel()

	adapter := &bridgeStubAdapter{inbound: make(chan InboundMessage, 1)}
	runtime := &bridgeStubRuntime{
		run: &agent.Run{ID: "run-bridge-contract"},
	}
	bridge := NewBridge(BridgeConfig{
		ChannelName:  "slack",
		TargetIDKey:  "channel_id",
		MessageIDKey: "message_id",
		ThreadIDKey:  "thread_ts",
	}, adapter, runtime, agent.NewInMemorySessionStore(), eventbus.NewInMemoryBus(), DefaultStatusReminderDelay).
		WithPolicy(PolicyConfig{ReplyInThread: true})

	bridge.handleInbound(context.Background(), InboundMessage{
		ChannelID: "slack",
		SenderID:  "U123",
		Content:   "/status",
		RawEvent: map[string]any{
			"channel_id":    "C123",
			"message_id":    "m-1",
			"thread_ts":     "t-1",
			"automation_id": "auto-1",
			"source_type":   "group",
			"mentioned":     true,
			"sender_name":   "Alice",
		},
	})

	req := runtime.InteractionRequest()
	if req == nil {
		t.Fatal("expected Interact request")
	}
	if req.AutomationID != "auto-1" {
		t.Fatalf("req.AutomationID = %q, want auto-1", req.AutomationID)
	}
	if got := req.Metadata[meta.KeyThreadID]; got != "t-1" {
		t.Fatalf("thread metadata = %#v, want t-1", got)
	}
	if got := req.Metadata[meta.KeyChatType]; got != meta.ChatTypeGroup.String() {
		t.Fatalf("chat type metadata = %#v, want %q", got, meta.ChatTypeGroup.String())
	}

	sent := adapter.Messages()
	if len(sent) != 1 {
		t.Fatalf("len(sent) = %d, want 1", len(sent))
	}
	if sent[0].ReplyToID != "t-1" {
		t.Fatalf("sent[0].ReplyToID = %q, want thread reply target", sent[0].ReplyToID)
	}
	if got := sent[0].Metadata[meta.KeyStatusKind]; got != meta.StatusKindControlStatus.String() {
		t.Fatalf("status_kind = %#v, want %q", got, meta.StatusKindControlStatus.String())
	}
}
