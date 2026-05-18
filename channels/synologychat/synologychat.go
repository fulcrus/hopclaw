// Package synologychat implements a channels.Adapter for the Synology Chat
// bot integration.
//
// Synology Chat supports incoming webhooks (for sending messages into a
// channel) and outgoing webhooks (where Synology posts messages to your
// endpoint). This adapter uses the incoming webhook for Send and accepts
// outgoing webhook payloads via HandleInbound.
package synologychat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/channels"
	"github.com/fulcrus/hopclaw/logging"
)

var log = logging.WithSubsystem("synologychat")

var _ channels.HTTPInboundAdapter = (*Adapter)(nil)

// Config holds the configuration for the Synology Chat adapter.
type Config struct {
	// BaseURL is the Synology NAS URL (e.g., "https://nas.example.com:5001").
	BaseURL string `json:"base_url" yaml:"base_url"`

	// WebhookURL is the incoming webhook URL for sending messages.
	// Typically looks like: https://nas:5001/webapi/entry.cgi?api=SYNO.Chat.External&method=incoming&version=2&token=XXXXX
	WebhookURL string `json:"webhook_url" yaml:"webhook_url"`

	// BotToken is the token used to verify outgoing webhook requests from
	// Synology Chat.
	BotToken string `json:"bot_token" yaml:"bot_token"`
}

// Adapter implements channels.Adapter for Synology Chat.
type Adapter struct {
	config Config
	client *http.Client

	base channels.BaseAdapter
}

// New creates a new Synology Chat adapter with the given configuration.
func New(cfg Config) *Adapter {
	return &Adapter{
		config: cfg,
		client: &http.Client{Timeout: 30 * time.Second},
		base:   channels.NewBaseAdapter("synologychat"),
	}
}

// Connect marks the adapter as connected. Synology Chat uses webhooks for
// both directions, so no persistent connection is established.
func (a *Adapter) Connect(_ context.Context) error {
	if a.config.WebhookURL == "" {
		return fmt.Errorf("synologychat: webhook_url is required")
	}
	if !a.base.MarkConnected(nil) {
		return nil
	}
	log.Info("synologychat: adapter connected")
	return nil
}

// Disconnect tears down the adapter and closes all subscriber channels.
func (a *Adapter) Disconnect(_ context.Context) error {
	if _, ok := a.base.MarkDisconnected(); !ok {
		return nil
	}
	log.Info("synologychat: adapter disconnected")
	return nil
}

// Send delivers an outbound message via the Synology Chat incoming webhook.
//
// The payload is sent as a form-encoded "payload_json" parameter as required
// by the Synology Chat incoming webhook API.
func (a *Adapter) Send(ctx context.Context, msg channels.OutboundMessage) error {
	if a.base.Status() != channels.StatusConnected {
		return fmt.Errorf("synologychat: adapter is not connected")
	}

	payload := map[string]any{
		"text": msg.Content,
	}

	// If a specific user is targeted, include it in the payload.
	if strings.TrimSpace(msg.TargetID) != "" {
		payload["user_ids"] = []string{msg.TargetID}
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("synologychat: marshal payload: %w", err)
	}

	form := url.Values{
		"payload_json": {string(payloadJSON)},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.config.WebhookURL, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("synologychat: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("synologychat: send message: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("synologychat: read response: %w", err)
	}

	// Synology returns {"success":true} on success.
	var result struct {
		Success bool   `json:"success"`
		Error   *Error `json:"error,omitempty"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("synologychat: decode response: %w", err)
	}
	if !result.Success {
		errMsg := "unknown error"
		if result.Error != nil {
			errMsg = fmt.Sprintf("code %d", result.Error.Code)
		}
		return fmt.Errorf("synologychat: send failed: %s", errMsg)
	}
	return nil
}

// Error represents an error response from the Synology API.
type Error struct {
	Code int `json:"code"`
}

// Capabilities returns what this adapter supports.
func (a *Adapter) Capabilities() channels.ChannelCapabilityDescriptor {
	return channels.Capabilities{
		SendText:       true,
		SendRichText:   false,
		SendFile:       false,
		ReceiveMessage: true,
		ReceiveEvent:   false,
		Interactive:    true,
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
		return nil, channels.NewHTTPInboundError(http.StatusMethodNotAllowed, "synologychat: method %s not allowed", req.Method)
	}
	var payload OutgoingWebhookPayload
	if err := json.Unmarshal(req.Body, &payload); err != nil {
		return nil, channels.NewHTTPInboundError(http.StatusBadRequest, "synologychat: invalid webhook payload: %v", err)
	}
	if err := a.HandleInbound(payload); err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "invalid webhook token") {
			status = http.StatusUnauthorized
		}
		return nil, channels.NewHTTPInboundError(status, "%v", err)
	}
	return &channels.HTTPInboundResponse{
		StatusCode: http.StatusOK,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       []byte(`{"ok":true}`),
	}, nil
}

// OutgoingWebhookPayload represents the JSON body sent by Synology Chat when
// a user message triggers an outgoing webhook.
type OutgoingWebhookPayload struct {
	Token       string `json:"token"`
	ChannelID   int64  `json:"channel_id"`
	ChannelName string `json:"channel_name,omitempty"`
	UserID      int64  `json:"user_id"`
	Username    string `json:"username"`
	PostID      string `json:"post_id,omitempty"`
	Timestamp   string `json:"timestamp,omitempty"`
	Text        string `json:"text"`
}

// HandleInbound processes an outgoing webhook payload from Synology Chat.
// It verifies the token, extracts the message content, and publishes it to
// subscribers.
func (a *Adapter) HandleInbound(payload OutgoingWebhookPayload) error {
	// Verify the token if configured.
	if a.config.BotToken != "" && payload.Token != a.config.BotToken {
		return fmt.Errorf("synologychat: invalid webhook token")
	}

	content := strings.TrimSpace(payload.Text)
	if content == "" {
		return nil
	}

	raw := map[string]any{
		"channel_id":   payload.ChannelID,
		"channel_name": payload.ChannelName,
		"user_id":      fmt.Sprintf("%d", payload.UserID),
		"message_id":   payload.PostID,
		"post_id":      payload.PostID,
		"timestamp":    payload.Timestamp,
	}

	inbound := channels.InboundMessage{
		ChannelID:  "synologychat",
		SenderID:   fmt.Sprintf("%d", payload.UserID),
		SenderName: payload.Username,
		Content:    content,
		RawEvent:   raw,
	}

	log.Info("synologychat: received message",
		"user_id", payload.UserID,
		"username", payload.Username,
		"channel_id", payload.ChannelID,
		"content_length", len(content),
	)

	a.base.PublishInbound(inbound, func() {
		log.Warn("synologychat: subscriber channel full, dropping message")
	})
	return nil
}
