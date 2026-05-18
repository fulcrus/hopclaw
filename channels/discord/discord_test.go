package discord

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fulcrus/hopclaw/channels"
)

type discordRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn discordRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestNewAdapterInitialState(t *testing.T) {
	t.Parallel()

	adapter := New(Config{BotToken: "test-token"})
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
	if caps.SendRichText {
		t.Fatal("expected SendRichText=false")
	}
	if caps.SendFile {
		t.Fatal("expected SendFile=false")
	}
}

func TestSendNotConnectedReturnsError(t *testing.T) {
	t.Parallel()

	adapter := New(Config{BotToken: "test-token"})
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

	adapter := New(Config{BotToken: "test-token"})
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

	adapter := New(Config{BotToken: "test-token"})
	ch := adapter.SubscribeEvents()
	if ch == nil {
		t.Fatal("SubscribeEvents() returned nil")
	}
}

func TestDisconnectClosesSubscribers(t *testing.T) {
	t.Parallel()

	adapter := New(Config{BotToken: "test-token"})
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

	if got := adapter.Status(); got != channels.StatusDisconnected {
		t.Fatalf("Status() = %q after disconnect, want %q", got, channels.StatusDisconnected)
	}
}

func TestDisconnectIdempotent(t *testing.T) {
	t.Parallel()

	adapter := New(Config{BotToken: "test-token"})
	if err := adapter.Disconnect(context.Background()); err != nil {
		t.Fatalf("Disconnect() on already-disconnected = %v", err)
	}
}

func TestHandleMessageCreatePublishesToSubscribers(t *testing.T) {
	t.Parallel()

	adapter := New(Config{BotToken: "test-token"})
	sub := adapter.SubscribeEvents()

	event := messageCreateEvent{
		ID:        "msg-123",
		ChannelID: "ch-456",
		GuildID:   "guild-789",
		Content:   "hello from discord",
		Author: struct {
			ID       string `json:"id"`
			Username string `json:"username"`
			Bot      bool   `json:"bot"`
		}{
			ID:       "user-1",
			Username: "testuser",
			Bot:      false,
		},
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}

	adapter.handleMessageCreate(data)

	select {
	case msg := <-sub:
		if msg.Content != "hello from discord" {
			t.Fatalf("Content = %q", msg.Content)
		}
		if msg.SenderID != "user-1" {
			t.Fatalf("SenderID = %q", msg.SenderID)
		}
		if msg.SenderName != "testuser" {
			t.Fatalf("SenderName = %q", msg.SenderName)
		}
		if msg.ChannelID != "discord" {
			t.Fatalf("ChannelID = %q", msg.ChannelID)
		}
		if msg.RawEvent["channel_id"] != "ch-456" {
			t.Fatalf("RawEvent[channel_id] = %v", msg.RawEvent["channel_id"])
		}
		if msg.RawEvent["message_id"] != "msg-123" {
			t.Fatalf("RawEvent[message_id] = %v", msg.RawEvent["message_id"])
		}
		if msg.RawEvent["guild_id"] != "guild-789" {
			t.Fatalf("RawEvent[guild_id] = %v", msg.RawEvent["guild_id"])
		}
	default:
		t.Fatal("expected message on subscriber channel")
	}
}

func TestHandleMessageCreateIgnoresBots(t *testing.T) {
	t.Parallel()

	adapter := New(Config{BotToken: "test-token"})
	sub := adapter.SubscribeEvents()

	event := messageCreateEvent{
		ID:        "msg-bot",
		ChannelID: "ch-456",
		Content:   "bot message",
		Author: struct {
			ID       string `json:"id"`
			Username string `json:"username"`
			Bot      bool   `json:"bot"`
		}{
			ID:       "bot-1",
			Username: "botuser",
			Bot:      true,
		},
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}

	adapter.handleMessageCreate(data)

	select {
	case msg := <-sub:
		t.Fatalf("expected no message for bot, got %q", msg.Content)
	default:
	}
}

func TestHandleMessageCreateIgnoresEmptyContent(t *testing.T) {
	t.Parallel()

	adapter := New(Config{BotToken: "test-token"})
	sub := adapter.SubscribeEvents()

	event := messageCreateEvent{
		ID:        "msg-empty",
		ChannelID: "ch-456",
		Content:   "   ",
		Author: struct {
			ID       string `json:"id"`
			Username string `json:"username"`
			Bot      bool   `json:"bot"`
		}{
			ID:  "user-1",
			Bot: false,
		},
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}

	adapter.handleMessageCreate(data)

	select {
	case msg := <-sub:
		t.Fatalf("expected no message for empty content, got %q", msg.Content)
	default:
	}
}

func TestSendFormatsReplyReference(t *testing.T) {
	t.Parallel()

	msg := channels.OutboundMessage{
		TargetID:  "ch-123",
		Content:   "replying",
		ReplyToID: "msg-orig",
	}

	body := map[string]any{
		"content": msg.Content,
	}
	if msg.ReplyToID != "" {
		body["message_reference"] = map[string]string{
			"message_id": msg.ReplyToID,
		}
	}

	ref, ok := body["message_reference"].(map[string]string)
	if !ok {
		t.Fatal("expected message_reference in body")
	}
	if ref["message_id"] != "msg-orig" {
		t.Fatalf("message_reference.message_id = %q", ref["message_id"])
	}
}

func TestStreamingRendererLifecycle(t *testing.T) {
	t.Parallel()

	adapter := New(Config{BotToken: "test-token"})
	if !adapter.base.MarkConnected(nil) {
		t.Fatal("MarkConnected() = false, want true")
	}

	var requests []map[string]any
	adapter.httpClient = &http.Client{Transport: discordRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := make(map[string]any)
		if req.Body != nil {
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
		}
		body["_method"] = req.Method
		body["_path"] = req.URL.Path
		requests = append(requests, body)

		recorder := httptest.NewRecorder()
		recorder.Header().Set("Content-Type", "application/json")
		switch {
		case req.Method == http.MethodPost && req.URL.Path == "/api/v10/channels/ch-1/messages":
			_ = json.NewEncoder(recorder).Encode(map[string]any{"id": "msg-1"})
		case req.Method == http.MethodPatch && req.URL.Path == "/api/v10/channels/ch-1/messages/msg-1":
			_ = json.NewEncoder(recorder).Encode(map[string]any{"id": "msg-1"})
		default:
			t.Fatalf("unexpected request %s %s", req.Method, req.URL.Path)
		}
		return recorder.Result(), nil
	})}

	handle, err := adapter.BeginStreaming(context.Background(), channels.OutboundMessage{TargetID: "ch-1"})
	if err != nil {
		t.Fatalf("BeginStreaming() error = %v", err)
	}
	if err := adapter.UpdateStreaming(context.Background(), handle, "hello"); err != nil {
		t.Fatalf("UpdateStreaming() error = %v", err)
	}
	if err := adapter.EndStreaming(context.Background(), handle, channels.OutboundMessage{
		TargetID: "ch-1",
		Content:  "done",
	}); err != nil {
		t.Fatalf("EndStreaming() error = %v", err)
	}

	if len(requests) != 3 {
		t.Fatalf("request count = %d, want 3", len(requests))
	}
	if requests[0]["_method"] != http.MethodPost || requests[0]["content"] != "⏳ Thinking..." {
		t.Fatalf("begin request = %#v", requests[0])
	}
	if requests[1]["_method"] != http.MethodPatch || requests[1]["content"] != "hello" {
		t.Fatalf("update request = %#v", requests[1])
	}
	if requests[2]["_method"] != http.MethodPatch || requests[2]["content"] != "done" {
		t.Fatalf("end request = %#v", requests[2])
	}
}
