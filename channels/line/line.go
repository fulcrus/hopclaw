// Package line implements a channels.Adapter for the LINE Bot Messaging API.
//
// Inbound messages arrive via webhook; the gateway calls HandleWebhook after
// verifying the request signature. Outbound messages are sent via the reply
// or push API endpoints.
package line

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/channels"
	"github.com/fulcrus/hopclaw/logging"
)

var log = logging.WithSubsystem("line")

var _ channels.CapabilityReporter = (*Adapter)(nil)
var _ channels.HTTPInboundAdapter = (*Adapter)(nil)

const (
	apiReply = "https://api.line.me/v2/bot/message/reply"
	apiPush  = "https://api.line.me/v2/bot/message/push"
)

// Config holds the configuration for the LINE adapter.
type Config struct {
	ChannelSecret string `json:"channel_secret" yaml:"channel_secret"`
	ChannelToken  string `json:"channel_token" yaml:"channel_token"`
}

// Adapter implements channels.Adapter for the LINE Messaging API.
type Adapter struct {
	config Config
	client *http.Client

	base channels.BaseAdapter
}

// New creates a new LINE adapter with the given configuration.
func New(cfg Config) *Adapter {
	return &Adapter{
		config: cfg,
		client: &http.Client{Timeout: 30 * time.Second},
		base:   channels.NewBaseAdapter("line"),
	}
}

// Connect marks the adapter as connected. LINE uses webhooks for inbound
// messages, so no persistent connection is needed.
func (a *Adapter) Connect(_ context.Context) error {
	if a.config.ChannelSecret == "" {
		return fmt.Errorf("line: channel_secret is required")
	}
	if a.config.ChannelToken == "" {
		return fmt.Errorf("line: channel_token is required")
	}
	if !a.base.MarkConnected(nil) {
		return nil
	}
	log.Info("line: adapter connected")
	return nil
}

// Disconnect tears down the adapter and closes all subscriber channels.
func (a *Adapter) Disconnect(_ context.Context) error {
	if _, ok := a.base.MarkDisconnected(); !ok {
		return nil
	}
	log.Info("line: adapter disconnected")
	return nil
}

// Send delivers an outbound message through the LINE Messaging API.
//
// If msg.ReplyToID is set it is used as a reply token and the reply endpoint
// is called. Otherwise, msg.TargetID is used as the destination userId and the
// push endpoint is called.
func (a *Adapter) Send(ctx context.Context, msg channels.OutboundMessage) error {
	if a.base.Status() != channels.StatusConnected {
		return fmt.Errorf("line: adapter is not connected")
	}

	var (
		endpoint string
		payload  map[string]any
	)

	// Build message list: text block(s) + image/attachment messages.
	messages := buildLINEMessages(msg)

	if strings.TrimSpace(msg.ReplyToID) != "" {
		// Use the reply API when a reply token is available.
		endpoint = apiReply
		payload = map[string]any{
			"replyToken": msg.ReplyToID,
			"messages":   messages,
		}
	} else {
		if strings.TrimSpace(msg.TargetID) == "" {
			return fmt.Errorf("line: target_id (userId) is required when no reply_to_id is set")
		}
		endpoint = apiPush
		payload = map[string]any{
			"to":       msg.TargetID,
			"messages": messages,
		}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("line: marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("line: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.config.ChannelToken)

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("line: send message: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("line: read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("line: API returned %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

func (a *Adapter) HandleHTTPInbound(_ context.Context, req channels.HTTPInboundRequest) (*channels.HTTPInboundResponse, error) {
	if req.Method != http.MethodPost {
		return nil, channels.NewHTTPInboundError(http.StatusMethodNotAllowed, "line: method %s not allowed", req.Method)
	}
	signature := strings.TrimSpace(req.Header.Get("X-Line-Signature"))
	if err := a.HandleWebhook(req.Body, signature); err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "invalid webhook signature") {
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

func (a *Adapter) CapabilityMatrix() channels.CapabilityMatrix {
	return channels.CapabilityMatrix{
		WebhookInbound: true,
		PolicyControls: true,
		Dedupe:         true,
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

// VerifySignature validates the X-Line-Signature header using HMAC-SHA256
// of the channel secret. Returns true if the signature matches.
func (a *Adapter) VerifySignature(body []byte, signature string) bool {
	mac := hmac.New(sha256.New, []byte(a.config.ChannelSecret))
	mac.Write(body)
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}

// WebhookBody represents the top-level LINE webhook request body. Its JSON
// tags intentionally preserve LINE's camelCase webhook schema.
type WebhookBody struct {
	Destination string         `json:"destination"`
	Events      []WebhookEvent `json:"events"`
}

// WebhookEvent represents a single event in a LINE webhook payload. The JSON
// tags intentionally preserve LINE's camelCase webhook schema.
type WebhookEvent struct {
	Type       string        `json:"type"`
	Timestamp  int64         `json:"timestamp"`
	Source     WebhookSource `json:"source"`
	ReplyToken string        `json:"replyToken,omitempty"`
	Message    *EventMessage `json:"message,omitempty"`
}

// WebhookSource identifies the source of a webhook event using LINE's
// original camelCase field names in JSON tags.
type WebhookSource struct {
	Type    string `json:"type"`
	UserID  string `json:"userId,omitempty"`
	GroupID string `json:"groupId,omitempty"`
	RoomID  string `json:"roomId,omitempty"`
}

// EventMessage represents the message object within a webhook event using
// LINE's original camelCase field names in JSON tags.
type EventMessage struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// HandleWebhook processes a LINE webhook request body after signature
// verification. It extracts text messages and publishes them to subscribers.
func (a *Adapter) HandleWebhook(body []byte, signature string) error {
	if !a.VerifySignature(body, signature) {
		return fmt.Errorf("line: invalid webhook signature")
	}

	var wb WebhookBody
	if err := json.Unmarshal(body, &wb); err != nil {
		return fmt.Errorf("line: unmarshal webhook body: %w", err)
	}

	for _, event := range wb.Events {
		if event.Type != "message" || event.Message == nil || event.Message.Type != "text" {
			continue
		}

		content := strings.TrimSpace(event.Message.Text)
		if content == "" {
			continue
		}

		// Determine the conversation ID (user, group, or room).
		conversationID := event.Source.UserID
		if event.Source.GroupID != "" {
			conversationID = event.Source.GroupID
		} else if event.Source.RoomID != "" {
			conversationID = event.Source.RoomID
		}

		raw := map[string]any{
			"message_id":  event.Message.ID,
			"source_id":   conversationID,
			"source_type": event.Source.Type,
			"timestamp":   event.Timestamp,
		}
		if event.ReplyToken != "" {
			raw["reply_token"] = event.ReplyToken
		}
		if event.Source.GroupID != "" {
			raw["group_id"] = event.Source.GroupID
		}
		if event.Source.RoomID != "" {
			raw["room_id"] = event.Source.RoomID
		}

		inbound := channels.InboundMessage{
			ChannelID:  "line",
			SenderID:   event.Source.UserID,
			SenderName: "", // LINE does not include display name in webhooks.
			Content:    content,
			RawEvent:   raw,
		}

		log.Info("line: received message",
			"user_id", event.Source.UserID,
			"conversation_id", conversationID,
			"content_length", len(content),
		)

		a.base.PublishInbound(inbound, func() {
			log.Warn("line: subscriber channel full, dropping message")
		})
	}
	return nil
}

// lineMaxMessages is the maximum number of messages in a single LINE API call.
const lineMaxMessages = 5

// buildLINEMessages converts an OutboundMessage into a slice of LINE message
// objects. Blocks are rendered as plain text; image attachments become image
// messages. LINE allows up to 5 messages per API call.
func buildLINEMessages(msg channels.OutboundMessage) []any {
	// Build text content from blocks or raw content.
	content := msg.Content
	if len(msg.Blocks) > 0 {
		content = channels.ContentWithBlocks(msg, channels.RenderBlocksAsPlain)
	}

	// LINE text messages have a 5000-character limit.
	const lineTextMaxLen = 5000
	if len([]rune(content)) > lineTextMaxLen {
		content = channels.TruncateUTF8(content, lineTextMaxLen)
		log.Warn("line: text message truncated", "limit", lineTextMaxLen)
	}

	var messages []any
	if strings.TrimSpace(content) != "" {
		messages = append(messages, map[string]string{
			"type": "text",
			"text": content,
		})
	}

	// Add image messages for image attachments (up to LINE's 5 message limit).
	for _, att := range msg.Attachments {
		if len(messages) >= lineMaxMessages {
			log.Warn("line: truncating attachments to message limit", "limit", lineMaxMessages)
			break
		}
		uri := strings.TrimSpace(att.URI)
		if uri == "" {
			continue
		}
		_, images := channels.AttachmentsByKind([]channels.OutboundAttachment{att})
		if len(images) > 0 {
			messages = append(messages, map[string]string{
				"type":               "image",
				"originalContentUrl": uri,
				"previewImageUrl":    uri,
			})
		}
	}

	if len(messages) == 0 {
		messages = append(messages, map[string]string{
			"type": "text",
			"text": "(empty response)",
		})
	}
	return messages
}
