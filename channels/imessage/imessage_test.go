package imessage

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fulcrus/hopclaw/channels"
)

func TestConnectMissingBaseURLReturnsError(t *testing.T) {
	t.Parallel()

	adapter := New(Config{APIKey: "secret"})
	if err := adapter.Connect(context.Background()); err == nil {
		t.Fatal("expected error for missing base_url")
	}
}

func TestConnectMissingAPIKeyReturnsError(t *testing.T) {
	t.Parallel()

	adapter := New(Config{BaseURL: "https://imessage.example.com"})
	if err := adapter.Connect(context.Background()); err == nil {
		t.Fatal("expected error for missing api_key")
	}
}

func TestSendFormatsPayload(t *testing.T) {
	t.Parallel()

	var receivedBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/message/text" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&receivedBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	adapter := New(Config{BaseURL: server.URL, APIKey: "secret"})
	adapter.client = server.Client()
	if !adapter.base.MarkConnected(nil) {
		t.Fatal("MarkConnected() = false, want true")
	}

	err := adapter.Send(context.Background(), channels.OutboundMessage{
		TargetID: "chat-guid-123",
		Content:  "hello from hopclaw",
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	if receivedBody["chatGuid"] != "chat-guid-123" {
		t.Fatalf("chatGuid = %v", receivedBody["chatGuid"])
	}
	if receivedBody["message"] != "hello from hopclaw" {
		t.Fatalf("message = %v", receivedBody["message"])
	}
	if receivedBody["password"] != "secret" {
		t.Fatalf("password = %v", receivedBody["password"])
	}
}

func TestPollOncePublishesInboundWithChatGUID(t *testing.T) {
	t.Parallel()

	var (
		gotAfter    string
		gotPassword string
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/message" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		gotAfter = r.URL.Query().Get("after")
		gotPassword = r.URL.Query().Get("password")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(bridgePollResponse{
			Status: 200,
			Data: []bridgeMessage{
				{
					GUID:        "msg-1",
					Text:        " hello from bridge ",
					DateCreated: 12345,
					Handle: &bridgeHandle{
						Address:     "alice@example.com",
						DisplayName: "Alice",
					},
					AssociatedChat: &bridgeChat{ChatIdentifier: "chat-guid-123"},
				},
			},
		})
	}))
	t.Cleanup(server.Close)

	adapter := New(Config{BaseURL: server.URL, APIKey: "secret"})
	adapter.client = server.Client()
	adapter.lastPollTS = 100
	sub := adapter.SubscribeEvents()

	adapter.pollOnce(context.Background())

	if gotAfter != "100" {
		t.Fatalf("after = %q, want %q", gotAfter, "100")
	}
	if gotPassword != "secret" {
		t.Fatalf("password = %q, want %q", gotPassword, "secret")
	}
	if adapter.lastPollTS != 12345 {
		t.Fatalf("lastPollTS = %d, want %d", adapter.lastPollTS, 12345)
	}

	select {
	case msg := <-sub:
		if msg.ChannelID != "imessage:chat-guid-123" {
			t.Fatalf("ChannelID = %q", msg.ChannelID)
		}
		if msg.SenderID != "alice@example.com" {
			t.Fatalf("SenderID = %q", msg.SenderID)
		}
		if msg.SenderName != "Alice" {
			t.Fatalf("SenderName = %q", msg.SenderName)
		}
		if msg.Content != "hello from bridge" {
			t.Fatalf("Content = %q", msg.Content)
		}
		if got := msg.RawEvent["chat_guid"]; got != "chat-guid-123" {
			t.Fatalf("RawEvent[chat_guid] = %v", got)
		}
	default:
		t.Fatal("expected inbound message to be published")
	}
}
