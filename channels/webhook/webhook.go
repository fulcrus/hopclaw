// Package webhook provides a generic HTTP-based channel adapter that lets
// any messaging platform integrate with HopClaw without writing Go code.
//
// Inbound: external systems POST messages to the gateway endpoint
//
//	POST /channels/webhook/{id}/inbound
//	{"sender_id":"u1","content":"hello","metadata":{...}}
//
// Outbound: HopClaw POSTs responses to a configured callback URL.
package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/fulcrus/hopclaw/channels"
	"github.com/fulcrus/hopclaw/internal/meta"
	"github.com/fulcrus/hopclaw/logging"
)

var log = logging.WithSubsystem("webhook")

var webhookSendRetryPolicy = channels.SendRetryPolicy{
	MaxAttempts: 3,
	BaseBackoff: 200 * time.Millisecond,
	MaxBackoff:  2 * time.Second,
}

// Config configures a webhook channel instance.
type Config struct {
	ID          string `json:"id" yaml:"id"`
	CallbackURL string `json:"callback_url" yaml:"callback_url"`
	Secret      string `json:"secret,omitempty" yaml:"secret,omitempty"` // HMAC-SHA256 signing key
}

// Adapter implements channels.Adapter for webhook-based integrations.
type Adapter struct {
	config Config

	base       channels.BaseAdapter
	httpClient *http.Client
}

// New creates a webhook adapter.
func New(cfg Config) *Adapter {
	return &Adapter{
		config:     cfg,
		base:       channels.NewBaseAdapter("webhook"),
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (a *Adapter) Connect(_ context.Context) error {
	if a.config.ID == "" {
		return fmt.Errorf("webhook: id is required")
	}
	a.base.MarkConnected(nil)
	log.Info("webhook: adapter connected", "id", a.config.ID)
	return nil
}

func (a *Adapter) Disconnect(_ context.Context) error {
	cancel, _ := a.base.MarkDisconnected()
	if cancel != nil {
		cancel()
	}
	return nil
}

// Send delivers an outbound message by POSTing to the configured callback URL.
func (a *Adapter) Send(ctx context.Context, msg channels.OutboundMessage) error {
	return channels.RetrySend(ctx, webhookSendRetryPolicy, func(ctx context.Context) error {
		return a.sendOnce(ctx, msg)
	})
}

func (a *Adapter) sendOnce(ctx context.Context, msg channels.OutboundMessage) error {
	if strings.TrimSpace(a.config.CallbackURL) == "" {
		return fmt.Errorf("webhook: callback_url is not configured")
	}

	payload := OutboundPayload{
		ChannelID: a.config.ID,
		TargetID:  msg.TargetID,
		ReplyToID: msg.ReplyToID,
		Content:   msg.Content,
		Format:    msg.Format,
		Metadata:  msg.Metadata,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.config.CallbackURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if deliveryID := webhookDeliveryID(msg.Metadata); deliveryID != "" {
		req.Header.Set("X-HopClaw-Delivery-ID", deliveryID)
	}

	if a.config.Secret != "" {
		sig := signPayload(body, a.config.Secret)
		req.Header.Set("X-HopClaw-Signature", sig)
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return channels.MarkSendError(fmt.Errorf("webhook: callback failed: %w", err), true, 0)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		if rateLimited := channels.CheckRateLimit(resp); rateLimited != nil {
			return rateLimited
		}
		err := fmt.Errorf("webhook: callback returned %d", resp.StatusCode)
		return channels.MarkSendError(err, webhookRetryableStatus(resp.StatusCode), resp.StatusCode)
	}
	return nil
}

func (a *Adapter) Capabilities() channels.ChannelCapabilityDescriptor {
	return channels.Capabilities{
		SendText:       true,
		SendRichText:   false,
		SendFile:       false,
		ReceiveMessage: true,
		ReceiveEvent:   true,
	}
}

func (a *Adapter) CapabilityMatrix() channels.CapabilityMatrix {
	return channels.CapabilityMatrix{
		WebhookInbound: true,
	}
}

func (a *Adapter) Status() channels.Status {
	return a.base.Status()
}

func (a *Adapter) SubscribeEvents() <-chan channels.InboundMessage {
	return a.base.SubscribeEvents()
}

// HandleInbound is called by the gateway HTTP handler when an external
// system POSTs a message to /channels/webhook/{id}/inbound.
func (a *Adapter) HandleInbound(payload InboundPayload) {
	content := strings.TrimSpace(payload.Content)
	if content == "" {
		return
	}

	msg := channels.InboundMessage{
		ChannelID:  "webhook:" + a.config.ID,
		SenderID:   payload.SenderID,
		SenderName: payload.SenderName,
		Content:    content,
		RawEvent:   payload.Metadata,
	}
	if msg.RawEvent == nil {
		msg.RawEvent = make(map[string]any)
	}
	msg.RawEvent["webhook_id"] = a.config.ID

	a.base.PublishInbound(msg, func() {
		log.Warn("webhook: subscriber channel full", "id", a.config.ID)
	})
}

// VerifySignature checks the HMAC-SHA256 signature of an inbound request.
// Returns true if no secret is configured (open mode).
func (a *Adapter) VerifySignature(body []byte, signature string) bool {
	if a.config.Secret == "" {
		return true
	}
	expected := signPayload(body, a.config.Secret)
	return hmac.Equal([]byte(expected), []byte(signature))
}

// --- Payload types ---

// InboundPayload is the JSON body for inbound webhook messages.
type InboundPayload struct {
	SenderID   string         `json:"sender_id"`
	SenderName string         `json:"sender_name,omitempty"`
	Content    string         `json:"content"`
	ReplyToID  string         `json:"reply_to_id,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

// OutboundPayload is the JSON body POSTed to the callback URL.
type OutboundPayload struct {
	ChannelID string         `json:"channel_id"`
	TargetID  string         `json:"target_id"`
	ReplyToID string         `json:"reply_to_id,omitempty"`
	Content   string         `json:"content"`
	Format    string         `json:"format,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

func signPayload(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func webhookRetryableStatus(statusCode int) bool {
	switch statusCode {
	case http.StatusRequestTimeout, http.StatusTooEarly, http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return statusCode >= 500
}

func webhookDeliveryID(metadata map[string]any) string {
	if len(metadata) == 0 {
		return ""
	}
	for _, key := range []string{meta.KeyInteractionTurnID, meta.KeyMessageID, meta.KeyRunID} {
		if value := strings.TrimSpace(fmt.Sprint(metadata[key])); value != "" {
			return value
		}
	}
	return ""
}
