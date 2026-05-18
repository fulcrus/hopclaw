// Package zalo implements a channels.Adapter for the Zalo Official Account
// (OA) API.
//
// Inbound messages arrive via webhook; the gateway calls HandleWebhook with
// the raw request body. Outbound messages are sent via the Zalo OA API
// customer service message endpoint.
package zalo

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/fulcrus/hopclaw/channels"
	"github.com/fulcrus/hopclaw/logging"
)

var log = logging.WithSubsystem("zalo")

var _ channels.HTTPInboundAdapter = (*Adapter)(nil)

const apiSendMessage = "https://openapi.zalo.me/v3.0/oa/message/cs"

var zaloSendRetryPolicy = channels.SendRetryPolicy{
	MaxAttempts: 3,
	BaseBackoff: 250 * time.Millisecond,
	MaxBackoff:  2 * time.Second,
}

// Config holds the configuration for the Zalo OA adapter.
type Config struct {
	AppID        string `json:"app_id" yaml:"app_id"`
	SecretKey    string `json:"secret_key" yaml:"secret_key"`
	AccessToken  string `json:"access_token" yaml:"access_token"`
	RefreshToken string `json:"refresh_token" yaml:"refresh_token"`
}

// Adapter implements channels.Adapter for the Zalo OA API.
type Adapter struct {
	config  Config
	client  *http.Client
	sendURL string

	base        channels.BaseAdapter
	tokenMu     sync.RWMutex
	accessToken string
}

// New creates a new Zalo adapter with the given configuration.
func New(cfg Config) *Adapter {
	return &Adapter{
		config:      cfg,
		client:      &http.Client{Timeout: 30 * time.Second},
		sendURL:     apiSendMessage,
		base:        channels.NewBaseAdapter("zalo"),
		accessToken: cfg.AccessToken,
	}
}

// Connect marks the adapter as connected. Zalo OA uses webhooks for inbound
// messages, so no persistent connection is established.
func (a *Adapter) Connect(_ context.Context) error {
	if a.config.AppID == "" {
		return fmt.Errorf("zalo: app_id is required")
	}
	if a.config.SecretKey == "" {
		return fmt.Errorf("zalo: secret_key is required")
	}
	a.tokenMu.RLock()
	accessToken := a.accessToken
	a.tokenMu.RUnlock()
	if accessToken == "" {
		return fmt.Errorf("zalo: access_token is required")
	}
	if !a.base.MarkConnected(nil) {
		return nil
	}

	log.Info("zalo: adapter connected", "app_id", a.config.AppID)
	return nil
}

// Disconnect tears down the adapter and closes all subscriber channels.
func (a *Adapter) Disconnect(_ context.Context) error {
	if _, ok := a.base.MarkDisconnected(); !ok {
		return nil
	}
	log.Info("zalo: adapter disconnected")
	return nil
}

// Send delivers an outbound customer service message via the Zalo OA API.
// msg.TargetID is the recipient's Zalo user ID.
func (a *Adapter) Send(ctx context.Context, msg channels.OutboundMessage) error {
	return channels.RetrySend(ctx, zaloSendRetryPolicy, func(ctx context.Context) error {
		return a.sendOnce(ctx, msg)
	})
}

func (a *Adapter) sendOnce(ctx context.Context, msg channels.OutboundMessage) error {
	if a.base.Status() != channels.StatusConnected {
		return fmt.Errorf("zalo: adapter is not connected")
	}
	a.tokenMu.RLock()
	token := a.accessToken
	a.tokenMu.RUnlock()
	if strings.TrimSpace(msg.TargetID) == "" {
		return fmt.Errorf("zalo: target_id (user_id) is required")
	}

	payload := map[string]any{
		"recipient": map[string]string{
			"user_id": msg.TargetID,
		},
		"message": map[string]any{
			"text": msg.Content,
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("zalo: marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.sendURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("zalo: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("access_token", token)

	resp, err := a.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return channels.MarkSendError(fmt.Errorf("zalo: send message: %w", err), true, 0)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return channels.MarkSendError(fmt.Errorf("zalo: read response: %w", err), true, resp.StatusCode)
	}
	if resp.StatusCode >= 400 {
		if rateLimited := channels.CheckRateLimit(resp); rateLimited != nil {
			return rateLimited
		}
		err := fmt.Errorf("zalo: API returned %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
		return channels.MarkSendError(err, zaloRetryableStatus(resp.StatusCode), resp.StatusCode)
	}

	var result struct {
		Error   int    `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return channels.MarkSendError(fmt.Errorf("zalo: decode response: %w", err), true, resp.StatusCode)
	}
	if result.Error != 0 {
		return channels.MarkSendError(fmt.Errorf("zalo: API error %d: %s", result.Error, result.Message), false, resp.StatusCode)
	}
	return nil
}

// SetAccessToken updates the access token used for API calls. This is useful
// when the token is refreshed externally.
func (a *Adapter) SetAccessToken(token string) {
	a.tokenMu.Lock()
	defer a.tokenMu.Unlock()
	a.accessToken = token
}

// Capabilities returns what this adapter supports.
func (a *Adapter) Capabilities() channels.ChannelCapabilityDescriptor {
	return channels.Capabilities{
		SendText:       true,
		SendRichText:   false,
		SendFile:       false,
		ReceiveMessage: true,
		ReceiveEvent:   true,
		Interactive:    true,
		Mobile:         true,
		InlineDelivery: true,
	}
}

// Status returns the current connection state.
func (a *Adapter) Status() channels.Status {
	return a.base.Status()
}

// SubscribeEvents returns a channel that receives inbound messages.
func (a *Adapter) SubscribeEvents() <-chan channels.InboundMessage {
	return a.base.SubscribeEvents()
}

func (a *Adapter) HandleHTTPInbound(_ context.Context, req channels.HTTPInboundRequest) (*channels.HTTPInboundResponse, error) {
	if req.Method != http.MethodPost {
		return nil, channels.NewHTTPInboundError(http.StatusMethodNotAllowed, "zalo: method %s not allowed", req.Method)
	}

	// Verify webhook secret token using timing-safe comparison.
	if a.config.SecretKey != "" {
		token := req.Header.Get("X-Bot-Api-Secret-Token")
		if subtle.ConstantTimeCompare([]byte(token), []byte(a.config.SecretKey)) != 1 {
			return nil, channels.NewHTTPInboundError(http.StatusUnauthorized, "zalo: invalid webhook secret token")
		}
	}

	if err := a.HandleWebhook(req.Body); err != nil {
		return nil, channels.NewHTTPInboundError(http.StatusBadRequest, "%v", err)
	}
	return &channels.HTTPInboundResponse{
		StatusCode: http.StatusOK,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       []byte(`{"ok":true}`),
	}, nil
}

func zaloRetryableStatus(statusCode int) bool {
	switch statusCode {
	case http.StatusRequestTimeout, http.StatusTooEarly, http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return statusCode >= 500
}

// WebhookEvent represents the top-level structure of a Zalo OA webhook event.
type WebhookEvent struct {
	AppID       string     `json:"app_id"`
	OaID        string     `json:"oa_id"`
	UserIDByApp string     `json:"user_id_by_app"`
	EventName   string     `json:"event_name"`
	Timestamp   string     `json:"timestamp"`
	Sender      *Sender    `json:"sender,omitempty"`
	Recipient   *Recipient `json:"recipient,omitempty"`
	Message     *Message   `json:"message,omitempty"`
}

// Sender identifies the sender of an inbound message.
type Sender struct {
	ID string `json:"id"`
}

// Recipient identifies the OA that received the message.
type Recipient struct {
	ID string `json:"id"`
}

// Message represents the message payload within a webhook event.
type Message struct {
	MsgID string `json:"msg_id"`
	Text  string `json:"text,omitempty"`
}

// HandleWebhook processes an inbound Zalo OA webhook event. It extracts text
// messages from "user_send_text" events and publishes them to subscribers.
func (a *Adapter) HandleWebhook(body []byte) error {
	var event WebhookEvent
	if err := json.Unmarshal(body, &event); err != nil {
		return fmt.Errorf("zalo: unmarshal webhook: %w", err)
	}

	// Only process user text message events.
	if event.EventName != "user_send_text" {
		return nil
	}
	if event.Message == nil {
		return nil
	}

	content := strings.TrimSpace(event.Message.Text)
	if content == "" {
		return nil
	}

	senderID := ""
	if event.Sender != nil {
		senderID = event.Sender.ID
	}

	raw := map[string]any{
		"event_name":     event.EventName,
		"message_id":     event.Message.MsgID,
		"msg_id":         event.Message.MsgID,
		"timestamp":      event.Timestamp,
		"oa_id":          event.OaID,
		"user_id_by_app": event.UserIDByApp,
	}
	if senderID != "" {
		raw["user_id"] = senderID
	}

	inbound := channels.InboundMessage{
		ChannelID:  "zalo",
		SenderID:   senderID,
		SenderName: "", // Zalo does not include sender name in webhook events.
		Content:    content,
		RawEvent:   raw,
	}

	log.Info("zalo: received message",
		"sender_id", senderID,
		"msg_id", event.Message.MsgID,
		"content_length", len(content),
	)

	a.base.PublishInbound(inbound, func() {
		log.Warn("zalo: subscriber channel full, dropping message")
	})
	return nil
}
