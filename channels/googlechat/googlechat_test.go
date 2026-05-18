package googlechat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fulcrus/hopclaw/channels"
)

func TestNewAdapterInitialState(t *testing.T) {
	t.Parallel()

	adapter := New(Config{WebhookURL: "https://example.com/webhook"})
	if got := adapter.Status(); got != channels.StatusDisconnected {
		t.Fatalf("Status() = %q, want %q", got, channels.StatusDisconnected)
	}
}

func TestConnectMissingConfigReturnsError(t *testing.T) {
	t.Parallel()

	adapter := New(Config{})
	err := adapter.Connect(context.Background())
	if err == nil {
		t.Fatal("expected error when neither webhook_url nor service_account is set")
	}
}

func TestConnectWithWebhookURLSetsConnected(t *testing.T) {
	t.Parallel()

	adapter := New(Config{WebhookURL: "https://example.com/webhook"})
	if err := adapter.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if got := adapter.Status(); got != channels.StatusConnected {
		t.Fatalf("Status() = %q, want %q", got, channels.StatusConnected)
	}
}

func TestConnectIdempotent(t *testing.T) {
	t.Parallel()

	adapter := New(Config{WebhookURL: "https://example.com/webhook"})
	if err := adapter.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if err := adapter.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() second call error = %v", err)
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

	adapter := New(Config{WebhookURL: "https://example.com/webhook"})
	err := adapter.Send(context.Background(), channels.OutboundMessage{
		TargetID: "spaces/AAA",
		Content:  "hello",
	})
	if err == nil {
		t.Fatal("expected error when not connected")
	}
}

func TestSendViaWebhookFormatsPayload(t *testing.T) {
	t.Parallel()

	var receivedPayload map[string]string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&receivedPayload); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	adapter := New(Config{WebhookURL: server.URL})
	if err := adapter.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	err := adapter.Send(context.Background(), channels.OutboundMessage{
		Content: "hello from hopclaw",
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	if receivedPayload["text"] != "hello from hopclaw" {
		t.Fatalf("text = %q", receivedPayload["text"])
	}
}

func TestSendViaWebhookAPIError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	adapter := New(Config{WebhookURL: server.URL})
	if err := adapter.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	err := adapter.Send(context.Background(), channels.OutboundMessage{Content: "hello"})
	if err == nil {
		t.Fatal("expected error for webhook API failure")
	}
}

func TestSubscribeEventsReturnsChannel(t *testing.T) {
	t.Parallel()

	adapter := New(Config{WebhookURL: "https://example.com/webhook"})
	ch := adapter.SubscribeEvents()
	if ch == nil {
		t.Fatal("SubscribeEvents() returned nil")
	}
}

func TestDisconnectClosesSubscribers(t *testing.T) {
	t.Parallel()

	adapter := New(Config{WebhookURL: "https://example.com/webhook"})
	if err := adapter.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
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

	adapter := New(Config{WebhookURL: "https://example.com/webhook"})
	if err := adapter.Disconnect(context.Background()); err != nil {
		t.Fatalf("Disconnect() on already-disconnected = %v", err)
	}
}

func TestHandleEventTextMessage(t *testing.T) {
	t.Parallel()

	adapter := New(Config{WebhookURL: "https://example.com/webhook"})
	sub := adapter.SubscribeEvents()

	event := ChatEvent{
		Type:      "MESSAGE",
		EventTime: "2024-03-01T00:00:00Z",
		Message: &ChatMessage{
			Name: "spaces/AAA/messages/BBB",
			Text: "hello from google chat",
			Thread: &ChatThread{
				Name: "spaces/AAA/threads/CCC",
			},
		},
		User: &ChatUser{
			Name:        "users/123",
			DisplayName: "Test User",
			Email:       "test@example.com",
		},
		Space: &ChatSpace{
			Name: "spaces/AAA",
			Type: "ROOM",
		},
	}

	body, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}

	if err := adapter.HandleEvent(body); err != nil {
		t.Fatalf("HandleEvent() error = %v", err)
	}

	select {
	case msg := <-sub:
		if msg.Content != "hello from google chat" {
			t.Fatalf("Content = %q", msg.Content)
		}
		if msg.SenderID != "users/123" {
			t.Fatalf("SenderID = %q", msg.SenderID)
		}
		if msg.SenderName != "Test User" {
			t.Fatalf("SenderName = %q", msg.SenderName)
		}
		if msg.ChannelID != "googlechat" {
			t.Fatalf("ChannelID = %q", msg.ChannelID)
		}
		if msg.RawEvent["space_name"] != "spaces/AAA" {
			t.Fatalf("RawEvent[space_name] = %v", msg.RawEvent["space_name"])
		}
		if msg.RawEvent["thread_name"] != "spaces/AAA/threads/CCC" {
			t.Fatalf("RawEvent[thread_name] = %v", msg.RawEvent["thread_name"])
		}
	default:
		t.Fatal("expected message on subscriber channel")
	}
}

func TestHandleEventIgnoresNonMessageType(t *testing.T) {
	t.Parallel()

	adapter := New(Config{WebhookURL: "https://example.com/webhook"})
	sub := adapter.SubscribeEvents()

	event := ChatEvent{
		Type: "ADDED_TO_SPACE",
		Space: &ChatSpace{
			Name: "spaces/AAA",
		},
	}

	body, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}

	if err := adapter.HandleEvent(body); err != nil {
		t.Fatalf("HandleEvent() error = %v", err)
	}

	select {
	case msg := <-sub:
		t.Fatalf("expected no message for ADDED_TO_SPACE, got %q", msg.Content)
	default:
	}
}

func TestHandleEventVerificationTokenValid(t *testing.T) {
	t.Parallel()

	adapter := New(Config{WebhookURL: "https://example.com/webhook", VerificationKey: "secret-key"})
	sub := adapter.SubscribeEvents()

	event := ChatEvent{
		Type:  "MESSAGE",
		Token: "secret-key",
		Message: &ChatMessage{
			Name: "spaces/AAA/messages/BBB",
			Text: "verified message",
		},
	}

	body, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}

	if err := adapter.HandleEvent(body); err != nil {
		t.Fatalf("HandleEvent() error = %v", err)
	}

	select {
	case msg := <-sub:
		if msg.Content != "verified message" {
			t.Fatalf("Content = %q", msg.Content)
		}
	default:
		t.Fatal("expected message on subscriber channel")
	}
}

func TestHandleEventVerificationTokenInvalid(t *testing.T) {
	t.Parallel()

	adapter := New(Config{WebhookURL: "https://example.com/webhook", VerificationKey: "secret-key"})

	event := ChatEvent{
		Type:  "MESSAGE",
		Token: "wrong-key",
		Message: &ChatMessage{
			Text: "should fail",
		},
	}

	body, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}

	err = adapter.HandleEvent(body)
	if err == nil {
		t.Fatal("expected error for invalid verification token")
	}
}

func TestHandleHTTPInboundRejectsNonPOST(t *testing.T) {
	t.Parallel()

	adapter := New(Config{WebhookURL: "https://example.com/webhook"})
	_, err := adapter.HandleHTTPInbound(context.Background(), channels.HTTPInboundRequest{
		Method: http.MethodGet,
	})
	if err == nil {
		t.Fatal("expected error for non-POST method")
	}
	httpErr, ok := err.(*channels.HTTPInboundError)
	if !ok {
		t.Fatalf("expected HTTPInboundError, got %T", err)
	}
	if httpErr.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("StatusCode = %d, want %d", httpErr.StatusCode, http.StatusMethodNotAllowed)
	}
}
