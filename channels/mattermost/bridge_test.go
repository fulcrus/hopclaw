package mattermost

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fulcrus/hopclaw/channels"
)

func TestHandlePostedPopulatesAllRawEventFields(t *testing.T) {
	t.Parallel()

	adapter := New(Config{BaseURL: "https://mm.example.com", BotToken: "token"})
	adapter.botUserID = "bot-user-id"
	sub := adapter.SubscribeEvents()

	post := mmPost{
		ID:        "post-full",
		ChannelID: "ch-full",
		UserID:    "user-full",
		Message:   "full metadata test",
		RootID:    "root-full",
	}
	postJSON, err := json.Marshal(post)
	if err != nil {
		t.Fatalf("marshal post: %v", err)
	}

	data := postedData{
		Post:        string(postJSON),
		ChannelName: "town-square",
		ChannelType: "O",
		SenderName:  "fulluser",
		TeamID:      "team-full",
	}
	dataJSON, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal data: %v", err)
	}

	adapter.handlePosted(wsEvent{Event: "posted", Data: dataJSON})

	select {
	case msg := <-sub:
		if msg.RawEvent["channel_id"] != "ch-full" {
			t.Fatalf("RawEvent[channel_id] = %v", msg.RawEvent["channel_id"])
		}
		if msg.RawEvent["post_id"] != "post-full" {
			t.Fatalf("RawEvent[post_id] = %v", msg.RawEvent["post_id"])
		}
		if msg.RawEvent["root_id"] != "root-full" {
			t.Fatalf("RawEvent[root_id] = %v", msg.RawEvent["root_id"])
		}
		if msg.RawEvent["channel_name"] != "town-square" {
			t.Fatalf("RawEvent[channel_name] = %v", msg.RawEvent["channel_name"])
		}
		if msg.RawEvent["channel_type"] != "O" {
			t.Fatalf("RawEvent[channel_type] = %v", msg.RawEvent["channel_type"])
		}
		if msg.RawEvent["team_id"] != "team-full" {
			t.Fatalf("RawEvent[team_id] = %v", msg.RawEvent["team_id"])
		}
	default:
		t.Fatal("expected message on subscriber channel")
	}
}

func TestHandlePostedPublishesToMultipleSubscribers(t *testing.T) {
	t.Parallel()

	adapter := New(Config{BaseURL: "https://mm.example.com", BotToken: "token"})
	adapter.botUserID = "bot-user-id"
	sub1 := adapter.SubscribeEvents()
	sub2 := adapter.SubscribeEvents()

	post := mmPost{
		ID:        "post-multi",
		ChannelID: "ch-multi",
		UserID:    "user-multi",
		Message:   "multi sub test",
	}
	postJSON, err := json.Marshal(post)
	if err != nil {
		t.Fatalf("marshal post: %v", err)
	}

	data := postedData{Post: string(postJSON), SenderName: "multiuser"}
	dataJSON, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal data: %v", err)
	}

	adapter.handlePosted(wsEvent{Event: "posted", Data: dataJSON})

	for i, sub := range []<-chan channels.InboundMessage{sub1, sub2} {
		select {
		case msg := <-sub:
			if msg.Content != "multi sub test" {
				t.Fatalf("sub%d: Content = %q", i+1, msg.Content)
			}
		default:
			t.Fatalf("sub%d: expected message on subscriber channel", i+1)
		}
	}
}

func TestHandlePostedTrimsContentWhitespace(t *testing.T) {
	t.Parallel()

	adapter := New(Config{BaseURL: "https://mm.example.com", BotToken: "token"})
	adapter.botUserID = "bot-user-id"
	sub := adapter.SubscribeEvents()

	post := mmPost{
		ID:        "post-trim",
		ChannelID: "ch-trim",
		UserID:    "user-trim",
		Message:   "  trimmed message  ",
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
		if msg.Content != "trimmed message" {
			t.Fatalf("Content = %q, want %q", msg.Content, "trimmed message")
		}
	default:
		t.Fatal("expected message on subscriber channel")
	}
}

func TestSendIncludesRootIDWhenReplyToIDSet(t *testing.T) {
	t.Parallel()

	var receivedPayload map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&receivedPayload); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(server.Close)

	adapter := New(Config{BaseURL: server.URL, BotToken: "test-token"})
	if !adapter.base.MarkConnected(nil) {
		t.Fatal("MarkConnected() = false, want true")
	}

	err := adapter.Send(context.Background(), channels.OutboundMessage{
		TargetID:  "ch-reply",
		ReplyToID: "root-post-id",
		Content:   "threaded reply",
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	if receivedPayload["root_id"] != "root-post-id" {
		t.Fatalf("root_id = %v", receivedPayload["root_id"])
	}
	if receivedPayload["channel_id"] != "ch-reply" {
		t.Fatalf("channel_id = %v", receivedPayload["channel_id"])
	}
	if receivedPayload["message"] != "threaded reply" {
		t.Fatalf("message = %v", receivedPayload["message"])
	}
}

func TestSendOmitsRootIDWhenReplyToIDEmpty(t *testing.T) {
	t.Parallel()

	var receivedPayload map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&receivedPayload); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(server.Close)

	adapter := New(Config{BaseURL: server.URL, BotToken: "test-token"})
	if !adapter.base.MarkConnected(nil) {
		t.Fatal("MarkConnected() = false, want true")
	}

	err := adapter.Send(context.Background(), channels.OutboundMessage{
		TargetID: "ch-no-root",
		Content:  "no thread",
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	if _, hasRoot := receivedPayload["root_id"]; hasRoot {
		t.Fatalf("expected no root_id in payload, got %v", receivedPayload["root_id"])
	}
}
