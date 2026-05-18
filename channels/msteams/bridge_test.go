package msteams

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fulcrus/hopclaw/channels"
)

func TestHandleActivityPublishesToMultipleSubscribers(t *testing.T) {
	t.Parallel()

	adapter := New(Config{AppID: "app-id", Password: "password"})
	sub1 := adapter.SubscribeEvents()
	sub2 := adapter.SubscribeEvents()

	activity := Activity{
		Type:         "message",
		ID:           "activity-multi",
		Text:         "broadcast message",
		ServiceURL:   "https://smba.trafficmanager.net/teams/",
		ChannelID:    "msteams",
		From:         ChannelAccount{ID: "user-1", Name: "Alice"},
		Conversation: ConversationAccount{ID: "conv-456"},
	}

	adapter.HandleActivity(activity)

	for i, sub := range []<-chan channels.InboundMessage{sub1, sub2} {
		select {
		case msg := <-sub:
			if msg.Content != "broadcast message" {
				t.Fatalf("sub%d: Content = %q", i+1, msg.Content)
			}
			if msg.SenderID != "user-1" {
				t.Fatalf("sub%d: SenderID = %q", i+1, msg.SenderID)
			}
		default:
			t.Fatalf("sub%d: expected message on subscriber channel", i+1)
		}
	}
}

func TestHandleActivityTrimsWhitespace(t *testing.T) {
	t.Parallel()

	adapter := New(Config{AppID: "app-id", Password: "password"})
	sub := adapter.SubscribeEvents()

	adapter.HandleActivity(Activity{
		Type:         "message",
		ID:           "activity-ws",
		Text:         "  padded text  ",
		ServiceURL:   "https://smba.trafficmanager.net/teams/",
		From:         ChannelAccount{ID: "user-ws"},
		Conversation: ConversationAccount{ID: "conv-ws"},
	})

	select {
	case msg := <-sub:
		if msg.Content != "padded text" {
			t.Fatalf("Content = %q, want %q", msg.Content, "padded text")
		}
	default:
		t.Fatal("expected message on subscriber channel")
	}
}

func TestHandleActivityPopulatesRawEventFields(t *testing.T) {
	t.Parallel()

	adapter := New(Config{AppID: "app-id", Password: "password"})
	sub := adapter.SubscribeEvents()

	adapter.HandleActivity(Activity{
		Type:         "message",
		ID:           "activity-raw",
		Text:         "raw test",
		ServiceURL:   "https://custom.service.url/api/",
		ChannelID:    "webchat",
		From:         ChannelAccount{ID: "user-raw", Name: "Bob"},
		Conversation: ConversationAccount{ID: "conv-raw"},
	})

	select {
	case msg := <-sub:
		if msg.RawEvent["activity_id"] != "activity-raw" {
			t.Fatalf("RawEvent[activity_id] = %v", msg.RawEvent["activity_id"])
		}
		if msg.RawEvent["service_url"] != "https://custom.service.url/api/" {
			t.Fatalf("RawEvent[service_url] = %v", msg.RawEvent["service_url"])
		}
		if msg.RawEvent["channel_id"] != "webchat" {
			t.Fatalf("RawEvent[channel_id] = %v", msg.RawEvent["channel_id"])
		}
		if msg.RawEvent["conversation_id"] != "conv-raw" {
			t.Fatalf("RawEvent[conversation_id] = %v", msg.RawEvent["conversation_id"])
		}
	default:
		t.Fatal("expected message on subscriber channel")
	}
}

func TestSendBuildsCorrectEndpointAndPayload(t *testing.T) {
	t.Parallel()

	var receivedPath string
	var receivedAuth string
	var receivedActivity Activity

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		receivedAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&receivedActivity); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	adapter := New(Config{AppID: "app-id", Password: "password"})
	if !adapter.base.MarkConnected(nil) {
		t.Fatal("MarkConnected() = false, want true")
	}
	adapter.tokenMu.Lock()
	adapter.accessToken = "test-token-123"
	adapter.tokenMu.Unlock()

	err := adapter.Send(context.Background(), channels.OutboundMessage{
		TargetID:  "conv-endpoint",
		ReplyToID: "reply-id-1",
		Content:   "test reply",
		Metadata:  map[string]any{"service_url": server.URL},
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	if receivedPath != "/v3/conversations/conv-endpoint/activities" {
		t.Fatalf("path = %q", receivedPath)
	}
	if receivedAuth != "Bearer test-token-123" {
		t.Fatalf("Authorization = %q", receivedAuth)
	}
	if receivedActivity.Type != "message" {
		t.Fatalf("activity.Type = %q", receivedActivity.Type)
	}
	if receivedActivity.Text != "test reply" {
		t.Fatalf("activity.Text = %q", receivedActivity.Text)
	}
	if receivedActivity.Conversation.ID != "conv-endpoint" {
		t.Fatalf("activity.Conversation.ID = %q", receivedActivity.Conversation.ID)
	}
	if receivedActivity.ReplyToID != "reply-id-1" {
		t.Fatalf("activity.ReplyToID = %q", receivedActivity.ReplyToID)
	}
}

func TestSendOmitsReplyToIDWhenEmpty(t *testing.T) {
	t.Parallel()

	var receivedActivity Activity

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&receivedActivity); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	adapter := New(Config{AppID: "app-id", Password: "password"})
	if !adapter.base.MarkConnected(nil) {
		t.Fatal("MarkConnected() = false, want true")
	}
	adapter.tokenMu.Lock()
	adapter.accessToken = "test-token"
	adapter.tokenMu.Unlock()

	err := adapter.Send(context.Background(), channels.OutboundMessage{
		TargetID: "conv-no-reply",
		Content:  "no reply",
		Metadata: map[string]any{"service_url": server.URL},
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	if receivedActivity.ReplyToID != "" {
		t.Fatalf("expected empty ReplyToID, got %q", receivedActivity.ReplyToID)
	}
}
