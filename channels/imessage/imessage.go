// Package imessage implements channels.Adapter for iMessage via BlueBubbles/bridge.
package imessage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fulcrus/hopclaw/channels"
	"github.com/fulcrus/hopclaw/logging"
)

var log = logging.WithSubsystem("imessage")

// Config holds the configuration for the iMessage adapter.
type Config struct {
	BaseURL string `json:"base_url" yaml:"base_url"` // BlueBubbles/bridge URL
	APIKey  string `json:"api_key" yaml:"api_key"`   // password / API key
}

// Adapter implements channels.Adapter for iMessage via BlueBubbles/bridge.
type Adapter struct {
	config Config
	client *http.Client

	base       channels.BaseAdapter
	stateMu    sync.Mutex
	lastPollTS int64 // epoch millis of last seen message
}

// New creates a new iMessage adapter with the given configuration.
func New(cfg Config) *Adapter {
	return &Adapter{
		config:     cfg,
		client:     &http.Client{Timeout: 30 * time.Second},
		base:       channels.NewBaseAdapter("imessage"),
		lastPollTS: time.Now().UnixMilli(),
	}
}

// bridgeMessage represents a message from the BlueBubbles API. Its JSON tags
// intentionally preserve the upstream camelCase wire format.
type bridgeMessage struct {
	GUID           string        `json:"guid"`
	ChatGUID       string        `json:"chats,omitempty"` // fallback
	Text           string        `json:"text"`
	IsFromMe       bool          `json:"isFromMe"`
	Handle         *bridgeHandle `json:"handle,omitempty"`
	DateCreated    int64         `json:"dateCreated"`
	AssociatedChat *bridgeChat   `json:"chats_list,omitempty"`
}

// bridgeHandle preserves BlueBubbles camelCase field names in JSON tags.
type bridgeHandle struct {
	Address     string `json:"address"`
	DisplayName string `json:"displayName"`
}

// bridgeChat preserves BlueBubbles camelCase field names in JSON tags.
type bridgeChat struct {
	ChatIdentifier string `json:"chatIdentifier"`
}

// bridgePollResponse is the response from the messages endpoint. The nested
// JSON tags intentionally mirror the provider payload.
type bridgePollResponse struct {
	Status  int             `json:"status"`
	Message string          `json:"message"`
	Data    []bridgeMessage `json:"data"`
}

func chatGUID(msg bridgeMessage) string {
	if msg.AssociatedChat != nil {
		if id := strings.TrimSpace(msg.AssociatedChat.ChatIdentifier); id != "" {
			return id
		}
	}
	return strings.TrimSpace(msg.ChatGUID)
}

// Connect starts the polling goroutine to receive iMessage messages.
func (a *Adapter) Connect(ctx context.Context) error {
	if a.config.BaseURL == "" {
		return fmt.Errorf("imessage: base_url is required")
	}
	if a.config.APIKey == "" {
		return fmt.Errorf("imessage: api_key is required")
	}
	pollCtx, cancel := context.WithCancel(ctx)
	if !a.base.MarkConnected(cancel) {
		cancel()
		return nil
	}

	go a.pollLoop(pollCtx)

	log.Info("imessage: adapter connected")
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
	log.Info("imessage: adapter disconnected")
	return nil
}

// Send delivers an outbound message through the BlueBubbles bridge.
func (a *Adapter) Send(ctx context.Context, msg channels.OutboundMessage) error {
	if a.base.Status() != channels.StatusConnected {
		return fmt.Errorf("imessage: adapter is not connected")
	}
	if strings.TrimSpace(msg.TargetID) == "" {
		return fmt.Errorf("imessage: target_id (chatGuid) is required")
	}

	payload := map[string]string{
		"chatGuid": msg.TargetID,
		"message":  msg.Content,
		"password": a.config.APIKey,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("imessage: marshal send payload: %w", err)
	}

	url := strings.TrimRight(a.config.BaseURL, "/") + "/api/v1/message/text"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("imessage: create send request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("imessage: send message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("imessage: send returned status %d: %s", resp.StatusCode, string(respBody))
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

// pollLoop polls the BlueBubbles messages endpoint at a 3-second interval.
func (a *Adapter) pollLoop(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
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

// pollOnce fetches new messages from the BlueBubbles bridge.
func (a *Adapter) pollOnce(ctx context.Context) {
	a.stateMu.Lock()
	afterTS := a.lastPollTS
	a.stateMu.Unlock()

	url := strings.TrimRight(a.config.BaseURL, "/") +
		"/api/v1/message?after=" + strconv.FormatInt(afterTS, 10) +
		"&password=" + a.config.APIKey

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		log.Error("imessage: create poll request", "error", err)
		return
	}

	resp, err := a.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		log.Error("imessage: poll request failed", "error", err)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Error("imessage: read poll response", "error", err)
		return
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Warn("imessage: poll returned non-2xx", "status", resp.StatusCode)
		return
	}

	var pollResp bridgePollResponse
	if err := json.Unmarshal(body, &pollResp); err != nil {
		log.Warn("imessage: decode poll response", "error", err)
		return
	}

	var maxTS int64
	for _, m := range pollResp.Data {
		if m.IsFromMe {
			continue
		}
		if m.DateCreated > maxTS {
			maxTS = m.DateCreated
		}
		a.handleMessage(m)
	}

	if maxTS > 0 {
		a.stateMu.Lock()
		if maxTS > a.lastPollTS {
			a.lastPollTS = maxTS
		}
		a.stateMu.Unlock()
	}
}

// handleMessage processes a single BlueBubbles message and publishes it.
func (a *Adapter) handleMessage(msg bridgeMessage) {
	text := strings.TrimSpace(msg.Text)
	if text == "" {
		return
	}
	chatID := chatGUID(msg)

	senderID := ""
	senderName := ""
	if msg.Handle != nil {
		senderID = msg.Handle.Address
		senderName = msg.Handle.DisplayName
	}

	rawEvent := map[string]any{
		"guid":         msg.GUID,
		"date_created": msg.DateCreated,
	}
	if chatID != "" {
		rawEvent["chat_guid"] = chatID
	}

	channelID := "imessage"
	if chatID != "" {
		channelID += ":" + chatID
	}

	inbound := channels.InboundMessage{
		ChannelID:  channelID,
		SenderID:   senderID,
		SenderName: senderName,
		Content:    text,
		RawEvent:   rawEvent,
	}

	log.Info("imessage: received message",
		"sender", senderID,
		"content_length", len(text),
	)

	a.base.PublishInbound(inbound, func() {
		log.Warn("imessage: subscriber channel full, dropping message")
	})
}
