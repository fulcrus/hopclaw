package signal

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

	adapter := New(Config{BaseURL: "http://127.0.0.1:8080", Number: "+15551234567"})
	if got := adapter.Status(); got != channels.StatusDisconnected {
		t.Fatalf("Status() = %q, want %q", got, channels.StatusDisconnected)
	}
}

func TestConnectMissingBaseURLReturnsError(t *testing.T) {
	t.Parallel()

	adapter := New(Config{Number: "+15551234567"})
	err := adapter.Connect(context.Background())
	if err == nil {
		t.Fatal("expected error for missing base_url")
	}
}

func TestConnectMissingNumberReturnsError(t *testing.T) {
	t.Parallel()

	adapter := New(Config{BaseURL: "http://127.0.0.1:8080"})
	err := adapter.Connect(context.Background())
	if err == nil {
		t.Fatal("expected error for missing number")
	}
}

func TestConnectSetsConnectedStatus(t *testing.T) {
	t.Parallel()

	adapter := New(Config{BaseURL: "http://127.0.0.1:8080", Number: "+15551234567"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := adapter.Connect(ctx); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if got := adapter.Status(); got != channels.StatusConnected {
		t.Fatalf("Status() = %q, want %q", got, channels.StatusConnected)
	}

	adapter.Disconnect(context.Background())
}

func TestConnectIdempotent(t *testing.T) {
	t.Parallel()

	adapter := New(Config{BaseURL: "http://127.0.0.1:8080", Number: "+15551234567"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := adapter.Connect(ctx); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if err := adapter.Connect(ctx); err != nil {
		t.Fatalf("Connect() second call error = %v", err)
	}

	adapter.Disconnect(context.Background())
}

func TestCapabilities(t *testing.T) {
	t.Parallel()

	caps := New(Config{}).Capabilities()
	if !caps.SendText {
		t.Fatal("expected SendText=true")
	}
	if !caps.ReceiveMessage || !caps.ReceiveEvent {
		t.Fatal("expected receive capabilities to be true")
	}
	if caps.SendRichText {
		t.Fatal("expected SendRichText=false")
	}
	if caps.SendFile {
		t.Fatal("expected SendFile=false")
	}
}

func TestSendNotConnectedReturnsError(t *testing.T) {
	t.Parallel()

	adapter := New(Config{BaseURL: "http://127.0.0.1:8080", Number: "+15551234567"})
	err := adapter.Send(context.Background(), channels.OutboundMessage{
		TargetID: "+15559876543",
		Content:  "hello",
	})
	if err == nil {
		t.Fatal("expected error when not connected")
	}
}

func TestSendEmptyTargetReturnsError(t *testing.T) {
	t.Parallel()

	adapter := New(Config{BaseURL: "http://127.0.0.1:8080", Number: "+15551234567"})
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

func TestSendFormatsPayloadCorrectly(t *testing.T) {
	t.Parallel()

	var receivedPayload map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/send" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&receivedPayload); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	adapter := New(Config{BaseURL: server.URL, Number: "+15551234567", AuthToken: "test-auth"})
	if !adapter.base.MarkConnected(nil) {
		t.Fatal("MarkConnected() = false, want true")
	}

	err := adapter.Send(context.Background(), channels.OutboundMessage{
		TargetID: "+15559876543",
		Content:  "hello from hopclaw",
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	if receivedPayload["message"] != "hello from hopclaw" {
		t.Fatalf("message = %v", receivedPayload["message"])
	}
	if receivedPayload["number"] != "+15551234567" {
		t.Fatalf("number = %v", receivedPayload["number"])
	}
	recipients, ok := receivedPayload["recipients"].([]any)
	if !ok || len(recipients) != 1 || recipients[0] != "+15559876543" {
		t.Fatalf("recipients = %v", receivedPayload["recipients"])
	}
}

func TestSendIncludesAuthToken(t *testing.T) {
	t.Parallel()

	var receivedAuth string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	adapter := New(Config{BaseURL: server.URL, Number: "+15551234567", AuthToken: "my-secret-token"})
	if !adapter.base.MarkConnected(nil) {
		t.Fatal("MarkConnected() = false, want true")
	}

	_ = adapter.Send(context.Background(), channels.OutboundMessage{
		TargetID: "+15559876543",
		Content:  "test",
	})

	if receivedAuth != "Bearer my-secret-token" {
		t.Fatalf("Authorization = %q, want %q", receivedAuth, "Bearer my-secret-token")
	}
}

func TestSubscribeEventsReturnsChannel(t *testing.T) {
	t.Parallel()

	adapter := New(Config{BaseURL: "http://127.0.0.1:8080", Number: "+15551234567"})
	ch := adapter.SubscribeEvents()
	if ch == nil {
		t.Fatal("SubscribeEvents() returned nil")
	}
}

func TestDisconnectClosesSubscribers(t *testing.T) {
	t.Parallel()

	adapter := New(Config{BaseURL: "http://127.0.0.1:8080", Number: "+15551234567"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := adapter.Connect(ctx); err != nil {
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

	adapter := New(Config{BaseURL: "http://127.0.0.1:8080", Number: "+15551234567"})
	if err := adapter.Disconnect(context.Background()); err != nil {
		t.Fatalf("Disconnect() on already-disconnected = %v", err)
	}
}

func TestHandleMessagePublishesToSubscribers(t *testing.T) {
	t.Parallel()

	adapter := New(Config{BaseURL: "http://127.0.0.1:8080", Number: "+15551234567"})
	sub := adapter.SubscribeEvents()

	msg := signalMessage{
		Envelope: signalEnvelope{
			Source:     "+15559876543",
			SourceName: "Test User",
			Timestamp:  1709000000000,
			DataMessage: &signalDataMessage{
				Message:   "hello from signal",
				Timestamp: 1709000000000,
			},
		},
	}

	adapter.handleMessage(msg)

	select {
	case inbound := <-sub:
		if inbound.Content != "hello from signal" {
			t.Fatalf("Content = %q", inbound.Content)
		}
		if inbound.SenderID != "+15559876543" {
			t.Fatalf("SenderID = %q", inbound.SenderID)
		}
		if inbound.SenderName != "Test User" {
			t.Fatalf("SenderName = %q", inbound.SenderName)
		}
		if inbound.ChannelID != "signal" {
			t.Fatalf("ChannelID = %q", inbound.ChannelID)
		}
	default:
		t.Fatal("expected message on subscriber channel")
	}
}

func TestHandleMessageGroupMessage(t *testing.T) {
	t.Parallel()

	adapter := New(Config{BaseURL: "http://127.0.0.1:8080", Number: "+15551234567"})
	sub := adapter.SubscribeEvents()

	msg := signalMessage{
		Envelope: signalEnvelope{
			Source:     "+15559876543",
			SourceName: "Test User",
			DataMessage: &signalDataMessage{
				Message: "group message",
				GroupInfo: &signalGroupInfo{
					GroupID: "group-abc-123",
				},
			},
		},
	}

	adapter.handleMessage(msg)

	select {
	case inbound := <-sub:
		if inbound.ChannelID != "signal:group-abc-123" {
			t.Fatalf("ChannelID = %q, want %q", inbound.ChannelID, "signal:group-abc-123")
		}
		if inbound.RawEvent["group_id"] != "group-abc-123" {
			t.Fatalf("RawEvent[group_id] = %v", inbound.RawEvent["group_id"])
		}
	default:
		t.Fatal("expected message on subscriber channel")
	}
}

func TestHandleMessageIgnoresNilDataMessage(t *testing.T) {
	t.Parallel()

	adapter := New(Config{BaseURL: "http://127.0.0.1:8080", Number: "+15551234567"})
	sub := adapter.SubscribeEvents()

	adapter.handleMessage(signalMessage{
		Envelope: signalEnvelope{
			Source:      "+15559876543",
			DataMessage: nil,
		},
	})

	select {
	case msg := <-sub:
		t.Fatalf("expected no message for nil DataMessage, got %q", msg.Content)
	default:
	}
}

func TestHandleMessageIgnoresEmptyText(t *testing.T) {
	t.Parallel()

	adapter := New(Config{BaseURL: "http://127.0.0.1:8080", Number: "+15551234567"})
	sub := adapter.SubscribeEvents()

	adapter.handleMessage(signalMessage{
		Envelope: signalEnvelope{
			Source: "+15559876543",
			DataMessage: &signalDataMessage{
				Message: "   ",
			},
		},
	})

	select {
	case msg := <-sub:
		t.Fatalf("expected no message for empty text, got %q", msg.Content)
	default:
	}
}
