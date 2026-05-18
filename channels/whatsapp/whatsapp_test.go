package whatsapp

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

	adapter := New(Config{PhoneID: "123", APIToken: "token"})
	if got := adapter.Status(); got != channels.StatusDisconnected {
		t.Fatalf("Status() = %q, want %q", got, channels.StatusDisconnected)
	}
}

func TestConnectMissingPhoneIDReturnsError(t *testing.T) {
	t.Parallel()

	adapter := New(Config{APIToken: "token"})
	err := adapter.Connect(context.Background())
	if err == nil {
		t.Fatal("expected error for missing phone_id")
	}
}

func TestConnectMissingAPITokenReturnsError(t *testing.T) {
	t.Parallel()

	adapter := New(Config{PhoneID: "123"})
	err := adapter.Connect(context.Background())
	if err == nil {
		t.Fatal("expected error for missing api_token")
	}
}

func TestConnectSetsConnectedStatus(t *testing.T) {
	t.Parallel()

	adapter := New(Config{PhoneID: "123", APIToken: "token"})
	if err := adapter.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if got := adapter.Status(); got != channels.StatusConnected {
		t.Fatalf("Status() = %q, want %q", got, channels.StatusConnected)
	}
}

func TestConnectIdempotent(t *testing.T) {
	t.Parallel()

	adapter := New(Config{PhoneID: "123", APIToken: "token"})
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

	adapter := New(Config{PhoneID: "123", APIToken: "token"})
	err := adapter.Send(context.Background(), channels.OutboundMessage{
		TargetID: "8613800138000",
		Content:  "hello",
	})
	if err == nil {
		t.Fatal("expected error when not connected")
	}
}

func TestSendEmptyTargetReturnsError(t *testing.T) {
	t.Parallel()

	adapter := New(Config{PhoneID: "123", APIToken: "token"})
	if err := adapter.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	err := adapter.Send(context.Background(), channels.OutboundMessage{
		TargetID: "",
		Content:  "hello",
	})
	if err == nil {
		t.Fatal("expected error for empty target_id")
	}
}

func TestSendFormatsCloudAPIPayload(t *testing.T) {
	t.Parallel()

	var receivedPayload map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&receivedPayload); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		// Verify authorization header.
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-token" {
			t.Errorf("Authorization = %q, want %q", auth, "Bearer test-token")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{"messages": []map[string]string{{"id": "wamid-1"}}})
	}))
	t.Cleanup(server.Close)

	adapter := New(Config{PhoneID: "123456", APIToken: "test-token", BaseURL: server.URL})
	if err := adapter.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	err := adapter.Send(context.Background(), channels.OutboundMessage{
		TargetID: "8613800138000",
		Content:  "hello from hopclaw",
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	if receivedPayload["messaging_product"] != "whatsapp" {
		t.Fatalf("messaging_product = %v", receivedPayload["messaging_product"])
	}
	if receivedPayload["to"] != "8613800138000" {
		t.Fatalf("to = %v", receivedPayload["to"])
	}
	if receivedPayload["type"] != "text" {
		t.Fatalf("type = %v", receivedPayload["type"])
	}
	textMap, ok := receivedPayload["text"].(map[string]any)
	if !ok {
		t.Fatalf("text is not a map: %T", receivedPayload["text"])
	}
	if textMap["body"] != "hello from hopclaw" {
		t.Fatalf("text.body = %v", textMap["body"])
	}
}

func TestSendIncludesReplyContext(t *testing.T) {
	t.Parallel()

	var receivedPayload map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&receivedPayload); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{"messages": []map[string]string{{"id": "wamid-1"}}})
	}))
	t.Cleanup(server.Close)

	adapter := New(Config{PhoneID: "123456", APIToken: "test-token", BaseURL: server.URL})
	if err := adapter.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	err := adapter.Send(context.Background(), channels.OutboundMessage{
		TargetID:  "8613800138000",
		ReplyToID: "wamid-original",
		Content:   "reply from hopclaw",
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	contextMap, ok := receivedPayload["context"].(map[string]any)
	if !ok {
		t.Fatalf("context is not a map: %T", receivedPayload["context"])
	}
	if contextMap["message_id"] != "wamid-original" {
		t.Fatalf("context.message_id = %v", contextMap["message_id"])
	}
}

func TestSendAPIErrorReturnsError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":{"message":"invalid phone","code":400}}`))
	}))
	t.Cleanup(server.Close)

	adapter := New(Config{PhoneID: "123456", APIToken: "test-token", BaseURL: server.URL})
	if err := adapter.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	err := adapter.Send(context.Background(), channels.OutboundMessage{
		TargetID: "invalid",
		Content:  "hello",
	})
	if err == nil {
		t.Fatal("expected error for API error response")
	}
}

func TestSubscribeEventsReturnsChannel(t *testing.T) {
	t.Parallel()

	adapter := New(Config{PhoneID: "123", APIToken: "token"})
	ch := adapter.SubscribeEvents()
	if ch == nil {
		t.Fatal("SubscribeEvents() returned nil")
	}
}

func TestDisconnectClosesSubscribers(t *testing.T) {
	t.Parallel()

	adapter := New(Config{PhoneID: "123", APIToken: "token"})
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

	adapter := New(Config{PhoneID: "123", APIToken: "token"})
	if err := adapter.Disconnect(context.Background()); err != nil {
		t.Fatalf("Disconnect() on already-disconnected = %v", err)
	}
}

func TestHandleInboundTextMessage(t *testing.T) {
	t.Parallel()

	adapter := New(Config{PhoneID: "123", APIToken: "token"})
	sub := adapter.SubscribeEvents()

	payload := WebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []WebhookEntry{
			{
				ID: "entry-1",
				Changes: []WebhookChange{
					{
						Field: "messages",
						Value: WebhookValue{
							MessagingProduct: "whatsapp",
							Metadata: WebhookMetadata{
								DisplayPhoneNumber: "15550001234",
								PhoneNumberID:      "123456",
							},
							Contacts: []WebhookContact{
								{
									WaID:    "8613800138000",
									Profile: WebhookProfile{Name: "Test User"},
								},
							},
							Messages: []WebhookMessage{
								{
									From:      "8613800138000",
									ID:        "wamid-123",
									Timestamp: "1709000000",
									Type:      "text",
									Text:      &WebhookTextBody{Body: "hello from whatsapp"},
								},
							},
						},
					},
				},
			},
		},
	}

	adapter.HandleInbound(payload)

	select {
	case msg := <-sub:
		if msg.Content != "hello from whatsapp" {
			t.Fatalf("Content = %q", msg.Content)
		}
		if msg.SenderID != "8613800138000" {
			t.Fatalf("SenderID = %q", msg.SenderID)
		}
		if msg.SenderName != "Test User" {
			t.Fatalf("SenderName = %q", msg.SenderName)
		}
		if msg.ChannelID != "whatsapp" {
			t.Fatalf("ChannelID = %q", msg.ChannelID)
		}
		if msg.RawEvent["message_id"] != "wamid-123" {
			t.Fatalf("RawEvent[message_id] = %v", msg.RawEvent["message_id"])
		}
		if msg.RawEvent["from"] != "8613800138000" {
			t.Fatalf("RawEvent[from] = %v", msg.RawEvent["from"])
		}
	default:
		t.Fatal("expected message on subscriber channel")
	}
}

func TestHandleInboundIgnoresNonTextMessages(t *testing.T) {
	t.Parallel()

	adapter := New(Config{PhoneID: "123", APIToken: "token"})
	sub := adapter.SubscribeEvents()

	payload := WebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []WebhookEntry{
			{
				Changes: []WebhookChange{
					{
						Field: "messages",
						Value: WebhookValue{
							Messages: []WebhookMessage{
								{
									From: "8613800138000",
									ID:   "wamid-img",
									Type: "image",
								},
							},
						},
					},
				},
			},
		},
	}

	adapter.HandleInbound(payload)

	select {
	case msg := <-sub:
		t.Fatalf("expected no message for image type, got %q", msg.Content)
	default:
	}
}

func TestHandleInboundIgnoresNonMessageField(t *testing.T) {
	t.Parallel()

	adapter := New(Config{PhoneID: "123", APIToken: "token"})
	sub := adapter.SubscribeEvents()

	payload := WebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []WebhookEntry{
			{
				Changes: []WebhookChange{
					{
						Field: "statuses",
						Value: WebhookValue{},
					},
				},
			},
		},
	}

	adapter.HandleInbound(payload)

	select {
	case msg := <-sub:
		t.Fatalf("expected no message for statuses field, got %q", msg.Content)
	default:
	}
}

func TestHandleInboundIncludesReplyContext(t *testing.T) {
	t.Parallel()

	adapter := New(Config{PhoneID: "123", APIToken: "token"})
	sub := adapter.SubscribeEvents()

	payload := WebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []WebhookEntry{
			{
				Changes: []WebhookChange{
					{
						Field: "messages",
						Value: WebhookValue{
							Messages: []WebhookMessage{
								{
									From: "8613800138000",
									ID:   "wamid-reply",
									Type: "text",
									Text: &WebhookTextBody{Body: "reply to something"},
									Context: &WebhookMsgContext{
										MessageID: "wamid-original",
									},
								},
							},
						},
					},
				},
			},
		},
	}

	adapter.HandleInbound(payload)

	select {
	case msg := <-sub:
		if msg.RawEvent["reply_to_message_id"] != "wamid-original" {
			t.Fatalf("RawEvent[reply_to_message_id] = %v", msg.RawEvent["reply_to_message_id"])
		}
	default:
		t.Fatal("expected message on subscriber channel")
	}
}

func TestHandleHTTPInboundRejectsNonPOST(t *testing.T) {
	t.Parallel()

	adapter := New(Config{PhoneID: "123", APIToken: "token"})
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

func TestHandleHTTPInboundRejectsInvalidJSON(t *testing.T) {
	t.Parallel()

	adapter := New(Config{PhoneID: "123", APIToken: "token"})
	_, err := adapter.HandleHTTPInbound(context.Background(), channels.HTTPInboundRequest{
		Method: http.MethodPost,
		Body:   []byte(`{invalid`),
	})
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestNewAdapterUsesDefaultBaseURL(t *testing.T) {
	t.Parallel()

	adapter := New(Config{PhoneID: "123", APIToken: "token"})
	if adapter.baseURL != defaultBaseURL {
		t.Fatalf("baseURL = %q, want %q", adapter.baseURL, defaultBaseURL)
	}
}

func TestNewAdapterUsesCustomBaseURL(t *testing.T) {
	t.Parallel()

	adapter := New(Config{PhoneID: "123", APIToken: "token", BaseURL: "https://custom.api.com/v1"})
	if adapter.baseURL != "https://custom.api.com/v1" {
		t.Fatalf("baseURL = %q, want %q", adapter.baseURL, "https://custom.api.com/v1")
	}
}
