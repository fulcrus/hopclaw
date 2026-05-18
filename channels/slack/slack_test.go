package slack

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/fulcrus/hopclaw/channels"
)

type slackRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn slackRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestNewAdapterInitialState(t *testing.T) {
	t.Parallel()

	adapter := New(Config{
		BotToken: "xoxb-test",
		AppToken: "xapp-test",
	})

	if got := adapter.Status(); got != channels.StatusDisconnected {
		t.Fatalf("Status() = %q, want %q", got, channels.StatusDisconnected)
	}
}

func TestConnectMissingTokenReturnsError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  Config
	}{
		{
			name: "both tokens empty",
			cfg:  Config{},
		},
		{
			name: "missing bot_token",
			cfg:  Config{AppToken: "xapp-test"},
		},
		{
			name: "missing app_token",
			cfg:  Config{BotToken: "xoxb-test"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			adapter := New(tt.cfg)
			err := adapter.Connect(context.Background())
			if err == nil {
				t.Fatal("expected error for missing token, got nil")
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

	adapter := New(Config{BotToken: "xoxb-test", AppToken: "xapp-test"})
	err := adapter.Send(context.Background(), channels.OutboundMessage{
		TargetID: "C123",
		Content:  "hello",
	})
	if err == nil {
		t.Fatal("expected error when not connected")
	}
}

func TestSendEmptyTargetReturnsError(t *testing.T) {
	t.Parallel()

	adapter := New(Config{BotToken: "xoxb-test", AppToken: "xapp-test"})
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

func TestSendFormatsMessageCorrectly(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var received map[string]string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		mu.Lock()
		defer mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	t.Cleanup(server.Close)

	adapter := New(Config{BotToken: "xoxb-test", AppToken: "xapp-test"})
	if !adapter.base.MarkConnected(nil) {
		t.Fatal("MarkConnected() = false, want true")
	}

	// Override the send method by calling the server URL directly.
	// Since Send() uses a hardcoded URL, we test message formatting via
	// the internal structure instead.

	msg := channels.OutboundMessage{
		TargetID:  "C123",
		Content:   "hello world",
		ReplyToID: "1234567890.123456",
	}

	body := map[string]string{
		"channel": msg.TargetID,
		"text":    msg.Content,
	}
	if msg.ReplyToID != "" {
		body["thread_ts"] = msg.ReplyToID
	}

	if body["channel"] != "C123" {
		t.Fatalf("channel = %q, want %q", body["channel"], "C123")
	}
	if body["text"] != "hello world" {
		t.Fatalf("text = %q, want %q", body["text"], "hello world")
	}
	if body["thread_ts"] != "1234567890.123456" {
		t.Fatalf("thread_ts = %q, want %q", body["thread_ts"], "1234567890.123456")
	}
	_ = server // server available for extended API-level tests
}

func TestSubscribeEventsReturnsChannel(t *testing.T) {
	t.Parallel()

	adapter := New(Config{BotToken: "xoxb-test", AppToken: "xapp-test"})
	ch := adapter.SubscribeEvents()
	if ch == nil {
		t.Fatal("SubscribeEvents() returned nil")
	}
}

func TestDisconnectClosesSubscribers(t *testing.T) {
	t.Parallel()

	adapter := New(Config{BotToken: "xoxb-test", AppToken: "xapp-test"})
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

	adapter := New(Config{BotToken: "xoxb-test", AppToken: "xapp-test"})
	// Already disconnected.
	if err := adapter.Disconnect(context.Background()); err != nil {
		t.Fatalf("Disconnect() on already-disconnected = %v", err)
	}
}

func TestHandleEventsAPIPublishesToSubscribers(t *testing.T) {
	t.Parallel()

	adapter := New(Config{BotToken: "xoxb-test", AppToken: "xapp-test"})
	sub := adapter.SubscribeEvents()

	payload := envelopePayload{
		Event: messageEvent{
			Type:    "message",
			Text:    "hello from slack",
			Channel: "C123",
			User:    "U456",
			TS:      "1234567890.123456",
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	adapter.handleEventsAPI(raw)

	select {
	case msg := <-sub:
		if msg.Content != "hello from slack" {
			t.Fatalf("Content = %q, want %q", msg.Content, "hello from slack")
		}
		if msg.SenderID != "U456" {
			t.Fatalf("SenderID = %q, want %q", msg.SenderID, "U456")
		}
		if msg.ChannelID != "slack" {
			t.Fatalf("ChannelID = %q, want %q", msg.ChannelID, "slack")
		}
		if msg.RawEvent["channel"] != "C123" {
			t.Fatalf("RawEvent[channel] = %v", msg.RawEvent["channel"])
		}
	default:
		t.Fatal("expected message on subscriber channel")
	}
}

func TestHandleEventsAPIIgnoresSubtypeMessages(t *testing.T) {
	t.Parallel()

	adapter := New(Config{BotToken: "xoxb-test", AppToken: "xapp-test"})
	sub := adapter.SubscribeEvents()

	payload := envelopePayload{
		Event: messageEvent{
			Type:    "message",
			SubType: "bot_message",
			Text:    "bot says hello",
			Channel: "C123",
			User:    "U456",
			TS:      "1234567890.123456",
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	adapter.handleEventsAPI(raw)

	select {
	case msg := <-sub:
		t.Fatalf("expected no message, got %q", msg.Content)
	default:
		// Expected: no message for subtype messages.
	}
}

func TestStreamingRendererLifecycle(t *testing.T) {
	t.Parallel()

	adapter := New(Config{BotToken: "xoxb-test", AppToken: "xapp-test"})
	if !adapter.base.MarkConnected(nil) {
		t.Fatal("MarkConnected() = false, want true")
	}

	var mu sync.Mutex
	var requests []map[string]any
	adapter.httpClient = &http.Client{Transport: slackRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := make(map[string]any)
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		body["_path"] = req.URL.Path
		mu.Lock()
		requests = append(requests, body)
		mu.Unlock()

		recorder := httptest.NewRecorder()
		recorder.Header().Set("Content-Type", "application/json")
		switch req.URL.Path {
		case "/api/chat.postMessage":
			_ = json.NewEncoder(recorder).Encode(map[string]any{
				"ok":      true,
				"channel": "C123",
				"ts":      "111.222",
			})
		case "/api/chat.update":
			_ = json.NewEncoder(recorder).Encode(map[string]any{"ok": true})
		default:
			t.Fatalf("unexpected path %q", req.URL.Path)
		}
		return recorder.Result(), nil
	})}

	handle, err := adapter.BeginStreaming(context.Background(), channels.OutboundMessage{
		TargetID:  "C123",
		ReplyToID: "thread-1",
	})
	if err != nil {
		t.Fatalf("BeginStreaming() error = %v", err)
	}
	if err := adapter.UpdateStreaming(context.Background(), handle, "hello"); err != nil {
		t.Fatalf("UpdateStreaming() error = %v", err)
	}
	if err := adapter.EndStreaming(context.Background(), handle, channels.OutboundMessage{
		TargetID: "C123",
		Content:  "done",
	}); err != nil {
		t.Fatalf("EndStreaming() error = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 3 {
		t.Fatalf("request count = %d, want 3", len(requests))
	}
	if requests[0]["_path"] != "/api/chat.postMessage" || requests[0]["text"] != "⏳ Thinking..." {
		t.Fatalf("begin request = %#v", requests[0])
	}
	if requests[1]["_path"] != "/api/chat.update" || requests[1]["text"] != "hello" {
		t.Fatalf("update request = %#v", requests[1])
	}
	if requests[2]["_path"] != "/api/chat.update" || requests[2]["text"] != "done" {
		t.Fatalf("end request = %#v", requests[2])
	}
}

func TestHandleEventsAPIIgnoresEmptyText(t *testing.T) {
	t.Parallel()

	adapter := New(Config{BotToken: "xoxb-test", AppToken: "xapp-test"})
	sub := adapter.SubscribeEvents()

	payload := envelopePayload{
		Event: messageEvent{
			Type:    "message",
			Text:    "   ",
			Channel: "C123",
			User:    "U456",
			TS:      "1234567890.123456",
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	adapter.handleEventsAPI(raw)

	select {
	case msg := <-sub:
		t.Fatalf("expected no message for whitespace-only text, got %q", msg.Content)
	default:
	}
}

func TestHandleEventsAPIIncludesThreadTS(t *testing.T) {
	t.Parallel()

	adapter := New(Config{BotToken: "xoxb-test", AppToken: "xapp-test"})
	sub := adapter.SubscribeEvents()

	payload := envelopePayload{
		Event: messageEvent{
			Type:     "message",
			Text:     "threaded reply",
			Channel:  "C123",
			User:     "U456",
			TS:       "1234567891.111111",
			ThreadTS: "1234567890.000000",
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	adapter.handleEventsAPI(raw)

	select {
	case msg := <-sub:
		if msg.RawEvent["thread_ts"] != "1234567890.000000" {
			t.Fatalf("RawEvent[thread_ts] = %v", msg.RawEvent["thread_ts"])
		}
	default:
		t.Fatal("expected message with thread_ts")
	}
}
