// Package googlechat implements a channels.Adapter for the Google Chat API.
//
// Inbound messages arrive via webhook (Google Chat sends HTTP POSTs to the
// configured endpoint). The gateway calls HandleEvent with the parsed request
// body. Outbound messages are sent via the Google Chat REST API or a webhook
// URL.
package googlechat

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/fulcrus/hopclaw/channels"
	"github.com/fulcrus/hopclaw/logging"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

var log = logging.WithSubsystem("googlechat")

var _ channels.HTTPInboundAdapter = (*Adapter)(nil)

const apiBase = "https://chat.googleapis.com/v1"
const chatBotScope = "https://www.googleapis.com/auth/chat.bot"

// Config holds the configuration for the Google Chat adapter.
type Config struct {
	// ServiceAccount is the path to the service account JSON key file.
	// Used when sending messages via the REST API.
	ServiceAccount string `json:"service_account,omitempty" yaml:"service_account,omitempty"`

	// WebhookURL is an incoming webhook URL for the target space.
	// When set, outbound messages are sent to this URL instead of the REST API.
	WebhookURL string `json:"webhook_url,omitempty" yaml:"webhook_url,omitempty"`

	// VerificationKey is the token used to verify inbound webhook requests
	// from Google Chat.
	VerificationKey string `json:"verification_key,omitempty" yaml:"verification_key,omitempty"`
}

// Adapter implements channels.Adapter for Google Chat.
type Adapter struct {
	config Config
	client *http.Client

	base        channels.BaseAdapter
	tokenMu     sync.RWMutex
	tokenSource oauth2.TokenSource
}

// New creates a new Google Chat adapter with the given configuration.
func New(cfg Config) *Adapter {
	return &Adapter{
		config: cfg,
		client: &http.Client{Timeout: 30 * time.Second},
		base:   channels.NewBaseAdapter("googlechat"),
	}
}

// Connect marks the adapter as connected. Google Chat uses webhooks for
// inbound messages, so no persistent connection is established.
func (a *Adapter) Connect(_ context.Context) error {
	if a.config.WebhookURL == "" && a.config.ServiceAccount == "" {
		return fmt.Errorf("googlechat: either webhook_url or service_account is required")
	}
	if a.base.Status() == channels.StatusConnected {
		return nil
	}
	var tokenSource oauth2.TokenSource
	if a.config.WebhookURL == "" {
		var err error
		tokenSource, err = loadServiceAccountTokenSource(a.config.ServiceAccount)
		if err != nil {
			return err
		}
	}
	if !a.base.MarkConnected(nil) {
		return nil
	}
	a.tokenMu.Lock()
	a.tokenSource = tokenSource
	a.tokenMu.Unlock()
	log.Info("googlechat: adapter connected")
	return nil
}

// Disconnect tears down the adapter and closes all subscriber channels.
func (a *Adapter) Disconnect(_ context.Context) error {
	if _, ok := a.base.MarkDisconnected(); !ok {
		return nil
	}
	a.tokenMu.Lock()
	a.tokenSource = nil
	a.tokenMu.Unlock()
	log.Info("googlechat: adapter disconnected")
	return nil
}

// Send delivers an outbound message to Google Chat.
//
// If a WebhookURL is configured, the message is POSTed there. Otherwise,
// msg.TargetID must be a space resource name (e.g., "spaces/AAAA") and the
// message is sent via the REST API.
func (a *Adapter) Send(ctx context.Context, msg channels.OutboundMessage) error {
	if a.base.Status() != channels.StatusConnected {
		return fmt.Errorf("googlechat: adapter is not connected")
	}

	if a.config.WebhookURL != "" {
		return a.sendViaWebhook(ctx, msg)
	}
	return a.sendViaAPI(ctx, msg)
}

// sendViaWebhook sends a message to the configured webhook URL.
// When blocks or attachments are present, a Cards v2 payload is used.
func (a *Adapter) sendViaWebhook(ctx context.Context, msg channels.OutboundMessage) error {
	payload := buildGoogleChatPayload(msg)

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("googlechat: marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.config.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("googlechat: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("googlechat: webhook send: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("googlechat: webhook returned %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// sendViaAPI sends a message to a space using the Google Chat REST API.
func (a *Adapter) sendViaAPI(ctx context.Context, msg channels.OutboundMessage) error {
	if strings.TrimSpace(msg.TargetID) == "" {
		return fmt.Errorf("googlechat: target_id (space name) is required")
	}

	payload := buildGoogleChatPayload(msg)

	// If replying to a thread, include the thread name.
	threadName := ""
	if msg.Metadata != nil {
		if v, ok := msg.Metadata["thread_name"].(string); ok {
			threadName = v
		}
	}

	if threadName != "" {
		payload["thread"] = map[string]string{"name": threadName}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("googlechat: marshal payload: %w", err)
	}

	endpoint := fmt.Sprintf("%s/%s/messages", apiBase, strings.TrimPrefix(msg.TargetID, "/"))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("googlechat: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	a.tokenMu.RLock()
	tokenSource := a.tokenSource
	a.tokenMu.RUnlock()
	if tokenSource == nil {
		return fmt.Errorf("googlechat: service account token source is not initialized")
	}
	token, err := tokenSource.Token()
	if err != nil {
		return fmt.Errorf("googlechat: acquire service account token: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("googlechat: API send: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("googlechat: read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("googlechat: API returned %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// Capabilities returns what this adapter supports.
func (a *Adapter) Capabilities() channels.ChannelCapabilityDescriptor {
	return channels.Capabilities{
		SendText:       true,
		SendRichText:   true,
		SendFile:       false,
		ReceiveMessage: true,
		ReceiveEvent:   true,
		Interactive:    true,
		InlineDelivery: true,
	}
}

func (a *Adapter) CapabilityMatrix() channels.CapabilityMatrix {
	return channels.CapabilityMatrix{
		Threading: true,
		RichCards: true,
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
		return nil, channels.NewHTTPInboundError(http.StatusMethodNotAllowed, "googlechat: method %s not allowed", req.Method)
	}
	if err := a.HandleEvent(req.Body); err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "invalid verification token") {
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

// ChatEvent represents a Google Chat event delivered to the webhook endpoint.
type ChatEvent struct {
	Type           string           `json:"type"`
	EventTime      string           `json:"eventTime"`
	Token          string           `json:"token,omitempty"`
	Message        *ChatMessage     `json:"message,omitempty"`
	User           *ChatUser        `json:"user,omitempty"`
	Space          *ChatSpace       `json:"space,omitempty"`
	ConfigComplete *json.RawMessage `json:"configCompleteRedirectUrl,omitempty"`
}

// ChatMessage represents a message within a Google Chat event.
type ChatMessage struct {
	Name         string      `json:"name"`
	Text         string      `json:"text"`
	Thread       *ChatThread `json:"thread,omitempty"`
	ArgumentText string      `json:"argumentText,omitempty"`
}

// ChatThread identifies a thread within a space.
type ChatThread struct {
	Name string `json:"name"`
}

// ChatUser represents the sender of a Google Chat message.
type ChatUser struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Email       string `json:"email,omitempty"`
	Type        string `json:"type"`
}

// ChatSpace represents a Google Chat space.
type ChatSpace struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// HandleEvent processes an inbound Google Chat event delivered by the gateway
// webhook handler. Only MESSAGE events with text content are published.
func (a *Adapter) HandleEvent(body []byte) error {
	var event ChatEvent
	if err := json.Unmarshal(body, &event); err != nil {
		return fmt.Errorf("googlechat: unmarshal event: %w", err)
	}

	// Verify the token if a verification key is configured (timing-safe comparison).
	if a.config.VerificationKey != "" {
		if subtle.ConstantTimeCompare([]byte(event.Token), []byte(a.config.VerificationKey)) != 1 {
			return fmt.Errorf("googlechat: invalid verification token")
		}
	}

	// Only process MESSAGE events.
	if event.Type != "MESSAGE" {
		return nil
	}
	if event.Message == nil {
		return nil
	}

	content := strings.TrimSpace(event.Message.Text)
	if content == "" {
		return nil
	}

	raw := map[string]any{
		"event_type": event.Type,
		"event_time": event.EventTime,
	}
	if event.Message.Name != "" {
		raw["message_name"] = event.Message.Name
		raw["message_id"] = event.Message.Name
	}
	if event.Space != nil {
		raw["space_name"] = event.Space.Name
		raw["space_type"] = event.Space.Type
	}
	if event.Message.Thread != nil {
		raw["thread_name"] = event.Message.Thread.Name
	}

	senderID := ""
	senderName := ""
	if event.User != nil {
		senderID = event.User.Name
		senderName = event.User.DisplayName
	}

	spaceName := ""
	if event.Space != nil {
		spaceName = event.Space.Name
	}

	inbound := channels.InboundMessage{
		ChannelID:  "googlechat",
		SenderID:   senderID,
		SenderName: senderName,
		Content:    content,
		RawEvent:   raw,
	}

	log.Info("googlechat: received message",
		"sender", senderID,
		"space", spaceName,
		"content_length", len(content),
	)

	a.base.PublishInbound(inbound, func() {
		log.Warn("googlechat: subscriber channel full, dropping message")
	})
	return nil
}

func loadServiceAccountTokenSource(path string) (oauth2.TokenSource, error) {
	data, err := os.ReadFile(strings.TrimSpace(path))
	if err != nil {
		return nil, fmt.Errorf("googlechat: read service account file: %w", err)
	}
	jwtConfig, err := google.JWTConfigFromJSON(data, chatBotScope)
	if err != nil {
		return nil, fmt.Errorf("googlechat: parse service account file: %w", err)
	}
	return jwtConfig.TokenSource(context.Background()), nil
}

// buildGoogleChatPayload builds a Google Chat message payload. When blocks
// or attachments are present, a Cards v2 payload with sections and widgets
// is constructed; otherwise a plain text message is returned.
func buildGoogleChatPayload(msg channels.OutboundMessage) map[string]any {
	if len(msg.Blocks) == 0 && len(msg.Attachments) == 0 {
		return map[string]any{"text": msg.Content}
	}

	var sections []map[string]any
	for _, b := range msg.Blocks {
		content := strings.TrimSpace(b.Content)
		if content == "" {
			continue
		}
		section := map[string]any{}
		if title := strings.TrimSpace(b.Title); title != "" {
			section["header"] = title
		}
		section["widgets"] = []map[string]any{
			{"textParagraph": map[string]string{"text": content}},
		}
		sections = append(sections, section)
	}

	if len(msg.Attachments) > 0 {
		var widgets []map[string]any
		for _, att := range msg.Attachments {
			uri := strings.TrimSpace(att.URI)
			if uri == "" {
				continue
			}
			label := strings.TrimSpace(att.Label)
			if label == "" {
				label = uri
			}
			widgets = append(widgets, map[string]any{
				"textParagraph": map[string]string{
					"text": fmt.Sprintf("<a href=\"%s\">%s</a>", channels.EscapeHTML(uri), channels.EscapeHTML(label)),
				},
			})
		}
		if len(widgets) > 0 {
			sections = append(sections, map[string]any{
				"header":  "Attachments",
				"widgets": widgets,
			})
		}
	}

	header := map[string]any{"title": "HopClaw"}
	if subtitle := strings.TrimSpace(msg.Content); subtitle != "" {
		header["subtitle"] = channels.TruncateUTF8(subtitle, 80)
	}

	card := map[string]any{
		"header":   header,
		"sections": sections,
	}

	cardID := fmt.Sprintf("hopclaw_%d", time.Now().UnixNano())

	return map[string]any{
		"text": msg.Content,
		"cardsV2": []map[string]any{
			{
				"cardId": cardID,
				"card":   card,
			},
		},
	}
}
