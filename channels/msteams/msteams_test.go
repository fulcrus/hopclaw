package msteams

import (
	"context"
	"net/http"
	"testing"

	"github.com/fulcrus/hopclaw/channels"
)

func TestNewAdapterInitialState(t *testing.T) {
	t.Parallel()

	adapter := New(Config{AppID: "app-id", Password: "password"})
	if got := adapter.Status(); got != channels.StatusDisconnected {
		t.Fatalf("Status() = %q, want %q", got, channels.StatusDisconnected)
	}
}

func TestConnectMissingAppIDReturnsError(t *testing.T) {
	t.Parallel()

	adapter := New(Config{Password: "password"})
	err := adapter.Connect(context.Background())
	if err == nil {
		t.Fatal("expected error for missing app_id")
	}
}

func TestConnectMissingPasswordReturnsError(t *testing.T) {
	t.Parallel()

	adapter := New(Config{AppID: "app-id"})
	err := adapter.Connect(context.Background())
	if err == nil {
		t.Fatal("expected error for missing password")
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

	adapter := New(Config{AppID: "app-id", Password: "password"})
	err := adapter.Send(context.Background(), channels.OutboundMessage{
		TargetID: "conv-123",
		Content:  "hello",
		Metadata: map[string]any{"service_url": "https://smba.trafficmanager.net"},
	})
	if err == nil {
		t.Fatal("expected error when not connected")
	}
}

func TestSendEmptyTargetReturnsError(t *testing.T) {
	t.Parallel()

	adapter := New(Config{AppID: "app-id", Password: "password"})
	if !adapter.base.MarkConnected(nil) {
		t.Fatal("MarkConnected() = false, want true")
	}

	err := adapter.Send(context.Background(), channels.OutboundMessage{
		TargetID: "",
		Content:  "hello",
		Metadata: map[string]any{"service_url": "https://smba.trafficmanager.net"},
	})
	if err == nil {
		t.Fatal("expected error for empty target_id")
	}
}

func TestSendMissingServiceURLReturnsError(t *testing.T) {
	t.Parallel()

	adapter := New(Config{AppID: "app-id", Password: "password"})
	if !adapter.base.MarkConnected(nil) {
		t.Fatal("MarkConnected() = false, want true")
	}

	err := adapter.Send(context.Background(), channels.OutboundMessage{
		TargetID: "conv-123",
		Content:  "hello",
	})
	if err == nil {
		t.Fatal("expected error for missing service_url in metadata")
	}
}

func TestSubscribeEventsReturnsChannel(t *testing.T) {
	t.Parallel()

	adapter := New(Config{AppID: "app-id", Password: "password"})
	ch := adapter.SubscribeEvents()
	if ch == nil {
		t.Fatal("SubscribeEvents() returned nil")
	}
}

func TestDisconnectClosesSubscribers(t *testing.T) {
	t.Parallel()

	adapter := New(Config{AppID: "app-id", Password: "password"})
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

	adapter := New(Config{AppID: "app-id", Password: "password"})
	if err := adapter.Disconnect(context.Background()); err != nil {
		t.Fatalf("Disconnect() on already-disconnected = %v", err)
	}
}

func TestHandleActivityTextMessage(t *testing.T) {
	t.Parallel()

	adapter := New(Config{AppID: "app-id", Password: "password"})
	sub := adapter.SubscribeEvents()

	activity := Activity{
		Type:       "message",
		ID:         "activity-1",
		Text:       "hello from teams",
		ServiceURL: "https://smba.trafficmanager.net/teams/",
		ChannelID:  "msteams",
		From: ChannelAccount{
			ID:   "user-1",
			Name: "Test User",
		},
		Conversation: ConversationAccount{ID: "conv-123"},
	}

	adapter.HandleActivity(activity)

	select {
	case msg := <-sub:
		if msg.Content != "hello from teams" {
			t.Fatalf("Content = %q", msg.Content)
		}
		if msg.SenderID != "user-1" {
			t.Fatalf("SenderID = %q", msg.SenderID)
		}
		if msg.SenderName != "Test User" {
			t.Fatalf("SenderName = %q", msg.SenderName)
		}
		if msg.ChannelID != "msteams" {
			t.Fatalf("ChannelID = %q", msg.ChannelID)
		}
		if msg.RawEvent["activity_id"] != "activity-1" {
			t.Fatalf("RawEvent[activity_id] = %v", msg.RawEvent["activity_id"])
		}
		if msg.RawEvent["conversation_id"] != "conv-123" {
			t.Fatalf("RawEvent[conversation_id] = %v", msg.RawEvent["conversation_id"])
		}
		if msg.RawEvent["service_url"] != "https://smba.trafficmanager.net/teams/" {
			t.Fatalf("RawEvent[service_url] = %v", msg.RawEvent["service_url"])
		}
	default:
		t.Fatal("expected message on subscriber channel")
	}
}

func TestHandleActivityIgnoresNonMessage(t *testing.T) {
	t.Parallel()

	adapter := New(Config{AppID: "app-id", Password: "password"})
	sub := adapter.SubscribeEvents()

	adapter.HandleActivity(Activity{Type: "typing", Text: "should be ignored"})

	select {
	case msg := <-sub:
		t.Fatalf("expected no message for typing activity, got %q", msg.Content)
	default:
	}
}

func TestHandleActivityIgnoresEmptyText(t *testing.T) {
	t.Parallel()

	adapter := New(Config{AppID: "app-id", Password: "password"})
	sub := adapter.SubscribeEvents()

	adapter.HandleActivity(Activity{Type: "message", Text: "   "})

	select {
	case msg := <-sub:
		t.Fatalf("expected no message for empty text, got %q", msg.Content)
	default:
	}
}

func TestHandleHTTPInboundPublishesMessage(t *testing.T) {
	t.Parallel()

	adapter := New(Config{AppID: "app-id", Password: "password"})
	sub := adapter.SubscribeEvents()

	resp, err := adapter.HandleHTTPInbound(context.Background(), channels.HTTPInboundRequest{
		Method: http.MethodPost,
		Body:   []byte(`{"type":"message","id":"activity-2","text":"hello over webhook","serviceUrl":"https://smba.example","from":{"id":"user-2","name":"Webhook User"},"conversation":{"id":"conv-456"}}`),
	})
	if err != nil {
		t.Fatalf("HandleHTTPInbound() error = %v", err)
	}
	if resp != nil {
		t.Fatalf("HandleHTTPInbound() response = %#v, want nil for default 200/ok handling", resp)
	}

	select {
	case msg := <-sub:
		if msg.Content != "hello over webhook" {
			t.Fatalf("Content = %q", msg.Content)
		}
		if msg.SenderID != "user-2" {
			t.Fatalf("SenderID = %q", msg.SenderID)
		}
	default:
		t.Fatal("expected message on subscriber channel")
	}
}

func TestHandleHTTPInboundRejectsInvalidJSON(t *testing.T) {
	t.Parallel()

	adapter := New(Config{AppID: "app-id", Password: "password"})
	_, err := adapter.HandleHTTPInbound(context.Background(), channels.HTTPInboundRequest{
		Method: http.MethodPost,
		Body:   []byte(`{"type":`),
	})
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	inboundErr, ok := err.(*channels.HTTPInboundError)
	if !ok {
		t.Fatalf("error type = %T, want *channels.HTTPInboundError", err)
	}
	if inboundErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("StatusCode = %d", inboundErr.StatusCode)
	}
}
