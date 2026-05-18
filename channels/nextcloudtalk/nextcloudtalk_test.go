package nextcloudtalk

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

	adapter := New(Config{
		BaseURL:  "https://nextcloud.example.com",
		Username: "bot",
		Password: "secret",
	})
	if got := adapter.Status(); got != channels.StatusDisconnected {
		t.Fatalf("Status() = %q, want %q", got, channels.StatusDisconnected)
	}
}

func TestDisconnectClosesSubscribers(t *testing.T) {
	t.Parallel()

	adapter := New(Config{
		BaseURL:  "https://nextcloud.example.com",
		Username: "bot",
		Password: "secret",
	})
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

func TestHandleMessagePublishesInbound(t *testing.T) {
	t.Parallel()

	adapter := New(Config{
		BaseURL:  "https://nextcloud.example.com",
		Username: "bot",
		Password: "secret",
	})
	sub := adapter.SubscribeEvents()

	adapter.handleMessage(talkMessage{
		ID:               101,
		Token:            "room-token",
		ActorID:          "user-1",
		ActorDisplayName: "Alice",
		Message:          "hello from talk",
		Timestamp:        1700000000,
	})

	select {
	case msg := <-sub:
		if msg.ChannelID != "nextcloudtalk:room-token" {
			t.Fatalf("ChannelID = %q", msg.ChannelID)
		}
		if msg.SenderID != "user-1" {
			t.Fatalf("SenderID = %q", msg.SenderID)
		}
		if msg.SenderName != "Alice" {
			t.Fatalf("SenderName = %q", msg.SenderName)
		}
		if msg.Content != "hello from talk" {
			t.Fatalf("Content = %q", msg.Content)
		}
		if got := msg.RawEvent["id"]; got != int64(101) {
			t.Fatalf("RawEvent[id] = %v", got)
		}
	default:
		t.Fatal("expected message on subscriber channel")
	}
}

func TestConnectMissingBaseURLReturnsError(t *testing.T) {
	t.Parallel()

	adapter := New(Config{Username: "bot", Password: "secret"})
	if err := adapter.Connect(context.Background()); err == nil {
		t.Fatal("expected error for missing base_url")
	}
}

func TestConnectMissingCredentialsReturnsError(t *testing.T) {
	t.Parallel()

	adapter := New(Config{BaseURL: "https://nextcloud.example.com", Username: "bot"})
	if err := adapter.Connect(context.Background()); err == nil {
		t.Fatal("expected error for missing password")
	}
}

func TestSendFormatsPayloadAndHeaders(t *testing.T) {
	t.Parallel()

	var (
		gotUser string
		gotPass string
		gotBody map[string]any
		gotOCS  string
		gotCT   string
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ocs/v2.php/apps/spreed/api/v1/chat/room-token" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		gotUser, gotPass, _ = r.BasicAuth()
		gotOCS = r.Header.Get("OCS-APIRequest")
		gotCT = r.Header.Get("Content-Type")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	adapter := New(Config{
		BaseURL:  server.URL,
		Username: "bot",
		Password: "secret",
	})
	adapter.client = server.Client()
	if !adapter.base.MarkConnected(nil) {
		t.Fatal("MarkConnected() = false, want true")
	}

	err := adapter.Send(context.Background(), channels.OutboundMessage{
		TargetID:  "room-token",
		Content:   "hello nextcloud",
		ReplyToID: "message-1",
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	if gotUser != "bot" || gotPass != "secret" {
		t.Fatalf("basic auth = %q / %q", gotUser, gotPass)
	}
	if gotOCS != "true" {
		t.Fatalf("OCS-APIRequest = %q, want true", gotOCS)
	}
	if gotCT != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", gotCT)
	}
	if gotBody["message"] != "hello nextcloud" {
		t.Fatalf("message = %v", gotBody["message"])
	}
	if gotBody["replyTo"] != "message-1" {
		t.Fatalf("replyTo = %v", gotBody["replyTo"])
	}
}

func TestPollRoomInitialisesWithoutPublishingHistoricalMessages(t *testing.T) {
	t.Parallel()

	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"ocs": {
				"data": [
					{"id": 101, "token": "room-token", "actorType": "users", "actorId": "alice", "actorDisplayName": "Alice", "message": "old 1", "timestamp": 1700000000},
					{"id": 102, "token": "room-token", "actorType": "users", "actorId": "bob", "actorDisplayName": "Bob", "message": "old 2", "timestamp": 1700000001}
				]
			}
		}`))
	}))
	t.Cleanup(server.Close)

	adapter := New(Config{
		BaseURL:  server.URL,
		Username: "bot",
		Password: "secret",
	})
	adapter.client = server.Client()
	sub := adapter.SubscribeEvents()

	adapter.pollRoom(context.Background(), "room-token")

	if gotQuery != "lookIntoFuture=0&limit=100" {
		t.Fatalf("query = %q", gotQuery)
	}
	if adapter.lastMessageID != 102 {
		t.Fatalf("lastMessageID = %d, want %d", adapter.lastMessageID, 102)
	}
	select {
	case msg := <-sub:
		t.Fatalf("unexpected message = %#v", msg)
	default:
	}
}

func TestPollRoomPublishesOnlyNewUserMessages(t *testing.T) {
	t.Parallel()

	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"ocs": {
				"data": [
					{"id": 100, "token": "room-token", "actorType": "users", "actorId": "alice", "actorDisplayName": "Alice", "message": "already seen", "timestamp": 1700000000},
					{"id": 101, "token": "room-token", "actorType": "users", "actorId": "alice", "actorDisplayName": "Alice", "message": "new user message", "timestamp": 1700000001},
					{"id": 102, "token": "room-token", "actorType": "bots", "actorId": "bot", "actorDisplayName": "Bot", "message": "bot event", "timestamp": 1700000002},
					{"id": 103, "token": "room-token", "actorType": "users", "actorId": "system", "actorDisplayName": "System", "message": "system event", "messageType": "system", "timestamp": 1700000003},
					{"id": 104, "token": "room-token", "actorType": "users", "actorId": "bot", "actorDisplayName": "Bot", "message": "self message", "timestamp": 1700000004}
				]
			}
		}`))
	}))
	t.Cleanup(server.Close)

	adapter := New(Config{
		BaseURL:  server.URL,
		Username: "bot",
		Password: "secret",
	})
	adapter.client = server.Client()
	adapter.lastMessageID = 100
	sub := adapter.SubscribeEvents()

	adapter.pollRoom(context.Background(), "room-token")

	if gotQuery != "lookIntoFuture=0&limit=100&lastKnownMessageId=100&lookIntoFuture=1" {
		t.Fatalf("query = %q", gotQuery)
	}
	if adapter.lastMessageID != 104 {
		t.Fatalf("lastMessageID = %d, want %d", adapter.lastMessageID, 104)
	}

	select {
	case msg := <-sub:
		if msg.ChannelID != "nextcloudtalk:room-token" {
			t.Fatalf("ChannelID = %q", msg.ChannelID)
		}
		if msg.SenderID != "alice" {
			t.Fatalf("SenderID = %q", msg.SenderID)
		}
		if msg.Content != "new user message" {
			t.Fatalf("Content = %q", msg.Content)
		}
	default:
		t.Fatal("expected new user message to be published")
	}
}
