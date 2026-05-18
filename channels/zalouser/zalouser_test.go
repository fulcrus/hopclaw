package zalouser

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fulcrus/hopclaw/channels"
)

func TestNewAdapterInitialState(t *testing.T) {
	t.Parallel()

	adapter := New(Config{Cookie: "cookie", BaseURL: "https://zalo.example.com"})
	if got := adapter.Status(); got != channels.StatusDisconnected {
		t.Fatalf("Status() = %q, want %q", got, channels.StatusDisconnected)
	}
}

func TestConnectMissingCookieReturnsError(t *testing.T) {
	t.Parallel()

	adapter := New(Config{BaseURL: "https://zalo.example.com"})
	if err := adapter.Connect(context.Background()); err == nil {
		t.Fatal("expected error for missing cookie")
	}
}

func TestConnectMissingBaseURLReturnsError(t *testing.T) {
	t.Parallel()

	adapter := New(Config{Cookie: "cookie"})
	if err := adapter.Connect(context.Background()); err == nil {
		t.Fatal("expected error for missing base_url")
	}
}

func TestConnectSetsConnectedStatus(t *testing.T) {
	t.Parallel()

	adapter := New(Config{Cookie: "cookie", BaseURL: "https://zalo.example.com"})
	if err := adapter.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if got := adapter.Status(); got != channels.StatusConnected {
		t.Fatalf("Status() = %q, want %q", got, channels.StatusConnected)
	}
}

func TestSendFormatsPayloadCorrectly(t *testing.T) {
	t.Parallel()

	var (
		receivedCookie string
		receivedBody   map[string]any
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/message/sms" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		receivedCookie = r.Header.Get("Cookie")
		if err := json.NewDecoder(r.Body).Decode(&receivedBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	adapter := New(Config{
		Cookie:  "zpw_sek=secret",
		IMEI:    "imei-123",
		BaseURL: server.URL,
	})
	if err := adapter.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	err := adapter.Send(context.Background(), channels.OutboundMessage{
		TargetID: "user-42",
		Content:  "hello from hopclaw",
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	if receivedCookie != "zpw_sek=secret" {
		t.Fatalf("Cookie header = %q", receivedCookie)
	}
	if receivedBody["toid"] != "user-42" {
		t.Fatalf("toid = %v", receivedBody["toid"])
	}
	if receivedBody["msg"] != "hello from hopclaw" {
		t.Fatalf("msg = %v", receivedBody["msg"])
	}
	if receivedBody["imei"] != "imei-123" {
		t.Fatalf("imei = %v", receivedBody["imei"])
	}
}

func TestHandleInboundPublishesMessage(t *testing.T) {
	t.Parallel()

	adapter := New(Config{Cookie: "cookie", BaseURL: "https://zalo.example.com"})
	if !adapter.base.MarkConnected(nil) {
		t.Fatal("MarkConnected() = false, want true")
	}
	sub := adapter.SubscribeEvents()

	adapter.HandleInbound(map[string]any{
		"fromUid":         "user-42",
		"fromDisplayName": "Zalo User",
		"content":         "hello inbound",
		"msgId":           "msg-123",
	})

	select {
	case msg := <-sub:
		if msg.ChannelID != "zalouser" {
			t.Fatalf("ChannelID = %q", msg.ChannelID)
		}
		if msg.SenderID != "user-42" {
			t.Fatalf("SenderID = %q", msg.SenderID)
		}
		if msg.SenderName != "Zalo User" {
			t.Fatalf("SenderName = %q", msg.SenderName)
		}
		if msg.Content != "hello inbound" {
			t.Fatalf("Content = %q", msg.Content)
		}
		if got := msg.RawEvent["message_id"]; got != "msg-123" {
			t.Fatalf("RawEvent[message_id] = %v", got)
		}
	default:
		t.Fatal("expected inbound message to be published")
	}
}

func TestHandleHTTPInboundRejectsWrongMethod(t *testing.T) {
	t.Parallel()

	adapter := New(Config{Cookie: "cookie", BaseURL: "https://zalo.example.com"})
	_, err := adapter.HandleHTTPInbound(context.Background(), channels.HTTPInboundRequest{
		Method: http.MethodGet,
	})
	if err == nil {
		t.Fatal("expected method error")
	}
	var inboundErr *channels.HTTPInboundError
	if !errors.As(err, &inboundErr) {
		t.Fatalf("err = %v, want HTTPInboundError", err)
	}
	if inboundErr.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("StatusCode = %d, want %d", inboundErr.StatusCode, http.StatusMethodNotAllowed)
	}
}

func TestHandleHTTPInboundPublishesMessageAndReturnsOK(t *testing.T) {
	t.Parallel()

	adapter := New(Config{Cookie: "cookie", BaseURL: "https://zalo.example.com"})
	if !adapter.base.MarkConnected(nil) {
		t.Fatal("MarkConnected() = false, want true")
	}
	sub := adapter.SubscribeEvents()

	resp, err := adapter.HandleHTTPInbound(context.Background(), channels.HTTPInboundRequest{
		Method: http.MethodPost,
		Body: []byte(`{
			"fromUid":"user-42",
			"fromDisplayName":"Zalo User",
			"content":"hello webhook",
			"msgId":"msg-456"
		}`),
	})
	if err != nil {
		t.Fatalf("HandleHTTPInbound() error = %v", err)
	}
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	select {
	case msg := <-sub:
		if msg.Content != "hello webhook" {
			t.Fatalf("Content = %q", msg.Content)
		}
		if got := msg.RawEvent["msg_id"]; got != "msg-456" {
			t.Fatalf("RawEvent[msg_id] = %v", got)
		}
	default:
		t.Fatal("expected webhook message to be published")
	}
}

func TestDisconnectClosesSubscribers(t *testing.T) {
	t.Parallel()

	adapter := New(Config{Cookie: "cookie", BaseURL: "https://zalo.example.com"})
	if err := adapter.Connect(context.Background()); err != nil {
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
