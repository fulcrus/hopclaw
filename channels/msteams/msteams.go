// Package msteams implements a channels.Adapter for the Microsoft Teams
// Bot Framework.
//
// The adapter authenticates with the Bot Framework token endpoint on Connect,
// sends outbound messages as Bot Framework activities, and receives inbound
// activities via HandleActivity (called by the gateway webhook handler).
package msteams

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/fulcrus/hopclaw/channels"
	"github.com/fulcrus/hopclaw/logging"
)

var log = logging.WithSubsystem("msteams")

var _ channels.CapabilityReporter = (*Adapter)(nil)
var _ channels.HTTPInboundAdapter = (*Adapter)(nil)

const tokenEndpoint = "https://login.microsoftonline.com/botframework.com/oauth2/v2.0/token"

// Config holds the configuration for the MS Teams Bot Framework adapter.
type Config struct {
	AppID    string `json:"app_id" yaml:"app_id"`
	Password string `json:"password" yaml:"password"`
}

// Adapter implements channels.Adapter for Microsoft Teams via Bot Framework.
type Adapter struct {
	config Config
	client *http.Client

	base        channels.BaseAdapter
	tokenMu     sync.RWMutex
	accessToken string
	tokenExpiry time.Time
}

// New creates a new MS Teams adapter with the given configuration.
func New(cfg Config) *Adapter {
	return &Adapter{
		config: cfg,
		client: &http.Client{Timeout: 30 * time.Second},
		base:   channels.NewBaseAdapter("msteams"),
	}
}

// Connect authenticates with the Bot Framework token endpoint and starts a
// background goroutine to refresh the token before it expires.
func (a *Adapter) Connect(ctx context.Context) error {
	if a.config.AppID == "" {
		return fmt.Errorf("msteams: app_id is required")
	}
	if a.config.Password == "" {
		return fmt.Errorf("msteams: password is required")
	}
	if a.base.Status() == channels.StatusConnected {
		return nil
	}

	a.base.SetStatus(channels.StatusConnecting)

	if err := a.fetchToken(ctx); err != nil {
		a.base.SetStatus(channels.StatusError)
		return fmt.Errorf("msteams: authenticate: %w", err)
	}

	refreshCtx, cancel := context.WithCancel(ctx)
	if !a.base.MarkConnected(cancel) {
		cancel()
		return nil
	}

	go a.refreshLoop(refreshCtx)

	log.Info("msteams: adapter connected", "app_id", a.config.AppID)
	return nil
}

// Disconnect stops the token refresh loop and closes all subscriber channels.
func (a *Adapter) Disconnect(_ context.Context) error {
	cancel, ok := a.base.MarkDisconnected()
	if !ok {
		return nil
	}
	if cancel != nil {
		cancel()
	}
	a.tokenMu.Lock()
	a.accessToken = ""
	a.tokenExpiry = time.Time{}
	a.tokenMu.Unlock()
	log.Info("msteams: adapter disconnected")
	return nil
}

// Send delivers an outbound message as a Bot Framework activity.
//
// msg.TargetID is the conversation ID. msg.Metadata should contain
// "service_url" (the Bot Framework service URL from the inbound activity).
func (a *Adapter) Send(ctx context.Context, msg channels.OutboundMessage) error {
	if a.base.Status() != channels.StatusConnected {
		return fmt.Errorf("msteams: adapter is not connected")
	}
	a.tokenMu.RLock()
	token := a.accessToken
	a.tokenMu.RUnlock()
	if strings.TrimSpace(msg.TargetID) == "" {
		return fmt.Errorf("msteams: target_id (conversation id) is required")
	}

	serviceURL := ""
	if msg.Metadata != nil {
		if v, ok := msg.Metadata["service_url"].(string); ok {
			serviceURL = v
		}
	}
	if serviceURL == "" {
		return fmt.Errorf("msteams: metadata.service_url is required")
	}

	activity := Activity{
		Type:         "message",
		Text:         msg.Content,
		Conversation: ConversationAccount{ID: msg.TargetID},
	}
	if strings.TrimSpace(msg.ReplyToID) != "" {
		activity.ReplyToID = msg.ReplyToID
	}

	body, err := json.Marshal(activity)
	if err != nil {
		return fmt.Errorf("msteams: marshal activity: %w", err)
	}

	endpoint := fmt.Sprintf("%s/v3/conversations/%s/activities",
		strings.TrimRight(serviceURL, "/"),
		url.PathEscape(msg.TargetID),
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("msteams: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("msteams: send activity: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("msteams: read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("msteams: API returned %d: %s", resp.StatusCode, string(respBody))
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

// Status returns the current connection state.
func (a *Adapter) Status() channels.Status {
	return a.base.Status()
}

// SubscribeEvents returns a channel that receives inbound messages.
func (a *Adapter) SubscribeEvents() <-chan channels.InboundMessage {
	return a.base.SubscribeEvents()
}

// Activity represents a Bot Framework activity (subset of fields).
type Activity struct {
	Type         string              `json:"type"`
	ID           string              `json:"id,omitempty"`
	Timestamp    string              `json:"timestamp,omitempty"`
	Text         string              `json:"text,omitempty"`
	ServiceURL   string              `json:"serviceUrl,omitempty"`
	ChannelID    string              `json:"channelId,omitempty"`
	From         ChannelAccount      `json:"from,omitempty"`
	Recipient    ChannelAccount      `json:"recipient,omitempty"`
	Conversation ConversationAccount `json:"conversation"`
	ReplyToID    string              `json:"replyToId,omitempty"`
}

// ChannelAccount identifies a user or bot in a conversation.
type ChannelAccount struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

// ConversationAccount identifies a conversation.
type ConversationAccount struct {
	ID               string `json:"id"`
	ConversationType string `json:"conversationType,omitempty"`
}

// HandleActivity processes an inbound Bot Framework activity delivered by the
// gateway webhook handler. Only "message" activities with text are published.
func (a *Adapter) HandleActivity(activity Activity) {
	if activity.Type != "message" {
		return
	}

	content := strings.TrimSpace(activity.Text)
	if content == "" {
		return
	}

	raw := map[string]any{
		"activity_id":     activity.ID,
		"service_url":     activity.ServiceURL,
		"channel_id":      activity.ChannelID,
		"conversation_id": activity.Conversation.ID,
	}
	if strings.TrimSpace(activity.Conversation.ConversationType) != "" {
		raw["conversation_type"] = activity.Conversation.ConversationType
	}
	if strings.TrimSpace(activity.ReplyToID) != "" {
		raw["reply_to_id"] = activity.ReplyToID
	}

	inbound := channels.InboundMessage{
		ChannelID:  "msteams",
		SenderID:   activity.From.ID,
		SenderName: activity.From.Name,
		Content:    content,
		RawEvent:   raw,
	}

	log.Info("msteams: received activity",
		"from", activity.From.ID,
		"conversation", activity.Conversation.ID,
		"content_length", len(content),
	)

	a.base.PublishInbound(inbound, func() {
		log.Warn("msteams: subscriber channel full, dropping message")
	})
}

// HandleHTTPInbound adapts Bot Framework webhook requests to the shared
// HTTPInboundAdapter contract used by the gateway.
func (a *Adapter) HandleHTTPInbound(_ context.Context, req channels.HTTPInboundRequest) (*channels.HTTPInboundResponse, error) {
	var activity Activity
	if err := json.Unmarshal(req.Body, &activity); err != nil {
		return nil, channels.NewHTTPInboundError(http.StatusBadRequest, "invalid Bot Framework activity payload")
	}
	a.HandleActivity(activity)
	return nil, nil
}

func (a *Adapter) CapabilityMatrix() channels.CapabilityMatrix {
	return channels.CapabilityMatrix{
		Threading:      true,
		PolicyControls: true,
		Dedupe:         true,
	}
}

// tokenResponse is the OAuth2 token endpoint response.
type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"` // seconds
	TokenType   string `json:"token_type"`
}

// fetchToken requests an access token from the Bot Framework token endpoint.
func (a *Adapter) fetchToken(ctx context.Context) error {
	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {a.config.AppID},
		"client_secret": {a.config.Password},
		"scope":         {"https://api.botframework.com/.default"},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, string(body))
	}

	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return fmt.Errorf("decode token response: %w", err)
	}
	if tr.AccessToken == "" {
		return fmt.Errorf("empty access token in response")
	}

	a.tokenMu.Lock()
	a.accessToken = tr.AccessToken
	// Refresh a bit before actual expiry to avoid race conditions.
	a.tokenExpiry = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second).Add(-2 * time.Minute)
	a.tokenMu.Unlock()
	return nil
}

// refreshLoop periodically refreshes the access token before it expires.
func (a *Adapter) refreshLoop(ctx context.Context) {
	for {
		a.tokenMu.RLock()
		waitDuration := time.Until(a.tokenExpiry)
		a.tokenMu.RUnlock()

		if waitDuration < 30*time.Second {
			waitDuration = 30 * time.Second
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(waitDuration):
		}

		err := a.fetchToken(ctx)
		if err != nil {
			log.Error("msteams: token refresh failed", "error", err)
			// Brief backoff before retrying.
			select {
			case <-ctx.Done():
				return
			case <-time.After(10 * time.Second):
			}
			continue
		}
		log.Info("msteams: token refreshed")
	}
}
