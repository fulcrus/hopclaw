package matrix

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fulcrus/hopclaw/channels"
)

func TestHandleTimelineEventPublishesToMultipleSubscribers(t *testing.T) {
	t.Parallel()

	adapter := New(Config{
		HomeServer:  "https://matrix.example.org",
		UserID:      "@bot:matrix.example.org",
		AccessToken: "token",
	})
	sub1 := adapter.SubscribeEvents()
	sub2 := adapter.SubscribeEvents()

	evt := syncEvent{
		Type:    "m.room.message",
		EventID: "$evt-multi",
		Sender:  "@alice:matrix.example.org",
		Content: map[string]any{
			"msgtype": "m.text",
			"body":    "broadcast to all",
		},
	}

	adapter.handleTimelineEvent("!room:matrix.example.org", evt)

	for i, sub := range []<-chan channels.InboundMessage{sub1, sub2} {
		select {
		case msg := <-sub:
			if msg.Content != "broadcast to all" {
				t.Fatalf("sub%d: Content = %q", i+1, msg.Content)
			}
			if msg.SenderID != "@alice:matrix.example.org" {
				t.Fatalf("sub%d: SenderID = %q", i+1, msg.SenderID)
			}
		default:
			t.Fatalf("sub%d: expected message on subscriber channel", i+1)
		}
	}
}

func TestHandleTimelineEventExtractsMsgtype(t *testing.T) {
	t.Parallel()

	adapter := New(Config{
		HomeServer:  "https://matrix.example.org",
		UserID:      "@bot:matrix.example.org",
		AccessToken: "token",
	})
	sub := adapter.SubscribeEvents()

	evt := syncEvent{
		Type:    "m.room.message",
		EventID: "$evt-notice",
		Sender:  "@user:matrix.example.org",
		Content: map[string]any{
			"msgtype": "m.notice",
			"body":    "notice message",
		},
	}

	adapter.handleTimelineEvent("!room:matrix.example.org", evt)

	select {
	case msg := <-sub:
		if msg.RawEvent["msgtype"] != "m.notice" {
			t.Fatalf("RawEvent[msgtype] = %v", msg.RawEvent["msgtype"])
		}
	default:
		t.Fatal("expected message on subscriber channel")
	}
}

func TestHandleTimelineEventSetsRoomIDInRawEvent(t *testing.T) {
	t.Parallel()

	adapter := New(Config{
		HomeServer:  "https://matrix.example.org",
		UserID:      "@bot:matrix.example.org",
		AccessToken: "token",
	})
	sub := adapter.SubscribeEvents()

	evt := syncEvent{
		Type:    "m.room.message",
		EventID: "$evt-room",
		Sender:  "@user:matrix.example.org",
		Content: map[string]any{
			"msgtype": "m.text",
			"body":    "room test",
		},
	}

	adapter.handleTimelineEvent("!specific-room:matrix.example.org", evt)

	select {
	case msg := <-sub:
		if msg.RawEvent["room_id"] != "!specific-room:matrix.example.org" {
			t.Fatalf("RawEvent[room_id] = %v", msg.RawEvent["room_id"])
		}
	default:
		t.Fatal("expected message on subscriber channel")
	}
}

func TestSendRichTextSetsMatrixFormat(t *testing.T) {
	t.Parallel()

	var receivedBody map[string]any
	var receivedMethod string
	var receivedPath string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedMethod = r.Method
		receivedPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&receivedBody); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"event_id":"$sent-1"}`))
	}))
	t.Cleanup(server.Close)

	adapter := New(Config{
		HomeServer:  server.URL,
		UserID:      "@bot:matrix.example.org",
		AccessToken: "test-token",
	})
	if !adapter.base.MarkConnected(nil) {
		t.Fatal("MarkConnected() = false, want true")
	}

	err := adapter.Send(context.Background(), channels.OutboundMessage{
		TargetID: "!room:matrix.org",
		Content:  "<b>bold text</b>",
		Format:   "markdown",
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	if receivedMethod != http.MethodPut {
		t.Fatalf("method = %q, want PUT", receivedMethod)
	}
	if !strings.Contains(receivedPath, "!room:matrix.org") {
		t.Fatalf("path = %q, expected room ID in path", receivedPath)
	}
	if receivedBody["format"] != "org.matrix.custom.html" {
		t.Fatalf("format = %v", receivedBody["format"])
	}
	if receivedBody["formatted_body"] != "<b>bold text</b>" {
		t.Fatalf("formatted_body = %v", receivedBody["formatted_body"])
	}
}

func TestSendPlainTextOmitsFormat(t *testing.T) {
	t.Parallel()

	var receivedBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&receivedBody); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"event_id":"$sent-2"}`))
	}))
	t.Cleanup(server.Close)

	adapter := New(Config{
		HomeServer:  server.URL,
		UserID:      "@bot:matrix.example.org",
		AccessToken: "test-token",
	})
	if !adapter.base.MarkConnected(nil) {
		t.Fatal("MarkConnected() = false, want true")
	}

	err := adapter.Send(context.Background(), channels.OutboundMessage{
		TargetID: "!room:matrix.org",
		Content:  "plain text",
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	if _, hasFormat := receivedBody["format"]; hasFormat {
		t.Fatalf("expected no format field for plain text, got %v", receivedBody["format"])
	}
	if receivedBody["msgtype"] != "m.text" {
		t.Fatalf("msgtype = %v", receivedBody["msgtype"])
	}
}
