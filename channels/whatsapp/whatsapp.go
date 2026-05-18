// Package whatsapp implements a channels.Adapter for the WhatsApp Cloud API.
//
// Inbound messages arrive via webhook; the gateway calls HandleInbound with
// the parsed webhook payload. Outbound messages are sent via the Cloud API.
package whatsapp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/channels"
	"github.com/fulcrus/hopclaw/logging"
)

var log = logging.WithSubsystem("whatsapp")

var _ channels.CapabilityReporter = (*Adapter)(nil)
var _ channels.HTTPInboundAdapter = (*Adapter)(nil)

const defaultBaseURL = "https://graph.facebook.com/v21.0"

// Config holds the configuration for the WhatsApp Cloud API adapter.
type Config struct {
	PhoneID  string `json:"phone_id" yaml:"phone_id"`
	APIToken string `json:"api_token" yaml:"api_token"`
	BaseURL  string `json:"base_url,omitempty" yaml:"base_url,omitempty"`
}

// Adapter implements channels.Adapter for WhatsApp Cloud API.
type Adapter struct {
	config  Config
	baseURL string
	client  *http.Client

	base channels.BaseAdapter
}

// New creates a new WhatsApp adapter with the given configuration.
func New(cfg Config) *Adapter {
	base := strings.TrimRight(cfg.BaseURL, "/")
	if base == "" {
		base = defaultBaseURL
	}
	return &Adapter{
		config:  cfg,
		baseURL: base,
		client:  &http.Client{Timeout: 30 * time.Second},
		base:    channels.NewBaseAdapter("whatsapp"),
	}
}

// Connect marks the adapter as connected. WhatsApp uses webhooks for inbound
// messages, so no persistent connection is established.
func (a *Adapter) Connect(_ context.Context) error {
	if a.config.PhoneID == "" {
		return fmt.Errorf("whatsapp: phone_id is required")
	}
	if a.config.APIToken == "" {
		return fmt.Errorf("whatsapp: api_token is required")
	}
	if !a.base.MarkConnected(nil) {
		return nil
	}
	log.Info("whatsapp: adapter connected", "phone_id", a.config.PhoneID)
	return nil
}

// Disconnect tears down the adapter and closes all subscriber channels.
func (a *Adapter) Disconnect(_ context.Context) error {
	cancel, ok := a.base.MarkDisconnected()
	if !ok {
		return nil
	}
	if cancel != nil {
		cancel()
	}
	log.Info("whatsapp: adapter disconnected")
	return nil
}

// Maximum text length per WhatsApp message.
const whatsappMaxMessageLen = 4096

// Send delivers an outbound text message via the WhatsApp Cloud API.
// msg.TargetID is the recipient phone number (with country code).
// Long messages are automatically split into chunks of up to 4096 characters.
func (a *Adapter) Send(ctx context.Context, msg channels.OutboundMessage) error {
	if a.base.Status() != channels.StatusConnected {
		return fmt.Errorf("whatsapp: adapter is not connected")
	}
	if strings.TrimSpace(msg.TargetID) == "" {
		return fmt.Errorf("whatsapp: target_id (recipient phone) is required")
	}

	// Render blocks as plain text (WhatsApp has minimal formatting).
	content := msg.Content
	if len(msg.Blocks) > 0 {
		content = channels.ContentWithBlocks(msg, channels.RenderBlocksAsPlain)
	}

	chunks := channels.ChunkText(content, whatsappMaxMessageLen)
	for _, chunk := range chunks {
		if err := a.sendChunk(ctx, msg.TargetID, chunk, msg.ReplyToID); err != nil {
			return err
		}
	}

	// Send attachments as native WhatsApp document/image messages.
	for _, att := range msg.Attachments {
		uri := strings.TrimSpace(att.URI)
		if uri == "" {
			continue
		}
		if err := a.sendMedia(ctx, msg.TargetID, msg.ReplyToID, att); err != nil {
			log.Warn("whatsapp: send media failed", "uri", uri, "error", err)
		}
	}
	return nil
}

// sendMedia sends a document or image message via the WhatsApp Cloud API.
func (a *Adapter) sendMedia(ctx context.Context, to, replyToID string, att channels.OutboundAttachment) error {
	uri := strings.TrimSpace(att.URI)
	_, images := channels.AttachmentsByKind([]channels.OutboundAttachment{att})
	isImage := len(images) > 0

	mediaType := "document"
	if isImage {
		mediaType = "image"
	}

	mediaObj := map[string]string{"link": uri}
	label := strings.TrimSpace(att.Label)
	if label != "" {
		if isImage {
			mediaObj["caption"] = label
		} else {
			mediaObj["caption"] = label
			mediaObj["filename"] = label
		}
	}

	payload := map[string]any{
		"messaging_product": "whatsapp",
		"to":                to,
		"type":              mediaType,
		mediaType:           mediaObj,
	}
	if replyTo := strings.TrimSpace(replyToID); replyTo != "" {
		payload["context"] = map[string]string{"message_id": replyTo}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("whatsapp: marshal %s payload: %w", mediaType, err)
	}

	url := fmt.Sprintf("%s/%s/messages", a.baseURL, a.config.PhoneID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("whatsapp: create %s request: %w", mediaType, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.config.APIToken)

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("whatsapp: %s send: %w", mediaType, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("whatsapp: %s returned %d: %s", mediaType, resp.StatusCode, string(respBody))
	}
	return nil
}

// sendChunk sends a single text chunk via the WhatsApp Cloud API.
func (a *Adapter) sendChunk(ctx context.Context, to, text, replyToID string) error {
	payload := map[string]any{
		"messaging_product": "whatsapp",
		"to":                to,
		"type":              "text",
		"text": map[string]string{
			"body": text,
		},
	}
	if replyTo := strings.TrimSpace(replyToID); replyTo != "" {
		payload["context"] = map[string]string{"message_id": replyTo}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("whatsapp: marshal payload: %w", err)
	}

	url := fmt.Sprintf("%s/%s/messages", a.baseURL, a.config.PhoneID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("whatsapp: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.config.APIToken)

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("whatsapp: send message: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("whatsapp: read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("whatsapp: API returned %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
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

func (a *Adapter) HandleHTTPInbound(_ context.Context, req channels.HTTPInboundRequest) (*channels.HTTPInboundResponse, error) {
	if req.Method != http.MethodPost {
		return nil, channels.NewHTTPInboundError(http.StatusMethodNotAllowed, "whatsapp: method %s not allowed", req.Method)
	}
	var payload WebhookPayload
	if err := json.Unmarshal(req.Body, &payload); err != nil {
		return nil, channels.NewHTTPInboundError(http.StatusBadRequest, "whatsapp: invalid webhook payload: %v", err)
	}
	a.HandleInbound(payload)
	return &channels.HTTPInboundResponse{
		StatusCode: http.StatusOK,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       []byte(`{"ok":true}`),
	}, nil
}

// WebhookPayload represents the top-level structure of a WhatsApp Cloud API
// webhook notification.
type WebhookPayload struct {
	Object string         `json:"object"`
	Entry  []WebhookEntry `json:"entry"`
}

// WebhookEntry represents a single entry in the webhook payload.
type WebhookEntry struct {
	ID      string          `json:"id"`
	Changes []WebhookChange `json:"changes"`
}

// WebhookChange represents a change within a webhook entry.
type WebhookChange struct {
	Value WebhookValue `json:"value"`
	Field string       `json:"field"`
}

// WebhookValue holds the messaging data within a change.
type WebhookValue struct {
	MessagingProduct string           `json:"messaging_product"`
	Metadata         WebhookMetadata  `json:"metadata"`
	Contacts         []WebhookContact `json:"contacts,omitempty"`
	Messages         []WebhookMessage `json:"messages,omitempty"`
}

// WebhookMetadata holds phone number metadata from the webhook.
type WebhookMetadata struct {
	DisplayPhoneNumber string `json:"display_phone_number"`
	PhoneNumberID      string `json:"phone_number_id"`
}

// WebhookContact holds contact info from the webhook.
type WebhookContact struct {
	WaID    string         `json:"wa_id"`
	Profile WebhookProfile `json:"profile"`
}

// WebhookProfile holds the profile name of a contact.
type WebhookProfile struct {
	Name string `json:"name"`
}

// WebhookMessage represents a single inbound WhatsApp message.
type WebhookMessage struct {
	From      string             `json:"from"`
	ID        string             `json:"id"`
	Timestamp string             `json:"timestamp"`
	Type      string             `json:"type"`
	Text      *WebhookTextBody   `json:"text,omitempty"`
	Context   *WebhookMsgContext `json:"context,omitempty"`
}

// WebhookTextBody holds the body of a text message.
type WebhookTextBody struct {
	Body string `json:"body"`
}

// WebhookMsgContext holds context for a reply message.
type WebhookMsgContext struct {
	MessageID string `json:"message_id"`
}

// HandleInbound processes a webhook payload from the WhatsApp Cloud API and
// publishes parsed text messages to subscribers. The gateway HTTP handler
// calls this method after receiving and unmarshaling the webhook body.
func (a *Adapter) HandleInbound(payload WebhookPayload) {
	for _, entry := range payload.Entry {
		for _, change := range entry.Changes {
			if change.Field != "messages" {
				continue
			}
			a.processMessages(change.Value)
		}
	}
}

// processMessages extracts text messages from a webhook value and fans them
// out to all subscribers.
func (a *Adapter) processMessages(value WebhookValue) {
	// Build a lookup of sender names by wa_id.
	names := make(map[string]string, len(value.Contacts))
	for _, c := range value.Contacts {
		names[c.WaID] = c.Profile.Name
	}

	for _, m := range value.Messages {
		if m.Type != "text" || m.Text == nil {
			continue
		}
		content := strings.TrimSpace(m.Text.Body)
		if content == "" {
			continue
		}

		raw := map[string]any{
			"message_id":      m.ID,
			"from":            m.From,
			"timestamp":       m.Timestamp,
			"phone_number_id": value.Metadata.PhoneNumberID,
		}
		if m.Context != nil {
			raw["reply_to_message_id"] = m.Context.MessageID
		}

		inbound := channels.InboundMessage{
			ChannelID:  "whatsapp",
			SenderID:   m.From,
			SenderName: names[m.From],
			Content:    content,
			RawEvent:   raw,
		}

		log.Info("whatsapp: received message",
			"from", m.From,
			"message_id", m.ID,
			"content_length", len(content),
		)

		a.base.PublishInbound(inbound, func() {
			log.Warn("whatsapp: subscriber channel full, dropping message")
		})
	}
}
