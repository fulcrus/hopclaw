// Package zalouser implements channels.Adapter for Zalo Personal Account.
// This is a lightweight adapter since the actual Zalo protocol is complex;
// inbound messages are expected to arrive via HandleInbound from an external
// polling or webhook mechanism.
package zalouser

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

var log = logging.WithSubsystem("zalouser")

var _ channels.HTTPInboundAdapter = (*Adapter)(nil)

// Config holds the configuration for the Zalo personal account adapter.
type Config struct {
	Cookie  string `json:"cookie" yaml:"cookie"`     // session cookie for Zalo API
	IMEI    string `json:"imei" yaml:"imei"`         // device IMEI for Zalo API
	BaseURL string `json:"base_url" yaml:"base_url"` // Zalo internal API base URL
}

// Adapter implements channels.Adapter for Zalo personal accounts.
type Adapter struct {
	config Config
	client *http.Client

	base channels.BaseAdapter
}

// New creates a new Zalo personal account adapter with the given configuration.
func New(cfg Config) *Adapter {
	return &Adapter{
		config: cfg,
		client: &http.Client{Timeout: 30 * time.Second},
		base:   channels.NewBaseAdapter("zalouser"),
	}
}

// Connect marks the adapter as connected. Actual message reception is handled
// by HandleInbound, which should be called by an external polling mechanism
// or webhook handler.
func (a *Adapter) Connect(_ context.Context) error {
	if a.config.Cookie == "" {
		return fmt.Errorf("zalouser: cookie is required")
	}
	if a.config.BaseURL == "" {
		return fmt.Errorf("zalouser: base_url is required")
	}
	if !a.base.MarkConnected(nil) {
		return nil
	}

	log.Info("zalouser: adapter connected")
	return nil
}

// Disconnect marks the adapter as disconnected and closes all subscriber channels.
func (a *Adapter) Disconnect(_ context.Context) error {
	cancel, ok := a.base.MarkDisconnected()
	if !ok {
		return nil
	}
	if cancel != nil {
		cancel()
	}
	log.Info("zalouser: adapter disconnected")
	return nil
}

// Send delivers an outbound message through the Zalo internal API.
// msg.TargetID is the recipient's Zalo user ID.
func (a *Adapter) Send(ctx context.Context, msg channels.OutboundMessage) error {
	if a.base.Status() != channels.StatusConnected {
		return fmt.Errorf("zalouser: adapter is not connected")
	}
	if strings.TrimSpace(msg.TargetID) == "" {
		return fmt.Errorf("zalouser: target_id (recipient user ID) is required")
	}

	payload := map[string]any{
		"toid": msg.TargetID,
		"msg":  msg.Content,
		"imei": a.config.IMEI,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("zalouser: marshal send payload: %w", err)
	}

	url := strings.TrimRight(a.config.BaseURL, "/") + "/api/message/sms"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("zalouser: create send request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", a.config.Cookie)

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("zalouser: send message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("zalouser: send returned status %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// HandleInbound processes a raw inbound message payload from an external
// source (e.g., webhook or polling mechanism). This is the primary way
// messages enter the adapter.
func (a *Adapter) HandleInbound(payload map[string]any) {
	if a.base.Status() != channels.StatusConnected {
		return
	}

	senderID, _ := payload["fromUid"].(string)
	senderName, _ := payload["fromDisplayName"].(string)
	content, _ := payload["content"].(string)
	msgID, _ := payload["msgId"].(string)

	if strings.TrimSpace(content) == "" {
		return
	}

	rawEvent := map[string]any{
		"message_id": msgID,
		"msg_id":     msgID,
		"user_id":    senderID,
		"payload":    payload,
	}

	inbound := channels.InboundMessage{
		ChannelID:  "zalouser",
		SenderID:   senderID,
		SenderName: senderName,
		Content:    content,
		RawEvent:   rawEvent,
	}

	log.Info("zalouser: received message",
		"sender", senderID,
		"content_length", len(content),
	)

	a.base.PublishInbound(inbound, func() {
		log.Warn("zalouser: subscriber channel full, dropping message")
	})
}

func (a *Adapter) HandleHTTPInbound(_ context.Context, req channels.HTTPInboundRequest) (*channels.HTTPInboundResponse, error) {
	if req.Method != http.MethodPost {
		return nil, channels.NewHTTPInboundError(http.StatusMethodNotAllowed, "zalouser: method %s not allowed", req.Method)
	}
	var payload map[string]any
	if err := json.Unmarshal(req.Body, &payload); err != nil {
		return nil, channels.NewHTTPInboundError(http.StatusBadRequest, "zalouser: invalid inbound payload: %v", err)
	}
	a.HandleInbound(payload)
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
		ReceiveEvent:   false,
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
