package matrix

import (
	"context"
	"testing"

	"github.com/fulcrus/hopclaw/channels"
)

func TestNewAdapterInitialState(t *testing.T) {
	t.Parallel()

	adapter := New(Config{
		HomeServer:  "https://matrix.example.org",
		UserID:      "@bot:matrix.example.org",
		AccessToken: "test-token",
	})
	if got := adapter.Status(); got != channels.StatusDisconnected {
		t.Fatalf("Status() = %q, want %q", got, channels.StatusDisconnected)
	}
}

func TestConnectMissingConfigReturnsError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  Config
	}{
		{
			name: "missing home_server",
			cfg:  Config{AccessToken: "token"},
		},
		{
			name: "missing access_token",
			cfg:  Config{HomeServer: "https://matrix.example.org"},
		},
		{
			name: "both missing",
			cfg:  Config{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			adapter := New(tt.cfg)
			err := adapter.Connect(context.Background())
			if err == nil {
				t.Fatal("expected error for missing config")
			}
		})
	}
}

func TestCapabilities(t *testing.T) {
	t.Parallel()

	caps := New(Config{}).Capabilities()
	if !caps.SendText {
		t.Fatal("expected SendText=true")
	}
	if !caps.SendRichText {
		t.Fatal("expected SendRichText=true")
	}
	if !caps.ReceiveMessage || !caps.ReceiveEvent {
		t.Fatal("expected receive capabilities to be true")
	}
	if caps.SendFile {
		t.Fatal("expected SendFile=false")
	}
}

func TestSendNotConnectedReturnsError(t *testing.T) {
	t.Parallel()

	adapter := New(Config{
		HomeServer:  "https://matrix.example.org",
		AccessToken: "token",
	})
	err := adapter.Send(context.Background(), channels.OutboundMessage{
		TargetID: "!room:matrix.org",
		Content:  "hello",
	})
	if err == nil {
		t.Fatal("expected error when not connected")
	}
}

func TestSendEmptyTargetReturnsError(t *testing.T) {
	t.Parallel()

	adapter := New(Config{
		HomeServer:  "https://matrix.example.org",
		AccessToken: "token",
	})
	if !adapter.base.MarkConnected(nil) {
		t.Fatal("MarkConnected() = false, want true")
	}

	err := adapter.Send(context.Background(), channels.OutboundMessage{
		TargetID: "",
		Content:  "hello",
	})
	if err == nil {
		t.Fatal("expected error for empty target_id")
	}
}

func TestSubscribeEventsReturnsChannel(t *testing.T) {
	t.Parallel()

	adapter := New(Config{
		HomeServer:  "https://matrix.example.org",
		AccessToken: "token",
	})
	ch := adapter.SubscribeEvents()
	if ch == nil {
		t.Fatal("SubscribeEvents() returned nil")
	}
}

func TestDisconnectClosesSubscribers(t *testing.T) {
	t.Parallel()

	adapter := New(Config{
		HomeServer:  "https://matrix.example.org",
		AccessToken: "token",
	})
	if !adapter.base.MarkConnected(nil) {
		t.Fatal("MarkConnected() = false, want true")
	}

	sub := adapter.SubscribeEvents()
	if err := adapter.Disconnect(context.Background()); err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}

	select {
	case _, ok := <-sub:
		if ok {
			t.Fatal("expected subscriber channel to be closed")
		}
	default:
		t.Fatal("subscriber channel should be closed after disconnect")
	}
}

func TestDisconnectIdempotent(t *testing.T) {
	t.Parallel()

	adapter := New(Config{
		HomeServer:  "https://matrix.example.org",
		AccessToken: "token",
	})
	if err := adapter.Disconnect(context.Background()); err != nil {
		t.Fatalf("Disconnect() on already-disconnected = %v", err)
	}
}

func TestHandleTimelineEventTextMessage(t *testing.T) {
	t.Parallel()

	adapter := New(Config{
		HomeServer:  "https://matrix.example.org",
		UserID:      "@bot:matrix.example.org",
		AccessToken: "token",
	})
	sub := adapter.SubscribeEvents()

	evt := syncEvent{
		Type:    "m.room.message",
		EventID: "$evt-123",
		Sender:  "@user:matrix.example.org",
		Content: map[string]any{
			"msgtype": "m.text",
			"body":    "hello from matrix",
		},
		OriginTS: 1709000000000,
	}

	adapter.handleTimelineEvent("!room:matrix.example.org", evt)

	select {
	case msg := <-sub:
		if msg.Content != "hello from matrix" {
			t.Fatalf("Content = %q", msg.Content)
		}
		if msg.SenderID != "@user:matrix.example.org" {
			t.Fatalf("SenderID = %q", msg.SenderID)
		}
		if msg.ChannelID != "matrix" {
			t.Fatalf("ChannelID = %q", msg.ChannelID)
		}
		if msg.RawEvent["room_id"] != "!room:matrix.example.org" {
			t.Fatalf("RawEvent[room_id] = %v", msg.RawEvent["room_id"])
		}
		if msg.RawEvent["event_id"] != "$evt-123" {
			t.Fatalf("RawEvent[event_id] = %v", msg.RawEvent["event_id"])
		}
		if msg.RawEvent["msgtype"] != "m.text" {
			t.Fatalf("RawEvent[msgtype] = %v", msg.RawEvent["msgtype"])
		}
	default:
		t.Fatal("expected message on subscriber channel")
	}
}

func TestHandleTimelineEventSkipsSelfMessages(t *testing.T) {
	t.Parallel()

	adapter := New(Config{
		HomeServer:  "https://matrix.example.org",
		UserID:      "@bot:matrix.example.org",
		AccessToken: "token",
	})
	sub := adapter.SubscribeEvents()

	evt := syncEvent{
		Type:    "m.room.message",
		EventID: "$evt-self",
		Sender:  "@bot:matrix.example.org",
		Content: map[string]any{
			"msgtype": "m.text",
			"body":    "bot message",
		},
	}

	adapter.handleTimelineEvent("!room:matrix.example.org", evt)

	select {
	case msg := <-sub:
		t.Fatalf("expected no message for self, got %q", msg.Content)
	default:
	}
}

func TestHandleTimelineEventSkipsNonMessageType(t *testing.T) {
	t.Parallel()

	adapter := New(Config{
		HomeServer:  "https://matrix.example.org",
		UserID:      "@bot:matrix.example.org",
		AccessToken: "token",
	})
	sub := adapter.SubscribeEvents()

	evt := syncEvent{
		Type:    "m.room.member",
		EventID: "$evt-member",
		Sender:  "@user:matrix.example.org",
		Content: map[string]any{
			"membership": "join",
		},
	}

	adapter.handleTimelineEvent("!room:matrix.example.org", evt)

	select {
	case msg := <-sub:
		t.Fatalf("expected no message for m.room.member, got %q", msg.Content)
	default:
	}
}

func TestHandleTimelineEventSkipsEmptyBody(t *testing.T) {
	t.Parallel()

	adapter := New(Config{
		HomeServer:  "https://matrix.example.org",
		UserID:      "@bot:matrix.example.org",
		AccessToken: "token",
	})
	sub := adapter.SubscribeEvents()

	evt := syncEvent{
		Type:    "m.room.message",
		EventID: "$evt-empty",
		Sender:  "@user:matrix.example.org",
		Content: map[string]any{
			"msgtype": "m.text",
			"body":    "   ",
		},
	}

	adapter.handleTimelineEvent("!room:matrix.example.org", evt)

	select {
	case msg := <-sub:
		t.Fatalf("expected no message for empty body, got %q", msg.Content)
	default:
	}
}
