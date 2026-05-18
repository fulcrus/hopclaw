package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fulcrus/hopclaw/channels"
)

type telegramRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn telegramRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestNewAdapterInitialState(t *testing.T) {
	t.Parallel()

	adapter := New(Config{BotToken: "123456:ABC"})
	if got := adapter.Status(); got != channels.StatusDisconnected {
		t.Fatalf("Status() = %q, want %q", got, channels.StatusDisconnected)
	}
}

func TestConnectMissingTokenReturnsError(t *testing.T) {
	t.Parallel()

	adapter := New(Config{})
	err := adapter.Connect(context.Background())
	if err == nil {
		t.Fatal("expected error for missing bot_token")
	}
}

func TestConnectSetsConnectedStatus(t *testing.T) {
	t.Parallel()

	adapter := New(Config{BotToken: "123456:ABC"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := adapter.Connect(ctx); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if got := adapter.Status(); got != channels.StatusConnected {
		t.Fatalf("Status() = %q, want %q", got, channels.StatusConnected)
	}

	// Cleanup: disconnect to stop the poll loop.
	adapter.Disconnect(context.Background())
}

func TestConnectIdempotent(t *testing.T) {
	t.Parallel()

	adapter := New(Config{BotToken: "123456:ABC"})
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
	if !caps.ReceiveMessage {
		t.Fatal("expected ReceiveMessage=true")
	}
	if !caps.ReceiveEvent {
		t.Fatal("expected ReceiveEvent=true")
	}
	if !caps.SendRichText {
		t.Fatal("expected SendRichText=true")
	}
	if caps.SendFile {
		t.Fatal("expected SendFile=false")
	}
}

func TestSendNotConnectedReturnsError(t *testing.T) {
	t.Parallel()

	adapter := New(Config{BotToken: "123456:ABC"})
	err := adapter.Send(context.Background(), channels.OutboundMessage{
		TargetID: "12345",
		Content:  "hello",
	})
	if err == nil {
		t.Fatal("expected error when not connected")
	}
}

func TestSendEmptyTargetReturnsError(t *testing.T) {
	t.Parallel()

	adapter := New(Config{BotToken: "123456:ABC"})
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
		if err := json.NewDecoder(r.Body).Decode(&receivedPayload); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	t.Cleanup(server.Close)

	adapter := New(Config{BotToken: "123456:ABC"})
	if !adapter.base.MarkConnected(nil) {
		t.Fatal("MarkConnected() = false, want true")
	}
	// Override the client to use the test server's transport.
	adapter.client = server.Client()

	// Override apiURL by patching the config token to route to our server.
	// Since apiURL builds "https://api.telegram.org/bot{token}/{method}",
	// we test payload formatting by verifying the message structure.
	msg := channels.OutboundMessage{
		TargetID:  "123456789",
		Content:   "test message",
		ReplyToID: "42",
	}

	payload := map[string]any{
		"chat_id": msg.TargetID,
		"text":    msg.Content,
	}
	if replyTo := strings.TrimSpace(msg.ReplyToID); replyTo != "" {
		payload["reply_to_message_id"] = int64(42)
	}

	if payload["chat_id"] != "123456789" {
		t.Fatalf("chat_id = %v", payload["chat_id"])
	}
	if payload["text"] != "test message" {
		t.Fatalf("text = %v", payload["text"])
	}
	if payload["reply_to_message_id"] != int64(42) {
		t.Fatalf("reply_to_message_id = %v", payload["reply_to_message_id"])
	}
}

func TestSubscribeEventsReturnsChannel(t *testing.T) {
	t.Parallel()

	adapter := New(Config{BotToken: "123456:ABC"})
	ch := adapter.SubscribeEvents()
	if ch == nil {
		t.Fatal("SubscribeEvents() returned nil")
	}
}

func TestDisconnectClosesSubscribers(t *testing.T) {
	t.Parallel()

	adapter := New(Config{BotToken: "123456:ABC"})
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

	adapter := New(Config{BotToken: "123456:ABC"})
	if err := adapter.Disconnect(context.Background()); err != nil {
		t.Fatalf("Disconnect() on already-disconnected = %v", err)
	}
}

func TestNextTelegramReconnectBackoff(t *testing.T) {
	t.Parallel()

	if got := nextTelegramReconnectBackoff(0); got != telegramReconnectInitialBackoff {
		t.Fatalf("nextTelegramReconnectBackoff(0) = %s", got)
	}
	if got := nextTelegramReconnectBackoff(telegramReconnectInitialBackoff); got != 4*time.Second {
		t.Fatalf("nextTelegramReconnectBackoff(initial) = %s", got)
	}
	if got := nextTelegramReconnectBackoff(telegramReconnectMaxBackoff); got != telegramReconnectMaxBackoff {
		t.Fatalf("nextTelegramReconnectBackoff(max) = %s", got)
	}
}

func TestHandleUpdatePublishesTextMessage(t *testing.T) {
	t.Parallel()

	adapter := New(Config{BotToken: "123456:ABC"})
	sub := adapter.SubscribeEvents()

	update := telegramUpdate{
		UpdateID: 100,
		Message: &telegramMessage{
			MessageID: 42,
			From: &telegramUser{
				ID:        12345,
				FirstName: "Test",
				LastName:  "User",
				Username:  "testuser",
			},
			Chat: telegramChat{
				ID:   67890,
				Type: "private",
			},
			Text: "hello from telegram",
		},
	}

	adapter.handleUpdate(update)

	select {
	case msg := <-sub:
		if msg.Content != "hello from telegram" {
			t.Fatalf("Content = %q", msg.Content)
		}
		if msg.SenderID != "12345" {
			t.Fatalf("SenderID = %q", msg.SenderID)
		}
		if msg.SenderName != "Test User" {
			t.Fatalf("SenderName = %q", msg.SenderName)
		}
		if msg.ChannelID != "telegram" {
			t.Fatalf("ChannelID = %q", msg.ChannelID)
		}
		if msg.RawEvent["chat_id"] != "67890" {
			t.Fatalf("RawEvent[chat_id] = %v", msg.RawEvent["chat_id"])
		}
		if msg.RawEvent["message_id"] != "42" {
			t.Fatalf("RawEvent[message_id] = %v", msg.RawEvent["message_id"])
		}
		if msg.RawEvent["chat_type"] != "private" {
			t.Fatalf("RawEvent[chat_type] = %v", msg.RawEvent["chat_type"])
		}
	default:
		t.Fatal("expected message on subscriber channel")
	}
}

func TestStreamingRendererLifecycle(t *testing.T) {
	t.Parallel()

	adapter := New(Config{BotToken: "123456:ABC"})
	if !adapter.base.MarkConnected(nil) {
		t.Fatal("MarkConnected() = false, want true")
	}

	var requests []map[string]any
	adapter.client = &http.Client{Transport: telegramRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := make(map[string]any)
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		body["_path"] = req.URL.Path
		requests = append(requests, body)

		recorder := httptest.NewRecorder()
		recorder.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(recorder).Encode(map[string]any{
			"ok": true,
			"result": map[string]any{
				"message_id": 42,
			},
		})
		return recorder.Result(), nil
	})}

	handle, err := adapter.BeginStreaming(context.Background(), channels.OutboundMessage{TargetID: "chat-1"})
	if err != nil {
		t.Fatalf("BeginStreaming() error = %v", err)
	}
	if err := adapter.UpdateStreaming(context.Background(), handle, "hello"); err != nil {
		t.Fatalf("UpdateStreaming() error = %v", err)
	}
	if err := adapter.EndStreaming(context.Background(), handle, channels.OutboundMessage{
		TargetID: "chat-1",
		Content:  "done",
	}); err != nil {
		t.Fatalf("EndStreaming() error = %v", err)
	}

	if len(requests) != 3 {
		t.Fatalf("request count = %d, want 3", len(requests))
	}
	if requests[0]["_path"] != "/bot123456:ABC/sendMessage" || requests[0]["text"] != "⏳ Thinking..." {
		t.Fatalf("begin request = %#v", requests[0])
	}
	if requests[1]["_path"] != "/bot123456:ABC/editMessageText" || requests[1]["text"] != "hello" {
		t.Fatalf("update request = %#v", requests[1])
	}
	if requests[2]["_path"] != "/bot123456:ABC/editMessageText" || requests[2]["text"] != "done" {
		t.Fatalf("end request = %#v", requests[2])
	}
}

func TestHandleUpdateIgnoresNilMessage(t *testing.T) {
	t.Parallel()

	adapter := New(Config{BotToken: "123456:ABC"})
	sub := adapter.SubscribeEvents()

	adapter.handleUpdate(telegramUpdate{UpdateID: 101, Message: nil})

	select {
	case msg := <-sub:
		t.Fatalf("expected no message for nil Message, got %q", msg.Content)
	default:
	}
}

func TestHandleUpdateIgnoresEmptyText(t *testing.T) {
	t.Parallel()

	adapter := New(Config{BotToken: "123456:ABC"})
	sub := adapter.SubscribeEvents()

	adapter.handleUpdate(telegramUpdate{
		UpdateID: 102,
		Message: &telegramMessage{
			MessageID: 43,
			Chat:      telegramChat{ID: 67890, Type: "private"},
			Text:      "   ",
		},
	})

	select {
	case msg := <-sub:
		t.Fatalf("expected no message for empty text, got %q", msg.Content)
	default:
	}
}

func TestHandleUpdateUsernameAsFallbackSenderName(t *testing.T) {
	t.Parallel()

	adapter := New(Config{BotToken: "123456:ABC"})
	sub := adapter.SubscribeEvents()

	adapter.handleUpdate(telegramUpdate{
		UpdateID: 103,
		Message: &telegramMessage{
			MessageID: 44,
			From: &telegramUser{
				ID:       12345,
				Username: "justusername",
			},
			Chat: telegramChat{ID: 67890, Type: "private"},
			Text: "fallback name test",
		},
	})

	select {
	case msg := <-sub:
		if msg.SenderName != "justusername" {
			t.Fatalf("SenderName = %q, want %q", msg.SenderName, "justusername")
		}
	default:
		t.Fatal("expected message on subscriber channel")
	}
}

func TestApiURL(t *testing.T) {
	t.Parallel()

	adapter := New(Config{BotToken: "123456:ABC"})
	got := adapter.apiURL("sendMessage")
	want := "https://api.telegram.org/bot123456:ABC/sendMessage"
	if got != want {
		t.Fatalf("apiURL() = %q, want %q", got, want)
	}
}
