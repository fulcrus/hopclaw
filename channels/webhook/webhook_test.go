package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/fulcrus/hopclaw/channels"
)

func computeHMAC(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestNewAdapterInitialState(t *testing.T) {
	t.Parallel()

	adapter := New(Config{ID: "test-webhook"})
	if got := adapter.Status(); got != channels.StatusDisconnected {
		t.Fatalf("Status() = %q, want %q", got, channels.StatusDisconnected)
	}
}

func TestConnectMissingIDReturnsError(t *testing.T) {
	t.Parallel()

	adapter := New(Config{})
	err := adapter.Connect(context.Background())
	if err == nil {
		t.Fatal("expected error for missing id")
	}
}

func TestConnectSetsConnectedStatus(t *testing.T) {
	t.Parallel()

	adapter := New(Config{ID: "test-webhook"})
	if err := adapter.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if got := adapter.Status(); got != channels.StatusConnected {
		t.Fatalf("Status() = %q, want %q", got, channels.StatusConnected)
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
	if caps.SendRichText {
		t.Fatal("expected SendRichText=false")
	}
	if caps.SendFile {
		t.Fatal("expected SendFile=false")
	}
}

func TestSendMissingCallbackURLReturnsError(t *testing.T) {
	t.Parallel()

	adapter := New(Config{ID: "test-webhook"})
	err := adapter.Send(context.Background(), channels.OutboundMessage{
		TargetID: "user-1",
		Content:  "hello",
	})
	if err == nil {
		t.Fatal("expected error when callback_url is not configured")
	}
}

func TestSendFormatsPayloadCorrectly(t *testing.T) {
	t.Parallel()

	var receivedPayload OutboundPayload

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&receivedPayload); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	adapter := New(Config{ID: "wh-1", CallbackURL: server.URL})
	err := adapter.Send(context.Background(), channels.OutboundMessage{
		TargetID:  "user-1",
		ReplyToID: "msg-orig",
		Content:   "hello from hopclaw",
		Format:    "text",
		Metadata:  map[string]any{"custom": "value"},
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	if receivedPayload.ChannelID != "wh-1" {
		t.Fatalf("ChannelID = %q", receivedPayload.ChannelID)
	}
	if receivedPayload.TargetID != "user-1" {
		t.Fatalf("TargetID = %q", receivedPayload.TargetID)
	}
	if receivedPayload.ReplyToID != "msg-orig" {
		t.Fatalf("ReplyToID = %q", receivedPayload.ReplyToID)
	}
	if receivedPayload.Content != "hello from hopclaw" {
		t.Fatalf("Content = %q", receivedPayload.Content)
	}
	if receivedPayload.Format != "text" {
		t.Fatalf("Format = %q", receivedPayload.Format)
	}
}

func TestSendIncludesHMACSignature(t *testing.T) {
	t.Parallel()

	var receivedSignature string
	var receivedBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedSignature = r.Header.Get("X-HopClaw-Signature")
		buf := make([]byte, 4096)
		n, _ := r.Body.Read(buf)
		receivedBody = buf[:n]
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	secret := "my-webhook-secret"
	adapter := New(Config{ID: "wh-signed", CallbackURL: server.URL, Secret: secret})

	err := adapter.Send(context.Background(), channels.OutboundMessage{
		TargetID: "user-1",
		Content:  "signed message",
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	if receivedSignature == "" {
		t.Fatal("expected X-HopClaw-Signature header")
	}

	expectedSig := computeHMAC(receivedBody, secret)
	if receivedSignature != expectedSig {
		t.Fatalf("signature mismatch: got %q, want %q", receivedSignature, expectedSig)
	}
}

func TestSendCallbackErrorReturnsError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	adapter := New(Config{ID: "wh-err", CallbackURL: server.URL})
	err := adapter.Send(context.Background(), channels.OutboundMessage{
		TargetID: "user-1",
		Content:  "hello",
	})
	if err == nil {
		t.Fatal("expected error for callback failure")
	}
}

func TestSendRetriesTransientCallbackFailure(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	adapter := New(Config{ID: "wh-retry", CallbackURL: server.URL})
	adapter.httpClient = server.Client()

	if err := adapter.Send(context.Background(), channels.OutboundMessage{TargetID: "user-1", Content: "hello"}); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if got := attempts.Load(); got != 3 {
		t.Fatalf("attempts = %d, want 3", got)
	}
}

func TestSendDoesNotRetryPermanentCallbackFailure(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	t.Cleanup(server.Close)

	adapter := New(Config{ID: "wh-bad-request", CallbackURL: server.URL})
	adapter.httpClient = server.Client()

	err := adapter.Send(context.Background(), channels.OutboundMessage{TargetID: "user-1", Content: "hello"})
	if err == nil {
		t.Fatal("Send() error = nil, want permanent failure")
	}
	var sendErr *channels.SendError
	if !errors.As(err, &sendErr) || sendErr.Retryable {
		t.Fatalf("error = %#v, want permanent SendError", err)
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("attempts = %d, want 1", got)
	}
}

func TestSubscribeEventsReturnsChannel(t *testing.T) {
	t.Parallel()

	adapter := New(Config{ID: "test-webhook"})
	ch := adapter.SubscribeEvents()
	if ch == nil {
		t.Fatal("SubscribeEvents() returned nil")
	}
}

func TestDisconnectClosesSubscribers(t *testing.T) {
	t.Parallel()

	adapter := New(Config{ID: "test-webhook"})
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

func TestHandleInboundTextMessage(t *testing.T) {
	t.Parallel()

	adapter := New(Config{ID: "wh-inbound"})
	sub := adapter.SubscribeEvents()

	adapter.HandleInbound(InboundPayload{
		SenderID:   "user-1",
		SenderName: "Test User",
		Content:    "hello via webhook",
		Metadata: map[string]any{
			"custom_field": "value",
		},
	})

	select {
	case msg := <-sub:
		if msg.Content != "hello via webhook" {
			t.Fatalf("Content = %q", msg.Content)
		}
		if msg.SenderID != "user-1" {
			t.Fatalf("SenderID = %q", msg.SenderID)
		}
		if msg.SenderName != "Test User" {
			t.Fatalf("SenderName = %q", msg.SenderName)
		}
		if msg.ChannelID != "webhook:wh-inbound" {
			t.Fatalf("ChannelID = %q", msg.ChannelID)
		}
		if msg.RawEvent["webhook_id"] != "wh-inbound" {
			t.Fatalf("RawEvent[webhook_id] = %v", msg.RawEvent["webhook_id"])
		}
		if msg.RawEvent["custom_field"] != "value" {
			t.Fatalf("RawEvent[custom_field] = %v", msg.RawEvent["custom_field"])
		}
	default:
		t.Fatal("expected message on subscriber channel")
	}
}

func TestHandleInboundIgnoresEmptyContent(t *testing.T) {
	t.Parallel()

	adapter := New(Config{ID: "wh-empty"})
	sub := adapter.SubscribeEvents()

	adapter.HandleInbound(InboundPayload{
		SenderID: "user-1",
		Content:  "   ",
	})

	select {
	case msg := <-sub:
		t.Fatalf("expected no message for empty content, got %q", msg.Content)
	default:
	}
}

func TestHandleInboundNilMetadata(t *testing.T) {
	t.Parallel()

	adapter := New(Config{ID: "wh-nil-meta"})
	sub := adapter.SubscribeEvents()

	adapter.HandleInbound(InboundPayload{
		SenderID: "user-1",
		Content:  "no metadata",
	})

	select {
	case msg := <-sub:
		if msg.RawEvent == nil {
			t.Fatal("expected non-nil RawEvent even with nil metadata")
		}
		if msg.RawEvent["webhook_id"] != "wh-nil-meta" {
			t.Fatalf("RawEvent[webhook_id] = %v", msg.RawEvent["webhook_id"])
		}
	default:
		t.Fatal("expected message on subscriber channel")
	}
}

func TestVerifySignatureValidSecret(t *testing.T) {
	t.Parallel()

	secret := "test-secret"
	adapter := New(Config{ID: "wh-sig", Secret: secret})

	body := []byte(`{"content":"hello"}`)
	sig := computeHMAC(body, secret)

	if !adapter.VerifySignature(body, sig) {
		t.Fatal("expected valid signature to pass verification")
	}
}

func TestVerifySignatureInvalidSecret(t *testing.T) {
	t.Parallel()

	adapter := New(Config{ID: "wh-sig", Secret: "correct-secret"})
	body := []byte(`{"content":"hello"}`)
	wrongSig := computeHMAC(body, "wrong-secret")

	if adapter.VerifySignature(body, wrongSig) {
		t.Fatal("expected invalid signature to fail verification")
	}
}

func TestVerifySignatureNoSecretConfigured(t *testing.T) {
	t.Parallel()

	adapter := New(Config{ID: "wh-no-sig"})
	body := []byte(`{"content":"hello"}`)

	if !adapter.VerifySignature(body, "any-signature") {
		t.Fatal("expected verification to pass when no secret is configured")
	}
}

func TestVerifySignatureTamperedBody(t *testing.T) {
	t.Parallel()

	secret := "test-secret"
	adapter := New(Config{ID: "wh-tamper", Secret: secret})

	originalBody := []byte(`{"content":"hello"}`)
	sig := computeHMAC(originalBody, secret)

	tamperedBody := []byte(`{"content":"hacked"}`)
	if adapter.VerifySignature(tamperedBody, sig) {
		t.Fatal("expected tampered body to fail signature verification")
	}
}
