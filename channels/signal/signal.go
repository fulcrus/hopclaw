// Package signal implements channels.Adapter for Signal via signal-cli REST API.
package signal

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

var log = logging.WithSubsystem("signal")

var _ channels.CapabilityReporter = (*Adapter)(nil)

// Config holds the configuration for the Signal adapter.
type Config struct {
	BaseURL   string `json:"base_url" yaml:"base_url"`     // e.g. http://127.0.0.1:8080
	Number    string `json:"number" yaml:"number"`         // phone number registered with signal-cli
	AuthToken string `json:"auth_token" yaml:"auth_token"` // optional bearer token
}

// Adapter implements channels.Adapter for Signal via signal-cli REST API.
type Adapter struct {
	config Config
	client *http.Client

	base channels.BaseAdapter
}

// New creates a new Signal adapter with the given configuration.
func New(cfg Config) *Adapter {
	return &Adapter{
		config: cfg,
		client: &http.Client{Timeout: 30 * time.Second},
		base:   channels.NewBaseAdapter("signal"),
	}
}

// signalMessage represents a message received from the signal-cli receive
// endpoint. Its JSON tags intentionally preserve the upstream camelCase wire
// format.
type signalMessage struct {
	Envelope signalEnvelope `json:"envelope"`
}

// signalEnvelope preserves signal-cli camelCase field names in JSON tags.
type signalEnvelope struct {
	Source      string             `json:"source"`
	SourceName  string             `json:"sourceName"`
	Timestamp   int64              `json:"timestamp"`
	DataMessage *signalDataMessage `json:"dataMessage,omitempty"`
}

// signalDataMessage preserves signal-cli camelCase field names in JSON tags.
type signalDataMessage struct {
	Message   string           `json:"message"`
	Timestamp int64            `json:"timestamp"`
	GroupInfo *signalGroupInfo `json:"groupInfo,omitempty"`
}

// signalGroupInfo preserves signal-cli camelCase field names in JSON tags.
type signalGroupInfo struct {
	GroupID string `json:"groupId"`
}

// Connect starts the polling goroutine to receive messages from signal-cli.
func (a *Adapter) Connect(ctx context.Context) error {
	if a.config.BaseURL == "" {
		return fmt.Errorf("signal: base_url is required")
	}
	if a.config.Number == "" {
		return fmt.Errorf("signal: number is required")
	}
	if a.base.Status() == channels.StatusConnected {
		return nil
	}

	a.base.SetStatus(channels.StatusConnecting)

	pollCtx, cancel := context.WithCancel(ctx)
	if !a.base.MarkConnected(cancel) {
		cancel()
		return nil
	}

	go a.pollLoop(pollCtx)

	log.Info("signal: adapter connected", "number", a.config.Number)
	return nil
}

// Disconnect stops the polling goroutine and closes all subscriber channels.
func (a *Adapter) Disconnect(_ context.Context) error {
	cancel, ok := a.base.MarkDisconnected()
	if !ok {
		return nil
	}
	if cancel != nil {
		cancel()
	}
	log.Info("signal: adapter disconnected")
	return nil
}

// Send delivers an outbound message through the signal-cli v2 send endpoint.
func (a *Adapter) Send(ctx context.Context, msg channels.OutboundMessage) error {
	if a.base.Status() != channels.StatusConnected {
		return fmt.Errorf("signal: adapter is not connected")
	}
	if strings.TrimSpace(msg.TargetID) == "" {
		return fmt.Errorf("signal: target_id (recipient) is required")
	}

	// Render blocks as plain text for Signal (no rich formatting API).
	content := msg.Content
	if len(msg.Blocks) > 0 {
		content = channels.ContentWithBlocks(msg, channels.RenderBlocksAsPlain)
	} else if len(msg.Attachments) > 0 {
		if att := channels.RenderAttachmentsAsText(msg.Attachments); att != "" {
			content = content + "\n\n" + att
		}
	}

	payload := map[string]any{
		"message":    content,
		"number":     a.config.Number,
		"recipients": []string{msg.TargetID},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("signal: marshal send payload: %w", err)
	}

	url := strings.TrimRight(a.config.BaseURL, "/") + "/v2/send"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("signal: create send request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if a.config.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+a.config.AuthToken)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("signal: send message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("signal: send returned status %d: %s", resp.StatusCode, string(respBody))
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

// pollLoop polls the signal-cli receive endpoint at a 2-second interval.
func (a *Adapter) pollLoop(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.pollOnce(ctx)
		}
	}
}

// pollOnce fetches new messages from the signal-cli receive endpoint.
func (a *Adapter) pollOnce(ctx context.Context) {
	url := strings.TrimRight(a.config.BaseURL, "/") + "/v1/receive/" + a.config.Number

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		log.Error("signal: create poll request", "error", err)
		return
	}
	if a.config.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+a.config.AuthToken)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		log.Error("signal: poll request failed", "error", err)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Error("signal: read poll response", "error", err)
		return
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Warn("signal: poll returned non-2xx", "status", resp.StatusCode)
		return
	}

	var messages []signalMessage
	if err := json.Unmarshal(body, &messages); err != nil {
		log.Warn("signal: decode poll response", "error", err)
		return
	}

	for _, m := range messages {
		a.handleMessage(m)
	}
}

// handleMessage processes a single signal message and publishes it to subscribers.
func (a *Adapter) handleMessage(msg signalMessage) {
	if msg.Envelope.DataMessage == nil {
		return
	}
	text := strings.TrimSpace(msg.Envelope.DataMessage.Message)
	if text == "" {
		return
	}

	channelID := "signal"
	rawEvent := map[string]any{
		"source":     msg.Envelope.Source,
		"timestamp":  msg.Envelope.DataMessage.Timestamp,
		"message_id": fmt.Sprintf("%d", msg.Envelope.DataMessage.Timestamp),
	}
	if msg.Envelope.DataMessage.GroupInfo != nil {
		rawEvent["group_id"] = msg.Envelope.DataMessage.GroupInfo.GroupID
		channelID = "signal:" + msg.Envelope.DataMessage.GroupInfo.GroupID
	}

	inbound := channels.InboundMessage{
		ChannelID:  channelID,
		SenderID:   msg.Envelope.Source,
		SenderName: msg.Envelope.SourceName,
		Content:    text,
		RawEvent:   rawEvent,
	}

	log.Info("signal: received message",
		"sender", msg.Envelope.Source,
		"content_length", len(text),
	)

	a.base.PublishInbound(inbound, func() {
		log.Warn("signal: subscriber channel full, dropping message")
	})
}
