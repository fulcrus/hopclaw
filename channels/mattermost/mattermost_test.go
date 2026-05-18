package mattermost

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/fulcrus/hopclaw/channels"
)

func TestNewAdapterInitialState(t *testing.T) {
	t.Parallel()

	adapter := New(Config{
		BaseURL:  "https://mm.example.com",
		BotToken: "bot-token",
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
			name: "missing base_url",
			cfg:  Config{BotToken: "token"},
		},
		{
			name: "missing bot_token",
			cfg:  Config{BaseURL: "https://mm.example.com"},
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

	adapter := New(Config{BaseURL: "https://mm.example.com", BotToken: "token"})
	err := adapter.Send(context.Background(), channels.OutboundMessage{
		TargetID: "ch-123",
		Content:  "hello",
	})
	if err == nil {
		t.Fatal("expected error when not connected")
	}
}

func TestSendEmptyTargetReturnsError(t *testing.T) {
	t.Parallel()

	adapter := New(Config{BaseURL: "https://mm.example.com", BotToken: "token"})
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

	adapter := New(Config{BaseURL: "https://mm.example.com", BotToken: "token"})
	ch := adapter.SubscribeEvents()
	if ch == nil {
		t.Fatal("SubscribeEvents() returned nil")
	}
}

func TestDisconnectClosesSubscribers(t *testing.T) {
	t.Parallel()

	adapter := New(Config{BaseURL: "https://mm.example.com", BotToken: "token"})
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

	adapter := New(Config{BaseURL: "https://mm.example.com", BotToken: "token"})
	if err := adapter.Disconnect(context.Background()); err != nil {
		t.Fatalf("Disconnect() on already-disconnected = %v", err)
	}
}

func TestHandlePostedPublishesToSubscribers(t *testing.T) {
	t.Parallel()

	adapter := New(Config{BaseURL: "https://mm.example.com", BotToken: "token"})
	adapter.botUserID = "bot-user-id"
	sub := adapter.SubscribeEvents()

	post := mmPost{
		ID:        "post-123",
		ChannelID: "ch-456",
		UserID:    "user-789",
		Message:   "hello from mattermost",
		RootID:    "root-000",
	}
	postJSON, err := json.Marshal(post)
	if err != nil {
		t.Fatalf("marshal post: %v", err)
	}

	data := postedData{
		Post:        string(postJSON),
		ChannelName: "general",
		ChannelType: "O",
		SenderName:  "testuser",
		TeamID:      "team-abc",
	}
	dataJSON, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal data: %v", err)
	}

	evt := wsEvent{
		Event: "posted",
		Data:  dataJSON,
		Broadcast: wsBroadcast{
			ChannelID: "ch-456",
		},
	}

	adapter.handlePosted(evt)

	select {
	case msg := <-sub:
		if msg.Content != "hello from mattermost" {
			t.Fatalf("Content = %q", msg.Content)
		}
		if msg.SenderID != "user-789" {
			t.Fatalf("SenderID = %q", msg.SenderID)
		}
		if msg.SenderName != "testuser" {
			t.Fatalf("SenderName = %q", msg.SenderName)
		}
		if msg.ChannelID != "mattermost" {
			t.Fatalf("ChannelID = %q", msg.ChannelID)
		}
		if msg.RawEvent["channel_id"] != "ch-456" {
			t.Fatalf("RawEvent[channel_id] = %v", msg.RawEvent["channel_id"])
		}
		if msg.RawEvent["post_id"] != "post-123" {
			t.Fatalf("RawEvent[post_id] = %v", msg.RawEvent["post_id"])
		}
		if msg.RawEvent["root_id"] != "root-000" {
			t.Fatalf("RawEvent[root_id] = %v", msg.RawEvent["root_id"])
		}
		if msg.RawEvent["channel_name"] != "general" {
			t.Fatalf("RawEvent[channel_name] = %v", msg.RawEvent["channel_name"])
		}
	default:
		t.Fatal("expected message on subscriber channel")
	}
}

func TestHandlePostedSkipsBotMessages(t *testing.T) {
	t.Parallel()

	adapter := New(Config{BaseURL: "https://mm.example.com", BotToken: "token"})
	adapter.botUserID = "bot-user-id"
	sub := adapter.SubscribeEvents()

	post := mmPost{
		ID:        "post-bot",
		ChannelID: "ch-456",
		UserID:    "bot-user-id",
		Message:   "bot message",
	}
	postJSON, err := json.Marshal(post)
	if err != nil {
		t.Fatalf("marshal post: %v", err)
	}

	data := postedData{Post: string(postJSON)}
	dataJSON, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal data: %v", err)
	}

	adapter.handlePosted(wsEvent{Event: "posted", Data: dataJSON})

	select {
	case msg := <-sub:
		t.Fatalf("expected no message for bot, got %q", msg.Content)
	default:
	}
}

func TestHandlePostedSkipsSystemMessages(t *testing.T) {
	t.Parallel()

	adapter := New(Config{BaseURL: "https://mm.example.com", BotToken: "token"})
	adapter.botUserID = "bot-user-id"
	sub := adapter.SubscribeEvents()

	post := mmPost{
		ID:        "post-sys",
		ChannelID: "ch-456",
		UserID:    "user-789",
		Message:   "system message",
		Type:      "system_join_channel",
	}
	postJSON, err := json.Marshal(post)
	if err != nil {
		t.Fatalf("marshal post: %v", err)
	}

	data := postedData{Post: string(postJSON)}
	dataJSON, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal data: %v", err)
	}

	adapter.handlePosted(wsEvent{Event: "posted", Data: dataJSON})

	select {
	case msg := <-sub:
		t.Fatalf("expected no message for system post, got %q", msg.Content)
	default:
	}
}

func TestHandlePostedSkipsEmptyMessage(t *testing.T) {
	t.Parallel()

	adapter := New(Config{BaseURL: "https://mm.example.com", BotToken: "token"})
	adapter.botUserID = "bot-user-id"
	sub := adapter.SubscribeEvents()

	post := mmPost{
		ID:        "post-empty",
		ChannelID: "ch-456",
		UserID:    "user-789",
		Message:   "   ",
	}
	postJSON, err := json.Marshal(post)
	if err != nil {
		t.Fatalf("marshal post: %v", err)
	}

	data := postedData{Post: string(postJSON)}
	dataJSON, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal data: %v", err)
	}

	adapter.handlePosted(wsEvent{Event: "posted", Data: dataJSON})

	select {
	case msg := <-sub:
		t.Fatalf("expected no message for empty content, got %q", msg.Content)
	default:
	}
}
