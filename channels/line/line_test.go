package line

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/fulcrus/hopclaw/channels"
)

const testChannelSecret = "test-secret-key"

func computeSignature(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func TestNewAdapterInitialState(t *testing.T) {
	t.Parallel()

	adapter := New(Config{ChannelSecret: testChannelSecret, ChannelToken: "token"})
	if got := adapter.Status(); got != channels.StatusDisconnected {
		t.Fatalf("Status() = %q, want %q", got, channels.StatusDisconnected)
	}
}

func TestConnectMissingSecretReturnsError(t *testing.T) {
	t.Parallel()

	adapter := New(Config{ChannelToken: "token"})
	err := adapter.Connect(context.Background())
	if err == nil {
		t.Fatal("expected error for missing channel_secret")
	}
}

func TestConnectMissingTokenReturnsError(t *testing.T) {
	t.Parallel()

	adapter := New(Config{ChannelSecret: testChannelSecret})
	err := adapter.Connect(context.Background())
	if err == nil {
		t.Fatal("expected error for missing channel_token")
	}
}

func TestConnectSetsConnectedStatus(t *testing.T) {
	t.Parallel()

	adapter := New(Config{ChannelSecret: testChannelSecret, ChannelToken: "token"})
	if err := adapter.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if got := adapter.Status(); got != channels.StatusConnected {
		t.Fatalf("Status() = %q, want %q", got, channels.StatusConnected)
	}
}

func TestConnectIdempotent(t *testing.T) {
	t.Parallel()

	adapter := New(Config{ChannelSecret: testChannelSecret, ChannelToken: "token"})
	if err := adapter.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if err := adapter.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() second call error = %v", err)
	}
}

func TestCapabilities(t *testing.T) {
	t.Parallel()

	caps := New(Config{}).Capabilities()
	if !caps.SendText {
		t.Fatal("expected SendText=true")
	}
	if !caps.ReceiveMessage || !caps.ReceiveEvent {
		t.Fatal("expected receive capabilities to be true")
	}
	if caps.SendRichText || caps.SendFile {
		t.Fatal("expected SendRichText and SendFile to be false")
	}
}

func TestSendNotConnectedReturnsError(t *testing.T) {
	t.Parallel()

	adapter := New(Config{ChannelSecret: testChannelSecret, ChannelToken: "token"})
	err := adapter.Send(context.Background(), channels.OutboundMessage{
		TargetID: "U123",
		Content:  "hello",
	})
	if err == nil {
		t.Fatal("expected error when not connected")
	}
}

func TestSendEmptyTargetAndReplyReturnsError(t *testing.T) {
	t.Parallel()

	adapter := New(Config{ChannelSecret: testChannelSecret, ChannelToken: "token"})
	if err := adapter.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	err := adapter.Send(context.Background(), channels.OutboundMessage{
		TargetID:  "",
		ReplyToID: "",
		Content:   "hello",
	})
	if err == nil {
		t.Fatal("expected error for empty target_id when no reply_to_id")
	}
}

func TestVerifySignatureValid(t *testing.T) {
	t.Parallel()

	adapter := New(Config{ChannelSecret: testChannelSecret, ChannelToken: "token"})
	body := []byte(`{"events":[]}`)
	sig := computeSignature(body, testChannelSecret)

	if !adapter.VerifySignature(body, sig) {
		t.Fatal("expected valid signature to pass verification")
	}
}

func TestVerifySignatureInvalid(t *testing.T) {
	t.Parallel()

	adapter := New(Config{ChannelSecret: testChannelSecret, ChannelToken: "token"})
	body := []byte(`{"events":[]}`)

	if adapter.VerifySignature(body, "invalid-signature") {
		t.Fatal("expected invalid signature to fail verification")
	}
}

func TestVerifySignatureTamperedBody(t *testing.T) {
	t.Parallel()

	adapter := New(Config{ChannelSecret: testChannelSecret, ChannelToken: "token"})
	originalBody := []byte(`{"events":[]}`)
	sig := computeSignature(originalBody, testChannelSecret)
	tamperedBody := []byte(`{"events":[{"type":"message"}]}`)

	if adapter.VerifySignature(tamperedBody, sig) {
		t.Fatal("expected tampered body to fail signature verification")
	}
}

func TestHandleWebhookInvalidSignature(t *testing.T) {
	t.Parallel()

	adapter := New(Config{ChannelSecret: testChannelSecret, ChannelToken: "token"})
	body := []byte(`{"events":[]}`)

	err := adapter.HandleWebhook(body, "invalid-sig")
	if err == nil {
		t.Fatal("expected error for invalid signature")
	}
}

func TestHandleWebhookTextMessage(t *testing.T) {
	t.Parallel()

	adapter := New(Config{ChannelSecret: testChannelSecret, ChannelToken: "token"})
	sub := adapter.SubscribeEvents()

	wb := WebhookBody{
		Destination: "dest-123",
		Events: []WebhookEvent{
			{
				Type:       "message",
				Timestamp:  1709000000000,
				ReplyToken: "reply-token-abc",
				Source: WebhookSource{
					Type:   "user",
					UserID: "U123",
				},
				Message: &EventMessage{
					ID:   "msg-456",
					Type: "text",
					Text: "hello from line",
				},
			},
		},
	}

	body, err := json.Marshal(wb)
	if err != nil {
		t.Fatalf("marshal webhook body: %v", err)
	}
	sig := computeSignature(body, testChannelSecret)

	if err := adapter.HandleWebhook(body, sig); err != nil {
		t.Fatalf("HandleWebhook() error = %v", err)
	}

	select {
	case msg := <-sub:
		if msg.Content != "hello from line" {
			t.Fatalf("Content = %q", msg.Content)
		}
		if msg.SenderID != "U123" {
			t.Fatalf("SenderID = %q", msg.SenderID)
		}
		if msg.ChannelID != "line" {
			t.Fatalf("ChannelID = %q", msg.ChannelID)
		}
		if msg.RawEvent["message_id"] != "msg-456" {
			t.Fatalf("RawEvent[message_id] = %v", msg.RawEvent["message_id"])
		}
		if msg.RawEvent["reply_token"] != "reply-token-abc" {
			t.Fatalf("RawEvent[reply_token] = %v", msg.RawEvent["reply_token"])
		}
	default:
		t.Fatal("expected message on subscriber channel")
	}
}

func TestHandleWebhookGroupMessage(t *testing.T) {
	t.Parallel()

	adapter := New(Config{ChannelSecret: testChannelSecret, ChannelToken: "token"})
	sub := adapter.SubscribeEvents()

	wb := WebhookBody{
		Events: []WebhookEvent{
			{
				Type: "message",
				Source: WebhookSource{
					Type:    "group",
					UserID:  "U123",
					GroupID: "G456",
				},
				Message: &EventMessage{
					ID:   "msg-group",
					Type: "text",
					Text: "group message",
				},
			},
		},
	}

	body, err := json.Marshal(wb)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	sig := computeSignature(body, testChannelSecret)

	if err := adapter.HandleWebhook(body, sig); err != nil {
		t.Fatalf("HandleWebhook() error = %v", err)
	}

	select {
	case msg := <-sub:
		if msg.RawEvent["source_id"] != "G456" {
			t.Fatalf("RawEvent[source_id] = %v, want group ID", msg.RawEvent["source_id"])
		}
		if msg.RawEvent["group_id"] != "G456" {
			t.Fatalf("RawEvent[group_id] = %v", msg.RawEvent["group_id"])
		}
	default:
		t.Fatal("expected message on subscriber channel")
	}
}

func TestHandleWebhookIgnoresNonMessageEvents(t *testing.T) {
	t.Parallel()

	adapter := New(Config{ChannelSecret: testChannelSecret, ChannelToken: "token"})
	sub := adapter.SubscribeEvents()

	wb := WebhookBody{
		Events: []WebhookEvent{
			{
				Type: "follow",
				Source: WebhookSource{
					Type:   "user",
					UserID: "U123",
				},
			},
		},
	}

	body, err := json.Marshal(wb)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	sig := computeSignature(body, testChannelSecret)

	if err := adapter.HandleWebhook(body, sig); err != nil {
		t.Fatalf("HandleWebhook() error = %v", err)
	}

	select {
	case msg := <-sub:
		t.Fatalf("expected no message for follow event, got %q", msg.Content)
	default:
	}
}

func TestHandleHTTPInboundRejectsNonPOST(t *testing.T) {
	t.Parallel()

	adapter := New(Config{ChannelSecret: testChannelSecret, ChannelToken: "token"})
	_, err := adapter.HandleHTTPInbound(context.Background(), channels.HTTPInboundRequest{
		Method: http.MethodGet,
	})
	if err == nil {
		t.Fatal("expected error for non-POST method")
	}
	httpErr, ok := err.(*channels.HTTPInboundError)
	if !ok {
		t.Fatalf("expected HTTPInboundError, got %T", err)
	}
	if httpErr.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("StatusCode = %d, want %d", httpErr.StatusCode, http.StatusMethodNotAllowed)
	}
}

func TestHandleHTTPInboundInvalidSignatureReturns401(t *testing.T) {
	t.Parallel()

	adapter := New(Config{ChannelSecret: testChannelSecret, ChannelToken: "token"})

	body := []byte(`{"events":[]}`)
	header := http.Header{}
	header.Set("X-Line-Signature", "invalid-sig")

	_, err := adapter.HandleHTTPInbound(context.Background(), channels.HTTPInboundRequest{
		Method: http.MethodPost,
		Header: header,
		Body:   body,
	})
	if err == nil {
		t.Fatal("expected error for invalid signature")
	}
	httpErr, ok := err.(*channels.HTTPInboundError)
	if !ok {
		t.Fatalf("expected HTTPInboundError, got %T", err)
	}
	if httpErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("StatusCode = %d, want %d", httpErr.StatusCode, http.StatusUnauthorized)
	}
}

func TestDisconnectClosesSubscribers(t *testing.T) {
	t.Parallel()

	adapter := New(Config{ChannelSecret: testChannelSecret, ChannelToken: "token"})
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

func TestDisconnectIdempotent(t *testing.T) {
	t.Parallel()

	adapter := New(Config{ChannelSecret: testChannelSecret, ChannelToken: "token"})
	if err := adapter.Disconnect(context.Background()); err != nil {
		t.Fatalf("Disconnect() on already-disconnected = %v", err)
	}
}
